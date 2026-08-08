package ui

import (
	"testing"

	"mailtui/internal/message"
)

func TestMessageProjectionFiltersHeadersWithoutChangingSourceOrder(t *testing.T) {
	messages := []message.Message{
		{Path: "/mail/c", From: "Carol <carol@example.com>", Subject: "Third"},
		{Path: "/mail/a", To: "billing@example.com", Subject: "First"},
		{Path: "/mail/b", Cc: "Alice <alice@example.com>", Subject: "Second"},
	}

	projection := projectMessages(messages, "EXAMPLE.COM", "/mail/b")
	if projection.Len() != 3 || projection.SelectedPosition() != 2 || projection.SelectedPath() != "/mail/b" {
		t.Fatalf("projection = %#v", projection)
	}
	for position, path := range []string{"/mail/c", "/mail/a", "/mail/b"} {
		if got := projection.Message(position); got == nil || got.Path != path {
			t.Fatalf("message %d = %#v, want %s", position, got, path)
		}
	}

	projection = projectMessages(messages, "billing", "/mail/b")
	if projection.Len() != 1 || projection.SelectedPath() != "/mail/a" {
		t.Fatalf("recipient projection = %#v", projection)
	}
}

func TestMessageProjectionPreservesPathAcrossReorderAndHydration(t *testing.T) {
	selectedPath := "/mail/b"
	initial := []message.Message{{Path: "/mail/a"}, {Path: selectedPath, Subject: "Header"}}
	projection := projectMessages(initial, "", selectedPath)
	if projection.SelectedPosition() != 1 {
		t.Fatalf("initial position = %d", projection.SelectedPosition())
	}

	replacement := []message.Message{
		{Path: "/mail/new"},
		{Path: selectedPath, Subject: "Hydrated body"},
		{Path: "/mail/a"},
	}
	projection = projectMessages(replacement, "", selectedPath)
	if projection.SelectedPosition() != 1 || projection.Selected() == nil || projection.Selected().Subject != "Hydrated body" {
		t.Fatalf("replacement projection = %#v", projection)
	}
}

func TestMessageProjectionFallsBackToFirstVisibleMessage(t *testing.T) {
	messages := []message.Message{
		{Path: "/mail/a", Subject: "Alpha"},
		{Path: "/mail/b", Subject: "Beta"},
	}
	projection := projectMessages(messages, "beta", "/mail/a")
	if projection.SelectedPath() != "/mail/b" || projection.SelectedPosition() != 0 {
		t.Fatalf("fallback projection = %#v", projection)
	}

	empty := projectMessages(messages, "missing", "/mail/a")
	if empty.Len() != 0 || empty.Selected() != nil || empty.SelectedPath() != "" || empty.SelectedPosition() != -1 {
		t.Fatalf("empty projection = %#v", empty)
	}
}

func TestMessageProjectionNavigationClampsAtEdges(t *testing.T) {
	messages := []message.Message{{Path: "/mail/a"}, {Path: "/mail/c"}, {Path: "/mail/d"}}
	projection := projectMessages(messages, "", "/mail/d")
	if got := projection.Move(1); got != "/mail/d" {
		t.Fatalf("next after last = %q", got)
	}
	if got := projection.Move(-1); got != "/mail/c" {
		t.Fatalf("previous before last = %q", got)
	}
	if got := projection.Boundary(false); got != "/mail/a" {
		t.Fatalf("first = %q", got)
	}
	if got := projection.Boundary(true); got != "/mail/d" {
		t.Fatalf("last = %q", got)
	}

	first := projectMessages(messages, "", "/mail/a")
	if got := first.Move(-1); got != "/mail/a" {
		t.Fatalf("previous before first = %q", got)
	}
	empty := projectMessages(nil, "", "")
	if empty.Move(1) != "" || empty.Boundary(true) != "" {
		t.Fatal("empty projection returned a navigation target")
	}
}
