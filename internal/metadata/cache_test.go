package metadata

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mailtui/internal/message"
)

func TestStoreRoundTripAndInvalidation(t *testing.T) {
	store := NewAt(filepath.Join(t.TempDir(), "cache"))
	want := []message.Message{{Path: "/mail/cur/1", From: "Alice", Subject: "Cached", Date: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)}}
	if err := store.Save("/mail", "fingerprint-a", want); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Load("/mail", "fingerprint-a")
	if !ok || len(got) != 1 || got[0].Subject != want[0].Subject || got[0].LoadState() != message.LoadHeaderOnly {
		t.Fatalf("unexpected cache result: %#v, %v", got, ok)
	}
	if _, ok := store.Load("/mail", "fingerprint-b"); ok {
		t.Fatal("stale fingerprint produced a cache hit")
	}
}

func TestStoreProjectsOnlyHeaderLifecycleStates(t *testing.T) {
	store := NewAt(filepath.Join(t.TempDir(), "cache"))
	headerFailure := errors.New("bad header")
	hydrationFailure := errors.New("network read failed")
	messages := []message.Message{
		(message.Message{Path: "/mail/cur/ready", Subject: "Ready", Body: "must not be cached"}).MarkContentReady(),
		(message.Message{Path: "/mail/cur/invalid", Subject: "Invalid"}).MarkHeaderInvalid(headerFailure),
		(message.Message{Path: "/mail/cur/unavailable", Subject: "Unavailable"}).MarkContentUnavailable(hydrationFailure),
	}
	if err := store.Save("/mail", "fingerprint", messages); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Load("/mail", "fingerprint")
	if !ok || len(got) != 3 {
		t.Fatalf("cache result = %#v, %v", got, ok)
	}
	if got[0].LoadState() != message.LoadHeaderOnly || got[0].Body != "" || got[0].LoadError() != nil {
		t.Fatalf("ready content escaped into cache: %#v", got[0])
	}
	if got[1].LoadState() != message.LoadHeaderInvalid || got[1].LoadError() == nil || got[1].LoadError().Error() != headerFailure.Error() {
		t.Fatalf("header failure was not preserved: %#v", got[1])
	}
	if got[2].LoadState() != message.LoadHeaderOnly || got[2].LoadError() != nil {
		t.Fatalf("hydration failure was persisted as a header failure: %#v", got[2])
	}
}

func TestExistingVersionOneCacheLoadsIntoExplicitHeaderState(t *testing.T) {
	dir := t.TempDir()
	store := NewAt(dir)
	if err := os.WriteFile(store.filePath("/mail"), []byte(`{"version":1,"folder":"/mail","fingerprint":"fingerprint","messages":[{"path":"/mail/cur/broken","subject":"[invalid message]","error":"legacy parse error"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	messages, ok := store.Load("/mail", "fingerprint")
	if !ok || len(messages) != 1 || messages[0].LoadState() != message.LoadHeaderInvalid || messages[0].LoadError() == nil || messages[0].LoadError().Error() != "legacy parse error" {
		t.Fatalf("legacy cache result = %#v, %v", messages, ok)
	}
}
