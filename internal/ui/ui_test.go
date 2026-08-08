package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"mailtui/internal/attachment"
	"mailtui/internal/maildir"
	"mailtui/internal/message"
	"mailtui/internal/readsession"
)

func TestFilterMessagesSearchesHeaders(t *testing.T) {
	m := testModel()
	m.interaction.query = "alice"
	projection := m.messageProjection()
	if projection.Len() != 1 || projection.Message(0).Path != "/mail/cur/alice" {
		t.Fatalf("projection = %#v", projection)
	}
	m.interaction.query = "billing@example.com"
	projection = m.messageProjection()
	if projection.Len() != 1 || projection.Message(0).Path != "/mail/cur/bank" {
		t.Fatalf("recipient projection = %#v", projection)
	}
	m.interaction.query = "missing"
	if projection := m.messageProjection(); projection.Len() != 0 {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestSearchInteractionCanApplyAndCancel(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(Model)
	if m.interaction.mode != searchMode || m.interaction.focus != messagesPane {
		t.Fatalf("search not activated: %#v", m)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "alice"})
	m = updated.(Model)
	if m.interaction.query != "alice" || m.messageProjection().Len() != 1 {
		t.Fatalf("query = %q", m.interaction.query)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(Model)
	if m.interaction.mode != navigationMode || m.interaction.query != "" {
		t.Fatalf("search was not cancelled: %#v", m)
	}
}

func TestSearchCancelRestoresPathFilteredOutByDraft(t *testing.T) {
	m := testModel()
	m.interaction.focus = readerPane
	m.interaction.selectedPath = "/mail/cur/bank"
	mutateFolderMessages(&m, 0, func(messages []message.Message) {
		messages[1].Body = strings.Repeat("reader line\n", 80)
	})
	m.interaction.readerScroll = 7

	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "alice"})
	m = updated.(Model)
	if selected := m.selectedMessage(); selected == nil || selected.Path != "/mail/cur/alice" {
		t.Fatalf("draft did not select its first visible path: %#v", selected)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(Model)
	if selected := m.selectedMessage(); selected == nil || selected.Path != "/mail/cur/bank" || m.interaction.focus != readerPane || m.interaction.readerScroll != 7 {
		t.Fatalf("cancel did not restore interaction state: selected=%#v interaction=%#v", selected, m.interaction)
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
	wideBody := m.bodyView(calculateLayout(130, 32), m.messageProjection())
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
	m.interaction.focus = readerPane
	narrow = m.View().Content
	if !strings.Contains(narrow, "READER") || !strings.Contains(narrow, "Alice's message body") {
		t.Fatalf("unexpected narrow reader view")
	}
}

func TestReaderPagingUsesRenderedViewport(t *testing.T) {
	for _, size := range [][2]int{{60, 32}, {72, 10}, {90, 32}, {112, 32}} {
		m := testModel()
		m.width, m.height = size[0], size[1]
		m.interaction.focus = readerPane
		viewportHeight := calculateLayout(m.width, m.height).reader.contentHeight
		maximum := m.interactionContext().readerMaxScroll

		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = updated.(Model)
		if want := min(viewportHeight, maximum); m.interaction.readerScroll != want {
			t.Fatalf("size %dx%d: Page Down moved %d rows, want %d", m.width, m.height, m.interaction.readerScroll, want)
		}

		m.interaction.readerScroll = viewportHeight * 2
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
		m = updated.(Model)
		start := min(viewportHeight*2, maximum)
		if want := max(0, start-viewportHeight); m.interaction.readerScroll != want {
			t.Fatalf("size %dx%d: Page Up moved to %d, want %d", m.width, m.height, m.interaction.readerScroll, want)
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
				m.interaction.mode = searchMode
				m.interaction.query = "alice\nbilling@example.com"
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
			if got := lipgloss.Height(m.footerView(m.messageProjection())); got != 1 {
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
	m.interaction.focus = messagesPane
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	selected := m.selectedMessage()
	if selected == nil || selected.Subject != "Invoice available" || m.interaction.readerScroll != 0 {
		t.Fatalf("unexpected selection: %#v", selected)
	}
}

func TestMessageNavigationWithPathsIsNotUndoneByReconciliation(t *testing.T) {
	m := testModel()
	m.interaction.focus = messagesPane
	mutateFolderMessages(&m, 0, func(messages []message.Message) {
		for index := range messages {
			messages[index].Path = fmt.Sprintf("/mail/INBOX/cur/%d", index)
		}
	})
	m.reconcileInteraction()

	for _, test := range []struct {
		name string
		key  tea.KeyPressMsg
		want int
	}{
		{name: "down", key: tea.KeyPressMsg{Code: tea.KeyDown}, want: 1},
		{name: "home", key: tea.KeyPressMsg{Code: tea.KeyHome}, want: 0},
		{name: "end", key: tea.KeyPressMsg{Code: tea.KeyEnd}, want: 1},
		{name: "up", key: tea.KeyPressMsg{Code: tea.KeyUp}, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			updated, _ := m.Update(test.key)
			m = updated.(Model)
			projection := m.messageProjection()
			messages := m.loadedFolders.messages(m.folders[0].Path)
			if projection.SelectedPosition() != test.want || m.interaction.selectedPath != messages[test.want].Path {
				t.Fatalf("position/path = %d/%q, want %d/%q", projection.SelectedPosition(), m.interaction.selectedPath, test.want, messages[test.want].Path)
			}
		})
	}
}

func TestSearchQueryEditPreservesVisibleSelectedPath(t *testing.T) {
	m := testModel()
	m.interaction.focus = messagesPane
	mutateFolderMessages(&m, 0, func(messages []message.Message) {
		messages[0].Path = "/mail/INBOX/cur/alice"
		messages[1].Path = "/mail/INBOX/cur/bank"
	})
	m.interaction.selectedPath = "/mail/INBOX/cur/bank"
	m.reconcileInteraction()

	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(Model)

	if m.messageProjection().Len() != 2 {
		t.Fatalf("query should retain both messages, got %d", m.messageProjection().Len())
	}
	if selected := m.selectedMessage(); selected == nil || selected.Path != "/mail/INBOX/cur/bank" || m.messageProjection().SelectedPosition() != 1 {
		t.Fatalf("query edit moved visible selection: selected=%#v interaction=%#v", selected, m.interaction)
	}
}

func TestRichMessageIsDefaultAndCanToggleToPlainText(t *testing.T) {
	m := testModel()
	m.interaction.focus = readerPane
	mutateFolderMessages(&m, 0, func(messages []message.Message) {
		messages[0].Path = "/mail/INBOX/cur/1"
		messages[0].Body = "Unique plain fallback"
		messages[0].RichBody = "# Rich heading\n\nA **formatted** message."
	})

	geometry := newPaneGeometry(60, 24)
	rich := m.readerPane(geometry, m.messageProjection())
	if !strings.Contains(rich, "Rich") || !strings.Contains(rich, "heading") || !strings.Contains(rich, "RICH") || strings.Contains(rich, "Unique plain fallback") {
		t.Fatalf("rich view was not selected by default:\n%s", rich)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	m = updated.(Model)
	plain := m.readerPane(geometry, m.messageProjection())
	if !strings.Contains(plain, "Unique plain fallback") || !strings.Contains(plain, "PLAIN") || strings.Contains(plain, "formatted") {
		t.Fatalf("plain view was not selected after toggle:\n%s", plain)
	}
}

func TestNewDefersAllFolderIO(t *testing.T) {
	folders := []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX"}}
	m := New("/network", folders)
	if m.loadedFolders.hasSnapshot(folders[0].Path) || m.loadingFolder != "" {
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
	if cmd == nil || m.loadingFolder != "/network/INBOX" || m.loadedFolders.hasSnapshot("/network/INBOX") {
		t.Fatalf("folder scan did not start asynchronously: %#v", m)
	}

	summary := message.Message{Path: "/network/INBOX/cur/1", From: "Alice", Subject: "Header ready"}
	updated, _ = m.Update(readsession.FolderUpdate{Request: folderRequest, Messages: []message.Message{summary}, Done: true})
	m = updated.(Model)
	messages := m.loadedFolders.messages("/network/INBOX")
	if m.loadingFolder != "" || len(messages) != 1 || messages[0].LoadState() != message.LoadHeaderOnly {
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
		folders:        []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX"}},
		loadingMessage: summary.Path,
		width:          130,
		height:         32,
		reads:          reads,
	}
	setFolderMessages(&m, "/network/INBOX", []message.Message{summary})
	failure := errors.New("permission denied")

	updated, _ := m.Update(readsession.MessageUpdate{Request: request, Message: summary.MarkContentUnavailable(failure)})
	m = updated.(Model)
	stored := m.loadedFolders.messages("/network/INBOX")[0]
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
		folders: []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX"}},
		width:   130,
		height:  32,
		reads:   &stubReader{},
	}
	setFolderMessages(&m, "/network/INBOX", []message.Message{invalid})

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
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if cmd == nil || m.interaction.folderCursor != 1 || m.loadedFolders.hasSnapshot("/network/Other") {
		t.Fatalf("folder navigation blocked or eagerly loaded: %#v", m)
	}
}

func TestFolderNavigationSelectsFirstPathInLoadedFolder(t *testing.T) {
	m := Model{
		folders: []maildir.Folder{
			{Path: "/mail/INBOX"},
			{Path: "/mail/Other"},
		},
		reads: &stubReader{},
	}
	setFolderMessages(&m, "/mail/INBOX", []message.Message{{Path: "/mail/INBOX/cur/a"}})
	setFolderMessages(&m, "/mail/Other", []message.Message{{Path: "/mail/Other/cur/first"}, {Path: "/mail/Other/cur/second"}})
	m.interaction.query = "old filter"
	m.interaction.selectedPath = "/mail/INBOX/cur/a"

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.interaction.folderCursor != 1 || m.interaction.query != "" || m.interaction.selectedPath != "/mail/Other/cur/first" {
		t.Fatalf("folder navigation did not select first path: %#v", m.interaction)
	}
}

func TestRefreshKeyForcesSelectedFolderReload(t *testing.T) {
	m := testModel()
	m.folders[0].Path = "/network/INBOX"
	setFolderMessages(&m, "/network/INBOX", []message.Message{
		(message.Message{Path: "/mail/cur/alice", Subject: "First message"}).MarkContentReady(),
	})

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(Model)
	if cmd == nil || m.refreshingFolder != "/network/INBOX" || m.interaction.selectedPath != "/mail/cur/alice" {
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
		folders: []maildir.Folder{{Path: "/network/INBOX"}},
		reads:   reads,
	}
	setFolderMessages(&messageModel, "/network/INBOX", []message.Message{summary})
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
		folders:       []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX"}},
		loadingFolder: "/network/INBOX",
		reads:         reads,
	}
	setFolderMessages(&m, "/network/INBOX", []message.Message{})
	first := readsession.FolderUpdate{Request: request, Messages: []message.Message{{Path: "/network/INBOX/cur/1", Subject: "First batch"}}}
	updated, _ := m.Update(first)
	m = updated.(Model)
	if len(m.loadedFolders.messages("/network/INBOX")) != 1 || m.loadingFolder == "" {
		t.Fatalf("first batch was not progressive: %#v", m)
	}
	updated, _ = m.Update(readsession.FolderUpdate{Request: request, Messages: first.Messages, Done: true})
	m = updated.(Model)
	if m.loadingFolder != "" {
		t.Fatalf("completed batch kept loading state: %#v", m)
	}
}

func TestProgressiveRefreshPreservesSelectedPathUntilFinalSnapshot(t *testing.T) {
	messageA := message.Message{Path: "/network/INBOX/cur/a", Subject: "A"}
	messageB := message.Message{Path: "/network/INBOX/cur/b", Subject: "B"}
	newer := message.Message{Path: "/network/INBOX/cur/new", Subject: "New"}
	reads := &stubReader{}
	request := reads.RequestFolder("/network/INBOX", true)
	m := Model{
		folders:          []maildir.Folder{{Path: "/network/INBOX"}},
		loadingFolder:    "/network/INBOX",
		refreshingFolder: "/network/INBOX",
		reads:            reads,
	}
	setFolderMessages(&m, "/network/INBOX", []message.Message{messageA, messageB})
	m.loadedFolders.begin("/network/INBOX", true)
	m.interaction.selectedPath = messageB.Path

	updated, _ := m.Update(readsession.FolderUpdate{Request: request, Messages: []message.Message{newer}})
	m = updated.(Model)
	if m.interaction.selectedPath != messageB.Path {
		t.Fatalf("partial refresh replaced selected path: %#v", m.interaction)
	}

	updated, _ = m.Update(readsession.FolderUpdate{
		Request: request, Messages: []message.Message{newer, messageA, messageB}, Done: true,
	})
	m = updated.(Model)
	if m.interaction.selectedPath != messageB.Path || m.messageProjection().SelectedPosition() != 2 {
		t.Fatalf("completed refresh moved selection: projection=%#v interaction=%#v", m.messageProjection(), m.interaction)
	}
}

func TestRefreshCompletionKeepsReadErrorVisible(t *testing.T) {
	reads := &stubReader{}
	request := reads.RequestFolder("/network/INBOX", true)
	m := Model{
		folders:          []maildir.Folder{{Path: "/network/INBOX"}},
		loadingFolder:    "/network/INBOX",
		refreshingFolder: "/network/INBOX",
		reads:            reads,
	}
	setFolderMessages(&m, "/network/INBOX", []message.Message{})
	m.loadedFolders.begin("/network/INBOX", true)
	batch := readsession.FolderUpdate{Request: request, Err: errors.New("permission denied"), HadReadErrors: true, Done: true}
	updated, _ := m.Update(batch)
	m = updated.(Model)
	if m.refreshingFolder != "" || !strings.Contains(m.status, "some messages could not be read") {
		t.Fatalf("refresh error was hidden: %#v", m)
	}
}

func TestRefreshFailureRestoresLastGoodSnapshot(t *testing.T) {
	previous := []message.Message{{Path: "/network/INBOX/cur/previous", Subject: "Previous"}}
	reads := &stubReader{}
	request := reads.RequestFolder("/network/INBOX", true)
	m := Model{
		folders:          []maildir.Folder{{Path: "/network/INBOX"}},
		loadingFolder:    "/network/INBOX",
		refreshingFolder: "/network/INBOX",
		reads:            reads,
	}
	setFolderMessages(&m, "/network/INBOX", previous)
	m.loadedFolders.begin("/network/INBOX", true)
	m.replaceFolderMessages("/network/INBOX", []message.Message{{Path: "/network/INBOX/cur/partial", Subject: "Partial"}})

	updated, _ := m.Update(readsession.FolderUpdate{Request: request, Fatal: true, Err: errors.New("folder unavailable")})
	m = updated.(Model)
	if got := m.loadedFolders.messages("/network/INBOX"); !reflect.DeepEqual(got, previous) {
		t.Fatalf("refresh failure did not restore the last good snapshot: %#v", got)
	}
	if m.loadedFolders.phase("/network/INBOX") != folderLoaded || m.loadingFolder != "" || m.refreshingFolder != "" || m.status != "Could not refresh the folder" {
		t.Fatalf("refresh failure state = %#v", m)
	}
}

func TestAttachmentPickerIsDiscoverable(t *testing.T) {
	m := testModel()
	mutateFolderMessages(&m, 0, func(messages []message.Message) {
		messages[0].Path = "/mail/cur/1"
		messages[0].Attachments = []message.Attachment{{Name: "invoice.pdf", MediaType: "application/pdf", Size: 4096}}
	})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = updated.(Model)
	if m.interaction.mode != attachmentsMode || !strings.Contains(m.View().Content, "invoice.pdf") {
		t.Fatalf("attachment picker did not open: %#v", m)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || m.interaction.mode != navigationMode || !m.openingAttachment {
		t.Fatalf("attachment open was not scheduled: %#v", m)
	}
}

func TestAttachmentOpenFailureKeepsMaterializedPathVisible(t *testing.T) {
	m := testModel()
	m.openingAttachment = true
	updated, _ := m.Update(attachmentOpened{
		result: attachment.OpenResult{Path: "/cache/mailtui/attachments/invoice.pdf"},
		err:    errors.New("xdg-open is not installed"),
	})
	m = updated.(Model)
	if m.openingAttachment || !strings.Contains(m.status, "/cache/mailtui/attachments/invoice.pdf") || !strings.Contains(m.status, "could not open") {
		t.Fatalf("partial attachment-open failure was not actionable: %#v", m)
	}
}

func TestAsyncFolderReplacementReconcilesAttachmentPicker(t *testing.T) {
	m := testModel()
	m.folders[0].Path = "/mail/INBOX"
	messages := m.loadedFolders.messages("/mail/INBOX")
	messages[0].Path = "/mail/INBOX/cur/1"
	messages[0].Attachments = []message.Attachment{{Name: "invoice.pdf", MediaType: "application/pdf"}}
	setFolderMessages(&m, "/mail/INBOX", messages)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = updated.(Model)
	m.interaction.readerScroll = 8

	request := (&stubReader{}).RequestFolder("/mail/INBOX", true)
	replacement := []message.Message{{Path: "/mail/INBOX/cur/2", Subject: "Replacement"}}
	updated, _ = m.Update(readsession.FolderUpdate{Request: request, Messages: replacement})
	m = updated.(Model)
	if m.interaction.mode != navigationMode || m.interaction.attachmentCursor != 0 || m.interaction.selectedPath != replacement[0].Path || m.interaction.readerScroll != 0 {
		t.Fatalf("folder replacement left stale interaction state: %#v", m.interaction)
	}
}

func TestProgressiveReplacementPreservesSelectedMessagePath(t *testing.T) {
	messageA := (message.Message{Path: "/mail/cur/a", Subject: "A"}).MarkContentReady()
	messageB := (message.Message{Path: "/mail/cur/b", Subject: "B"}).MarkContentReady()
	newer := (message.Message{Path: "/mail/cur/new", Subject: "New"}).MarkContentReady()
	m := Model{
		folders: []maildir.Folder{{Path: "/mail"}},
		width:   130, height: 32, reads: &stubReader{},
	}
	setFolderMessages(&m, "/mail", []message.Message{messageA, messageB})
	m.interaction.selectedPath = messageB.Path
	m.reconcileInteraction()
	if selected := m.selectedMessage(); selected == nil || selected.Path != messageB.Path {
		t.Fatalf("initial selection = %#v", selected)
	}

	m.replaceFolderMessages("/mail", []message.Message{newer, messageA, messageB})
	if selected := m.selectedMessage(); selected == nil || selected.Path != messageB.Path || m.messageProjection().SelectedPosition() != 2 {
		t.Fatalf("replacement moved selection: selected=%#v interaction=%#v", selected, m.interaction)
	}
}

func TestSearchCancelPreservesSelectedPathAcrossReplacement(t *testing.T) {
	messageA := (message.Message{Path: "/mail/cur/a", Subject: "Alpha"}).MarkContentReady()
	messageB := (message.Message{Path: "/mail/cur/b", Subject: "Beta"}).MarkContentReady()
	newer := (message.Message{Path: "/mail/cur/new", Subject: "Newest"}).MarkContentReady()
	m := Model{
		folders: []maildir.Folder{{Path: "/mail"}},
		width:   130, height: 32, reads: &stubReader{}, documents: newReaderDocuments(),
	}
	setFolderMessages(&m, "/mail", []message.Message{messageA, messageB})
	m.interaction.focus = readerPane
	m.interaction.selectedPath = messageB.Path
	m.reconcileInteraction()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	m = updated.(Model)
	m.replaceFolderMessages("/mail", []message.Message{newer, messageA, messageB})
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(Model)
	if selected := m.selectedMessage(); selected == nil || selected.Path != messageB.Path || m.messageProjection().SelectedPosition() != 2 {
		t.Fatalf("search cancel moved selection after replacement: selected=%#v interaction=%#v", selected, m.interaction)
	}
}

func TestHiddenReaderInteractionDoesNotBuildDocumentCache(t *testing.T) {
	m := testModel()
	m.width = 60
	m.interaction.focus = foldersPane
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	if m.interaction.focus != messagesPane || m.documents.Len() != 0 {
		t.Fatalf("hidden reader built a document: focus=%v cache=%d", m.interaction.focus, m.documents.Len())
	}
}

func TestSamePathHydrationInvalidatesReaderDocument(t *testing.T) {
	path := "/mail/cur/1"
	oldMessage := (message.Message{Path: path, Subject: "Subject", Body: "old body"}).MarkContentReady()
	m := Model{
		folders: []maildir.Folder{{Path: "/mail"}},
		width:   130, height: 32, reads: &stubReader{}, documents: newReaderDocuments(),
	}
	setFolderMessages(&m, "/mail", []message.Message{oldMessage})
	if view := m.View().Content; !strings.Contains(view, "old body") {
		t.Fatalf("old body was not rendered:\n%s", view)
	}
	newMessage := (message.Message{Path: path, Subject: "Subject", Body: "new body\nwith another line"}).MarkContentReady()
	request := (&stubReader{}).RequestMessage(message.Message{Path: path})
	updated, _ := m.Update(readsession.MessageUpdate{Request: request, Message: newMessage})
	m = updated.(Model)
	view := m.View().Content
	if !strings.Contains(view, "new body") || strings.Contains(view, "old body") {
		t.Fatalf("same-path hydration reused stale document:\n%s", view)
	}
}

func testModel() Model {
	m := Model{
		root: "/backup/mail", folders: []maildir.Folder{{Path: "/mail/INBOX", Name: "INBOX"}}, width: 130, height: 32, reads: &stubReader{},
		documents: newReaderDocuments(),
	}
	setFolderMessages(&m, "/mail/INBOX", []message.Message{
		(message.Message{Path: "/mail/cur/alice", From: "Alice <alice@example.com>", To: "me@example.com", Subject: "First message", Body: "Alice's message body", Date: time.Date(2026, 8, 2, 14, 0, 0, 0, time.Local)}).MarkContentReady(),
		(message.Message{Path: "/mail/cur/bank", From: "Bank <bank@example.com>", To: "billing@example.com", Subject: "Invoice available", Body: "Your invoice has arrived.", Date: time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local)}).MarkContentReady(),
	})
	return m
}

func setFolderMessages(model *Model, path string, messages []message.Message) {
	model.loadedFolders.replace(path, messages)
	model.loadedFolders.complete(path)
}

func mutateFolderMessages(model *Model, folderIndex int, mutate func([]message.Message)) {
	path := model.folders[folderIndex].Path
	messages := model.loadedFolders.messages(path)
	mutate(messages)
	setFolderMessages(model, path, messages)
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
