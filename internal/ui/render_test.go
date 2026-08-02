package ui

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"mailtui/internal/message"
)

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
