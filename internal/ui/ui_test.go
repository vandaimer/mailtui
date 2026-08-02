package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailtui/internal/maildir"
	"mailtui/internal/message"
	"mailtui/internal/readsession"
)

func TestFilterMessagesSearchesHeaders(t *testing.T) {
	m := testModel()
	m.query = "alice"
	matches := m.filteredMessageIndexes()
	if len(matches) != 1 || matches[0] != 0 {
		t.Fatalf("matches = %#v", matches)
	}
	m.query = "billing@example.com"
	matches = m.filteredMessageIndexes()
	if len(matches) != 1 || matches[0] != 1 {
		t.Fatalf("recipient matches = %#v", matches)
	}
	m.query = "missing"
	if matches := m.filteredMessageIndexes(); len(matches) != 0 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestSearchInteractionCanApplyAndCancel(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(Model)
	if !m.searching || m.focus != messagesPane {
		t.Fatalf("search not activated: %#v", m)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "alice"})
	m = updated.(Model)
	if m.query != "alice" || len(m.filteredMessageIndexes()) != 1 {
		t.Fatalf("query = %q", m.query)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(Model)
	if m.searching || m.query != "" {
		t.Fatalf("search was not cancelled: %#v", m)
	}
}

func TestResponsiveViews(t *testing.T) {
	m := testModel()
	m.width, m.height = 130, 32
	wide := m.View().Content
	if got := lipgloss.Width(wide); got != 130 {
		t.Fatalf("wide view width = %d", got)
	}
	if got := lipgloss.Height(wide); got != 32 {
		t.Fatalf("wide view height = %d", got)
	}
	wideBody := m.bodyView(calculateLayout(130, 32))
	if got := lipgloss.Width(wideBody); got != 130 {
		t.Fatalf("wide body width = %d", got)
	}
	if got := lipgloss.Height(wideBody); got != 30 {
		t.Fatalf("wide body height = %d", got)
	}
	for _, label := range []string{"FOLDERS", "INBOX", "READER", "First message", "Alice's message body"} {
		if !strings.Contains(wide, label) {
			t.Fatalf("wide view missing %q", label)
		}
	}

	m.width = 60
	narrow := m.View().Content
	if got := lipgloss.Width(narrow); got != 60 {
		t.Fatalf("narrow view width = %d", got)
	}
	if !strings.Contains(narrow, "FOLDERS") || strings.Contains(narrow, "READER") {
		t.Fatalf("unexpected narrow folder view")
	}
	m.focus = readerPane
	narrow = m.View().Content
	if !strings.Contains(narrow, "READER") || !strings.Contains(narrow, "Alice's message body") {
		t.Fatalf("unexpected narrow reader view")
	}
}

func TestReaderPagingUsesRenderedViewport(t *testing.T) {
	for _, size := range [][2]int{{60, 32}, {72, 10}, {90, 32}, {112, 32}} {
		m := testModel()
		m.width, m.height = size[0], size[1]
		m.focus = readerPane
		viewportHeight := calculateLayout(m.width, m.height).reader.contentHeight

		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = updated.(Model)
		if m.readerScroll != viewportHeight {
			t.Fatalf("size %dx%d: Page Down moved %d rows, want %d", m.width, m.height, m.readerScroll, viewportHeight)
		}

		m.readerScroll = viewportHeight * 2
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		m = updated.(Model)
		if m.readerScroll != viewportHeight {
			t.Fatalf("size %dx%d: Page Up moved to %d, want %d", m.width, m.height, m.readerScroll, viewportHeight)
		}
	}
}

func TestResponsiveLayoutFillsBreakpointAndMinimumSizes(t *testing.T) {
	for _, width := range []int{42, 71, 72, 111, 112} {
		for _, height := range []int{10, 11, 32} {
			m := testModel()
			m.width, m.height = width, height
			view := m.View().Content
			if got := lipgloss.Width(view); got != width {
				t.Errorf("size %dx%d: view width = %d", width, height, got)
			}
			if got := lipgloss.Height(view); got != height {
				t.Errorf("size %dx%d: view height = %d", width, height, got)
			}
		}
	}
}

func TestViewFillsTerminalWithStatusMessage(t *testing.T) {
	for _, width := range []int{60, 90, 130} {
		m := testModel()
		m.width, m.height = width, 32
		m.status = "The selected message has no attachments"
		view := m.View().Content
		if got := lipgloss.Height(view); got != m.height {
			t.Errorf("width %d: view height = %d, want %d", width, got, m.height)
		}
		for index, line := range strings.Split(view, "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("width %d: line %d width = %d", width, index, got)
			}
		}
	}
}

func TestHeaderAndFooterKeepLayoutChromeToOneLine(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Model)
	}{
		{
			name: "maildir root",
			mutate: func(m *Model) {
				m.root = "/backup/mail\nwith-newline"
			},
		},
		{
			name: "pasted search",
			mutate: func(m *Model) {
				m.searching = true
				m.query = "alice\nbilling@example.com"
			},
		},
		{
			name: "status error",
			mutate: func(m *Model) {
				m.status = "read failed\npermission denied"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := testModel()
			test.mutate(&m)
			if got := lipgloss.Height(m.headerView()); got != 1 {
				t.Fatalf("header height = %d", got)
			}
			if got := lipgloss.Height(m.footerView()); got != 1 {
				t.Fatalf("footer height = %d", got)
			}
			if got := lipgloss.Height(m.View().Content); got != m.height {
				t.Fatalf("view height = %d, want %d", got, m.height)
			}
		})
	}
}

func TestMessageSelectionUpdatesPreview(t *testing.T) {
	m := testModel()
	m.focus = messagesPane
	m.move(1)
	selected := m.selectedMessage()
	if selected == nil || selected.Subject != "Invoice available" || m.readerScroll != 0 {
		t.Fatalf("unexpected selection: %#v", selected)
	}
}

func TestRichMessageIsDefaultAndCanToggleToPlainText(t *testing.T) {
	m := testModel()
	m.focus = readerPane
	m.folders[0].Messages[0].Path = "/mail/INBOX/cur/1"
	m.folders[0].Messages[0].Body = "Unique plain fallback"
	m.folders[0].Messages[0].RichBody = "# Rich heading\n\nA **formatted** message."

	geometry := newPaneGeometry(60, 24)
	rich := m.readerPane(geometry)
	if !strings.Contains(rich, "Rich") || !strings.Contains(rich, "heading") || !strings.Contains(rich, "RICH") || strings.Contains(rich, "Unique plain fallback") {
		t.Fatalf("rich view was not selected by default:\n%s", rich)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = updated.(Model)
	plain := m.readerPane(geometry)
	if !strings.Contains(plain, "Unique plain fallback") || !strings.Contains(plain, "PLAIN") || strings.Contains(plain, "formatted") {
		t.Fatalf("plain view was not selected after toggle:\n%s", plain)
	}
}

func TestNewDefersAllFolderIO(t *testing.T) {
	folders := []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX"}}
	m := New("/network", folders)
	if m.folders[0].Messages != nil || m.loadingFolder != "" {
		t.Fatalf("New performed or started synchronous IO: %#v", m)
	}
	if m.Init() == nil {
		t.Fatal("Init did not schedule the initial asynchronous scan")
	}
}

func TestAsyncResultsHydrateInTwoPhases(t *testing.T) {
	reads := &stubReader{}
	m := Model{
		root:    "/network",
		folders: []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX"}},
		width:   130,
		height:  32,
		reads:   reads,
	}

	folderRequest := reads.RequestFolder("/network/INBOX", false)
	updated, cmd := m.Update(folderRequest)
	m = updated.(Model)
	if cmd == nil || m.loadingFolder != "/network/INBOX" || m.folders[0].Messages != nil {
		t.Fatalf("folder scan did not start asynchronously: %#v", m)
	}

	summary := message.Message{Path: "/network/INBOX/cur/1", From: "Alice", Subject: "Header ready"}
	updated, _ = m.Update(readsession.FolderUpdate{Request: folderRequest, Messages: []message.Message{summary}, Done: true})
	m = updated.(Model)
	if m.loadingFolder != "" || len(m.folders[0].Messages) != 1 || m.folders[0].Messages[0].LoadState() != message.LoadHeaderOnly {
		t.Fatalf("unexpected header phase: %#v", m)
	}
	if !strings.Contains(m.View().Content, "Loading content") {
		t.Fatal("reader does not expose the deferred body load")
	}

	messageRequest := reads.RequestMessage(summary)
	updated, cmd = m.Update(messageRequest)
	m = updated.(Model)
	if cmd == nil || m.loadingMessage != summary.Path {
		t.Fatalf("message load did not start asynchronously: %#v", m)
	}
	full := summary
	full.Body = "Content arrived"
	full = full.MarkContentReady()
	updated, _ = m.Update(readsession.MessageUpdate{Request: messageRequest, Message: full})
	m = updated.(Model)
	if m.loadingMessage != "" || !strings.Contains(m.View().Content, "Content arrived") {
		t.Fatalf("message was not hydrated: %#v", m)
	}
}

func TestHydrationFailurePreservesHeadersAndDoesNotBecomeBodyContent(t *testing.T) {
	reads := &stubReader{}
	summary := message.Message{Path: "/network/INBOX/cur/1", From: "Alice", Subject: "Header survives"}
	request := reads.RequestMessage(summary)
	m := Model{
		folders:        []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX", Messages: []message.Message{summary}}},
		loadingMessage: summary.Path,
		width:          130,
		height:         32,
		reads:          reads,
	}
	failure := errors.New("permission denied")

	updated, _ := m.Update(readsession.MessageUpdate{Request: request, Message: summary.MarkContentUnavailable(failure)})
	m = updated.(Model)
	stored := m.folders[0].Messages[0]
	if stored.Subject != summary.Subject || stored.From != summary.From || stored.LoadState() != message.LoadContentUnavailable || stored.LoadError() != failure || stored.Body != "" {
		t.Fatalf("stored failure = %#v", stored)
	}
	if m.loadingMessage != "" || m.queueSelectedMessage(0) != nil {
		t.Fatalf("terminal failure was left loading or retryable: %#v", m)
	}
	view := m.View().Content
	if !strings.Contains(view, "Could not load message content") || !strings.Contains(view, "permission denied") {
		t.Fatalf("failure state was not rendered explicitly:\n%s", view)
	}
}

func TestInvalidHeaderIsTerminalAndRenderedSeparately(t *testing.T) {
	failure := errors.New("malformed header")
	invalid := (message.Message{Path: "/network/INBOX/cur/broken", Subject: "[invalid message]"}).MarkHeaderInvalid(failure)
	m := Model{
		folders: []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX", Messages: []message.Message{invalid}}},
		width:   130,
		height:  32,
		reads:   &stubReader{},
	}

	if m.queueSelectedMessage(0) != nil {
		t.Fatal("invalid header was scheduled for hydration")
	}
	view := m.View().Content
	if !strings.Contains(view, "Invalid message") || !strings.Contains(view, "malformed header") || strings.Contains(view, "Loading content") {
		t.Fatalf("invalid header state was not distinct:\n%s", view)
	}
}

func TestFolderNavigationIsDebounced(t *testing.T) {
	m := Model{folders: []maildir.Folder{{Path: "/network/INBOX"}, {Path: "/network/Other"}}, reads: &stubReader{}}
	cmd := m.move(1)
	if cmd == nil || m.folderIndex != 1 || m.folders[1].Messages != nil {
		t.Fatalf("folder navigation blocked or eagerly loaded: %#v", m)
	}
}

func TestRefreshKeyForcesSelectedFolderReload(t *testing.T) {
	m := testModel()
	m.folders[0].Path = "/network/INBOX"

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(Model)
	if cmd == nil || m.refreshingFolder != "/network/INBOX" || m.messageIndex != 0 {
		t.Fatalf("refresh was not scheduled: %#v", m)
	}
	due, ok := cmd().(folderReadDue)
	if !ok || due.path != "/network/INBOX" || !due.refresh {
		t.Fatalf("unexpected refresh schedule: %#v", due)
	}
}

func TestDuplicateReadDueIsRejectedBeforeAllocatingNewGeneration(t *testing.T) {
	reads := &stubReader{}
	folderModel := Model{
		folders: []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX"}},
		reads:   reads,
	}
	due := folderReadDue{path: "/network/INBOX"}
	updated, firstCmd := folderModel.Update(due)
	folderModel = updated.(Model)
	if firstCmd == nil || folderModel.loadingFolder != due.path || reads.next != 0 {
		t.Fatalf("first folder schedule was not accepted before allocation: %#v", folderModel)
	}
	updated, duplicateCmd := folderModel.Update(due)
	folderModel = updated.(Model)
	if duplicateCmd != nil || reads.next != 0 {
		t.Fatalf("duplicate folder schedule allocated a generation: cmd=%v next=%d", duplicateCmd != nil, reads.next)
	}

	summary := message.Message{Path: "/network/INBOX/cur/1", Subject: "Summary"}
	messageModel := Model{
		folders: []maildir.Folder{{Path: "/network/INBOX", Messages: []message.Message{summary}}},
		reads:   reads,
	}
	messageDue := messageReadDue{summary: summary}
	updated, firstCmd = messageModel.Update(messageDue)
	messageModel = updated.(Model)
	if firstCmd == nil || messageModel.loadingMessage != summary.Path || reads.next != 0 {
		t.Fatalf("first message schedule was not accepted before allocation: %#v", messageModel)
	}
	updated, duplicateCmd = messageModel.Update(messageDue)
	if duplicateCmd != nil || reads.next != 0 {
		t.Fatalf("duplicate message schedule allocated a generation: cmd=%v next=%d", duplicateCmd != nil, reads.next)
	}
}

func TestProgressiveFolderBatchesAppearBeforeCompletion(t *testing.T) {
	reads := &stubReader{}
	request := reads.RequestFolder("/network/INBOX", false)
	m := Model{
		folders:       []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX", Messages: []message.Message{}}},
		loadingFolder: "/network/INBOX",
		reads:         reads,
	}
	first := readsession.FolderUpdate{Request: request, Messages: []message.Message{{Path: "/network/INBOX/cur/1", Subject: "First batch"}}}
	updated, _ := m.Update(first)
	m = updated.(Model)
	if len(m.folders[0].Messages) != 1 || m.loadingFolder == "" {
		t.Fatalf("first batch was not progressive: %#v", m)
	}
	updated, _ = m.Update(readsession.FolderUpdate{Request: request, Messages: first.Messages, Done: true})
	m = updated.(Model)
	if m.loadingFolder != "" {
		t.Fatalf("completed batch kept loading state: %#v", m)
	}
}

func TestRefreshCompletionKeepsReadErrorVisible(t *testing.T) {
	reads := &stubReader{}
	request := reads.RequestFolder("/network/INBOX", true)
	m := Model{
		folders:          []maildir.Folder{{Path: "/network/INBOX", Messages: []message.Message{}}},
		loadingFolder:    "/network/INBOX",
		refreshingFolder: "/network/INBOX",
		reads:            reads,
	}
	batch := readsession.FolderUpdate{Request: request, Err: errors.New("permission denied"), HadReadErrors: true, Done: true}
	updated, _ := m.Update(batch)
	m = updated.(Model)
	if m.refreshingFolder != "" || !strings.Contains(m.status, "some messages could not be read") {
		t.Fatalf("refresh error was hidden: %#v", m)
	}
}

func TestAttachmentPickerIsDiscoverable(t *testing.T) {
	m := testModel()
	m.folders[0].Messages[0].Path = "/mail/cur/1"
	m.folders[0].Messages[0].Attachments = []message.Attachment{{Name: "invoice.pdf", MediaType: "application/pdf", Size: 4096}}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = updated.(Model)
	if !m.attachmentPicker || !strings.Contains(m.View().Content, "invoice.pdf") {
		t.Fatalf("attachment picker did not open: %#v", m)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || m.attachmentPicker || !m.openingAttachment {
		t.Fatalf("attachment open was not scheduled: %#v", m)
	}
}

func testModel() Model {
	folders := []maildir.Folder{{
		Name: "INBOX",
		Messages: []message.Message{
			(message.Message{From: "Alice <alice@example.com>", To: "me@example.com", Subject: "First message", Body: "Alice's message body", Date: time.Date(2026, 8, 2, 14, 0, 0, 0, time.Local)}).MarkContentReady(),
			(message.Message{From: "Bank <bank@example.com>", To: "billing@example.com", Subject: "Invoice available", Body: "Your invoice has arrived.", Date: time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local)}).MarkContentReady(),
		},
	}}
	return Model{root: "/backup/mail", folders: folders, width: 130, height: 32, reads: &stubReader{}}
}

type stubReader struct{ next readsession.RequestID }

func (reader *stubReader) RequestFolder(path string, refresh bool) readsession.FolderRequest {
	reader.next++
	return readsession.FolderRequest{ID: reader.next, Path: path, Refresh: refresh}
}

func (reader *stubReader) ReadFolder(request readsession.FolderRequest) readsession.FolderUpdate {
	return readsession.FolderUpdate{Request: request, Done: true}
}

func (reader *stubReader) RequestMessage(summary message.Message) readsession.MessageRequest {
	reader.next++
	return readsession.MessageRequest{ID: reader.next, Path: summary.Path, Summary: summary}
}

func (reader *stubReader) ReadMessage(request readsession.MessageRequest) readsession.MessageUpdate {
	return readsession.MessageUpdate{Request: request}
}
