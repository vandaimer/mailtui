package attachment

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingRunner struct {
	name string
	args []string
	err  error
}

func (runner *recordingRunner) Start(name string, args ...string) error {
	runner.name = name
	runner.args = append([]string(nil), args...)
	return runner.err
}

func TestOpenMaterializesPrivatelyAndStartsPlatformOpener(t *testing.T) {
	messagePath := writeAttachmentMessage(t, "../../invoice.pdf", "PDF")
	destination := filepath.Join(t.TempDir(), "attachments")
	runner := &recordingRunner{}

	result, err := open(messagePath, 0, destination, runner)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path == "" || filepath.Dir(result.Path) != destination || strings.Contains(filepath.Base(result.Path), "..") {
		t.Fatalf("unsafe output path: %q", result.Path)
	}
	payload, err := os.ReadFile(result.Path)
	if err != nil || string(payload) != "PDF" {
		t.Fatalf("payload = %q, err = %v", payload, err)
	}
	info, err := os.Stat(result.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, err = %v", info.Mode().Perm(), err)
	}
	directory, err := os.Stat(destination)
	if err != nil || directory.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, err = %v", directory.Mode().Perm(), err)
	}
	wantName, wantArgs, err := openerCommand(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runner.name != wantName || strings.Join(runner.args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("opener = %q %#v, want %q %#v", runner.name, runner.args, wantName, wantArgs)
	}
}

func TestOpenReturnsMaterializedPathWhenPlatformLaunchFails(t *testing.T) {
	messagePath := writeAttachmentMessage(t, "invoice.pdf", "PDF")
	destination := filepath.Join(t.TempDir(), "attachments")
	want := errors.New("desktop opener unavailable")

	result, err := open(messagePath, 0, destination, &recordingRunner{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if result.Path == "" {
		t.Fatal("launch failure did not retain the materialized path")
	}
	payload, readErr := os.ReadFile(result.Path)
	if readErr != nil || string(payload) != "PDF" {
		t.Fatalf("retained payload = %q, err = %v", payload, readErr)
	}
}

func TestMaterializeIsDeterministicAndAtomicallyReplacesExistingOutput(t *testing.T) {
	messagePath := writeAttachmentMessage(t, "résumé.pdf", "first")
	destination := filepath.Join(t.TempDir(), "attachments")

	first, err := materialize(messagePath, 0, destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(first, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(messagePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(messagePath, attachmentMessage("résumé.pdf", "second"), 0o444); err != nil {
		t.Fatal(err)
	}
	second, err := materialize(messagePath, 0, destination)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("paths differ: %q != %q", first, second)
	}
	payload, err := os.ReadFile(second)
	if err != nil || string(payload) != "second" {
		t.Fatalf("replaced payload = %q, err = %v", payload, err)
	}
	info, err := os.Stat(second)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("replacement mode = %v, err = %v", info.Mode().Perm(), err)
	}
}

func TestMaterializeRejectsMissingAttachmentWithoutCreatingOutput(t *testing.T) {
	messagePath := writeAttachmentMessage(t, "invoice.pdf", "PDF")
	destination := filepath.Join(t.TempDir(), "attachments")

	if _, err := materialize(messagePath, 1, destination); err == nil {
		t.Fatal("missing attachment unexpectedly materialized")
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination = %v, want it not to exist", err)
	}
}

func TestMaterializeRejectsDestinationInsideMaildir(t *testing.T) {
	maildirRoot := t.TempDir()
	messagePath := filepath.Join(maildirRoot, "cur", "message")
	if err := os.Mkdir(filepath.Dir(messagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(messagePath, attachmentMessage("invoice.pdf", "PDF"), 0o444); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(maildirRoot, "attachment-cache")

	if _, err := materialize(messagePath, 0, destination); err == nil || !strings.Contains(err.Error(), "outside the Maildir") {
		t.Fatalf("error = %v, want Maildir destination rejection", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination = %v, want it not to exist", err)
	}
}

func TestSafeNameIsSafeWithinOneFilesystemName(t *testing.T) {
	name := safeName("../../" + strings.Repeat("界", 100) + "\x00report.pdf")
	if strings.ContainsAny(name, "/\\\x00") || len(name) > 160 {
		t.Fatalf("unsafe name %q (%d bytes)", name, len(name))
	}
	if safeName("..") != "unnamed-attachment" {
		t.Fatalf("dot path was not replaced: %q", safeName(".."))
	}
}

func writeAttachmentMessage(t *testing.T, name, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "message")
	if err := os.WriteFile(path, attachmentMessage(name, payload), 0o444); err != nil {
		t.Fatal(err)
	}
	return path
}

func attachmentMessage(name, payload string) []byte {
	return []byte("Content-Type: multipart/mixed; boundary=x\r\n\r\n" +
		"--x\r\nContent-Type: text/plain\r\n\r\nBody\r\n" +
		"--x\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=" + name + "\r\n\r\n" + payload + "\r\n--x--\r\n")
}
