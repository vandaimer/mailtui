package readsession

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestSupersededFolderRequestIsStaleBeforeIO(t *testing.T) {
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
	session.mu.Lock()
	session.latestFolders[older.Path] = older.ID + 1
	session.mu.Unlock()

	update := session.ReadFolder(older)
	if !update.Stale || !update.Done || listCalls != 0 {
		t.Fatalf("older generation was not rejected before I/O: %#v, calls=%d", update, listCalls)
	}
}

func TestDuplicateFolderRequestKeepsAcceptedGenerationCurrent(t *testing.T) {
	listCalls := 0
	session := newSession(dependencies{
		list: func(string) ([]string, string, error) {
			listCalls++
			return nil, "fingerprint", nil
		},
		scan: func([]string, int) <-chan maildir.HeaderBatch {
			batches := make(chan maildir.HeaderBatch, 1)
			batches <- maildir.HeaderBatch{Done: true}
			close(batches)
			return batches
		},
		hydrate:  message.ParseFile,
		metadata: metadata.NewAt(filepath.Join(t.TempDir(), "cache")),
	})

	accepted := session.RequestFolder("/mail/INBOX", false)
	duplicate := session.RequestFolder("/mail/INBOX", true)
	if duplicate != accepted {
		t.Fatalf("duplicate allocated generation %d; accepted generation is %d", duplicate.ID, accepted.ID)
	}
	if started := session.ReadFolder(accepted); started.Stale || !started.Started {
		t.Fatalf("accepted folder read was invalidated: %#v", started)
	}
	completed := session.ReadFolder(accepted)
	if completed.Stale || !completed.Done || listCalls != 1 {
		t.Fatalf("accepted folder read did not complete: %#v, list calls=%d", completed, listCalls)
	}
	if next := session.RequestFolder("/mail/INBOX", true); next.ID == accepted.ID || !next.Refresh {
		t.Fatalf("completed request remained pending: %#v", next)
	}
}

func TestStaleStartedFolderReadDrainsScannerExactlyOnce(t *testing.T) {
	batches := make(chan maildir.HeaderBatch)
	producerReleased := make(chan struct{})
	session := newSession(dependencies{
		list: func(string) ([]string, string, error) { return nil, "fingerprint", nil },
		scan: func([]string, int) <-chan maildir.HeaderBatch {
			go func() {
				batches <- maildir.HeaderBatch{Done: true}
				close(batches)
				close(producerReleased)
			}()
			return batches
		},
		hydrate:  message.ParseFile,
		metadata: metadata.NewAt(filepath.Join(t.TempDir(), "cache")),
	})
	request := session.RequestFolder("/mail/INBOX", false)
	if started := session.ReadFolder(request); !started.Started {
		t.Fatalf("folder read did not start: %#v", started)
	}

	// Simulate a superseding generation from a non-UI caller. Normal duplicate
	// requests are idempotent, but stale work must still release its producer.
	session.mu.Lock()
	session.latestFolders[request.Path] = request.ID + 1
	session.mu.Unlock()
	if stale := session.ReadFolder(request); !stale.Stale {
		t.Fatalf("superseded folder read was not stale: %#v", stale)
	}
	select {
	case <-producerReleased:
	case <-time.After(time.Second):
		t.Fatal("stale folder scan producer remained blocked")
	}
}

func TestMessageReadHydratesOnlyTheRequestedFile(t *testing.T) {
	folder, messagePath := syntheticMaildir(t, "Hydrated")
	_ = folder
	session := NewAt(filepath.Join(t.TempDir(), "cache"))
	request := session.RequestMessage(message.Message{Path: messagePath, From: "Cached sender", Subject: "Hydrated"})
	update := session.ReadMessage(request)
	if update.Stale || update.Message.LoadState() != message.LoadContentReady || update.Message.LoadError() != nil || update.Message.Subject != "Hydrated" || update.Message.Body != "Body" {
		t.Fatalf("message update = %#v", update)
	}
}

func TestMessageReadFailurePreservesSummaryAsOneTerminalResult(t *testing.T) {
	failure := errors.New("read failed")
	session := newSession(dependencies{
		list: maildir.ListMessagePaths,
		scan: maildir.ScanHeaderBatches,
		hydrate: func(string) (message.Message, error) {
			return message.Message{Body: "partial content"}, failure
		},
		metadata: metadata.NewAt(filepath.Join(t.TempDir(), "cache")),
	})
	summary := message.Message{Path: "/mail/cur/1", From: "Alice", Subject: "Preserved"}
	request := session.RequestMessage(summary)

	update := session.ReadMessage(request)
	if update.Stale || update.Message.Path != summary.Path || update.Message.From != summary.From || update.Message.Subject != summary.Subject {
		t.Fatalf("summary metadata was not preserved: %#v", update)
	}
	if update.Message.LoadState() != message.LoadContentUnavailable || update.Message.LoadError() != failure || update.Message.Body != "" || update.Message.NeedsHydration() {
		t.Fatalf("failure was not one terminal message: %#v", update.Message)
	}
}

func TestDuplicateMessageRequestKeepsAcceptedHydrationCurrent(t *testing.T) {
	hydrationStarted := make(chan struct{})
	releaseHydration := make(chan struct{})
	session := newSession(dependencies{
		list: maildir.ListMessagePaths,
		scan: maildir.ScanHeaderBatches,
		hydrate: func(path string) (message.Message, error) {
			close(hydrationStarted)
			<-releaseHydration
			return (message.Message{Path: path, Body: "Body"}).MarkContentReady(), nil
		},
		metadata: metadata.NewAt(filepath.Join(t.TempDir(), "cache")),
	})
	summary := message.Message{Path: "/mail/cur/1", Subject: "Accepted"}
	accepted := session.RequestMessage(summary)
	updates := make(chan MessageUpdate, 1)
	go func() { updates <- session.ReadMessage(accepted) }()
	<-hydrationStarted

	duplicate := session.RequestMessage(message.Message{Path: summary.Path, Subject: "Duplicate"})
	if duplicate.ID != accepted.ID || duplicate.Summary.Subject != summary.Subject {
		t.Fatalf("duplicate superseded accepted request: %#v", duplicate)
	}
	close(releaseHydration)
	update := <-updates
	if update.Stale || update.Message.LoadState() != message.LoadContentReady {
		t.Fatalf("accepted hydration was invalidated: %#v", update)
	}
	if next := session.RequestMessage(summary); next.ID == accepted.ID {
		t.Fatalf("completed hydration remained pending: %#v", next)
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
	older := session.RequestMessage(message.Message{Path: "/mail/cur/older"})
	_ = session.RequestMessage(message.Message{Path: "/mail/cur/newer"})

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
