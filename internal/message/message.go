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

// ExtractAttachment returns one decoded MIME attachment by the same order used
// in Message.Attachments. It performs no writes.
func ExtractAttachment(path string, target int) (Attachment, []byte, error) {
	if target < 0 {
		return Attachment{}, nil, errors.New("índice de anexo inválido")
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
		return Attachment{}, nil, fmt.Errorf("anexo %d não encontrado", target+1)
	}
	return attachment, payload, nil
}

func findAttachment(mediaType string, params map[string]string, data []byte, target int, seen *int) (Attachment, []byte, bool, error) {
	if !strings.HasPrefix(mediaType, "multipart/") {
		return Attachment{}, nil, false, nil
	}
	boundary := params["boundary"]
	if boundary == "" {
		return Attachment{}, nil, false, errors.New("multipart sem boundary")
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
		if disposition == "attachment" || filename != "" {
			if filename == "" {
				filename = "anexo-sem-nome"
			}
			if *seen == target {
				return Attachment{Name: filename, MediaType: partType, Size: len(partData)}, partData, true, nil
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
