// Package ui contains the Bubble Tea application. This initial extracted model
// preserves the MVP interaction while the pane-based redesign is developed.
package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mailtui/internal/maildir"
)

type screen int

const (
	foldersScreen screen = iota
	messagesScreen
	readerScreen
)

type Model struct {
	root                                             string
	folders                                          []maildir.Folder
	screen                                           screen
	folderIndex, messageIndex, scroll, width, height int
	status                                           string
}

func New(root string, folders []maildir.Folder) Model {
	return Model{root: root, folders: folders}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = value.Width, value.Height
	case tea.KeyMsg:
		switch value.String() {
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
				m.scroll = max(0, m.scroll-max(1, m.height-5))
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

func (m *Model) move(delta int) {
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

func (m *Model) open() {
	if m.screen == foldersScreen && len(m.folders) > 0 {
		err := maildir.Load(&m.folders[m.folderIndex])
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

func (m Model) View() string {
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

func (m Model) folderView() string {
	lines := []string{fmt.Sprintf("Pastas Maildir (%d)", len(m.folders)), ""}
	for index, folder := range m.folders {
		lines = append(lines, marker(index == m.folderIndex)+maildir.DisplayName(folder.Name))
	}
	return visible(lines, m.folderIndex+2, max(3, m.height-4))
}

func (m Model) messageView() string {
	folder := m.folders[m.folderIndex]
	lines := []string{fmt.Sprintf("%s — %d mensagens", maildir.DisplayName(folder.Name), len(folder.Messages)), ""}
	for index, message := range folder.Messages {
		date := message.Date.Format("2006-01-02")
		if message.Date.IsZero() {
			date = "----------"
		}
		line := fmt.Sprintf("%s  %-24s  %s", date, truncate(message.From, 24), empty(message.Subject, "(sem assunto)"))
		lines = append(lines, marker(index == m.messageIndex)+truncate(line, max(20, m.width-3)))
	}
	return visible(lines, m.messageIndex+2, max(3, m.height-4))
}

func (m Model) readerView() string {
	message := m.folders[m.folderIndex].Messages[m.messageIndex]
	lines := []string{"From: " + message.From, "To: " + message.To}
	if message.Cc != "" {
		lines = append(lines, "Cc: "+message.Cc)
	}
	if message.Bcc != "" {
		lines = append(lines, "Bcc: "+message.Bcc)
	}
	lines = append(lines, "Subject: "+message.Subject, "Date: "+message.DateText, "Message-ID: "+message.MessageID)
	if len(message.Attachments) > 0 {
		lines = append(lines, "", fmt.Sprintf("Anexos (%d):", len(message.Attachments)))
		for _, attachment := range message.Attachments {
			lines = append(lines, fmt.Sprintf("  • %s — %s, %d bytes", attachment.Name, attachment.MediaType, attachment.Size))
		}
	}
	lines = append(lines, "", strings.Repeat("─", min(max(1, m.width), 80)), "")
	lines = append(lines, wrap(message.Body, max(20, m.width))...)
	maxScroll := max(0, len(lines)-max(3, m.height-4))
	scroll := min(m.scroll, maxScroll)
	end := min(len(lines), scroll+max(3, m.height-4))
	return strings.Join(lines[scroll:end], "\n")
}

func wrap(value string, width int) []string {
	var output []string
	for _, source := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		line := source
		for len([]rune(line)) > width {
			runes := []rune(line)
			output = append(output, string(runes[:width]))
			line = string(runes[width:])
		}
		output = append(output, line)
	}
	return output
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

func truncate(value string, width int) string {
	return truncateRaw(strings.Join(strings.Fields(value), " "), width)
}

func oneLine(value string, width int) string { return truncate(value, width) }

func truncateRaw(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:max(0, width-1)]) + "…"
}

func empty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func clamp(value, low, high int) int { return min(max(value, low), high) }
