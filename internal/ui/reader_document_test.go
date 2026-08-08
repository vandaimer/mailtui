package ui

import (
	"errors"
	"fmt"
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"mailtui/internal/message"
)

func TestReaderDocumentComposesCompleteMessage(t *testing.T) {
	item := &message.Message{
		Path: "/mail/cur/1", Subject: "Quarterly report",
		From: "Alice <alice@example.com>", To: "Bob <bob@example.com>",
		Cc: "Carol <carol@example.com>", Bcc: "Archive <archive@example.com>",
		DateText: "8 Aug 2026", MessageID: "<report@example.com>", Body: "Plain body",
		Attachments: []message.Attachment{{Name: "report.pdf", MediaType: "application/pdf", Size: 2048}},
	}
	document := buildReaderDocument(item, 72, true)
	rendered := strings.Join(document.lines, "\n")
	for _, expected := range []string{
		"Quarterly report", "Alice", "Bob", "Carol", "Archive", "8 Aug 2026",
		"<report@example.com>", "report.pdf", "application/pdf", "2.0 KB", "Plain body",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("reader document missing %q:\n%s", expected, rendered)
		}
	}
	if document.mode != readerDocumentPlain {
		t.Fatalf("mode = %s", document.mode.Label())
	}
}

func TestReaderDocumentReportsActualRichAndFallbackModes(t *testing.T) {
	item := &message.Message{RichBody: "# Rich heading", Body: "Plain fallback"}
	rich := buildReaderDocument(item, 40, false)
	richOutput := strings.Join(rich.lines, "\n")
	if rich.mode != readerDocumentRich || !strings.Contains(richOutput, "Rich") || !strings.Contains(richOutput, "heading") {
		t.Fatalf("rich document = %#v", rich)
	}

	fallback := buildReaderDocumentWithMarkdown(item, 40, false, func(string, int) ([]string, error) {
		return nil, errors.New("renderer unavailable")
	})
	if fallback.mode != readerDocumentPlainFallback || fallback.mode.Label() != "PLAIN FALLBACK" ||
		!strings.Contains(strings.Join(fallback.lines, "\n"), "Plain fallback") {
		t.Fatalf("fallback document = %#v", fallback)
	}

	plain := buildReaderDocumentWithMarkdown(item, 40, true, func(string, int) ([]string, error) {
		t.Fatal("plain preference invoked Markdown rendering")
		return nil, nil
	})
	if plain.mode != readerDocumentPlain {
		t.Fatalf("plain mode = %s", plain.mode.Label())
	}
}

func TestReaderDocumentViewportClampsAndMeasuresOnce(t *testing.T) {
	document := readerDocument{lines: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}}
	if maximum := document.MaxScroll(3); maximum != 7 {
		t.Fatalf("maximum = %d", maximum)
	}
	viewport := document.Viewport(99, 3)
	if viewport.offset != 7 || viewport.maxScroll != 7 || viewport.progress != 100 || strings.Join(viewport.lines, "") != "789" {
		t.Fatalf("bottom viewport = %#v", viewport)
	}
	viewport = document.Viewport(-5, 3)
	if viewport.offset != 0 || viewport.progress != 0 || strings.Join(viewport.lines, "") != "012" {
		t.Fatalf("top viewport = %#v", viewport)
	}
}

func TestReaderDocumentRespectsTerminalCellWidthForUnicode(t *testing.T) {
	item := &message.Message{
		Subject:   "重要なお知らせ 📬 with a long subject",
		From:      "送信者 <sender@example.com>",
		To:        "受信者 <recipient@example.com>",
		MessageID: "<unicode@example.com>",
		RichBody:  "# 本文には日本語と emoji 🚀🚀🚀\n\naverylongunbrokenwordthatmustwrap safely.",
		Body:      "Plain fallback",
	}
	for _, width := range []int{8, 34} {
		document := buildReaderDocument(item, width, false)
		for index, line := range document.lines {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d, line %d = %d cells: %q", width, index, got, line)
			}
		}
	}
}

func TestReaderDocumentCacheKeepsWidthAndModeVariants(t *testing.T) {
	documents := newReaderDocuments()
	item := &message.Message{Path: "/mail/cur/1", Body: "A body that wraps differently at different widths"}
	documents.Document(item, 30, false)
	documents.Document(item, 60, false)
	documents.Document(item, 60, true)
	documents.Document(item, 30, false)
	if documents.Len() != 3 {
		t.Fatalf("cache entries = %d, want 3", documents.Len())
	}
	documents.Invalidate(item.Path)
	if documents.Len() != 0 {
		t.Fatalf("invalidation left %d variants", documents.Len())
	}
}

func TestReaderDocumentCacheEvictsLeastRecentlyUsedVariant(t *testing.T) {
	documents := newReaderDocuments()
	for index := range readerDocumentCacheLimit {
		item := &message.Message{Path: fmt.Sprintf("/mail/cur/%02d", index), Body: "body"}
		documents.Document(item, 40, false)
	}
	first := readerDocumentKey{path: "/mail/cur/00", width: 40}
	second := readerDocumentKey{path: "/mail/cur/01", width: 40}
	documents.Document(&message.Message{Path: first.path, Body: "body"}, 40, false)
	documents.Document(&message.Message{Path: "/mail/cur/32", Body: "body"}, 40, false)

	if documents.Len() != readerDocumentCacheLimit {
		t.Fatalf("cache entries = %d", documents.Len())
	}
	if _, found := documents.state.entries[first]; !found {
		t.Fatal("recently used document was evicted")
	}
	if _, found := documents.state.entries[second]; found {
		t.Fatal("least recently used document was retained")
	}
}

func TestRenderMarkdownProducesStyledWidthAwareOutput(t *testing.T) {
	lines, err := renderMarkdown("# Welcome\n\nHello **world**.\n\n- First\n- Second", 34)
	if err != nil {
		t.Fatal(err)
	}
	rendered := strings.Join(lines, "\n")
	for _, expected := range []string{"Welcome", "Hello", "world", "First", "Second", "\x1b["} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered markdown missing %q:\n%s", expected, rendered)
		}
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 34 {
			t.Fatalf("line width = %d, want <= 34: %q", width, line)
		}
	}
}

func TestRenderImagePreviewUsesPortableTrueColorCells(t *testing.T) {
	preview := message.ImagePreview{
		Width: 2, Height: 2,
		Pixels: []color.NRGBA{
			{R: 255, A: 255}, {G: 255, A: 255},
			{B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255},
		},
	}
	lines := renderImagePreview(preview, 10)
	if len(lines) != 1 || lipgloss.Width(lines[0]) != 2 {
		t.Fatalf("unexpected image preview: %#v", lines)
	}
	if !strings.Contains(lines[0], "\x1b[38;2;255;0;0m") || !strings.Contains(lines[0], "▀") {
		t.Fatalf("preview does not contain expected true-color cells: %q", lines[0])
	}
}
