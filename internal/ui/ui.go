// Package ui contains the responsive Bubble Tea mail reader.
package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailtui/internal/attachment"
	"mailtui/internal/maildir"
	"mailtui/internal/message"
	"mailtui/internal/metadata"
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
	folderDebounce   = 180 * time.Millisecond
	messageDebounce  = 120 * time.Millisecond
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
	loadingFolder             string
	refreshingFolder          string
	refreshHadErrors          bool
	loadingMessage            string
	spinnerFrame              int
	metadata                  *metadata.Store
	attachmentPicker          bool
	attachmentIndex           int
	openingAttachment         bool
	plainBody                 bool
	bodyRenderCache           map[bodyRenderKey][]string
}

type bodyRenderKey struct {
	path  string
	width int
	plain bool
}

func New(root string, folders []maildir.Folder) Model {
	return Model{
		root: root, folders: folders, focus: foldersPane, metadata: metadata.New(root),
		bodyRenderCache: make(map[bodyRenderKey][]string),
	}
}

type folderLoadRequest struct {
	path  string
	force bool
}
type folderLoaded struct {
	path     string
	messages []message.Message
	err      error
}
type folderScanStarted struct {
	path        string
	fingerprint string
	batches     <-chan maildir.HeaderBatch
	err         error
}
type folderBatchReceived struct {
	path        string
	fingerprint string
	batches     <-chan maildir.HeaderBatch
	batch       maildir.HeaderBatch
}
type metadataSaved struct{ err error }
type attachmentOpened struct {
	path string
	err  error
}
type messageLoadRequest struct{ path string }
type messageLoaded struct {
	path    string
	message message.Message
	err     error
}
type spinnerTick struct{}

func (m Model) Init() tea.Cmd {
	return m.queueSelectedFolder(0)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		if m.width != value.Width {
			m.bodyRenderCache = make(map[bodyRenderKey][]string)
		}
		m.width, m.height = value.Width, value.Height
	case folderLoadRequest:
		if value.path != m.selectedFolderPath() {
			if value.force && m.refreshingFolder == value.path {
				m.refreshingFolder = ""
				m.refreshHadErrors = false
				m.status = "Folder refresh cancelled"
			}
			return m, nil
		}
		if (!value.force && m.selectedFolderLoaded()) || m.loadingFolder == value.path {
			return m, nil
		}
		m.loadingFolder = value.path
		return m, tea.Batch(startFolderScanCmd(value.path, m.metadata, value.force), spinnerCmd())
	case folderLoaded:
		m.storeFolderResult(value)
		if value.path == m.selectedFolderPath() {
			return m, m.queueSelectedMessage(0)
		}
	case folderScanStarted:
		if value.err != nil {
			m.storeFolderResult(folderLoaded{path: value.path, messages: []message.Message{}, err: value.err})
			if m.refreshingFolder == value.path {
				m.refreshingFolder = ""
				m.refreshHadErrors = false
				m.status = "Could not refresh the folder"
			}
			return m, nil
		}
		m.replaceFolderMessages(value.path, []message.Message{})
		return m, nextFolderBatchCmd(value.path, value.fingerprint, value.batches)
	case folderBatchReceived:
		m.appendFolderBatch(value.path, value.batch)
		if m.refreshingFolder == value.path && value.batch.Err != nil {
			m.refreshHadErrors = true
		}
		if !value.batch.Done {
			return m, nextFolderBatchCmd(value.path, value.fingerprint, value.batches)
		}
		if m.loadingFolder == value.path {
			m.loadingFolder = ""
		}
		if m.refreshingFolder == value.path {
			m.refreshingFolder = ""
			if m.refreshHadErrors {
				m.status = "Folder refreshed; some messages could not be read"
			} else {
				m.status = fmt.Sprintf("Folder refreshed: %d messages", len(m.folderMessages(value.path)))
			}
			m.refreshHadErrors = false
		}
		commands := []tea.Cmd{saveMetadataCmd(m.metadata, value.path, value.fingerprint, m.folderMessages(value.path))}
		if value.path == m.selectedFolderPath() {
			commands = append(commands, m.queueSelectedMessage(0))
		}
		return m, tea.Batch(commands...)
	case metadataSaved:
		if value.err != nil {
			m.status = "Could not update the local cache"
		}
	case attachmentOpened:
		m.openingAttachment = false
		if value.err != nil {
			m.status = "Could not open attachment: " + value.err.Error()
		} else {
			m.status = "Attachment opened: " + filepath.Base(value.path)
		}
	case messageLoadRequest:
		selected := m.selectedMessage()
		if selected == nil || selected.Path != value.path || selected.Loaded || selected.Err != nil || m.loadingMessage == value.path {
			return m, nil
		}
		m.loadingMessage = value.path
		return m, tea.Batch(loadMessageCmd(value.path), spinnerCmd())
	case messageLoaded:
		m.storeMessageResult(value)
	case spinnerTick:
		m.spinnerFrame++
		if m.loadingFolder != "" || m.loadingMessage != "" || m.openingAttachment {
			return m, spinnerCmd()
		}
	case tea.KeyPressMsg:
		if m.attachmentPicker {
			return m.updateAttachmentPicker(value)
		}
		if m.searching {
			return m.updateSearch(value)
		}
		return m.updateNavigation(value)
	}
	return m, nil
}

func (m Model) updateAttachmentPicker(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	selected := m.selectedMessage()
	if selected == nil || len(selected.Attachments) == 0 {
		m.attachmentPicker = false
		return m, nil
	}
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "o":
		m.attachmentPicker = false
	case "up", "k":
		m.attachmentIndex = clamp(m.attachmentIndex-1, 0, len(selected.Attachments)-1)
	case "down", "j":
		m.attachmentIndex = clamp(m.attachmentIndex+1, 0, len(selected.Attachments)-1)
	case "enter":
		m.attachmentPicker = false
		m.openingAttachment = true
		return m, tea.Batch(openAttachmentCmd(selected.Path, m.attachmentIndex), spinnerCmd())
	}
	return m, nil
}

func (m Model) updateSearch(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		if key.Text != "" {
			m.query += key.Text
			m.messageIndex = 0
			m.readerScroll = 0
		}
	}
	return m, m.queueSelectedMessage(messageDebounce)
}

func (m Model) updateNavigation(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "/":
		m.searching = true
		m.queryBeforeSearch = m.query
		if m.focus == foldersPane {
			m.focus = messagesPane
		}
	case "o":
		selected := m.selectedMessage()
		if selected != nil && selected.Loaded && len(selected.Attachments) > 0 {
			m.attachmentPicker = true
			m.attachmentIndex = 0
			m.focus = readerPane
		} else {
			m.status = "The selected message has no attachments"
		}
	case "v":
		selected := m.selectedMessage()
		if selected == nil || !selected.Loaded || strings.TrimSpace(selected.RichBody) == "" {
			m.status = "This message has no rich HTML view"
			break
		}
		m.plainBody = !m.plainBody
		m.readerScroll = 0
		if m.plainBody {
			m.status = "Plain-text view"
		} else {
			m.status = "Rich HTML view"
		}
	case "r":
		path := m.selectedFolderPath()
		if path == "" {
			m.status = "No folder selected"
			break
		}
		if m.loadingFolder != "" {
			m.status = "Wait for the current folder read to finish"
			break
		}
		m.refreshingFolder = path
		m.refreshHadErrors = false
		m.status = "Refreshing the selected folder"
		m.messageIndex = 0
		m.readerScroll = 0
		cmd = refreshFolderCmd(path)
	case "tab":
		m.focus = (m.focus + 1) % 3
		cmd = m.ensureFocusedData()
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
		cmd = m.ensureFocusedData()
	case "left", "h":
		if m.focus > foldersPane {
			m.focus--
		}
	case "right", "l":
		if m.focus < readerPane {
			m.focus++
			cmd = m.ensureFocusedData()
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
			cmd = m.ensureFocusedData()
		}
	case "up", "k":
		cmd = m.move(-1)
	case "down", "j":
		cmd = m.move(1)
	case "pgup":
		if m.focus == readerPane {
			m.readerScroll = max(0, m.readerScroll-max(1, m.readerViewportHeight()))
		}
	case "pgdown":
		if m.focus == readerPane {
			m.readerScroll += max(1, m.readerViewportHeight())
		}
	case "home":
		cmd = m.moveToBoundary(false)
	case "end":
		cmd = m.moveToBoundary(true)
	}
	return m, cmd
}

func (m *Model) move(delta int) tea.Cmd {
	switch m.focus {
	case foldersPane:
		if len(m.folders) == 0 {
			return nil
		}
		next := clamp(m.folderIndex+delta, 0, len(m.folders)-1)
		if next != m.folderIndex {
			m.folderIndex = next
			m.messageIndex = 0
			m.readerScroll = 0
			m.query = ""
			return m.queueSelectedFolder(folderDebounce)
		}
	case messagesPane:
		matches := m.filteredMessageIndexes()
		if len(matches) > 0 {
			m.messageIndex = clamp(m.messageIndex+delta, 0, len(matches)-1)
			m.readerScroll = 0
			return m.queueSelectedMessage(messageDebounce)
		}
	case readerPane:
		m.readerScroll = max(0, m.readerScroll+delta)
	}
	return nil
}

func (m *Model) moveToBoundary(end bool) tea.Cmd {
	if m.focus == foldersPane && len(m.folders) > 0 {
		if end {
			m.folderIndex = len(m.folders) - 1
		} else {
			m.folderIndex = 0
		}
		m.messageIndex, m.readerScroll = 0, 0
		m.query = ""
		return m.queueSelectedFolder(folderDebounce)
	}
	if m.focus == messagesPane {
		matches := m.filteredMessageIndexes()
		if end && len(matches) > 0 {
			m.messageIndex = len(matches) - 1
		} else {
			m.messageIndex = 0
		}
		m.readerScroll = 0
		return m.queueSelectedMessage(messageDebounce)
	}
	return nil
}

func (m Model) ensureFocusedData() tea.Cmd {
	if !m.selectedFolderLoaded() {
		return m.queueSelectedFolder(0)
	}
	if m.focus >= messagesPane {
		return m.queueSelectedMessage(0)
	}
	return nil
}

func (m Model) queueSelectedFolder(delay time.Duration) tea.Cmd {
	path := m.selectedFolderPath()
	if path == "" || m.selectedFolderLoaded() {
		return nil
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return folderLoadRequest{path: path} })
}

func refreshFolderCmd(path string) tea.Cmd {
	return func() tea.Msg { return folderLoadRequest{path: path, force: true} }
}

func (m Model) queueSelectedMessage(delay time.Duration) tea.Cmd {
	selected := m.selectedMessage()
	if selected == nil || selected.Path == "" || selected.Loaded || selected.Err != nil {
		return nil
	}
	path := selected.Path
	return tea.Tick(delay, func(time.Time) tea.Msg { return messageLoadRequest{path: path} })
}

func startFolderScanCmd(path string, store *metadata.Store, force bool) tea.Cmd {
	return func() tea.Msg {
		paths, fingerprint, err := maildir.ListMessagePaths(path)
		if err != nil {
			return folderScanStarted{path: path, err: err}
		}
		if !force {
			if messages, found := store.Load(path, fingerprint); found {
				maildir.SortMessages(messages)
				return folderLoaded{path: path, messages: messages}
			}
		}
		return folderScanStarted{
			path: path, fingerprint: fingerprint,
			batches: maildir.ScanHeaderBatches(paths, 64),
		}
	}
}

func nextFolderBatchCmd(path, fingerprint string, batches <-chan maildir.HeaderBatch) tea.Cmd {
	return func() tea.Msg {
		batch, ok := <-batches
		if !ok {
			batch = maildir.HeaderBatch{Done: true}
		}
		return folderBatchReceived{path: path, fingerprint: fingerprint, batches: batches, batch: batch}
	}
}

func saveMetadataCmd(store *metadata.Store, path, fingerprint string, messages []message.Message) tea.Cmd {
	snapshot := append([]message.Message(nil), messages...)
	return func() tea.Msg {
		return metadataSaved{err: store.Save(path, fingerprint, snapshot)}
	}
}

func loadMessageCmd(path string) tea.Cmd {
	return func() tea.Msg {
		parsed, err := message.ParseFile(path)
		return messageLoaded{path: path, message: parsed, err: err}
	}
}

func openAttachmentCmd(messagePath string, index int) tea.Cmd {
	return func() tea.Msg {
		path, err := attachment.ExtractToCache(messagePath, index)
		if err == nil {
			err = attachment.OpenDefault(path)
		}
		return attachmentOpened{path: path, err: err}
	}
}

func spinnerCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTick{} })
}

func (m *Model) storeFolderResult(result folderLoaded) {
	m.replaceFolderMessages(result.path, result.messages)
	if m.loadingFolder == result.path {
		m.loadingFolder = ""
	}
	if result.err != nil {
		m.status = "Some messages could not be read"
	} else if result.path == m.selectedFolderPath() {
		m.status = ""
	}
}

func (m *Model) replaceFolderMessages(path string, messages []message.Message) {
	for index := range m.folders {
		if m.folders[index].Path == path {
			m.folders[index].Messages = messages
			return
		}
	}
}

func (m *Model) appendFolderBatch(path string, batch maildir.HeaderBatch) {
	for index := range m.folders {
		if m.folders[index].Path != path {
			continue
		}
		m.folders[index].Messages = append(m.folders[index].Messages, batch.Messages...)
		maildir.SortMessages(m.folders[index].Messages)
		break
	}
	if batch.Err != nil {
		m.status = "Some messages could not be read"
	}
}

func (m Model) folderMessages(path string) []message.Message {
	for index := range m.folders {
		if m.folders[index].Path == path {
			return m.folders[index].Messages
		}
	}
	return nil
}

func (m *Model) storeMessageResult(result messageLoaded) {
	for folderIndex := range m.folders {
		for messageIndex := range m.folders[folderIndex].Messages {
			if m.folders[folderIndex].Messages[messageIndex].Path != result.path {
				continue
			}
			if result.err != nil {
				m.folders[folderIndex].Messages[messageIndex].Err = result.err
				m.folders[folderIndex].Messages[messageIndex].Loaded = true
				m.folders[folderIndex].Messages[messageIndex].Body = "[this message could not be loaded]"
				m.status = "Could not load a message"
			} else {
				m.folders[folderIndex].Messages[messageIndex] = result.message
			}
			break
		}
	}
	if m.loadingMessage == result.path {
		m.loadingMessage = ""
	}
}

func (m Model) selectedFolderPath() string {
	if len(m.folders) == 0 {
		return ""
	}
	return m.folders[m.folderIndex].Path
}

func (m Model) selectedFolderLoaded() bool {
	return len(m.folders) > 0 && m.folders[m.folderIndex].Messages != nil
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

func (m Model) View() tea.View {
	view := tea.NewView(m.viewContent())
	view.AltScreen = true
	return view
}

func (m Model) viewContent() string {
	if m.width == 0 {
		return "Loading…"
	}
	if m.width < 42 || m.height < 10 {
		return "mailtui needs a terminal of at least 42×10\npress q to quit"
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
	if m.attachmentPicker {
		return fitSides(accentStyle.Render("ATTACHMENTS  ↑↓ select  Enter open  Esc cancel"), mutedStyle.Render("read only"), m.width)
	}
	if m.searching {
		count := len(m.filteredMessageIndexes())
		left := accentStyle.Render(" / ") + lipgloss.NewStyle().Foreground(textColor).Render(m.query+"█")
		right := mutedStyle.Render(fmt.Sprintf("%d result(s)  Enter apply  Esc cancel", count))
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
		left = lipgloss.NewStyle().Foreground(warning).Render("⚠ "+m.status) + "  " + left
	}
	right := mutedStyle.Render("q quit")
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
		} else if folder.Path == m.loadingFolder {
			count = " " + m.spinner()
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
		lines = []string{mutedStyle.Render("No folders found")}
	}
	lines = window(lines, m.folderIndex, contentHeight)
	return paneBox("FOLDERS", fmt.Sprintf("%d", len(m.folders)), lines, width, height, m.focus == foldersPane)
}

func (m Model) messagesPane(width, height int) string {
	if !m.selectedFolderLoaded() {
		lines := []string{"", accentStyle.Render(m.spinner() + " Loading messages"), mutedStyle.Render("Reading Maildir headers only…")}
		return paneBox("MESSAGES", "", lines, width, height, m.focus == messagesPane)
	}
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
			lines = []string{"", accentStyle.Render("No results"), mutedStyle.Render("Try another search term.")}
		} else {
			lines = []string{"", mutedStyle.Render("This folder is empty.")}
		}
	} else {
		for position := start; position < end; position++ {
			item := m.folders[m.folderIndex].Messages[matches[position]]
			available := max(10, width-4)
			first := fitSides(truncate(displaySender(item.From), max(4, available-10)), displayDate(item.Date), available)
			second := truncate(empty(item.Subject, "(no subject)"), available)
			preview := snippet(item.Body)
			if !item.Loaded {
				preview = "Select to load the preview"
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
		folderName = truncate(strings.ToUpper(maildir.DisplayName(m.folders[m.folderIndex].Name)), 22)
	}
	count := fmt.Sprintf("%d/%d", len(matches), m.folderMessageCount())
	return paneBox(folderName, count, lines, width, height, m.focus == messagesPane)
}

func (m Model) readerPane(width, height int) string {
	if !m.selectedFolderLoaded() {
		lines := []string{"", accentStyle.Render(m.spinner() + " Preparing folder"), mutedStyle.Render("The interface remains responsive while loading.")}
		return paneBox("READER", "", lines, width, height, m.focus == readerPane)
	}
	if m.loadingFolder != "" && m.loadingFolder == m.selectedFolderPath() {
		lines := []string{"", accentStyle.Render(m.spinner() + " Receiving message batches"), mutedStyle.Render(fmt.Sprintf("%d headers available so far…", m.folderMessageCount()))}
		return paneBox("READER", "", lines, width, height, m.focus == readerPane)
	}
	item := m.selectedMessage()
	contentHeight := paneContentHeight(height)
	available := max(10, width-4)
	if item == nil {
		lines := []string{"", accentStyle.Render("No message selected"), mutedStyle.Render("Choose a message or adjust your search.")}
		return paneBox("READER", "", lines, width, height, m.focus == readerPane)
	}
	if item.Err != nil && !item.Loaded {
		lines := []string{"", lipgloss.NewStyle().Foreground(warning).Render("Invalid message"), mutedStyle.Render(truncate(item.Err.Error(), available))}
		return paneBox("READER", "", lines, width, height, m.focus == readerPane)
	}
	if !item.Loaded {
		lines := []string{
			titleStyle.Render(truncate(empty(item.Subject, "(no subject)"), available)),
		}
		lines = append(lines, labelValue("From", item.From, available)...)
		lines = append(lines, labelValue("To", item.To, available)...)
		lines = append(lines, "", accentStyle.Render(m.spinner()+" Loading content…"), mutedStyle.Render("Only this file will be read in full."))
		return paneBox("READER", "", lines, width, height, m.focus == readerPane)
	}
	if m.attachmentPicker {
		return m.attachmentPickerPane(item, width, height)
	}

	var lines []string
	lines = append(lines, titleStyle.Render(truncate(empty(item.Subject, "(no subject)"), available)))
	lines = append(lines, labelValue("From", item.From, available)...)
	lines = append(lines, labelValue("To", item.To, available)...)
	if item.Cc != "" {
		lines = append(lines, labelValue("Cc", item.Cc, available)...)
	}
	if item.Bcc != "" {
		lines = append(lines, labelValue("Bcc", item.Bcc, available)...)
	}
	lines = append(lines, labelValue("Date", item.DateText, available)...)
	if len(item.Attachments) > 0 {
		lines = append(lines, "", accentStyle.Render(fmt.Sprintf("▣ %d attachment(s)", len(item.Attachments))))
		for _, attachment := range item.Attachments {
			lines = append(lines, truncate(fmt.Sprintf("  %s · %s · %s", attachment.Name, attachment.MediaType, formatBytes(attachment.Size)), available))
		}
	}
	lines = append(lines, "", mutedStyle.Render(strings.Repeat("─", available)), "")
	lines = append(lines, m.renderedMessageContent(item, available)...)

	maxScroll := max(0, len(lines)-contentHeight)
	scroll := clamp(m.readerScroll, 0, maxScroll)
	end := min(len(lines), scroll+contentHeight)
	indicator := "PLAIN"
	if !m.plainBody && strings.TrimSpace(item.RichBody) != "" {
		indicator = "RICH"
	}
	if maxScroll > 0 {
		indicator += fmt.Sprintf(" · %d%%", scroll*100/max(1, maxScroll))
	}
	return paneBox("READER", indicator, lines[scroll:end], width, height, m.focus == readerPane)
}

func (m Model) renderedMessageContent(item *message.Message, width int) []string {
	key := bodyRenderKey{path: item.Path, width: width, plain: m.plainBody}
	if item.Path != "" && m.bodyRenderCache != nil {
		if cached, found := m.bodyRenderCache[key]; found {
			return cached
		}
	}
	lines := renderMessageContent(item, width, m.plainBody)
	if item.Path != "" && m.bodyRenderCache != nil {
		m.bodyRenderCache[key] = lines
	}
	return lines
}

func (m Model) attachmentPickerPane(item *message.Message, width, height int) string {
	available := max(10, width-4)
	lines := []string{mutedStyle.Render(truncate(empty(item.Subject, "(no subject)"), available)), ""}
	for index, entry := range item.Attachments {
		line := fitSides(truncate(entry.Name, max(4, available-12)), formatBytes(entry.Size), available)
		if index == m.attachmentIndex {
			line = fillStyle(selectedStyle, "› "+line, available)
		} else {
			line = "  " + line
		}
		lines = append(lines, line, mutedStyle.Render("  "+truncate(entry.MediaType, available-2)))
	}
	lines = window(lines, m.attachmentIndex*2+2, paneContentHeight(height))
	return paneBox("ATTACHMENTS", fmt.Sprintf("%d", len(item.Attachments)), lines, width, height, true)
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
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
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
	clean := strings.Join(strings.Fields(value), " ")
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
