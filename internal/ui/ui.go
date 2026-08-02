// Package ui contains the responsive Bubble Tea mail reader.
package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mailtui/internal/maildir"
	"mailtui/internal/message"
)

type pane int

const (
	foldersPane pane = iota
	messagesPane
	readerPane
)

const (
	wideBreakpoint   = 112
	mediumBreakpoint = 72
)

var (
	accent       = lipgloss.Color("#A78BFA")
	accentStrong = lipgloss.Color("#7C3AED")
	cyan         = lipgloss.Color("#22D3EE")
	textColor    = lipgloss.Color("#E2E8F0")
	muted        = lipgloss.Color("#64748B")
	soft         = lipgloss.Color("#94A3B8")
	selectedBG   = lipgloss.Color("#312E81")
	warning      = lipgloss.Color("#FBBF24")

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(textColor)
	mutedStyle    = lipgloss.NewStyle().Foreground(muted)
	softStyle     = lipgloss.NewStyle().Foreground(soft)
	accentStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(selectedBG)
	labelStyle    = lipgloss.NewStyle().Bold(true).Foreground(cyan)
)

type Model struct {
	root                      string
	folders                   []maildir.Folder
	focus                     pane
	folderIndex, messageIndex int
	readerScroll              int
	width, height             int
	status                    string
	searching                 bool
	query, queryBeforeSearch  string
}

func New(root string, folders []maildir.Folder) Model {
	m := Model{root: root, folders: folders, focus: foldersPane}
	if len(m.folders) > 0 {
		m.loadSelectedFolder()
	}
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = value.Width, value.Height
	case tea.KeyMsg:
		if m.searching {
			return m.updateSearch(value)
		}
		return m.updateNavigation(value)
	}
	return m, nil
}

func (m Model) updateSearch(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.query = m.queryBeforeSearch
		m.searching = false
		m.messageIndex = 0
		m.readerScroll = 0
	case "enter":
		m.searching = false
	case "backspace":
		if len(m.query) > 0 {
			_, size := utf8.DecodeLastRuneInString(m.query)
			m.query = m.query[:len(m.query)-size]
			m.messageIndex = 0
			m.readerScroll = 0
		}
	case "ctrl+u":
		m.query = ""
		m.messageIndex = 0
		m.readerScroll = 0
	default:
		if key.Type == tea.KeyRunes {
			m.query += string(key.Runes)
			m.messageIndex = 0
			m.readerScroll = 0
		}
	}
	return m, nil
}

func (m Model) updateNavigation(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "/":
		m.searching = true
		m.queryBeforeSearch = m.query
		if m.focus == foldersPane {
			m.focus = messagesPane
		}
	case "tab":
		m.focus = (m.focus + 1) % 3
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
	case "left", "h":
		if m.focus > foldersPane {
			m.focus--
		}
	case "right", "l":
		if m.focus < readerPane {
			m.focus++
		}
	case "esc", "backspace":
		if m.query != "" {
			m.query = ""
			m.messageIndex = 0
		} else if m.focus > foldersPane {
			m.focus--
		}
	case "enter":
		if m.focus < readerPane {
			m.focus++
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		if m.focus == readerPane {
			m.readerScroll = max(0, m.readerScroll-max(1, m.readerViewportHeight()))
		}
	case "pgdown":
		if m.focus == readerPane {
			m.readerScroll += max(1, m.readerViewportHeight())
		}
	case "home":
		m.moveToBoundary(false)
	case "end":
		m.moveToBoundary(true)
	}
	return m, nil
}

func (m *Model) move(delta int) {
	switch m.focus {
	case foldersPane:
		if len(m.folders) == 0 {
			return
		}
		next := clamp(m.folderIndex+delta, 0, len(m.folders)-1)
		if next != m.folderIndex {
			m.folderIndex = next
			m.messageIndex = 0
			m.readerScroll = 0
			m.query = ""
			m.loadSelectedFolder()
		}
	case messagesPane:
		matches := m.filteredMessageIndexes()
		if len(matches) > 0 {
			m.messageIndex = clamp(m.messageIndex+delta, 0, len(matches)-1)
			m.readerScroll = 0
		}
	case readerPane:
		m.readerScroll = max(0, m.readerScroll+delta)
	}
}

func (m *Model) moveToBoundary(end bool) {
	if m.focus == foldersPane && len(m.folders) > 0 {
		if end {
			m.folderIndex = len(m.folders) - 1
		} else {
			m.folderIndex = 0
		}
		m.messageIndex, m.readerScroll = 0, 0
		m.query = ""
		m.loadSelectedFolder()
	}
	if m.focus == messagesPane {
		matches := m.filteredMessageIndexes()
		if end && len(matches) > 0 {
			m.messageIndex = len(matches) - 1
		} else {
			m.messageIndex = 0
		}
		m.readerScroll = 0
	}
}

func (m *Model) loadSelectedFolder() {
	if len(m.folders) == 0 {
		return
	}
	err := maildir.Load(&m.folders[m.folderIndex])
	if err != nil {
		m.status = "Algumas mensagens não puderam ser lidas"
	} else {
		m.status = ""
	}
}

func (m Model) filteredMessageIndexes() []int {
	if len(m.folders) == 0 || m.folders[m.folderIndex].Messages == nil {
		return nil
	}
	messages := m.folders[m.folderIndex].Messages
	query := strings.ToLower(strings.TrimSpace(m.query))
	indexes := make([]int, 0, len(messages))
	for index, item := range messages {
		if query == "" || strings.Contains(strings.ToLower(strings.Join([]string{
			item.Subject, item.From, item.To, item.Cc, item.Bcc,
		}, "\n")), query) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func (m Model) selectedMessage() *message.Message {
	if len(m.folders) == 0 {
		return nil
	}
	matches := m.filteredMessageIndexes()
	if len(matches) == 0 {
		return nil
	}
	selected := clamp(m.messageIndex, 0, len(matches)-1)
	return &m.folders[m.folderIndex].Messages[matches[selected]]
}

func (m Model) View() string {
	if m.width == 0 {
		return "Carregando…"
	}
	if m.width < 42 || m.height < 10 {
		return "mailtui precisa de um terminal com pelo menos 42×10\nq para sair"
	}

	header := m.headerView()
	foot := m.footerView()
	bodyHeight := max(5, m.height-lipgloss.Height(header)-lipgloss.Height(foot))
	var body string
	switch {
	case m.width >= wideBreakpoint:
		body = m.wideView(m.width, bodyHeight)
	case m.width >= mediumBreakpoint:
		body = m.mediumView(m.width, bodyHeight)
	default:
		body = m.narrowView(m.width, bodyHeight)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, foot)
}

func (m Model) headerView() string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accentStrong).Padding(0, 1).Render("MAILTUI")
	root := softStyle.Render("  " + filepath.Base(m.root))
	readonly := lipgloss.NewStyle().Bold(true).Foreground(cyan).Render("READ ONLY")
	gap := max(1, m.width-lipgloss.Width(brand)-lipgloss.Width(root)-lipgloss.Width(readonly))
	return brand + root + strings.Repeat(" ", gap) + readonly
}

func (m Model) footerView() string {
	if m.searching {
		count := len(m.filteredMessageIndexes())
		left := accentStyle.Render(" / ") + lipgloss.NewStyle().Foreground(textColor).Render(m.query+"█")
		right := mutedStyle.Render(fmt.Sprintf("%d resultado(s)  Enter aplicar  Esc cancelar", count))
		return fitSides(left, right, m.width)
	}
	left := softStyle.Render("Tab/←→ foco  ↑↓ navegar  / buscar  Enter avançar  Esc voltar")
	if m.status != "" {
		left = lipgloss.NewStyle().Foreground(warning).Render("⚠ "+m.status) + "  " + left
	}
	right := mutedStyle.Render("q sair")
	return fitSides(left, right, m.width)
}

func (m Model) wideView(width, height int) string {
	folderWidth := max(22, width*22/100)
	messageWidth := max(34, width*32/100)
	readerWidth := width - folderWidth - messageWidth
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.folderPane(folderWidth, height),
		m.messagesPane(messageWidth, height),
		m.readerPane(readerWidth, height),
	)
}

func (m Model) mediumView(width, height int) string {
	folderWidth := max(22, width*28/100)
	detailWidth := width - folderWidth
	listHeight := max(8, height*44/100)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.folderPane(folderWidth, height),
		lipgloss.JoinVertical(lipgloss.Left,
			m.messagesPane(detailWidth, listHeight),
			m.readerPane(detailWidth, height-listHeight),
		),
	)
}

func (m Model) narrowView(width, height int) string {
	switch m.focus {
	case foldersPane:
		return m.folderPane(width, height)
	case messagesPane:
		return m.messagesPane(width, height)
	default:
		return m.readerPane(width, height)
	}
}

func (m Model) folderPane(width, height int) string {
	contentHeight := paneContentHeight(height)
	lines := make([]string, 0, len(m.folders))
	for index, folder := range m.folders {
		name := maildir.DisplayName(folder.Name)
		count := ""
		if folder.Messages != nil {
			count = fmt.Sprintf(" %d", len(folder.Messages))
		}
		line := fitSides(truncate(name, max(1, width-8)), mutedStyle.Render(count), max(1, width-4))
		if index == m.folderIndex {
			line = fillStyle(selectedStyle, "› "+line, max(1, width-4))
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = []string{mutedStyle.Render("Nenhuma pasta encontrada")}
	}
	lines = window(lines, m.folderIndex, contentHeight)
	return paneBox("PASTAS", fmt.Sprintf("%d", len(m.folders)), lines, width, height, m.focus == foldersPane)
}

func (m Model) messagesPane(width, height int) string {
	matches := m.filteredMessageIndexes()
	contentHeight := paneContentHeight(height)
	rowsPerMessage := 3
	visibleCount := max(1, contentHeight/rowsPerMessage)
	selected := clamp(m.messageIndex, 0, max(0, len(matches)-1))
	start := clamp(selected-visibleCount/2, 0, max(0, len(matches)-visibleCount))
	end := min(len(matches), start+visibleCount)
	var lines []string
	if len(matches) == 0 {
		if m.query != "" {
			lines = []string{"", accentStyle.Render("Nenhum resultado"), mutedStyle.Render("Tente outro termo de busca.")}
		} else {
			lines = []string{"", mutedStyle.Render("Esta pasta está vazia.")}
		}
	} else {
		for position := start; position < end; position++ {
			item := m.folders[m.folderIndex].Messages[matches[position]]
			available := max(10, width-4)
			first := fitSides(truncate(displaySender(item.From), max(4, available-10)), displayDate(item.Date), available)
			second := truncate(empty(item.Subject, "(sem assunto)"), available)
			third := truncate(snippet(item.Body), available)
			if position == selected {
				lines = append(lines,
					fillStyle(selectedStyle, first, available),
					fillStyle(selectedStyle, second, available),
					fillStyle(selectedStyle, third, available),
				)
			} else {
				lines = append(lines, titleStyle.Render(first), second, mutedStyle.Render(third))
			}
		}
	}
	folderName := "MENSAGENS"
	if len(m.folders) > 0 {
		folderName = truncate(strings.ToUpper(maildir.DisplayName(m.folders[m.folderIndex].Name)), 22)
	}
	count := fmt.Sprintf("%d/%d", len(matches), m.folderMessageCount())
	return paneBox(folderName, count, lines, width, height, m.focus == messagesPane)
}

func (m Model) readerPane(width, height int) string {
	item := m.selectedMessage()
	contentHeight := paneContentHeight(height)
	available := max(10, width-4)
	if item == nil {
		lines := []string{"", accentStyle.Render("Nenhuma mensagem selecionada"), mutedStyle.Render("Escolha uma mensagem ou ajuste a busca.")}
		return paneBox("LEITURA", "", lines, width, height, m.focus == readerPane)
	}

	var lines []string
	lines = append(lines, titleStyle.Render(truncate(empty(item.Subject, "(sem assunto)"), available)))
	lines = append(lines, labelValue("De", item.From, available)...)
	lines = append(lines, labelValue("Para", item.To, available)...)
	if item.Cc != "" {
		lines = append(lines, labelValue("Cc", item.Cc, available)...)
	}
	if item.Bcc != "" {
		lines = append(lines, labelValue("Bcc", item.Bcc, available)...)
	}
	lines = append(lines, labelValue("Data", item.DateText, available)...)
	if len(item.Attachments) > 0 {
		lines = append(lines, "", accentStyle.Render(fmt.Sprintf("▣ %d anexo(s)", len(item.Attachments))))
		for _, attachment := range item.Attachments {
			lines = append(lines, truncate(fmt.Sprintf("  %s · %s · %s", attachment.Name, attachment.MediaType, formatBytes(attachment.Size)), available))
		}
	}
	lines = append(lines, "", mutedStyle.Render(strings.Repeat("─", available)), "")
	lines = append(lines, wrap(item.Body, available)...)

	maxScroll := max(0, len(lines)-contentHeight)
	scroll := clamp(m.readerScroll, 0, maxScroll)
	end := min(len(lines), scroll+contentHeight)
	indicator := ""
	if maxScroll > 0 {
		indicator = fmt.Sprintf("%d%%", scroll*100/max(1, maxScroll))
	}
	return paneBox("LEITURA", indicator, lines[scroll:end], width, height, m.focus == readerPane)
}

func paneBox(title, meta string, lines []string, width, height int, focused bool) string {
	innerWidth := max(1, width-2)
	innerHeight := max(1, height-2)
	heading := accentStyle.Render(" "+title) + " "
	if meta != "" {
		heading = fitSides(heading, mutedStyle.Render(meta+" "), innerWidth)
	}
	content := heading
	if innerHeight > 1 {
		body := strings.Join(lines, "\n")
		content += "\n" + body
	}
	borderColor := muted
	if focused {
		borderColor = accent
	}
	return lipgloss.NewStyle().
		Width(innerWidth).
		Height(innerHeight).
		MaxWidth(innerWidth).
		MaxHeight(innerHeight).
		Foreground(textColor).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Render(content)
}

func paneContentHeight(height int) int { return max(1, height-3) }

func (m Model) readerViewportHeight() int {
	if m.width >= wideBreakpoint {
		return max(1, m.height-5)
	}
	if m.width >= mediumBreakpoint {
		return max(1, (m.height-2)*56/100-3)
	}
	return max(1, m.height-5)
}

func (m Model) folderMessageCount() int {
	if len(m.folders) == 0 || m.folders[m.folderIndex].Messages == nil {
		return 0
	}
	return len(m.folders[m.folderIndex].Messages)
}

func labelValue(label, value string, width int) []string {
	prefix := labelStyle.Render(label + ": ")
	plainPrefixWidth := lipgloss.Width(prefix)
	wrapped := wrap(value, max(10, width-plainPrefixWidth))
	if len(wrapped) == 0 {
		return []string{prefix + "—"}
	}
	lines := []string{prefix + wrapped[0]}
	indent := strings.Repeat(" ", plainPrefixWidth)
	for _, line := range wrapped[1:] {
		lines = append(lines, indent+line)
	}
	return lines
}

func wrap(value string, width int) []string {
	var output []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if paragraph == "" {
			output = append(output, "")
			continue
		}
		words := strings.Fields(paragraph)
		line := ""
		for _, word := range words {
			if len([]rune(word)) > width {
				if line != "" {
					output = append(output, line)
					line = ""
				}
				runes := []rune(word)
				for len(runes) > width {
					output = append(output, string(runes[:width]))
					runes = runes[width:]
				}
				line = string(runes)
				continue
			}
			candidate := word
			if line != "" {
				candidate = line + " " + word
			}
			if len([]rune(candidate)) > width {
				output = append(output, line)
				line = word
			} else {
				line = candidate
			}
		}
		output = append(output, line)
	}
	return output
}

func window(lines []string, selected, height int) []string {
	if len(lines) <= height {
		return lines
	}
	start := clamp(selected-height/2, 0, len(lines)-height)
	return lines[start : start+height]
}

func displaySender(value string) string {
	if before, _, found := strings.Cut(value, "<"); found && strings.TrimSpace(before) != "" {
		return strings.Trim(strings.TrimSpace(before), "\"")
	}
	return empty(value, "Remetente desconhecido")
}

func displayDate(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	now := time.Now()
	if value.Year() == now.Year() && value.YearDay() == now.YearDay() {
		return value.Format("15:04")
	}
	if value.Year() == now.Year() {
		return value.Format("02 Jan")
	}
	return value.Format("02/01/06")
}

func snippet(value string) string {
	clean := strings.Join(strings.Fields(value), " ")
	return empty(clean, "Sem prévia de texto")
}

func formatBytes(size int) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func fitSides(left, right string, width int) string {
	space := width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		return truncate(left, max(1, width-lipgloss.Width(right)-1)) + " " + right
	}
	return left + strings.Repeat(" ", space) + right
}

func fillStyle(style lipgloss.Style, value string, width int) string {
	return style.Width(max(1, width)).MaxWidth(max(1, width)).Render(truncate(value, max(1, width)))
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) <= width {
		return string(runes)
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func empty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func clamp(value, low, high int) int { return min(max(value, low), high) }
