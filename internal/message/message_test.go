package message

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMultipartMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message")
	raw := "From: =?UTF-8?Q?Jos=C3=A9?= <jose@example.com>\r\nTo: me@example.com\r\nSubject: Backup completo\r\nDate: Fri, 01 Aug 2025 12:00:00 +0200\r\nMessage-ID: <1@example.com>\r\nContent-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nCorpo da mensagem.\r\n--x\r\nContent-Type: application/pdf; name=invoice.pdf\r\nContent-Disposition: attachment; filename=invoice.pdf\r\n\r\nPDFDATA\r\n--x--\r\n"
	writeMessage(t, path, raw)

	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(parsed.From, "José") || parsed.Body != "Corpo da mensagem." {
		t.Fatalf("unexpected message: %#v", parsed)
	}
	if len(parsed.Attachments) != 1 || parsed.Attachments[0].Name != "invoice.pdf" {
		t.Fatalf("unexpected attachments: %#v", parsed.Attachments)
	}
}

func TestHTMLFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "html")
	writeMessage(t, path, "Subject: HTML\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Ol&aacute;<br>mundo</p>")
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "Olá\nmundo" {
		t.Fatalf("body = %q", parsed.Body)
	}
}

func TestBase64Body(t *testing.T) {
	path := filepath.Join(t.TempDir(), "base64")
	writeMessage(t, path, "Subject: Encoded\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nT2zDoSwgYmFja3VwIQ==")
	parsed, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Body != "Olá, backup!" {
		t.Fatalf("body = %q", parsed.Body)
	}
}

func writeMessage(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o444); err != nil {
		t.Fatal(err)
	}
}
