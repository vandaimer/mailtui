package message

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMultipartMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message")
	raw := "From: =?UTF-8?Q?Ren=C3=A9e?= <renee@example.com>\r\nTo: me@example.com\r\nSubject: Complete backup\r\nDate: Fri, 01 Aug 2025 12:00:00 +0200\r\nMessage-ID: <1@example.com>\r\nContent-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nMessage body.\r\n--x\r\nContent-Type: application/pdf; name=invoice.pdf\r\nContent-Disposition: attachment; filename=invoice.pdf\r\n\r\nPDFDATA\r\n--x--\r\n"
	writeMessage(t, path, raw)

	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.From, "Renée") || parsed.Body != "Message body." {
		t.Fatalf("unexpected message: %#v", parsed)
	}
	if len(parsed.Attachments) != 1 || parsed.Attachments[0].Name != "invoice.pdf" {
		t.Fatalf("unexpected attachments: %#v", parsed.Attachments)
	}
	attachment, payload, err := ExtractAttachment(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if attachment.Name != "invoice.pdf" || string(payload) != "PDFDATA" {
		t.Fatalf("unexpected extracted attachment: %#v %q", attachment, payload)
	}
}

func TestHTMLFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "html")
	writeMessage(t, path, "Subject: HTML\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Hello<br>world</p>")
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "Hello\nworld" {
		t.Fatalf("body = %q", parsed.Body)
	}
	if !strings.Contains(parsed.RichBody, "Hello") || !strings.Contains(parsed.RichBody, "world") {
		t.Fatalf("rich body = %q", parsed.RichBody)
	}
}

func TestRichHTMLPreservesStructureAlongsidePlainText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alternative")
	raw := "Content-Type: multipart/alternative; boundary=x\r\n\r\n" +
		"--x\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPlain version.\r\n" +
		"--x\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<h1>Welcome</h1><p>Hello <strong>world</strong>.</p><ul><li>First</li><li>Second</li></ul>" +
		"<p><a href=\"https://example.com\">Open site</a></p>\r\n--x--\r\n"
	writeMessage(t, path, raw)

	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(parsed.Body) != "Plain version." {
		t.Fatalf("plain body = %q", parsed.Body)
	}
	for _, expected := range []string{"# Welcome", "**world**", "- First", "[Open site](https://example.com)"} {
		if !strings.Contains(parsed.RichBody, expected) {
			t.Fatalf("rich body missing %q:\n%s", expected, parsed.RichBody)
		}
	}
}

func TestBodyCharsetIsDecodedToUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latin1")
	raw := append([]byte("Subject: Charset\r\nContent-Type: text/plain; charset=iso-8859-1\r\n\r\nOl"), 0xe1)
	raw = append(raw, []byte(" mundo")...)
	if err := os.WriteFile(path, raw, 0o444); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "Olá mundo" {
		t.Fatalf("body = %q", parsed.Body)
	}
}

func TestInlineImageCreatesSmallPreviewWithoutRetainingPayload(t *testing.T) {
	var imageData bytes.Buffer
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	source.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	source.SetNRGBA(0, 1, color.NRGBA{B: 255, A: 255})
	source.SetNRGBA(1, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	if err := png.Encode(&imageData, source); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(imageData.Bytes())
	path := filepath.Join(t.TempDir(), "inline-image")
	raw := "Content-Type: multipart/related; boundary=x\r\n\r\n" +
		"--x\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Logo</p><img src=\"cid:logo\">\r\n" +
		"--x\r\nContent-Type: image/png; name=logo.png\r\nContent-Disposition: inline; filename=logo.png\r\n" +
		"Content-ID: <logo>\r\nContent-Transfer-Encoding: base64\r\n\r\n" + encoded + "\r\n--x--\r\n"
	writeMessage(t, path, raw)

	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Images) != 1 {
		t.Fatalf("image previews = %#v", parsed.Images)
	}
	preview := parsed.Images[0]
	if preview.Name != "logo.png" || preview.ContentID != "logo" || preview.Width != 2 || preview.Height != 2 || len(preview.Pixels) != 4 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	if len(parsed.Attachments) != 1 || !parsed.Attachments[0].Inline {
		t.Fatalf("attachments = %#v", parsed.Attachments)
	}
}

func TestBase64Body(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base64")
	writeMessage(t, path, "Subject: Encoded\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8sIGJhY2t1cCE=")
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "Hello, backup!" {
		t.Fatalf("body = %q", parsed.Body)
	}
}

func TestParseHeaderFileDoesNotHydrateBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large")
	writeMessage(t, path, "From: sender@example.com\r\nSubject: Lightweight\r\n\r\n"+strings.Repeat("x", 4*1024*1024))
	parsed, err := ParseHeaderFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Subject != "Lightweight" || parsed.Body != "" || parsed.Loaded {
		t.Fatalf("unexpected header result: %#v", parsed)
	}
}

func BenchmarkHeaderVersusFullMessage(b *testing.B) {
	path := filepath.Join(b.TempDir(), "large")
	contents := "From: sender@example.com\r\nSubject: Benchmark\r\n\r\n" + strings.Repeat("network-payload-", 512*1024)
	if err := os.WriteFile(path, []byte(contents), 0o444); err != nil {
		b.Fatal(err)
	}

	b.Run("headers-only", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := ParseHeaderFile(path); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("complete-message", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := ParseFile(path); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func writeMessage(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o444); err != nil {
		t.Fatal(err)
	}
}
