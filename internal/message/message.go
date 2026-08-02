// Package message parses local RFC 822/MIME message files without modifying
// them or their containing Maildir.
package message

import (
	"bytes"
	"encoding/base64"
	"errors"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"
)

type Attachment struct {
	Name      string
	MediaType string
	Size      int
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
	Attachments []Attachment
	Err         error
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

	m := Message{Path: path}
	m.From = decodeHeader(raw.Header.Get("From"))
	m.To = decodeHeader(raw.Header.Get("To"))
	m.Cc = decodeHeader(raw.Header.Get("Cc"))
	m.Bcc = decodeHeader(raw.Header.Get("Bcc"))
	m.Subject = decodeHeader(raw.Header.Get("Subject"))
	m.MessageID = raw.Header.Get("Message-ID")
	m.DateText = raw.Header.Get("Date")
	if date, dateErr := mail.ParseDate(m.DateText); dateErr == nil {
		m.Date = date
	}

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
	if err != nil {
		return m, err
	}
	if parts.plain != "" {
		m.Body = parts.plain
	} else {
		m.Body = htmlToText(parts.html)
	}
	if strings.TrimSpace(m.Body) == "" {
		m.Body = "[sem corpo de texto]"
	}
	return m, nil
}

func decodeHeader(value string) string {
	decoded, err := (&mime.WordDecoder{}).DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}

type textParts struct{ plain, html string }

func extractParts(mediaType string, params map[string]string, data []byte) (textParts, []Attachment, error) {
	var texts textParts
	var attachments []Attachment
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return texts, nil, errors.New("multipart sem boundary")
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
			if disposition == "attachment" || filename != "" {
				if filename == "" {
					filename = "anexo-sem-nome"
				}
				attachments = append(attachments, Attachment{Name: filename, MediaType: partType, Size: len(partData)})
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
			}
		}
		return texts, attachments, nil
	}
	if mediaType == "text/plain" {
		texts.plain = string(data)
	}
	if mediaType == "text/html" {
		texts.html = string(data)
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
