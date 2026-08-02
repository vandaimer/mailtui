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

func testModel() Model {
	folders := []maildir.Folder{{
		Name: "INBOX",
		Messages: []message.Message{
			{From: "Alice <alice@example.com>", To: "me@example.com", Subject: "Primeira mensagem", Body: "Corpo da Alice", Date: time.Date(2026, 8, 2, 14, 0, 0, 0, time.Local)},
			{From: "Banco <bank@example.com>", To: "billing@example.com", Subject: "Fatura disponível", Body: "A sua fatura chegou.", Date: time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local)},
		},
	}}
	return Model{root: "/backup/mail", folders: folders, width: 130, height: 32}
}
