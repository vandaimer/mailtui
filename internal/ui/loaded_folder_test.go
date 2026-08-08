package ui

import (
	"errors"
	"reflect"
	"testing"

	"mailtui/internal/message"
)

func TestLoadedFolderStatesLifecyclePreservesReadSessionOrder(t *testing.T) {
	states := newLoadedFolderStates()
	path := "/mail/INBOX"
	readOrder := []message.Message{{Path: path + "/cur/new", Subject: "New"}, {Path: path + "/cur/old", Subject: "Old"}}

	if states.phase(path) != folderUnloaded || states.hasSnapshot(path) {
		t.Fatalf("initial state = phase %d, snapshot %t", states.phase(path), states.hasSnapshot(path))
	}
	states.begin(path, false)
	if states.phase(path) != folderLoading || states.hasSnapshot(path) {
		t.Fatalf("started state = phase %d, snapshot %t", states.phase(path), states.hasSnapshot(path))
	}
	states.replace(path, readOrder)
	if got := states.messages(path); !reflect.DeepEqual(got, readOrder) {
		t.Fatalf("progressive snapshot reordered messages: %#v", got)
	}
	states.complete(path)
	if states.phase(path) != folderLoaded || !states.hasSnapshot(path) {
		t.Fatalf("completed state = phase %d, snapshot %t", states.phase(path), states.hasSnapshot(path))
	}

	readOrder[0].Subject = "mutated caller"
	if got := states.messages(path)[0].Subject; got != "New" {
		t.Fatalf("state retained caller slice: %q", got)
	}
}

func TestLoadedFolderStatesRefreshFailureRestoresLastGoodSnapshot(t *testing.T) {
	states := newLoadedFolderStates()
	path := "/mail/INBOX"
	lastGood := []message.Message{{Path: path + "/cur/a", Subject: "A"}, {Path: path + "/cur/b", Subject: "B"}}
	setLoadedFolderState(&states, path, lastGood)

	states.begin(path, true)
	states.replace(path, []message.Message{{Path: path + "/cur/new", Subject: "New"}})
	failure := errors.New("permission denied")
	states.fail(path, failure)

	if states.phase(path) != folderLoaded || !reflect.DeepEqual(states.messages(path), lastGood) || states.failure(path) != failure {
		t.Fatalf("failed refresh did not restore last good snapshot: %#v", states.folder(path))
	}
}

func TestLoadedFolderStatesInitialFailureHasNoSnapshot(t *testing.T) {
	states := newLoadedFolderStates()
	path := "/mail/INBOX"
	failure := errors.New("folder unavailable")

	states.begin(path, false)
	states.fail(path, failure)

	if states.phase(path) != folderFailed || states.hasSnapshot(path) || states.failure(path) != failure {
		t.Fatalf("initial failure = %#v", states.folder(path))
	}
}

func TestLoadedFolderStatesReplaceHydratedMessageByMaildirPath(t *testing.T) {
	states := newLoadedFolderStates()
	setLoadedFolderState(&states, "/mail/INBOX", []message.Message{{Path: "/mail/INBOX/cur/shared", Subject: "Header"}})
	setLoadedFolderState(&states, "/mail/Archive", []message.Message{{Path: "/mail/Archive/cur/other", Subject: "Other"}})

	hydrated := (message.Message{Path: "/mail/INBOX/cur/shared", Subject: "Header", Body: "Body"}).MarkContentReady()
	if !states.replaceMessage(hydrated.Path, hydrated) {
		t.Fatal("hydration path was not found")
	}
	if got := states.messages("/mail/INBOX")[0]; got.LoadState() != message.LoadContentReady || got.Body != "Body" {
		t.Fatalf("hydration replacement = %#v", got)
	}
	if got := states.messages("/mail/Archive")[0].Subject; got != "Other" {
		t.Fatalf("unrelated folder changed: %q", got)
	}
	if states.replaceMessage("/mail/missing", message.Message{Path: "/mail/missing"}) {
		t.Fatal("missing message path was reported as replaced")
	}
}

func setLoadedFolderState(states *loadedFolderStates, path string, messages []message.Message) {
	states.replace(path, messages)
	states.complete(path)
}
