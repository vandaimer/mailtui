package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"mailtui/internal/message"
)

// presentationFacts is the compact, immutable input to the presentation
// module. Model remains responsible for assembling these facts; rendering
// does not inspect orchestration, read-session, or loaded-folder state.
type presentationFacts struct {
	width, height int
	root          string
	layout        layoutPlan

	folders       []presentationFolder
	folderCursor  int
	selectedName  string
	selectedCount int

	projection          messageProjection
	selectedFolderReady bool
	selectedFolderPath  string
	focus               pane
	mode                interactionMode
	query               string
	readerScroll        int
	preferPlain         bool
	attachmentCursor    int

	loadingFolder     string
	refreshingFolder  string
	loadingMessage    string
	openingAttachment bool
	spinnerFrame      int
	status            string

	selectedMessage *message.Message
	reader          readerPresentation
}

type presentationFolder struct {
	name    string
	path    string
	count   int
	loaded  bool
	loading bool
}

type readerPresentation struct {
	viewport readerDocumentViewport
	mode     string
	ready    bool
}

// renderPresentation renders all terminal chrome and panes from facts. It is
// synchronous and side-effect free: no reads, writes, scheduling, or state
// mutation occur here.
func renderPresentation(facts presentationFacts) string {
	if facts.width == 0 {
		return "Loading…"
	}
	if !facts.layout.usable {
		return "mailtui needs a terminal of at least 42×10\npress q to quit"
	}
	p := presentation{facts: facts}
	header := p.header()
	footer := p.footer()
	body := p.body()
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

type presentation struct {
	facts presentationFacts
}

func (p presentation) header() string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accentStrong).Padding(0, 1).Render("MAILTUI")
	readonly := lipgloss.NewStyle().Bold(true).Foreground(cyan).Render("READ ONLY")
	rootWidth := max(1, p.facts.width-lipgloss.Width(brand)-lipgloss.Width(readonly)-1)
	root := softStyle.Render(truncate("  "+singleLine(filepath.Base(p.facts.root)), rootWidth))
	gap := max(1, p.facts.width-lipgloss.Width(brand)-lipgloss.Width(root)-lipgloss.Width(readonly))
	return brand + root + strings.Repeat(" ", gap) + readonly
}

func (p presentation) footer() string {
	if p.facts.mode == attachmentsMode {
		return fitSides(accentStyle.Render("ATTACHMENTS  ↑↓ select  Enter open  Esc cancel"), mutedStyle.Render("read only"), p.facts.width)
	}
	if p.facts.mode == searchMode {
		count := p.facts.projection.Len()
		prefix := accentStyle.Render(" / ")
		right := mutedStyle.Render(fmt.Sprintf("%d result(s)  Enter apply  Esc cancel", count))
		queryWidth := max(1, p.facts.width-lipgloss.Width(prefix)-lipgloss.Width(right)-1)
		left := prefix + lipgloss.NewStyle().Foreground(textColor).Render(truncate(singleLine(p.facts.query)+"█", queryWidth))
		return fitSides(left, right, p.facts.width)
	}
	left := softStyle.Render("Tab/←→ focus  ↑↓ navigate  / search  r refresh  v view  o attachments  Esc back")
	if p.facts.loadingFolder != "" {
		activity := "Reading headers…"
		if p.facts.refreshingFolder == p.facts.loadingFolder {
			activity = "Refreshing folder…"
		}
		left = accentStyle.Render(p.spinner()+" "+activity) + "  " + left
	} else if p.facts.loadingMessage != "" {
		left = accentStyle.Render(p.spinner()+" Reading message…") + "  " + left
	} else if p.facts.openingAttachment {
		left = accentStyle.Render(p.spinner()+" Opening attachment…") + "  " + left
	}
	if p.facts.status != "" {
		left = lipgloss.NewStyle().Foreground(warning).Render("⚠ "+singleLine(p.facts.status)) + "  " + left
	}
	return fitSides(left, mutedStyle.Render("q quit"), p.facts.width)
}

func (p presentation) body() string {
	layout := p.facts.layout
	switch layout.mode {
	case wideLayout:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			p.folderPane(layout.folders),
			p.messagesPane(layout.messages),
			p.readerPane(layout.reader),
		)
	case mediumLayout:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			p.folderPane(layout.folders),
			lipgloss.JoinVertical(lipgloss.Left,
				p.messagesPane(layout.messages),
				p.readerPane(layout.reader),
			),
		)
	default:
		switch p.facts.focus {
		case foldersPane:
			return p.folderPane(layout.folders)
		case messagesPane:
			return p.messagesPane(layout.messages)
		default:
			return p.readerPane(layout.reader)
		}
	}
}

func (p presentation) folderPane(geometry paneGeometry) string {
	lines := make([]string, 0, len(p.facts.folders))
	for index, folder := range p.facts.folders {
		count := ""
		if folder.loaded {
			count = fmt.Sprintf(" %d", folder.count)
		} else if folder.loading {
			count = " " + p.spinner()
		}
		line := fitSides(truncate(folder.name, max(1, geometry.width-8)), mutedStyle.Render(count), geometry.contentWidth)
		if index == p.facts.folderCursor {
			line = fillStyle(selectedStyle, "› "+line, geometry.contentWidth)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = []string{mutedStyle.Render("No folders found")}
	}
	lines = window(lines, p.facts.folderCursor, geometry.contentHeight)
	return paneBox("FOLDERS", fmt.Sprintf("%d", len(p.facts.folders)), lines, geometry, p.facts.focus == foldersPane)
}

func (p presentation) messagesPane(geometry paneGeometry) string {
	if !p.facts.selectedFolderReady {
		lines := []string{"", accentStyle.Render(p.spinner() + " Loading messages"), mutedStyle.Render("Reading Maildir headers only…")}
		return paneBox("MESSAGES", "", lines, geometry, p.facts.focus == messagesPane)
	}
	rowsPerMessage := 3
	visibleCount := max(1, geometry.contentHeight/rowsPerMessage)
	selected := max(0, p.facts.projection.SelectedPosition())
	start := clamp(selected-visibleCount/2, 0, max(0, p.facts.projection.Len()-visibleCount))
	end := min(p.facts.projection.Len(), start+visibleCount)
	var lines []string
	if p.facts.projection.Len() == 0 {
		if p.facts.query != "" {
			lines = []string{"", accentStyle.Render("No results"), mutedStyle.Render("Try another search term.")}
		} else {
			lines = []string{"", mutedStyle.Render("This folder is empty.")}
		}
	} else {
		for position := start; position < end; position++ {
			item := p.facts.projection.Message(position)
			if item == nil {
				continue
			}
			available := geometry.contentWidth
			first := fitSides(truncate(displaySender(item.From), max(4, available-10)), displayDate(item.Date), available)
			second := truncate(empty(item.Subject, "(no subject)"), available)
			preview := snippet(item.Body)
			switch item.LoadState() {
			case message.LoadHeaderOnly:
				preview = "Select to load the preview"
			case message.LoadHeaderInvalid:
				preview = "Invalid message"
			case message.LoadContentUnavailable:
				preview = "Message content unavailable"
			}
			third := truncate(preview, available)
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
	folderName := "MESSAGES"
	if p.facts.selectedName != "" {
		folderName = truncate(strings.ToUpper(p.facts.selectedName), 22)
	}
	count := fmt.Sprintf("%d/%d", p.facts.projection.Len(), p.facts.selectedCount)
	return paneBox(folderName, count, lines, geometry, p.facts.focus == messagesPane)
}

func (p presentation) readerPane(geometry paneGeometry) string {
	if !p.facts.selectedFolderReady {
		lines := []string{"", accentStyle.Render(p.spinner() + " Preparing folder"), mutedStyle.Render("The interface remains responsive while loading.")}
		return paneBox("READER", "", lines, geometry, p.facts.focus == readerPane)
	}
	if p.facts.loadingFolder != "" && p.facts.loadingFolder == p.facts.selectedFolderPath {
		lines := []string{"", accentStyle.Render(p.spinner() + " Receiving message batches"), mutedStyle.Render(fmt.Sprintf("%d headers available so far…", p.facts.selectedCount))}
		return paneBox("READER", "", lines, geometry, p.facts.focus == readerPane)
	}
	item := p.facts.selectedMessage
	available := geometry.contentWidth
	if item == nil {
		lines := []string{"", accentStyle.Render("No message selected"), mutedStyle.Render("Choose a message or adjust your search.")}
		return paneBox("READER", "", lines, geometry, p.facts.focus == readerPane)
	}
	if item.LoadState() == message.LoadHeaderInvalid {
		errText := "Invalid message"
		if item.LoadError() != nil {
			errText = truncate(item.LoadError().Error(), available)
		}
		lines := []string{"", lipgloss.NewStyle().Foreground(warning).Render("Invalid message"), mutedStyle.Render(errText)}
		return paneBox("READER", "", lines, geometry, p.facts.focus == readerPane)
	}
	if item.LoadState() == message.LoadContentUnavailable {
		errText := ""
		if item.LoadError() != nil {
			errText = truncate(item.LoadError().Error(), available)
		}
		lines := []string{"", lipgloss.NewStyle().Foreground(warning).Render("Could not load message content"), mutedStyle.Render(errText)}
		return paneBox("READER", "", lines, geometry, p.facts.focus == readerPane)
	}
	if item.LoadState() == message.LoadHeaderOnly {
		lines := []string{titleStyle.Render(truncate(empty(item.Subject, "(no subject)"), available))}
		lines = append(lines, labelValue("From", item.From, available)...)
		lines = append(lines, labelValue("To", item.To, available)...)
		lines = append(lines, "", accentStyle.Render(p.spinner()+" Loading content…"), mutedStyle.Render("Only this file will be read in full."))
		return paneBox("READER", "", lines, geometry, p.facts.focus == readerPane)
	}
	if p.facts.mode == attachmentsMode {
		return p.attachmentPickerPane(item, geometry)
	}
	indicator := p.facts.reader.mode
	if p.facts.reader.viewport.maxScroll > 0 {
		indicator += fmt.Sprintf(" · %d%%", p.facts.reader.viewport.progress)
	}
	return paneBox("READER", indicator, p.facts.reader.viewport.lines, geometry, p.facts.focus == readerPane)
}

func (p presentation) attachmentPickerPane(item *message.Message, geometry paneGeometry) string {
	available := geometry.contentWidth
	lines := []string{mutedStyle.Render(truncate(empty(item.Subject, "(no subject)"), available)), ""}
	for index, entry := range item.Attachments {
		line := fitSides(truncate(entry.Name, max(4, available-12)), formatBytes(entry.Size), available)
		if index == p.facts.attachmentCursor {
			line = fillStyle(selectedStyle, "› "+line, available)
		} else {
			line = "  " + line
		}
		lines = append(lines, line, mutedStyle.Render("  "+truncate(entry.MediaType, available-2)))
	}
	lines = window(lines, p.facts.attachmentCursor*2+2, geometry.contentHeight)
	return paneBox("ATTACHMENTS", fmt.Sprintf("%d", len(item.Attachments)), lines, geometry, true)
}

func (p presentation) spinner() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[p.facts.spinnerFrame%len(frames)]
}

func paneBox(title, meta string, lines []string, geometry paneGeometry, focused bool) string {
	heading := accentStyle.Render(" "+title) + " "
	if meta != "" {
		heading = fitSides(heading, mutedStyle.Render(meta+" "), geometry.innerWidth)
	}
	content := heading
	if geometry.innerHeight > 1 {
		body := strings.Join(lines, "\n")
		content += "\n" + body
	}
	borderColor := muted
	if focused {
		borderColor = accent
	}
	return lipgloss.NewStyle().
		Width(geometry.width).
		Height(geometry.height).
		MaxWidth(geometry.width).
		MaxHeight(geometry.height).
		Foreground(textColor).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Render(content)
}

func displaySender(value string) string {
	if before, _, found := strings.Cut(value, "<"); found && strings.TrimSpace(before) != "" {
		return strings.Trim(strings.TrimSpace(before), "\"")
	}
	return empty(value, "Unknown sender")
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
	clean := singleLine(value)
	return empty(clean, "No text preview")
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
