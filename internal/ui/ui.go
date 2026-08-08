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
	layout := calculateLayout(m.width, m.height)
	projection := m.messageProjection()
	return renderPresentation(m.presentationFacts(layout, projection))
}

func (m Model) presentationFacts(layout layoutPlan, projection messageProjection) presentationFacts {
	facts := presentationFacts{
		width: m.width, height: m.height, root: m.root, layout: layout,
		folders: make([]presentationFolder, 0, len(m.folders)), folderCursor: m.interaction.folderCursor,
		projection: projection, selectedFolderReady: m.selectedFolderLoaded(), selectedFolderPath: m.selectedFolderPath(),
		focus: m.interaction.focus, mode: m.interaction.mode, query: m.interaction.query,
		readerScroll: m.interaction.readerScroll, preferPlain: m.interaction.preferPlain,
		attachmentCursor: m.interaction.attachmentCursor, loadingFolder: m.loadingFolder,
		refreshingFolder: m.refreshingFolder, loadingMessage: m.loadingMessage,
		openingAttachment: m.openingAttachment, spinnerFrame: m.spinnerFrame, status: m.status,
		selectedMessage: projection.Selected(),
	}
	for _, folder := range m.folders {
		messages := m.loadedFolders.messages(folder.Path)
		facts.folders = append(facts.folders, presentationFolder{
			name: maildir.DisplayName(folder.Name), path: folder.Path,
			count: len(messages), loaded: m.loadedFolders.hasSnapshot(folder.Path), loading: folder.Path == m.loadingFolder,
		})
	}
	if len(m.folders) > 0 {
		index := clampCursor(m.interaction.folderCursor, len(m.folders))
		facts.selectedName = maildir.DisplayName(m.folders[index].Name)
		facts.selectedCount = len(m.loadedFolders.messages(m.folders[index].Path))
	}
	if layout.usable && facts.selectedMessage != nil && facts.selectedMessage.LoadState() == message.LoadContentReady && facts.mode != attachmentsMode {
		document := m.documents.Document(facts.selectedMessage, layout.reader.contentWidth, facts.preferPlain)
		facts.reader = readerPresentation{viewport: document.Viewport(facts.readerScroll, layout.reader.contentHeight), mode: document.mode.Label(), ready: true}
	}
	return facts
}

// The following small methods preserve the existing test-facing composition
// surface while delegating all rendering to presentation facts.
func (m Model) headerView() string {
	facts := m.presentationFacts(calculateLayout(m.width, m.height), m.messageProjection())
	return (presentation{facts: facts}).header()
}

func (m Model) footerView(projection messageProjection) string {
	facts := m.presentationFacts(calculateLayout(m.width, m.height), projection)
	return (presentation{facts: facts}).footer()
}

func (m Model) bodyView(layout layoutPlan, projection messageProjection) string {
	facts := m.presentationFacts(layout, projection)
	return (presentation{facts: facts}).body()
}

func (m Model) folderPane(geometry paneGeometry) string {
	facts := m.presentationFacts(calculateLayout(m.width, m.height), m.messageProjection())
	return (presentation{facts: facts}).folderPane(geometry)
}

func (m Model) messagesPane(geometry paneGeometry, projection messageProjection) string {
	facts := m.presentationFacts(calculateLayout(m.width, m.height), projection)
	return (presentation{facts: facts}).messagesPane(geometry)
}

func (m Model) readerPane(geometry paneGeometry, projection messageProjection) string {
	facts := m.presentationFacts(calculateLayout(m.width, m.height), projection)
	if facts.selectedMessage != nil && facts.selectedMessage.LoadState() == message.LoadContentReady && facts.mode != attachmentsMode {
		document := m.documents.Document(facts.selectedMessage, geometry.contentWidth, facts.preferPlain)
		facts.reader = readerPresentation{viewport: document.Viewport(facts.readerScroll, geometry.contentHeight), mode: document.mode.Label(), ready: true}
	}
	return (presentation{facts: facts}).readerPane(geometry)
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

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
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
