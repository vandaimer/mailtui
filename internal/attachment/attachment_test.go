package attachment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractToSanitizesNameAndWritesOutsideMessage(t *testing.T) {
	root := t.TempDir()
	messagePath := filepath.Join(root, "message")
	raw := "Content-Type: multipart/mixed; boundary=x\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nBody\r\n--x\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=../../invoice.pdf\r\n\r\nPDF\r\n--x--\r\n"
	if err := os.WriteFile(messagePath, []byte(raw), 0o444); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "attachments")
	extracted, err := ExtractTo(messagePath, 0, destination)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(extracted) != destination || strings.Contains(filepath.Base(extracted), "..") {
		t.Fatalf("unsafe output path: %s", extracted)
	}
	payload, err := os.ReadFile(extracted)
	if err != nil || string(payload) != "PDF" {
		t.Fatalf("payload = %q, err = %v", payload, err)
	}
	info, err := os.Stat(extracted)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, err = %v", info.Mode().Perm(), err)
	}
}
