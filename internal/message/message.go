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
	Err         error
	Loaded      bool
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
	data, err := os.ReadFile(path)
	if err != nil {
		return Message{}, err
	}
	raw, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return Message{}, err
	}

	m := fromHeaders(path, raw.Header)
	m.Loaded = true

	body, err := io.ReadAll(raw.Body)
	if err != nil {
		return m, err
	}
	body, err = decodeTransfer(body, raw.Header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return m, err
	}
	mediaType, params, _ := mime.ParseMediaType(raw.Header.Get("Content-Type"))
	if mediaType == "" {
		mediaType = "text/plain"
	}
	parts, attachments, err := extractParts(mediaType, params, body)
	m.Attachments = attachments
	m.Images = parts.images
	if err != nil {
		return m, err
	}
	if parts.html != "" {
		if richBody, richErr := htmlToMarkdown(parts.html); richErr == nil {
			m.RichBody = strings.TrimSpace(richBody)
		}
	}
	if parts.plain != "" {
		m.Body = parts.plain
	} else {
		m.Body = htmlToText(parts.html)
	}
	if strings.TrimSpace(m.Body) == "" {
		m.Body = "[no text body]"
	}
	return m, nil
}

// ExtractAttachment returns one decoded MIME attachment by the same order used
// in Message.Attachments. It performs no writes.
func ExtractAttachment(path string, target int) (Attachment, []byte, error) {
	if target < 0 {
		return Attachment{}, nil, errors.New("invalid attachment index")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Attachment{}, nil, err
	}
	raw, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return Attachment{}, nil, err
	}
	body, err := io.ReadAll(raw.Body)
	if err != nil {
		return Attachment{}, nil, err
	}
	body, err = decodeTransfer(body, raw.Header.Get("Content-Transfer-Encoding"))
	if err != nil {
		return Attachment{}, nil, err
	}
	mediaType, params, _ := mime.ParseMediaType(raw.Header.Get("Content-Type"))
	if mediaType == "" {
		mediaType = "text/plain"
	}
	seen := 0
	attachment, payload, found, err := findAttachment(mediaType, params, body, target, &seen)
	if err != nil {
		return Attachment{}, nil, err
	}
	if !found {
		return Attachment{}, nil, fmt.Errorf("attachment %d not found", target+1)
	}
	return attachment, payload, nil
}

func findAttachment(mediaType string, params map[string]string, data []byte, target int, seen *int) (Attachment, []byte, bool, error) {
	if !strings.HasPrefix(mediaType, "multipart/") {
		return Attachment{}, nil, false, nil
	}
	boundary := params["boundary"]
	if boundary == "" {
		return Attachment{}, nil, false, errors.New("multipart message has no boundary")
	}
	reader := multipart.NewReader(bytes.NewReader(data), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return Attachment{}, nil, false, nil
		}
		if err != nil {
			return Attachment{}, nil, false, err
		}
		partData, err := io.ReadAll(part)
		if err != nil {
			return Attachment{}, nil, false, err
		}
		partData, err = decodeTransfer(partData, part.Header.Get("Content-Transfer-Encoding"))
		if err != nil {
			return Attachment{}, nil, false, err
		}
		partType, partParams, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if partType == "" {
			partType = "text/plain"
		}
		disposition, dispositionParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		filename := decodeHeader(dispositionParams["filename"])
		if filename == "" {
			filename = decodeHeader(partParams["name"])
		}
		contentID := normalizeContentID(part.Header.Get("Content-ID"))
		if disposition == "attachment" || filename != "" {
			if filename == "" {
				filename = "unnamed-attachment"
			}
			if *seen == target {
				return Attachment{
					Name: filename, MediaType: partType, Size: len(partData),
					ContentID: contentID, Inline: disposition == "inline",
				}, partData, true, nil
			}
			*seen++
			continue
		}
		attachment, payload, found, nestedErr := findAttachment(partType, partParams, partData, target, seen)
		if nestedErr != nil || found {
			return attachment, payload, found, nestedErr
		}
	}
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

func extractParts(mediaType string, params map[string]string, data []byte) (textParts, []Attachment, error) {
	var texts textParts
	var attachments []Attachment
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return texts, nil, errors.New("multipart message has no boundary")
		}
		reader := multipart.NewReader(bytes.NewReader(data), boundary)
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return texts, attachments, err
			}
			partData, err := io.ReadAll(part)
			if err != nil {
				return texts, attachments, err
			}
			partData, err = decodeTransfer(partData, part.Header.Get("Content-Transfer-Encoding"))
			if err != nil {
				return texts, attachments, err
			}

			partType, partParams, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if partType == "" {
				partType = "text/plain"
			}
			disposition, dispositionParams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			filename := decodeHeader(dispositionParams["filename"])
			if filename == "" {
				filename = decodeHeader(partParams["name"])
			}
			contentID := normalizeContentID(part.Header.Get("Content-ID"))
			previewedImage := false
			if strings.HasPrefix(partType, "image/") && len(texts.images) < maxImagePreviews {
				if preview, ok := makeImagePreview(filename, partType, contentID, partData); ok {
					texts.images = append(texts.images, preview)
					previewedImage = true
				}
			}
			if disposition == "attachment" || filename != "" {
				if filename == "" {
					filename = "unnamed-attachment"
				}
				attachments = append(attachments, Attachment{
					Name: filename, MediaType: partType, Size: len(partData),
					ContentID: contentID, Inline: disposition == "inline",
				})
				continue
			}

			nested, nestedAttachments, nestedErr := extractParts(partType, partParams, partData)
			if nestedErr == nil {
				if texts.plain == "" {
					texts.plain = nested.plain
				}
				if texts.html == "" {
					texts.html = nested.html
				}
				attachments = append(attachments, nestedAttachments...)
				if !previewedImage {
					remaining := maxImagePreviews - len(texts.images)
					if remaining > len(nested.images) {
						remaining = len(nested.images)
					}
					if remaining > 0 {
						texts.images = append(texts.images, nested.images[:remaining]...)
					}
				}
			}
		}
		return texts, attachments, nil
	}
	if mediaType == "text/plain" {
		texts.plain = string(decodeCharset(data, params["charset"]))
	}
	if mediaType == "text/html" {
		texts.html = string(decodeCharset(data, params["charset"]))
	}
	if strings.HasPrefix(mediaType, "image/") {
		if preview, ok := makeImagePreview(decodeHeader(params["name"]), mediaType, "", data); ok {
			texts.images = append(texts.images, preview)
		}
	}
	return texts, attachments, nil
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
