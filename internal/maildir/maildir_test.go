package maildir

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverAndLoad(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "INBOX")
	makeMaildir(t, inbox)
	makeMaildir(t, filepath.Join(root, "Labels", "Invoices"))
	raw := "From: sender@example.com\r\nSubject: Backup\r\nDate: Fri, 01 Aug 2025 12:00:00 +0200\r\n\r\nComplete"
	if err := os.WriteFile(filepath.Join(inbox, "cur", "message:2,S"), []byte(raw), 0o444); err != nil {
		t.Fatal(err)
	}

	folders, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 2 || folders[0].Name != "INBOX" {
		t.Fatalf("unexpected folders: %#v", folders)
	}
	if err := Load(&folders[0]); err != nil {
		t.Fatal(err)
	}
	if len(folders[0].Messages) != 1 || folders[0].Messages[0].Subject != "Backup" {
		t.Fatalf("unexpected messages: %#v", folders[0].Messages)
	}
}

func TestSortFolders(t *testing.T) {
	folders := []Folder{{Name: "Vodafone"}, {Name: "Label 10"}, {Name: "[Gmail]/Trash"}, {Name: "Label 2"}, {Name: "alpha"}, {Name: "INBOX"}, {Name: "Gmail/Sent"}}
	SortFolders(folders)
	var names []string
	for _, folder := range folders {
		names = append(names, folder.Name)
	}
	want := []string{"INBOX", "[Gmail]/Trash", "Gmail/Sent", "alpha", "Label 2", "Label 10", "Vodafone"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
}

func TestDisplayName(t *testing.T) {
	if got := DisplayName("Labels/Invoices"); got != "Labels › Invoices" {
		t.Fatalf("DisplayName = %q", got)
	}
}

func makeMaildir(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}
