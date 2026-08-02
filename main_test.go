package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeMaildir(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindMaildirsAndParseMessages(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "INBOX")
	makeMaildir(t, inbox)
	makeMaildir(t, filepath.Join(root, "Labels", "Invoices"))
	raw := "From: =?UTF-8?Q?Jos=C3=A9?= <jose@example.com>\r\nTo: me@example.com\r\nSubject: Backup completo\r\nDate: Fri, 01 Aug 2025 12:00:00 +0200\r\nMessage-ID: <1@example.com>\r\nContent-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nCorpo da mensagem.\r\n--x\r\nContent-Type: application/pdf; name=invoice.pdf\r\nContent-Disposition: attachment; filename=invoice.pdf\r\n\r\nPDFDATA\r\n--x--\r\n"
	path := filepath.Join(inbox, "cur", "message:2,S")
	if err := os.WriteFile(path, []byte(raw), 0o444); err != nil {
		t.Fatal(err)
	}
	folders, err := findMaildirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 2 {
		t.Fatalf("got %d folders", len(folders))
	}
	var found *folder
	for i := range folders {
		if folders[i].Name == "INBOX" {
			found = &folders[i]
		}
	}
	if found == nil {
		t.Fatal("INBOX not found")
	}
	if err := loadFolder(found); err != nil {
		t.Fatal(err)
	}
	if len(found.Messages) != 1 {
		t.Fatalf("got %d messages", len(found.Messages))
	}
	msg := found.Messages[0]
	if !strings.Contains(msg.From, "José") || msg.Body != "Corpo da mensagem." {
		t.Fatalf("unexpected message: %#v", msg)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Name != "invoice.pdf" {
		t.Fatalf("unexpected attachments: %#v", msg.Attachments)
	}
}

func TestHTMLFallback(t *testing.T) {
	root := t.TempDir()
	makeMaildir(t, root)
	path := filepath.Join(root, "new", "html")
	raw := "Subject: HTML\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>Ol&aacute;<br>mundo</p>"
	if err := os.WriteFile(path, []byte(raw), 0o444); err != nil {
		t.Fatal(err)
	}
	msg, err := parseMessage(path)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Body != "Olá\nmundo" {
		t.Fatalf("body = %q", msg.Body)
	}
}

func TestBase64Body(t *testing.T) {
	root := t.TempDir()
	makeMaildir(t, root)
	path := filepath.Join(root, "new", "base64")
	raw := "Subject: Encoded\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nT2zDoSwgYmFja3VwIQ=="
	if err := os.WriteFile(path, []byte(raw), 0o444); err != nil {
		t.Fatal(err)
	}
	msg, err := parseMessage(path)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Body != "Olá, backup!" {
		t.Fatalf("body = %q", msg.Body)
	}
}
