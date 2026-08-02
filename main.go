package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type attachment struct {
	Name, MediaType string
	Size            int
}

type message struct {
	Path, From, To, Cc, Bcc, Subject, MessageID string
	Date                                        time.Time
	DateText, Body                              string
	Attachments                                 []attachment
	Err                                         error
}

type folder struct {
	Path, Name string
	Messages   []message
}

func decodeHeader(s string) string {
	v, err := (&mime.WordDecoder{}).DecodeHeader(s)
	if err != nil {
		return s
	}
	return v
}

func findMaildirs(root string) ([]folder, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("não é um diretório: %s", root)
	}
	var result []folder
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if isMaildir(path) {
			name, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if name == "." {
				name = filepath.Base(root)
			}
			result = append(result, folder{Path: path, Name: name})
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result, nil
}

func isMaildir(path string) bool {
	for _, name := range []string{"cur", "new", "tmp"} {
		info, err := os.Stat(filepath.Join(path, name))
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

func loadFolder(f *folder) error {
	if f.Messages != nil {
		return nil
	}
	f.Messages = []message{}
	var errs []error
	for _, bucket := range []string{"cur", "new"} {
		entries, err := os.ReadDir(filepath.Join(f.Path, bucket))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(f.Path, bucket, entry.Name())
			msg, err := parseMessage(path)
			if err != nil {
				msg = message{Path: path, Subject: "[mensagem inválida]", Err: err}
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
			f.Messages = append(f.Messages, msg)
		}
	}
	sort.SliceStable(f.Messages, func(i, j int) bool { return f.Messages[i].Date.After(f.Messages[j].Date) })
	return errors.Join(errs...)
}

func parseMessage(path string) (message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return message{}, err
	}
	raw, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return message{}, err
	}
	m := message{Path: path}
	m.From = decodeHeader(raw.Header.Get("From"))
	m.To = decodeHeader(raw.Header.Get("To"))
	m.Cc = decodeHeader(raw.Header.Get("Cc"))
	m.Bcc = decodeHeader(raw.Header.Get("Bcc"))
	m.Subject = decodeHeader(raw.Header.Get("Subject"))
	m.MessageID = raw.Header.Get("Message-ID")
	m.DateText = raw.Header.Get("Date")
	if date, dateErr := mail.ParseDate(m.DateText); dateErr == nil {
		m.Date = date
	}
	body, err := io.ReadAll(raw.Body)
	if err != nil {
		return m, err
	}
	body, err = decodeTransfer(body, raw.Header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return m, err
	}
	mediaType, params, _ := mime.ParseMediaType(raw.Header.Get("Content-Type"))
	if mediaType == "" {
		mediaType = "text/plain"
	}
	parts, attachments, err := extractParts(mediaType, params, raw.Header.Get("Content-Disposition"), body)
	m.Attachments = attachments
	if err != nil {
		return m, err
	}
	if parts.plain != "" {
		m.Body = parts.plain
	} else {
		m.Body = htmlToText(parts.html)
	}
	if strings.TrimSpace(m.Body) == "" {
		m.Body = "[sem corpo de texto]"
	}
	return m, nil
}

type textParts struct{ plain, html string }

func extractParts(mediaType string, params map[string]string, disposition string, data []byte) (textParts, []attachment, error) {
	var texts textParts
	var attachments []attachment
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return texts, nil, errors.New("multipart sem boundary")
		}
		reader := multipart.NewReader(bytes.NewReader(data), boundary)
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return texts, attachments, err
			}
			partData, err := io.ReadAll(part)
			if err != nil {
				return texts, attachments, err
			}
			partData, err = decodeTransfer(partData, part.Header.Get("Content-Transfer-Encoding"))
			if err != nil {
				return texts, attachments, err
			}
			pt, pp, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if pt == "" {
				pt = "text/plain"
			}
			pd, dp, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			filename := decodeHeader(dp["filename"])
			if filename == "" {
				filename = decodeHeader(pp["name"])
			}
			if pd == "attachment" || filename != "" {
				if filename == "" {
					filename = "anexo-sem-nome"
				}
				attachments = append(attachments, attachment{Name: filename, MediaType: pt, Size: len(partData)})
				continue
			}
			nested, nestedAttachments, err := extractParts(pt, pp, pd, partData)
			if err == nil {
				if texts.plain == "" {
					texts.plain = nested.plain
				}
				if texts.html == "" {
					texts.html = nested.html
				}
				attachments = append(attachments, nestedAttachments...)
			}
		}
		return texts, attachments, nil
	}
	if mediaType == "text/plain" {
		texts.plain = string(data)
	}
	if mediaType == "text/html" {
		texts.html = string(data)
	}
	return texts, attachments, nil
}

func decodeTransfer(data []byte, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary":
		return data, nil
	case "base64":
		return io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(data)))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(data)))
	default:
		// Unknown transfer encodings are left intact so one unusual MIME part
		// does not make the whole message inaccessible.
		return data, nil
	}
}

var tags = regexp.MustCompile(`(?s)<[^>]*>`)
var breaks = regexp.MustCompile(`(?i)<\s*(br\s*/?|/p|/div|/li|/tr|/h[1-6])\s*>`)

func htmlToText(value string) string {
	value = breaks.ReplaceAllString(value, "\n")
	value = tags.ReplaceAllString(value, "")
	return strings.TrimSpace(html.UnescapeString(value))
}

type screen int

const (
	foldersScreen screen = iota
	messagesScreen
	readerScreen
)

type model struct {
	root                                             string
	folders                                          []folder
	screen                                           screen
	folderIndex, messageIndex, scroll, width, height int
	status                                           string
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc", "backspace", "left":
			if m.screen == readerScreen {
				m.screen = messagesScreen
				m.scroll = 0
			} else if m.screen == messagesScreen {
				m.screen = foldersScreen
			}
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "pgup":
			if m.screen == readerScreen {
				m.scroll -= max(1, m.height-5)
				if m.scroll < 0 {
					m.scroll = 0
				}
			}
		case "pgdown":
			if m.screen == readerScreen {
				m.scroll += max(1, m.height-5)
			}
		case "enter", "right", "l":
			m.open()
		}
	}
	return m, nil
}

func (m *model) move(delta int) {
	if m.screen == readerScreen {
		m.scroll = max(0, m.scroll+delta)
		return
	}
	if m.screen == foldersScreen && len(m.folders) > 0 {
		m.folderIndex = clamp(m.folderIndex+delta, 0, len(m.folders)-1)
	}
	if m.screen == messagesScreen && len(m.folders[m.folderIndex].Messages) > 0 {
		m.messageIndex = clamp(m.messageIndex+delta, 0, len(m.folders[m.folderIndex].Messages)-1)
	}
}

func (m *model) open() {
	if m.screen == foldersScreen && len(m.folders) > 0 {
		err := loadFolder(&m.folders[m.folderIndex])
		m.messageIndex = 0
		m.screen = messagesScreen
		if err != nil {
			m.status = "Algumas mensagens não puderam ser lidas: " + err.Error()
		} else {
			m.status = ""
		}
	} else if m.screen == messagesScreen && len(m.folders[m.folderIndex].Messages) > 0 {
		m.screen = readerScreen
		m.scroll = 0
	}
}

func (m model) View() string {
	if m.width == 0 {
		return "Carregando…"
	}
	header := fmt.Sprintf("mailtui — %s\n", m.root)
	footer := "\n↑/↓ ou j/k navegar • Enter abrir • ←/Esc voltar • q sair"
	var content string
	switch m.screen {
	case foldersScreen:
		content = m.folderView()
	case messagesScreen:
		content = m.messageView()
	case readerScreen:
		content = m.readerView()
	}
	if m.status != "" {
		footer = "\nAVISO: " + oneLine(m.status, max(20, m.width-8)) + footer
	}
	return header + content + footer
}

func (m model) folderView() string {
	lines := []string{fmt.Sprintf("Pastas Maildir (%d)", len(m.folders)), ""}
	for i, f := range m.folders {
		lines = append(lines, marker(i == m.folderIndex)+f.Name)
	}
	return visible(lines, m.folderIndex+2, max(3, m.height-4))
}

func (m model) messageView() string {
	f := m.folders[m.folderIndex]
	lines := []string{fmt.Sprintf("%s — %d mensagens", f.Name, len(f.Messages)), ""}
	for i, msg := range f.Messages {
		date := msg.Date.Format("2006-01-02")
		if msg.Date.IsZero() {
			date = "----------"
		}
		line := fmt.Sprintf("%s  %-24s  %s", date, truncate(msg.From, 24), empty(msg.Subject, "(sem assunto)"))
		lines = append(lines, marker(i == m.messageIndex)+truncate(line, max(20, m.width-3)))
	}
	return visible(lines, m.messageIndex+2, max(3, m.height-4))
}

func (m model) readerView() string {
	msg := m.folders[m.folderIndex].Messages[m.messageIndex]
	lines := []string{"From: " + msg.From, "To: " + msg.To}
	if msg.Cc != "" {
		lines = append(lines, "Cc: "+msg.Cc)
	}
	if msg.Bcc != "" {
		lines = append(lines, "Bcc: "+msg.Bcc)
	}
	lines = append(lines, "Subject: "+msg.Subject, "Date: "+msg.DateText, "Message-ID: "+msg.MessageID)
	if len(msg.Attachments) > 0 {
		lines = append(lines, "", fmt.Sprintf("Anexos (%d):", len(msg.Attachments)))
		for _, a := range msg.Attachments {
			lines = append(lines, fmt.Sprintf("  • %s — %s, %d bytes", a.Name, a.MediaType, a.Size))
		}
	}
	lines = append(lines, "", strings.Repeat("─", min(max(1, m.width), 80)), "")
	lines = append(lines, wrap(msg.Body, max(20, m.width))...)
	maxScroll := max(0, len(lines)-max(3, m.height-4))
	scroll := min(m.scroll, maxScroll)
	end := min(len(lines), scroll+max(3, m.height-4))
	return strings.Join(lines[scroll:end], "\n")
}

func wrap(s string, width int) []string {
	var out []string
	for _, source := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		line := source
		for len([]rune(line)) > width {
			r := []rune(line)
			out = append(out, string(r[:width]))
			line = string(r[width:])
		}
		out = append(out, line)
	}
	return out
}

func visible(lines []string, selected, height int) string {
	start := max(0, selected-height+1)
	end := min(len(lines), start+height)
	return strings.Join(lines[start:end], "\n")
}
func marker(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}
func truncate(s string, n int) string {
	r := []rune(oneLine(s, n))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:max(0, n-1)]) + "…"
}
func oneLine(s string, n int) string { return truncateRaw(strings.Join(strings.Fields(s), " "), n) }
func truncateRaw(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:max(0, n-1)]) + "…"
}
func empty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
func clamp(v, low, high int) int { return min(max(v, low), high) }

func run(args []string) error {
	flags := flag.NewFlagSet("mailtui", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("uso: mailtui DIRETÓRIO_MAILDIR")
	}
	root, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return err
	}
	folders, err := findMaildirs(root)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		return fmt.Errorf("nenhuma pasta Maildir encontrada em %s", root)
	}
	_, err = tea.NewProgram(model{root: root, folders: folders}, tea.WithAltScreen()).Run()
	return err
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mailtui:", err)
		os.Exit(1)
	}
}
