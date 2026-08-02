package readsession

import (
	"os"
	"path/filepath"
	"testing"

	"mailtui/internal/maildir"
	"mailtui/internal/message"
	"mailtui/internal/metadata"
)

func TestFolderReadUsesCacheAndRefreshBypassesIt(t *testing.T) {
	folder, messagePath := syntheticMaildir(t, "Fresh")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	_, fingerprint, err := maildir.ListMessagePaths(folder)
	if err != nil {
		t.Fatal(err)
	}
	store := metadata.NewAt(cacheDir)
	if err := store.Save(folder, fingerprint, []message.Message{{Path: messagePath, Subject: "Stale"}}); err != nil {
		t.Fatal(err)
	}
	session := NewAt(cacheDir)

	cached := session.ReadFolder(session.RequestFolder(folder, false))
	if !cached.Done || cached.Started || len(cached.Messages) != 1 || cached.Messages[0].Subject != "Stale" {
		t.Fatalf("matching cache was not returned as one completed update: %#v", cached)
	}

	request := session.RequestFolder(folder, true)
	started := session.ReadFolder(request)
	if !started.Started || started.Done {
		t.Fatalf("refresh did not bypass the matching cache: %#v", started)
	}
	completed := readFolderToCompletion(t, session, request)
	if len(completed.Messages) != 1 || completed.Messages[0].Subject != "Fresh" {
		t.Fatalf("refresh did not parse the source header: %#v", completed)
	}
}

func TestFolderReadEmitsSortedProgressAndPersistsMetadata(t *testing.T) {
	folder, _ := syntheticMaildir(t, "Older")
	writeMessage(t, filepath.Join(folder, "cur", "newer"), "Newer", "Mon, 3 Aug 2026 12:00:00 +0200")
	cacheDir := filepath.Join(t.TempDir(), "cache")
	session := NewAt(cacheDir)
	request := session.RequestFolder(folder, false)

	if started := session.ReadFolder(request); !started.Started || started.Done {
		t.Fatalf("first update = %#v", started)
	}
	completed := readFolderToCompletion(t, session, request)
	if len(completed.Messages) != 2 || completed.Messages[0].Subject != "Newer" || completed.CacheErr != nil {
		t.Fatalf("completed update = %#v", completed)
	}

	cached := session.ReadFolder(session.RequestFolder(folder, false))
	if !cached.Done || len(cached.Messages) != 2 || cached.Messages[0].Subject != "Newer" {
		t.Fatalf("persisted metadata was not reusable: %#v", cached)
	}
}

func TestNewerFolderRequestMakesOlderGenerationStaleBeforeIO(t *testing.T) {
	listCalls := 0
	session := newSession(dependencies{
		list: func(string) ([]string, string, error) {
			listCalls++
			return nil, "fingerprint", nil
		},
		scan:     maildir.ScanHeaderBatches,
		hydrate:  message.ParseFile,
		metadata: metadata.NewAt(filepath.Join(t.TempDir(), "cache")),
	})
	older := session.RequestFolder("/mail/INBOX", false)
	_ = session.RequestFolder("/mail/INBOX", true)

	update := session.ReadFolder(older)
	if !update.Stale || !update.Done || listCalls != 0 {
		t.Fatalf("older generation was not rejected before I/O: %#v, calls=%d", update, listCalls)
	}
}

func TestMessageReadHydratesOnlyTheRequestedFile(t *testing.T) {
	folder, messagePath := syntheticMaildir(t, "Hydrated")
	_ = folder
	session := NewAt(filepath.Join(t.TempDir(), "cache"))
	request := session.RequestMessage(messagePath)
	update := session.ReadMessage(request)
	if update.Err != nil || update.Stale || !update.Message.Loaded || update.Message.Subject != "Hydrated" || update.Message.Body != "Body" {
		t.Fatalf("message update = %#v", update)
	}
}

func TestNewerMessageRequestDiscardsOlderHydration(t *testing.T) {
	hydrateCalls := 0
	session := newSession(dependencies{
		list: maildir.ListMessagePaths,
		scan: maildir.ScanHeaderBatches,
		hydrate: func(string) (message.Message, error) {
			hydrateCalls++
			return message.Message{}, nil
		},
		metadata: metadata.NewAt(filepath.Join(t.TempDir(), "cache")),
	})
	older := session.RequestMessage("/mail/cur/older")
	_ = session.RequestMessage("/mail/cur/newer")

	update := session.ReadMessage(older)
	if !update.Stale || hydrateCalls != 0 {
		t.Fatalf("older hydration was not rejected before I/O: %#v, calls=%d", update, hydrateCalls)
	}
}

func readFolderToCompletion(t *testing.T, session *Session, request FolderRequest) FolderUpdate {
	t.Helper()
	for {
		update := session.ReadFolder(request)
		if update.Done {
			return update
		}
	}
}

func syntheticMaildir(t *testing.T, subject string) (string, string) {
	t.Helper()
	folder := t.TempDir()
	for _, bucket := range []string{"cur", "new", "tmp"} {
		if err := os.Mkdir(filepath.Join(folder, bucket), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(folder, "new", "message")
	writeMessage(t, path, subject, "Sun, 2 Aug 2026 12:00:00 +0200")
	return folder, path
}

func writeMessage(t *testing.T, path, subject, date string) {
	t.Helper()
	contents := "From: Alice <alice@example.com>\r\nSubject: " + subject + "\r\nDate: " + date + "\r\n\r\nBody"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
