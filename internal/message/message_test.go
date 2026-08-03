package message

import (
	"bytes"
	"encoding/base64"
	"errors"
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

func TestNestedMIMETraversalKeepsDisplayedAndExtractedAttachmentOrder(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	imagePayload := imageData.Bytes()
	reportPayload := []byte("REPORT-PAYLOAD")
	unnamedPayload := []byte("unnamed payload")

	path := filepath.Join(t.TempDir(), "deeply-nested")
	raw := "Subject: Nested MIME\r\nContent-Type: multipart/mixed; boundary=outer\r\n\r\n" +
		"--outer\r\nContent-Type: multipart/alternative; boundary=alternative\r\n\r\n" +
		"--alternative\r\nContent-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: quoted-printable\r\n\r\nNested=20plain=20body.\r\n" +
		"--alternative\r\nContent-Type: multipart/related; boundary=related\r\n\r\n" +
		"--related\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>Nested <strong>HTML</strong> body.</p><img src=\"cid:green-logo\">\r\n" +
		"--related\r\nContent-Type: image/png\r\n" +
		"Content-Disposition: inline; filename*=UTF-8''gr%C3%BCn.png\r\n" +
		"Content-ID: <green-logo>\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(imagePayload) + "\r\n--related--\r\n" +
		"--alternative--\r\n" +
		"--outer\r\nContent-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(reportPayload) + "\r\n" +
		"--outer\r\nContent-Type: multipart/mixed; boundary=nested\r\n\r\n" +
		"--nested\r\nContent-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" +
		"unnamed=20payload\r\n--nested--\r\n" +
		"--outer--\r\n"
	writeMessage(t, path, raw)

	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "Nested plain body." {
		t.Fatalf("plain body = %q", parsed.Body)
	}
	if !strings.Contains(parsed.RichBody, "**HTML**") {
		t.Fatalf("rich body = %q", parsed.RichBody)
	}
	if len(parsed.Images) != 1 || parsed.Images[0].Name != "grün.png" || parsed.Images[0].ContentID != "green-logo" {
		t.Fatalf("image previews = %#v", parsed.Images)
	}

	want := []struct {
		name      string
		mediaType string
		contentID string
		inline    bool
		payload   []byte
	}{
		{name: "grün.png", mediaType: "image/png", contentID: "green-logo", inline: true, payload: imagePayload},
		{name: "résumé.pdf", mediaType: "application/pdf", payload: reportPayload},
		{name: "unnamed-attachment", mediaType: "application/octet-stream", payload: unnamedPayload},
	}
	if len(parsed.Attachments) != len(want) {
		t.Fatalf("attachments = %#v", parsed.Attachments)
	}
	for index, expected := range want {
		displayed := parsed.Attachments[index]
		if displayed.Name != expected.name || displayed.MediaType != expected.mediaType ||
			displayed.ContentID != expected.contentID || displayed.Inline != expected.inline ||
			displayed.Size != len(expected.payload) {
			t.Fatalf("displayed attachment %d = %#v", index, displayed)
		}

		extracted, payload, err := ExtractAttachment(path, index)
		if err != nil {
			t.Fatalf("extract attachment %d: %v", index, err)
		}
		if extracted != displayed {
			t.Fatalf("attachment %d metadata differs: displayed=%#v extracted=%#v", index, displayed, extracted)
		}
		if !bytes.Equal(payload, expected.payload) {
			t.Fatalf("attachment %d payload = %q, want %q", index, payload, expected.payload)
		}
	}
}

func TestExtractAttachmentRejectsIndexesOutsideUnifiedTraversal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one-attachment")
	writeMessage(t, path, "Content-Type: multipart/mixed; boundary=x\r\n\r\n"+
		"--x\r\nContent-Type: text/plain\r\n\r\nBody\r\n"+
		"--x\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=a.pdf\r\n\r\nA\r\n"+
		"--x--\r\n")

	for _, index := range []int{-1, 1} {
		if _, _, err := ExtractAttachment(path, index); err == nil {
			t.Fatalf("ExtractAttachment(%d) unexpectedly succeeded", index)
		}
	}
}

func TestMalformedNestedAlternativeDoesNotHideLaterAttachments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed-alternative")
	writeMessage(t, path, "Content-Type: multipart/mixed; boundary=outer\r\n\r\n"+
		"--outer\r\nContent-Type: text/plain\r\n\r\nFallback body\r\n"+
		"--outer\r\nContent-Type: multipart/alternative\r\n\r\nUnavailable alternative\r\n"+
		"--outer\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=later.pdf\r\n\r\nLATER\r\n"+
		"--outer--\r\n")

	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "Fallback body" || len(parsed.Attachments) != 1 || parsed.Attachments[0].Name != "later.pdf" {
		t.Fatalf("parsed message = %#v", parsed)
	}
	extracted, payload, err := ExtractAttachment(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if extracted != parsed.Attachments[0] || string(payload) != "LATER" {
		t.Fatalf("extracted attachment = %#v %q", extracted, payload)
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
	if parsed.Subject != "Lightweight" || parsed.Body != "" || parsed.LoadState() != LoadHeaderOnly || !parsed.NeedsHydration() {
		t.Fatalf("unexpected header result: %#v", parsed)
	}
}

func TestLoadStateTransitionsAreTerminalAndKeepFailuresOutOfBody(t *testing.T) {
	failure := errors.New("unreadable")
	summary := Message{Path: "/mail/cur/1", From: "Alice", Subject: "Subject"}

	invalid := summary.MarkHeaderInvalid(failure)
	if invalid.LoadState() != LoadHeaderInvalid || invalid.LoadError() != failure || invalid.NeedsHydration() {
		t.Fatalf("invalid header state = %#v", invalid)
	}
	unavailable := summary.MarkContentUnavailable(failure)
	if unavailable.LoadState() != LoadContentUnavailable || unavailable.LoadError() != failure || unavailable.Body != "" || unavailable.NeedsHydration() {
		t.Fatalf("unavailable content state = %#v", unavailable)
	}
	ready := summary.MarkContentReady()
	if ready.LoadState() != LoadContentReady || ready.LoadError() != nil || ready.NeedsHydration() {
		t.Fatalf("ready content state = %#v", ready)
	}
}

func TestParseFileDoesNotMarkPartialMIMETraversalReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed-multipart")
	writeMessage(t, path, "Subject: Broken\r\nContent-Type: multipart/mixed\r\n\r\nBody")

	parsed, err := ParseFile(path)
	if err == nil {
		t.Fatal("malformed multipart unexpectedly parsed")
	}
	if parsed.Subject != "Broken" || parsed.LoadState() != LoadHeaderOnly || parsed.Body != "" || len(parsed.Attachments) != 0 {
		t.Fatalf("partial parse escaped as content: %#v", parsed)
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
