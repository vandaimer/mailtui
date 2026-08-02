package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mailtui/internal/maildir"
	"mailtui/internal/message"
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
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(Model)
	if !m.searching || m.focus != messagesPane {
		t.Fatalf("search not activated: %#v", m)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("alice")})
	m = updated.(Model)
	if m.query != "alice" || len(m.filteredMessageIndexes()) != 1 {
		t.Fatalf("query = %q", m.query)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.searching || m.query != "" {
		t.Fatalf("search was not cancelled: %#v", m)
	}
}

func TestResponsiveViews(t *testing.T) {
	m := testModel()
	m.width, m.height = 130, 32
	wide := m.View()
	if got := lipgloss.Width(wide); got != 130 {
		t.Fatalf("wide view width = %d", got)
	}
	for _, label := range []string{"PASTAS", "INBOX", "LEITURA", "Primeira mensagem", "Corpo da Alice"} {
		if !strings.Contains(wide, label) {
			t.Fatalf("wide view missing %q", label)
		}
	}

	m.width = 60
	narrow := m.View()
	if got := lipgloss.Width(narrow); got != 60 {
		t.Fatalf("narrow view width = %d", got)
	}
	if !strings.Contains(narrow, "PASTAS") || strings.Contains(narrow, "LEITURA") {
		t.Fatalf("unexpected narrow folder view")
	}
	m.focus = readerPane
	narrow = m.View()
	if !strings.Contains(narrow, "LEITURA") || !strings.Contains(narrow, "Corpo da Alice") {
		t.Fatalf("unexpected narrow reader view")
	}
}

func TestMessageSelectionUpdatesPreview(t *testing.T) {
	m := testModel()
	m.focus = messagesPane
	m.move(1)
	selected := m.selectedMessage()
	if selected == nil || selected.Subject != "Fatura disponível" || m.readerScroll != 0 {
		t.Fatalf("unexpected selection: %#v", selected)
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
	m := Model{
		root:    "/network",
		folders: []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX"}},
		width:   130,
		height:  32,
	}

	updated, cmd := m.Update(folderLoadRequest{path: "/network/INBOX"})
	m = updated.(Model)
	if cmd == nil || m.loadingFolder != "/network/INBOX" || m.folders[0].Messages != nil {
		t.Fatalf("folder scan did not start asynchronously: %#v", m)
	}

	summary := message.Message{Path: "/network/INBOX/cur/1", From: "Alice", Subject: "Header pronto"}
	updated, _ = m.Update(folderLoaded{path: "/network/INBOX", messages: []message.Message{summary}})
	m = updated.(Model)
	if m.loadingFolder != "" || len(m.folders[0].Messages) != 1 || m.folders[0].Messages[0].Loaded {
		t.Fatalf("unexpected header phase: %#v", m)
	}
	if !strings.Contains(m.View(), "Carregando conteúdo") {
		t.Fatal("reader does not expose the deferred body load")
	}

	updated, cmd = m.Update(messageLoadRequest{path: summary.Path})
	m = updated.(Model)
	if cmd == nil || m.loadingMessage != summary.Path {
		t.Fatalf("message load did not start asynchronously: %#v", m)
	}
	full := summary
	full.Body, full.Loaded = "Conteúdo chegou", true
	updated, _ = m.Update(messageLoaded{path: summary.Path, message: full})
	m = updated.(Model)
	if m.loadingMessage != "" || !strings.Contains(m.View(), "Conteúdo chegou") {
		t.Fatalf("message was not hydrated: %#v", m)
	}
}

func TestFolderNavigationIsDebounced(t *testing.T) {
	m := Model{folders: []maildir.Folder{{Path: "/network/INBOX"}, {Path: "/network/Other"}}}
	cmd := m.move(1)
	if cmd == nil || m.folderIndex != 1 || m.folders[1].Messages != nil {
		t.Fatalf("folder navigation blocked or eagerly loaded: %#v", m)
	}
}

func TestProgressiveFolderBatchesAppearBeforeCompletion(t *testing.T) {
	m := Model{
		folders:       []maildir.Folder{{Path: "/network/INBOX", Name: "INBOX", Messages: []message.Message{}}},
		loadingFolder: "/network/INBOX",
	}
	first := maildir.HeaderBatch{Messages: []message.Message{{Path: "/network/INBOX/cur/1", Subject: "First batch"}}}
	updated, _ := m.Update(folderBatchReceived{path: "/network/INBOX", batch: first})
	m = updated.(Model)
	if len(m.folders[0].Messages) != 1 || m.loadingFolder == "" {
		t.Fatalf("first batch was not progressive: %#v", m)
	}
	updated, _ = m.Update(folderBatchReceived{path: "/network/INBOX", batch: maildir.HeaderBatch{Done: true}})
	m = updated.(Model)
	if m.loadingFolder != "" {
		t.Fatalf("completed batch kept loading state: %#v", m)
	}
}

func TestAttachmentPickerIsDiscoverable(t *testing.T) {
	m := testModel()
	m.folders[0].Messages[0].Path = "/mail/cur/1"
	m.folders[0].Messages[0].Attachments = []message.Attachment{{Name: "invoice.pdf", MediaType: "application/pdf", Size: 4096}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(Model)
	if !m.attachmentPicker || !strings.Contains(m.View(), "invoice.pdf") {
		t.Fatalf("attachment picker did not open: %#v", m)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || m.attachmentPicker || !m.openingAttachment {
		t.Fatalf("attachment open was not scheduled: %#v", m)
	}
}

func testModel() Model {
	folders := []maildir.Folder{{
		Name: "INBOX",
		Messages: []message.Message{
			{From: "Alice <alice@example.com>", To: "me@example.com", Subject: "Primeira mensagem", Body: "Corpo da Alice", Date: time.Date(2026, 8, 2, 14, 0, 0, 0, time.Local), Loaded: true},
			{From: "Banco <bank@example.com>", To: "billing@example.com", Subject: "Fatura disponível", Body: "A sua fatura chegou.", Date: time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local), Loaded: true},
		},
	}}
	return Model{root: "/backup/mail", folders: folders, width: 130, height: 32}
}
