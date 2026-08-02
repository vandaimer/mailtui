// Package message parses local RFC 822/MIME message files without modifying
// them or their containing Maildir.
package message

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

type Attachment struct {
	Name      string
	MediaType string
	Size      int
	ContentID string
	Inline    bool
}

// ImagePreview is a small, decoded thumbnail kept for terminal rendering. It
// deliberately stores only a few pixels rather than retaining attachment data.
type ImagePreview struct {
	Name      string
	MediaType string
	ContentID string
	Width     int
	Height    int
	Pixels    []color.NRGBA
}

// LoadState describes the only valid stages of a message during a read
// session. The zero value is a successfully parsed header whose content has
// not been read yet.
type LoadState uint8

const (
	LoadHeaderOnly LoadState = iota
	LoadHeaderInvalid
	LoadContentReady
	LoadContentUnavailable
)

type Message struct {
	Path        string
	From        string
	To          string
	Cc          string
	Bcc         string
	Subject     string
	MessageID   string
	Date        time.Time
	DateText    string
	Body        string
	RichBody    string
	Attachments []Attachment
	Images      []ImagePreview
	loadState   LoadState
	loadErr     error
}

func (m Message) LoadState() LoadState { return m.loadState }

func (m Message) LoadError() error { return m.loadErr }

func (m Message) NeedsHydration() bool {
	return m.Path != "" && m.loadState == LoadHeaderOnly
}

// MarkHeaderInvalid records a header parse failure. It is a terminal state.
func (m Message) MarkHeaderInvalid(err error) Message {
	m.Body = ""
	m.RichBody = ""
	m.Attachments = nil
	m.Images = nil
	return m.transitionTo(LoadHeaderInvalid, err)
}

// MarkContentReady records a successful full-message hydration.
func (m Message) MarkContentReady() Message {
	return m.transitionTo(LoadContentReady, nil)
}

// MarkContentUnavailable preserves header metadata while recording a terminal
// hydration failure. No display fallback is stored as message content.
func (m Message) MarkContentUnavailable(err error) Message {
	m.Body = ""
	m.RichBody = ""
	m.Attachments = nil
	m.Images = nil
	return m.transitionTo(LoadContentUnavailable, err)
}

func (m Message) transitionTo(next LoadState, err error) Message {
	if m.loadState != LoadHeaderOnly {
		panic("message load state is already terminal")
	}
	switch next {
	case LoadContentReady:
		if err != nil {
			panic("ready message cannot contain a load error")
		}
	case LoadHeaderInvalid, LoadContentUnavailable:
		if err == nil {
			panic("message failure state requires an error")
		}
	default:
		panic("invalid terminal message load state")
	}
	m.loadState = next
	m.loadErr = err
	return m
}

// ParseHeaderFile reads only the RFC 822 headers. It deliberately leaves the
// body on the backing filesystem, which keeps folder scans cheap on network
// mounts. ParseFile can later hydrate the selected message.
func ParseHeaderFile(path string) (Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return Message{}, err
	}
	defer file.Close()
	raw, err := mail.ReadMessage(bufio.NewReaderSize(file, 16*1024))
	if err != nil {
		return Message{}, err
	}
	return fromHeaders(path, raw.Header), nil
}

func ParseFile(path string) (Message, error) {
	raw, root, err := readMIMEFile(path)
	if err != nil {
		return Message{}, err
	}

	m := fromHeaders(path, raw.Header)
	contents, err := traverseMIME(root, true, -1)
	if err != nil {
		return m, err
	}
	m.Attachments = contents.attachments
	m.Images = contents.texts.images
	if contents.texts.html != "" {
		if richBody, richErr := htmlToMarkdown(contents.texts.html); richErr == nil {
			m.RichBody = strings.TrimSpace(richBody)
		}
	}
	if contents.texts.plain != "" {
		m.Body = contents.texts.plain
	} else {
		m.Body = htmlToText(contents.texts.html)
	}
	if strings.TrimSpace(m.Body) == "" {
		m.Body = "[no text body]"
	}
	return m.MarkContentReady(), nil
}

// ExtractAttachment returns one decoded MIME attachment by the same order used
// in Message.Attachments. It performs no writes.
func ExtractAttachment(path string, target int) (Attachment, []byte, error) {
	if target < 0 {
		return Attachment{}, nil, errors.New("invalid attachment index")
	}
	_, root, err := readMIMEFile(path)
	if err != nil {
		return Attachment{}, nil, err
	}
	contents, err := traverseMIME(root, false, target)
	if err != nil {
		return Attachment{}, nil, err
	}
	if target >= len(contents.attachments) {
		return Attachment{}, nil, fmt.Errorf("attachment %d not found", target+1)
	}
	return contents.attachments[target], contents.targetPayload, nil
}

func readMIMEFile(path string) (*mail.Message, mimeEntity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, mimeEntity{}, err
	}
	raw, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return nil, mimeEntity{}, err
	}
	body, err := io.ReadAll(raw.Body)
	if err != nil {
		return raw, mimeEntity{}, err
	}
	body, err = decodeTransfer(body, raw.Header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return raw, mimeEntity{}, err
	}
	return raw, newMIMEEntity(raw.Header, body, true), nil
}

func fromHeaders(path string, header mail.Header) Message {
	m := Message{Path: path}
	m.From = decodeHeader(header.Get("From"))
	m.To = decodeHeader(header.Get("To"))
	m.Cc = decodeHeader(header.Get("Cc"))
	m.Bcc = decodeHeader(header.Get("Bcc"))
	m.Subject = decodeHeader(header.Get("Subject"))
	m.MessageID = header.Get("Message-ID")
	m.DateText = header.Get("Date")
	if date, dateErr := mail.ParseDate(m.DateText); dateErr == nil {
		m.Date = date
	}
	return m
}

func decodeHeader(value string) string {
	decoded, err := (&mime.WordDecoder{}).DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

type textParts struct {
	plain, html string
	images      []ImagePreview
}

const (
	maxImagePreviews   = 8
	maxPreviewBytes    = 16 * 1024 * 1024
	maxPreviewPixels   = 40_000_000
	previewPixelWidth  = 48
	previewPixelHeight = 24
)

// mimeEntity is the single interpreted representation used while walking a
// message. Its payload is deliberately traversal-local: Message retains only
// attachment metadata and bounded image thumbnails.
type mimeEntity struct {
	mediaType   string
	params      map[string]string
	disposition string
	filename    string
	contentID   string
	data        []byte
	root        bool
}

func newMIMEEntity(header mail.Header, data []byte, root bool) mimeEntity {
	mediaType, params, _ := mime.ParseMediaType(header.Get("Content-Type"))
	if mediaType == "" {
		mediaType = "text/plain"
	}
	disposition, dispositionParams, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	filename := decodeHeader(dispositionParams["filename"])
	if filename == "" {
		filename = decodeHeader(params["name"])
	}
	return mimeEntity{
		mediaType: mediaType, params: params, disposition: disposition,
		filename: filename, contentID: normalizeContentID(header.Get("Content-ID")),
		data: data, root: root,
	}
}

func (entity mimeEntity) attachment() (Attachment, bool) {
	if entity.root || (entity.disposition != "attachment" && entity.filename == "") {
		return Attachment{}, false
	}
	name := entity.filename
	if name == "" {
		name = "unnamed-attachment"
	}
	return Attachment{
		Name: name, MediaType: entity.mediaType, Size: len(entity.data),
		ContentID: entity.contentID, Inline: entity.disposition == "inline",
	}, true
}

type mimeContents struct {
	texts         textParts
	attachments   []Attachment
	targetPayload []byte
}

type mimeTraversal struct {
	collectContent bool
	target         int
	contents       mimeContents
}

func traverseMIME(root mimeEntity, collectContent bool, target int) (mimeContents, error) {
	walk := mimeTraversal{collectContent: collectContent, target: target}
	err := walk.visit(root)
	return walk.contents, err
}

func (walk *mimeTraversal) visit(entity mimeEntity) error {
	if walk.collectContent && strings.HasPrefix(entity.mediaType, "image/") && len(walk.contents.texts.images) < maxImagePreviews {
		if preview, ok := makeImagePreview(entity.filename, entity.mediaType, entity.contentID, entity.data); ok {
			walk.contents.texts.images = append(walk.contents.texts.images, preview)
		}
	}

	if attachment, ok := entity.attachment(); ok {
		index := len(walk.contents.attachments)
		walk.contents.attachments = append(walk.contents.attachments, attachment)
		if index == walk.target {
			walk.contents.targetPayload = entity.data
		}
		return nil
	}

	if strings.HasPrefix(entity.mediaType, "multipart/") {
		boundary := entity.params["boundary"]
		if boundary == "" {
			return errors.New("multipart message has no boundary")
		}
		reader := multipart.NewReader(bytes.NewReader(entity.data), boundary)
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			partData, err := io.ReadAll(part)
			if err != nil {
				return err
			}
			partData, err = decodeTransfer(partData, part.Header.Get("Content-Transfer-Encoding"))
			if err != nil {
				return err
			}
			// A malformed nested multipart is treated as an unavailable body
			// alternative. Roll back anything collected from that subtree and
			// keep inspecting its siblings, matching the reader's fallback
			// behavior while keeping extraction in the same traversal order.
			before := walk.contents
			if err := walk.visit(newMIMEEntity(mail.Header(part.Header), partData, false)); err != nil {
				walk.contents = before
			}
		}
	}

	if !walk.collectContent {
		return nil
	}
	if entity.mediaType == "text/plain" && walk.contents.texts.plain == "" {
		walk.contents.texts.plain = string(decodeCharset(entity.data, entity.params["charset"]))
	}
	if entity.mediaType == "text/html" && walk.contents.texts.html == "" {
		walk.contents.texts.html = string(decodeCharset(entity.data, entity.params["charset"]))
	}
	return nil
}

func decodeTransfer(data []byte, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "7bit", "8bit", "binary":
		return data, nil
	case "base64":
		return io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(data)))
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(data)))
	default:
		return data, nil
	}
}

var tags = regexp.MustCompile(`(?s)<[^>]*>`)
var breaks = regexp.MustCompile(`(?i)<\s*(br\s*/?|/p|/div|/li|/tr|/h[1-6])\s*>`)

func htmlToText(value string) string {
	value = breaks.ReplaceAllString(value, "\n")
	value = tags.ReplaceAllString(value, "")
	return strings.TrimSpace(html.UnescapeString(value))
}

func htmlToMarkdown(value string) (string, error) {
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(table.WithHeaderPromotion(true)),
		),
	)
	return conv.ConvertString(value)
}

func decodeCharset(data []byte, label string) []byte {
	label = strings.TrimSpace(strings.ToLower(label))
	if label == "" || label == "utf-8" || label == "us-ascii" {
		return data
	}
	encoding, err := htmlindex.Get(label)
	if err != nil {
		return data
	}
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), encoding.NewDecoder()))
	if err != nil {
		return data
	}
	return decoded
}

func normalizeContentID(value string) string {
	return strings.Trim(strings.TrimSpace(value), "<>")
}

func makeImagePreview(name, mediaType, contentID string, data []byte) (ImagePreview, bool) {
	if len(data) == 0 || len(data) > maxPreviewBytes {
		return ImagePreview{}, false
	}
	configuration, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || configuration.Width <= 0 || configuration.Height <= 0 ||
		configuration.Width > maxPreviewPixels/configuration.Height {
		return ImagePreview{}, false
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ImagePreview{}, false
	}

	width, height := configuration.Width, configuration.Height
	scale := min(1.0, min(float64(previewPixelWidth)/float64(width), float64(previewPixelHeight)/float64(height)))
	thumbnailWidth := max(1, int(float64(width)*scale))
	thumbnailHeight := max(1, int(float64(height)*scale))
	pixels := make([]color.NRGBA, 0, thumbnailWidth*thumbnailHeight)
	bounds := decoded.Bounds()
	for y := range thumbnailHeight {
		sourceY := bounds.Min.Y + y*bounds.Dy()/thumbnailHeight
		for x := range thumbnailWidth {
			sourceX := bounds.Min.X + x*bounds.Dx()/thumbnailWidth
			pixels = append(pixels, color.NRGBAModel.Convert(decoded.At(sourceX, sourceY)).(color.NRGBA))
		}
	}
	if strings.TrimSpace(name) == "" {
		if contentID != "" {
			name = contentID
		} else {
			name = "Inline image"
		}
	}
	return ImagePreview{
		Name: name, MediaType: mediaType, ContentID: contentID,
		Width: thumbnailWidth, Height: thumbnailHeight, Pixels: pixels,
	}, true
}
