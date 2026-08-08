package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"

	"mailtui/internal/message"
)

const readerDocumentCacheLimit = 32

type readerDocumentMode uint8

const (
	readerDocumentPlain readerDocumentMode = iota
	readerDocumentRich
	readerDocumentPlainFallback
)

func (mode readerDocumentMode) Label() string {
	switch mode {
	case readerDocumentRich:
		return "RICH"
	case readerDocumentPlainFallback:
		return "PLAIN FALLBACK"
	default:
		return "PLAIN"
	}
}

type readerDocument struct {
	lines []string
	mode  readerDocumentMode
}

func (document readerDocument) MaxScroll(viewportRows int) int {
	return max(0, len(document.lines)-max(1, viewportRows))
}

func (document readerDocument) Viewport(offset, rows int) readerDocumentViewport {
	rows = max(1, rows)
	maximum := document.MaxScroll(rows)
	offset = clamp(offset, 0, maximum)
	end := min(len(document.lines), offset+rows)
	progress := 0
	if maximum > 0 {
		progress = offset * 100 / maximum
	}
	return readerDocumentViewport{
		lines: document.lines[offset:end], offset: offset,
		maxScroll: maximum, progress: progress,
	}
}

type readerDocumentViewport struct {
	lines     []string
	offset    int
	maxScroll int
	progress  int
}

type readerDocumentKey struct {
	path           string
	width          int
	requestedPlain bool
}

type readerDocuments struct {
	state *readerDocumentCache
}

type readerDocumentCache struct {
	entries map[readerDocumentKey]readerDocumentCacheEntry
	clock   uint64
}

type readerDocumentCacheEntry struct {
	document readerDocument
	used     uint64
}

func newReaderDocuments() readerDocuments {
	return readerDocuments{state: &readerDocumentCache{entries: make(map[readerDocumentKey]readerDocumentCacheEntry)}}
}

func (documents *readerDocuments) Document(item *message.Message, width int, preferPlain bool) readerDocument {
	if item == nil {
		return readerDocument{mode: readerDocumentPlain}
	}
	width = max(1, width)
	key := readerDocumentKey{path: item.Path, width: width, requestedPlain: preferPlain}
	if item.Path != "" {
		if cached, found := documents.cached(key); found {
			return cached
		}
	}
	document := buildReaderDocument(item, width, preferPlain)
	if item.Path != "" {
		documents.store(key, document)
	}
	return document
}

func (documents *readerDocuments) Invalidate(path string) {
	if documents == nil || documents.state == nil || path == "" {
		return
	}
	for key := range documents.state.entries {
		if key.path == path {
			delete(documents.state.entries, key)
		}
	}
}

func (documents *readerDocuments) Len() int {
	if documents == nil || documents.state == nil {
		return 0
	}
	return len(documents.state.entries)
}

func (documents *readerDocuments) cached(key readerDocumentKey) (readerDocument, bool) {
	if documents == nil || documents.state == nil {
		return readerDocument{}, false
	}
	entry, found := documents.state.entries[key]
	if !found {
		return readerDocument{}, false
	}
	documents.state.clock++
	entry.used = documents.state.clock
	documents.state.entries[key] = entry
	return entry.document, true
}

func (documents *readerDocuments) store(key readerDocumentKey, document readerDocument) {
	if documents == nil {
		return
	}
	if documents.state == nil {
		documents.state = &readerDocumentCache{entries: make(map[readerDocumentKey]readerDocumentCacheEntry)}
	}
	if _, found := documents.state.entries[key]; !found && len(documents.state.entries) >= readerDocumentCacheLimit {
		documents.evictOldest()
	}
	documents.state.clock++
	documents.state.entries[key] = readerDocumentCacheEntry{document: document, used: documents.state.clock}
}

func (documents *readerDocuments) evictOldest() {
	var oldest readerDocumentKey
	var oldestUse uint64
	found := false
	for key, entry := range documents.state.entries {
		if !found || entry.used < oldestUse {
			oldest, oldestUse, found = key, entry.used, true
		}
	}
	if found {
		delete(documents.state.entries, oldest)
	}
}

func buildReaderDocument(item *message.Message, width int, preferPlain bool) readerDocument {
	return buildReaderDocumentWithMarkdown(item, width, preferPlain, renderMarkdown)
}

func buildReaderDocumentWithMarkdown(item *message.Message, width int, preferPlain bool, markdown func(string, int) ([]string, error)) readerDocument {
	lines := []string{titleStyle.Render(truncate(empty(item.Subject, "(no subject)"), width))}
	lines = append(lines, labelValue("From", item.From, width)...)
	lines = append(lines, labelValue("To", item.To, width)...)
	if item.Cc != "" {
		lines = append(lines, labelValue("Cc", item.Cc, width)...)
	}
	if item.Bcc != "" {
		lines = append(lines, labelValue("Bcc", item.Bcc, width)...)
	}
	lines = append(lines, labelValue("Date", item.DateText, width)...)
	if item.MessageID != "" {
		lines = append(lines, labelValue("Message-ID", item.MessageID, width)...)
	}
	if len(item.Attachments) > 0 {
		lines = append(lines, "", accentStyle.Render(fmt.Sprintf("▣ %d attachment(s)", len(item.Attachments))))
		for _, attachment := range item.Attachments {
			lines = append(lines, truncate(fmt.Sprintf("  %s · %s · %s", attachment.Name, attachment.MediaType, formatBytes(attachment.Size)), width))
		}
	}
	lines = append(lines, "", mutedStyle.Render(strings.Repeat("─", width)), "")
	body, mode := renderReaderBody(item, width, preferPlain, markdown)
	lines = append(lines, body...)
	return readerDocument{lines: hardwrapReaderLines(lines, width), mode: mode}
}

func renderReaderBody(item *message.Message, width int, preferPlain bool, markdown func(string, int) ([]string, error)) ([]string, readerDocumentMode) {
	mode := readerDocumentPlain
	var lines []string
	if !preferPlain && strings.TrimSpace(item.RichBody) != "" {
		rendered, err := markdown(item.RichBody, width)
		if err == nil && len(rendered) > 0 {
			lines = rendered
			mode = readerDocumentRich
		} else {
			mode = readerDocumentPlainFallback
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
	return lines, mode
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
	return hardwrapReaderLines(strings.Split(rendered, "\n"), width), nil
}

func hardwrapReaderLines(lines []string, width int) []string {
	width = max(1, width)
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
	}
	return wrapped
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
