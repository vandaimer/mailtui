package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"

	"mailtui/internal/message"
)

func renderMessageContent(item *message.Message, width int, plain bool) []string {
	var lines []string
	if !plain && strings.TrimSpace(item.RichBody) != "" {
		if rendered, err := renderMarkdown(item.RichBody, width); err == nil && len(rendered) > 0 {
			lines = rendered
		}
	}
	if len(lines) == 0 {
		lines = wrap(item.Body, width)
	}

	if len(item.Images) > 0 {
		lines = append(lines, "", accentStyle.Render(fmt.Sprintf("▧ %d local image preview(s)", len(item.Images))))
		for _, preview := range item.Images {
			lines = append(lines, softStyle.Render(truncate(preview.Name, width)))
			lines = append(lines, renderImagePreview(preview, width)...)
		}
	}
	return lines
}

func renderMarkdown(value string, width int) ([]string, error) {
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styles.TokyoNightStyle),
		glamour.WithWordWrap(max(20, width)),
		glamour.WithTableWrap(true),
		glamour.WithInlineTableLinks(true),
		glamour.WithEmoji(),
	)
	if err != nil {
		return nil, err
	}
	rendered, err := renderer.Render(value)
	if err != nil {
		return nil, err
	}
	rendered = strings.Trim(rendered, "\n")
	if rendered == "" {
		return nil, nil
	}
	return strings.Split(rendered, "\n"), nil
}

func renderImagePreview(preview message.ImagePreview, maxWidth int) []string {
	if preview.Width <= 0 || preview.Height <= 0 || len(preview.Pixels) < preview.Width*preview.Height || maxWidth <= 0 {
		return nil
	}
	targetWidth := min(preview.Width, maxWidth)
	targetHeight := max(1, preview.Height*targetWidth/preview.Width)
	lines := make([]string, 0, (targetHeight+1)/2)
	background := color.NRGBA{R: 15, G: 23, B: 42, A: 255}
	for y := 0; y < targetHeight; y += 2 {
		var line strings.Builder
		line.Grow(targetWidth * 32)
		for x := range targetWidth {
			top := previewPixel(preview, x, y, targetWidth, targetHeight)
			bottom := background
			if y+1 < targetHeight {
				bottom = previewPixel(preview, x, y+1, targetWidth, targetHeight)
			}
			top = composite(top, background)
			bottom = composite(bottom, background)
			fmt.Fprintf(&line, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀",
				top.R, top.G, top.B, bottom.R, bottom.G, bottom.B)
		}
		line.WriteString("\x1b[0m")
		lines = append(lines, line.String())
	}
	return lines
}

func previewPixel(preview message.ImagePreview, x, y, targetWidth, targetHeight int) color.NRGBA {
	sourceX := min(preview.Width-1, x*preview.Width/targetWidth)
	sourceY := min(preview.Height-1, y*preview.Height/targetHeight)
	return preview.Pixels[sourceY*preview.Width+sourceX]
}

func composite(foreground, background color.NRGBA) color.NRGBA {
	if foreground.A == 255 {
		return foreground
	}
	alpha := uint32(foreground.A)
	inverse := 255 - alpha
	return color.NRGBA{
		R: uint8((uint32(foreground.R)*alpha + uint32(background.R)*inverse) / 255),
		G: uint8((uint32(foreground.G)*alpha + uint32(background.G)*inverse) / 255),
		B: uint8((uint32(foreground.B)*alpha + uint32(background.B)*inverse) / 255),
		A: 255,
	}
}
