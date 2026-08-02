package metadata

import (
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
	if !ok || len(got) != 1 || got[0].Subject != want[0].Subject || got[0].Loaded {
		t.Fatalf("unexpected cache result: %#v, %v", got, ok)
	}
	if _, ok := store.Load("/mail", "fingerprint-b"); ok {
		t.Fatal("stale fingerprint produced a cache hit")
	}
}
