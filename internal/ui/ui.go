// Package ui contains the responsive Bubble Tea mail reader.
package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"mailtui/internal/attachment"
	"mailtui/internal/maildir"
	"mailtui/internal/message"
	"mailtui/internal/readsession"
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
	root              string
	folders           []maildir.Folder
	loadedFolders     loadedFolderStates
	interaction       interactionState
	width, height     int
	status            string
	loadingFolder     string
	refreshingFolder  string
	loadingMessage    string
	spinnerFrame      int
	reads             readsession.Reader
	readAdapter       *readAdapter
	openingAttachment bool
	documents         readerDocuments
}

func New(root string, folders []maildir.Folder) Model {
	reads := readsession.New(root)
	model := Model{
		root: root, folders: folders, loadedFolders: newLoadedFolderStates(), reads: reads,
		readAdapter: newReadAdapter(reads), documents: newReaderDocuments(),
	}
	model.reconcileInteraction()
	return model
}

type attachmentOpened struct {
	result attachment.OpenResult
	err    error
}
type folderReadDue struct {
	path    string
	refresh bool
}
type messageReadDue struct{ summary message.Message }
type spinnerTick struct{}

func (m Model) Init() tea.Cmd {
	return m.queueSelectedFolder(0)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = value.Width, value.Height
		m.reconcileInteraction()
	case folderReadDue:
		if value.path != m.selectedFolderPath() || (!value.refresh && m.selectedFolderLoaded()) || m.loadingFolder == value.path {
			return m, nil
		}
		m.loadingFolder = value.path
		return m, tea.Batch(m.readAdapterFor().startFolder(value.path, value.refresh), spinnerCmd())
	case folderReadFact:
		return m, m.applyFolderReadFact(value)
	case messageReadDue:
		selected := m.selectedMessage()
		if selected == nil || selected.Path != value.summary.Path || !selected.NeedsHydration() || m.loadingMessage == value.summary.Path {
			return m, nil
		}
		m.loadingMessage = value.summary.Path
		return m, tea.Batch(m.readAdapterFor().startMessage(value.summary), spinnerCmd())
	case messageReadFact:
		m.storeMessageResult(value.path, value.message)
		return m, nil
	case attachmentOpened:
		m.openingAttachment = false
		if value.err != nil {
			if value.result.Path != "" {
				m.status = "Attachment extracted to " + value.result.Path + "; could not open it: " + value.err.Error()
			} else {
				m.status = "Could not open attachment: " + value.err.Error()
			}
		} else {
			m.status = "Attachment opened: " + filepath.Base(value.result.Path)
		}
	case spinnerTick:
		m.spinnerFrame++
		if m.loadingFolder != "" || m.loadingMessage != "" || m.openingAttachment {
			return m, spinnerCmd()
		}
	case tea.KeyPressMsg:
		return m.updateInteraction(value)
	}
	return m, nil
}

func (m Model) updateInteraction(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	context := m.reconcileInteraction()
	outcome := m.interaction.Apply(interactionInput{key: key.String(), text: key.Text}, context)
	m.reconcileInteraction()
	return m.finishInteraction(outcome)
}

func (m Model) finishInteraction(outcome interactionOutcome) (tea.Model, tea.Cmd) {
	if outcome.quit {
		return m, tea.Quit
	}
	switch outcome.notice {
	case noAttachmentsNotice:
		m.status = "The selected message has no attachments"
	case noRichBodyNotice:
		m.status = "This message has no rich HTML view"
	case plainBodyNotice:
		m.status = "Plain-text view"
	case richBodyNotice:
		m.status = "Rich HTML view"
	case noFolderNotice:
		m.status = "No folder selected"
	case folderBusyNotice:
		m.status = "Wait for the current folder read to finish"
	}
	if outcome.openAttachment {
		selected := m.selectedMessage()
		if selected != nil {
			m.openingAttachment = true
			return m, tea.Batch(openAttachmentCmd(selected.Path, outcome.attachmentIndex), spinnerCmd())
		}
	}
	if outcome.refreshFolder {
		path := m.selectedFolderPath()
		m.refreshingFolder = path
		m.status = "Refreshing the selected folder"
		return m, m.readAdapterFor().queueFolder(path, true, 0)
	}
	if outcome.folderRead == readImmediately && outcome.messageRead == readImmediately {
		if !m.selectedFolderLoaded() {
			return m, m.queueSelectedFolder(0)
		}
		if m.interaction.focus >= messagesPane {
			return m, m.queueSelectedMessage(0)
		}
		return m, nil
	}
	if outcome.folderRead != noRead {
		delay := time.Duration(0)
		if outcome.folderRead == readDeferred {
			delay = folderDebounce
		}
		return m, m.queueSelectedFolder(delay)
	}
	if outcome.messageRead != noRead {
		delay := time.Duration(0)
		if outcome.messageRead == readDeferred {
			delay = messageDebounce
		}
		return m, m.queueSelectedMessage(delay)
	}
	return m, nil
}

func (m *Model) queueSelectedFolder(delay time.Duration) tea.Cmd {
	path := m.selectedFolderPath()
	if path == "" || m.selectedFolderLoaded() {
		return nil
	}
	return m.readAdapterFor().queueFolder(path, false, delay)
}

func (m *Model) queueSelectedMessage(delay time.Duration) tea.Cmd {
	selected := m.selectedMessage()
	if selected == nil || !selected.NeedsHydration() {
		return nil
	}
	summary := *selected
	return m.readAdapterFor().queueMessage(summary, delay)
}

func openAttachmentCmd(messagePath string, index int) tea.Cmd {
	return func() tea.Msg {
		result, err := attachment.Open(messagePath, index)
		return attachmentOpened{result: result, err: err}
	}
}

func spinnerCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTick{} })
}

func (m Model) reader() readsession.Reader {
	if m.reads != nil {
		return m.reads
	}
	return readsession.New(m.root)
}

func (m *Model) readAdapterFor() *readAdapter {
	if m.readAdapter == nil {
		m.readAdapter = newReadAdapter(m.reader())
	}
	return m.readAdapter
}

func (m *Model) applyFolderReadFact(value folderReadFact) tea.Cmd {
	path := value.path
	if m.loadingFolder == "" {
		m.loadingFolder = path
	}
	if m.loadedFolders.phase(path) != folderLoading {
		m.loadedFolders.begin(path, value.refresh)
	}
	if value.fatal {
		m.loadedFolders.fail(path, value.err)
		if m.loadingFolder == path {
			m.loadingFolder = ""
		}
		m.reconcileInteraction()
		if m.refreshingFolder == path {
			m.refreshingFolder = ""
			m.status = "Could not refresh the folder"
		}
		return nil
	}
	if value.started {
		m.reconcileInteraction()
		return m.readAdapterFor().nextFolder(path)
	}
	m.replaceFolderMessages(path, value.messages)
	if value.err != nil {
		m.status = "Some messages could not be read"
	}
	if !value.done {
		return m.readAdapterFor().nextFolder(path)
	}
	m.loadedFolders.complete(path)
	if m.loadingFolder == path {
		m.loadingFolder = ""
	}
	if m.refreshingFolder == path {
		m.refreshingFolder = ""
		if value.hadReadErrors {
			m.status = "Folder refreshed; some messages could not be read"
		} else {
			m.status = fmt.Sprintf("Folder refreshed: %d messages", len(value.messages))
		}
	} else if path == m.selectedFolderPath() && !value.hadReadErrors {
		m.status = ""
	}
	if value.cacheErr != nil {
		m.status = "Could not update the local cache"
	}
	if path == m.selectedFolderPath() {
		m.reconcileInteraction()
		return m.queueSelectedMessage(0)
	}
	return nil
}

func (m *Model) replaceFolderMessages(path string, messages []message.Message) {
	m.loadedFolders.replace(path, messages)
	m.reconcileInteraction()
}

func (m *Model) storeMessageResult(path string, hydrated message.Message) {
	m.documents.Invalidate(path)
	if m.loadedFolders.replaceMessage(path, hydrated) && hydrated.LoadState() == message.LoadContentUnavailable {
		m.status = "Could not load a message"
	}
	if m.loadingMessage == path {
		m.loadingMessage = ""
	}
	m.reconcileInteraction()
}

func (m Model) selectedFolderPath() string {
	if len(m.folders) == 0 {
		return ""
	}
	return m.folders[clampCursor(m.interaction.folderCursor, len(m.folders))].Path
}

func (m Model) selectedFolderLoaded() bool {
	return len(m.folders) > 0 && m.loadedFolders.hasSnapshot(m.selectedFolderPath())
}

func (m Model) messageProjection() messageProjection {
	if len(m.folders) == 0 {
		return projectMessages(nil, m.interaction.query, m.interaction.selectedPath)
	}
	folderIndex := clampCursor(m.interaction.folderCursor, len(m.folders))
	return projectMessages(m.loadedFolders.messages(m.folders[folderIndex].Path), m.interaction.query, m.interaction.selectedPath)
}

func (m Model) selectedMessage() *message.Message {
	return m.messageProjection().Selected()
}

func (m *Model) reconcileInteraction() interactionContext {
	context := m.interactionContext()
	m.interaction.Reconcile(context)
	return context
}

func (m Model) interactionContext() interactionContext {
	projection := m.messageProjection()
	context := interactionContext{
		folderCount:   len(m.folders),
		messages:      projection,
		hasFolder:     len(m.folders) > 0,
		folderLoading: m.loadingFolder != "",
		preserveSelectedPath: m.refreshingFolder != "" && m.refreshingFolder == m.loadingFolder &&
			m.refreshingFolder == m.selectedFolderPath(),
	}
	selected := projection.Selected()
	if selected != nil {
		context.canPickAttachments = selected.LoadState() == message.LoadContentReady && len(selected.Attachments) > 0
		context.attachmentCount = len(selected.Attachments)
		context.hasRichBody = selected.LoadState() == message.LoadContentReady && strings.TrimSpace(selected.RichBody) != ""
	}
	layout := calculateLayout(m.width, m.height)
	if layout.usable && m.interaction.mode == navigationMode && m.interaction.focus == readerPane {
		context.readerBoundsValid = true
		context.readerPageRows = max(1, layout.reader.contentHeight)
		context.readerMaxScroll = m.readerMaxScroll(layout.reader, selected)
	}
	return context
}

func (m Model) readerMaxScroll(geometry paneGeometry, selected *message.Message) int {
	if selected == nil || selected.LoadState() != message.LoadContentReady {
		return 0
	}
	document := m.documents.Document(selected, geometry.contentWidth, m.interaction.preferPlain)
	return document.MaxScroll(geometry.contentHeight)
}

func (m Model) View() tea.View {
	view := tea.NewView(m.viewContent())
	view.AltScreen = true
	return view
}

func (m Model) viewContent() string {
	if m.width == 0 {
		return "Loading…"
	}
	layout := calculateLayout(m.width, m.height)
	if !layout.usable {
		return "mailtui needs a terminal of at least 42×10\npress q to quit"
	}

	header := m.headerView()
	projection := m.messageProjection()
	foot := m.footerView(projection)
	body := m.bodyView(layout, projection)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, foot)
}

func (m Model) headerView() string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accentStrong).Padding(0, 1).Render("MAILTUI")
	readonly := lipgloss.NewStyle().Bold(true).Foreground(cyan).Render("READ ONLY")
	rootWidth := max(1, m.width-lipgloss.Width(brand)-lipgloss.Width(readonly)-1)
	root := softStyle.Render(truncate("  "+singleLine(filepath.Base(m.root)), rootWidth))
	gap := max(1, m.width-lipgloss.Width(brand)-lipgloss.Width(root)-lipgloss.Width(readonly))
	return brand + root + strings.Repeat(" ", gap) + readonly
}

func (m Model) footerView(projection messageProjection) string {
	if m.interaction.mode == attachmentsMode {
		return fitSides(accentStyle.Render("ATTACHMENTS  ↑↓ select  Enter open  Esc cancel"), mutedStyle.Render("read only"), m.width)
	}
	if m.interaction.mode == searchMode {
		count := projection.Len()
		prefix := accentStyle.Render(" / ")
		right := mutedStyle.Render(fmt.Sprintf("%d result(s)  Enter apply  Esc cancel", count))
		queryWidth := max(1, m.width-lipgloss.Width(prefix)-lipgloss.Width(right)-1)
		left := prefix + lipgloss.NewStyle().Foreground(textColor).Render(truncate(singleLine(m.interaction.query)+"█", queryWidth))
		return fitSides(left, right, m.width)
	}
	left := softStyle.Render("Tab/←→ focus  ↑↓ navigate  / search  r refresh  v view  o attachments  Esc back")
	if m.loadingFolder != "" {
		activity := "Reading headers…"
		if m.refreshingFolder == m.loadingFolder {
			activity = "Refreshing folder…"
		}
		left = accentStyle.Render(m.spinner()+" "+activity) + "  " + left
	} else if m.loadingMessage != "" {
		left = accentStyle.Render(m.spinner()+" Reading message…") + "  " + left
	} else if m.openingAttachment {
		left = accentStyle.Render(m.spinner()+" Opening attachment…") + "  " + left
	}
	if m.status != "" {
		left = lipgloss.NewStyle().Foreground(warning).Render("⚠ "+singleLine(m.status)) + "  " + left
	}
	right := mutedStyle.Render("q quit")
	return fitSides(left, right, m.width)
}

func (m Model) bodyView(layout layoutPlan, projection messageProjection) string {
	switch layout.mode {
	case wideLayout:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			m.folderPane(layout.folders),
			m.messagesPane(layout.messages, projection),
			m.readerPane(layout.reader, projection),
		)
	case mediumLayout:
		return lipgloss.JoinHorizontal(lipgloss.Top,
			m.folderPane(layout.folders),
			lipgloss.JoinVertical(lipgloss.Left,
				m.messagesPane(layout.messages, projection),
				m.readerPane(layout.reader, projection),
			),
		)
	default:
		switch m.interaction.focus {
		case foldersPane:
			return m.folderPane(layout.folders)
		case messagesPane:
			return m.messagesPane(layout.messages, projection)
		default:
			return m.readerPane(layout.reader, projection)
		}
	}
}

func (m Model) folderPane(geometry paneGeometry) string {
	lines := make([]string, 0, len(m.folders))
	for index, folder := range m.folders {
		name := maildir.DisplayName(folder.Name)
		count := ""
		if m.loadedFolders.hasSnapshot(folder.Path) {
			count = fmt.Sprintf(" %d", len(m.loadedFolders.messages(folder.Path)))
		} else if folder.Path == m.loadingFolder {
			count = " " + m.spinner()
		}
		line := fitSides(truncate(name, max(1, geometry.width-8)), mutedStyle.Render(count), geometry.contentWidth)
		if index == m.interaction.folderCursor {
			line = fillStyle(selectedStyle, "› "+line, geometry.contentWidth)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = []string{mutedStyle.Render("No folders found")}
	}
	lines = window(lines, m.interaction.folderCursor, geometry.contentHeight)
	return paneBox("FOLDERS", fmt.Sprintf("%d", len(m.folders)), lines, geometry, m.interaction.focus == foldersPane)
}

func (m Model) messagesPane(geometry paneGeometry, projection messageProjection) string {
	if !m.selectedFolderLoaded() {
		lines := []string{"", accentStyle.Render(m.spinner() + " Loading messages"), mutedStyle.Render("Reading Maildir headers only…")}
		return paneBox("MESSAGES", "", lines, geometry, m.interaction.focus == messagesPane)
	}
	rowsPerMessage := 3
	visibleCount := max(1, geometry.contentHeight/rowsPerMessage)
	selected := max(0, projection.SelectedPosition())
	start := clamp(selected-visibleCount/2, 0, max(0, projection.Len()-visibleCount))
	end := min(projection.Len(), start+visibleCount)
	var lines []string
	if projection.Len() == 0 {
		if m.interaction.query != "" {
			lines = []string{"", accentStyle.Render("No results"), mutedStyle.Render("Try another search term.")}
		} else {
			lines = []string{"", mutedStyle.Render("This folder is empty.")}
		}
	} else {
		for position := start; position < end; position++ {
			item := projection.Message(position)
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
	if len(m.folders) > 0 {
		folderName = truncate(strings.ToUpper(maildir.DisplayName(m.folders[m.interaction.folderCursor].Name)), 22)
	}
	count := fmt.Sprintf("%d/%d", projection.Len(), m.folderMessageCount())
	return paneBox(folderName, count, lines, geometry, m.interaction.focus == messagesPane)
}

func (m Model) readerPane(geometry paneGeometry, projection messageProjection) string {
	if !m.selectedFolderLoaded() {
		lines := []string{"", accentStyle.Render(m.spinner() + " Preparing folder"), mutedStyle.Render("The interface remains responsive while loading.")}
		return paneBox("READER", "", lines, geometry, m.interaction.focus == readerPane)
	}
	if m.loadingFolder != "" && m.loadingFolder == m.selectedFolderPath() {
		lines := []string{"", accentStyle.Render(m.spinner() + " Receiving message batches"), mutedStyle.Render(fmt.Sprintf("%d headers available so far…", m.folderMessageCount()))}
		return paneBox("READER", "", lines, geometry, m.interaction.focus == readerPane)
	}
	item := projection.Selected()
	available := geometry.contentWidth
	if item == nil {
		lines := []string{"", accentStyle.Render("No message selected"), mutedStyle.Render("Choose a message or adjust your search.")}
		return paneBox("READER", "", lines, geometry, m.interaction.focus == readerPane)
	}
	if item.LoadState() == message.LoadHeaderInvalid {
		lines := []string{"", lipgloss.NewStyle().Foreground(warning).Render("Invalid message"), mutedStyle.Render(truncate(item.LoadError().Error(), available))}
		return paneBox("READER", "", lines, geometry, m.interaction.focus == readerPane)
	}
	if item.LoadState() == message.LoadContentUnavailable {
		lines := []string{
			"",
			lipgloss.NewStyle().Foreground(warning).Render("Could not load message content"),
			mutedStyle.Render(truncate(item.LoadError().Error(), available)),
		}
		return paneBox("READER", "", lines, geometry, m.interaction.focus == readerPane)
	}
	if item.LoadState() == message.LoadHeaderOnly {
		lines := []string{
			titleStyle.Render(truncate(empty(item.Subject, "(no subject)"), available)),
		}
		lines = append(lines, labelValue("From", item.From, available)...)
		lines = append(lines, labelValue("To", item.To, available)...)
		lines = append(lines, "", accentStyle.Render(m.spinner()+" Loading content…"), mutedStyle.Render("Only this file will be read in full."))
		return paneBox("READER", "", lines, geometry, m.interaction.focus == readerPane)
	}
	if m.interaction.mode == attachmentsMode {
		return m.attachmentPickerPane(item, geometry)
	}

	document := m.documents.Document(item, available, m.interaction.preferPlain)
	viewport := document.Viewport(m.interaction.readerScroll, geometry.contentHeight)
	indicator := document.mode.Label()
	if viewport.maxScroll > 0 {
		indicator += fmt.Sprintf(" · %d%%", viewport.progress)
	}
	return paneBox("READER", indicator, viewport.lines, geometry, m.interaction.focus == readerPane)
}

func (m Model) attachmentPickerPane(item *message.Message, geometry paneGeometry) string {
	available := geometry.contentWidth
	lines := []string{mutedStyle.Render(truncate(empty(item.Subject, "(no subject)"), available)), ""}
	for index, entry := range item.Attachments {
		line := fitSides(truncate(entry.Name, max(4, available-12)), formatBytes(entry.Size), available)
		if index == m.interaction.attachmentCursor {
			line = fillStyle(selectedStyle, "› "+line, available)
		} else {
			line = "  " + line
		}
		lines = append(lines, line, mutedStyle.Render("  "+truncate(entry.MediaType, available-2)))
	}
	lines = window(lines, m.interaction.attachmentCursor*2+2, geometry.contentHeight)
	return paneBox("ATTACHMENTS", fmt.Sprintf("%d", len(item.Attachments)), lines, geometry, true)
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

func (m Model) folderMessageCount() int {
	if len(m.folders) == 0 {
		return 0
	}
	return len(m.loadedFolders.messages(m.selectedFolderPath()))
}

func (m Model) spinner() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[m.spinnerFrame%len(frames)]
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
	width = max(1, width)
	var output []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if paragraph == "" {
			output = append(output, "")
			continue
		}
		wrapped := ansi.Wordwrap(paragraph, width, "")
		wrapped = ansi.Hardwrap(wrapped, width, false)
		output = append(output, strings.Split(wrapped, "\n")...)
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

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
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
	return ansi.Truncate(singleLine(value), width, "…")
}

func empty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func clamp(value, low, high int) int { return min(max(value, low), high) }
