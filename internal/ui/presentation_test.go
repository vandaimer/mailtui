package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"mailtui/internal/message"
)

func TestPresentationRendersVisibleStatesFromFacts(t *testing.T) {
	ready := (message.Message{Path: "/mail/cur/1", Subject: "Hello", From: "Alice", Body: "Body"}).MarkContentReady()
	invalid := (message.Message{Path: "/mail/cur/broken", Subject: "Broken"}).MarkHeaderInvalid(errors.New("malformed header"))
	unavailable := (message.Message{Path: "/mail/cur/unavailable", Subject: "Unavailable"}).MarkContentUnavailable(errors.New("permission denied"))
	tests := []struct {
		name     string
		facts    presentationFacts
		want     []string
		dontWant []string
	}{
		{
			name:  "loading",
			facts: presentationTestFacts(false, nil),
			want:  []string{"Loading messages", "Preparing folder"},
		},
		{
			name:  "empty folder",
			facts: presentationTestFacts(true, nil),
			want:  []string{"This folder is empty", "No message selected"},
		},
		{
			name: "search has no results",
			facts: func() presentationFacts {
				facts := presentationTestFacts(true, nil)
				facts.query = "missing"
				facts.mode = searchMode
				return facts
			}(),
			want: []string{"No results", "0 result(s)"},
		},
		{
			name:     "invalid message",
			facts:    presentationTestFacts(true, &invalid),
			want:     []string{"Invalid message", "malformed header"},
			dontWant: []string{"Loading content"},
		},
		{
			name:  "unavailable message",
			facts: presentationTestFacts(true, &unavailable),
			want:  []string{"Could not load message content", "permission denied"},
		},
		{
			name: "attachment picker",
			facts: func() presentationFacts {
				item := ready
				item.Attachments = []message.Attachment{{Name: "invoice.pdf", MediaType: "application/pdf", Size: 2048}}
				facts := presentationTestFacts(true, &item)
				facts.mode = attachmentsMode
				return facts
			}(),
			want: []string{"ATTACHMENTS", "invoice.pdf", "Enter open"},
		},
		{
			name: "ready reader",
			facts: func() presentationFacts {
				facts := presentationTestFacts(true, &ready)
				facts.reader = readerPresentation{ready: true, mode: "PLAIN", viewport: readerDocumentViewport{lines: []string{"Hello", "Body"}}}
				return facts
			}(),
			want: []string{"Hello", "Body", "PLAIN"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := renderPresentation(test.facts)
			for _, expected := range test.want {
				if !strings.Contains(output, expected) {
					t.Fatalf("output missing %q:\n%s", expected, output)
				}
			}
			for _, unexpected := range test.dontWant {
				if strings.Contains(output, unexpected) {
					t.Fatalf("output unexpectedly contains %q:\n%s", unexpected, output)
				}
			}
		})
	}
}

func TestPresentationFillsPlannedTerminalDimensions(t *testing.T) {
	for _, size := range [][2]int{{42, 10}, {72, 20}, {130, 32}} {
		facts := presentationTestFacts(true, nil)
		facts.width, facts.height = size[0], size[1]
		facts.layout = calculateLayout(size[0], size[1])
		output := renderPresentation(facts)
		if got := lipgloss.Width(output); got != size[0] {
			t.Errorf("%dx%d width = %d", size[0], size[1], got)
		}
		if got := lipgloss.Height(output); got != size[1] {
			t.Errorf("%dx%d height = %d", size[0], size[1], got)
		}
	}
}

func TestPresentationDoesNotMutateFacts(t *testing.T) {
	item := (message.Message{Path: "/mail/cur/1", Subject: "Hello", Body: "Body"}).MarkContentReady()
	facts := presentationTestFacts(true, &item)
	facts.reader = readerPresentation{ready: true, mode: "PLAIN", viewport: readerDocumentViewport{lines: []string{"Body"}}}
	original := facts
	_ = renderPresentation(facts)
	if !reflect.DeepEqual(facts, original) {
		t.Fatalf("presentation mutated its input facts: before=%#v after=%#v", original, facts)
	}
}

func presentationTestFacts(loaded bool, item *message.Message) presentationFacts {
	var messages []message.Message
	if item != nil {
		messages = []message.Message{*item}
	}
	projection := projectMessages(messages, "", "")
	return presentationFacts{
		width: 130, height: 32, root: "/backup", layout: calculateLayout(130, 32),
		folders:      []presentationFolder{{name: "INBOX", path: "/mail/INBOX", count: len(messages), loaded: loaded}},
		selectedName: "INBOX", selectedCount: len(messages), folderCursor: 0,
		projection: projection, selectedFolderReady: loaded, selectedFolderPath: "/mail/INBOX",
		focus: readerPane, mode: navigationMode, selectedMessage: projection.Selected(), spinnerFrame: 0,
	}
}
