package ui

import (
	"errors"
	"testing"

	"mailtui/internal/message"
	"mailtui/internal/readsession"
)

func TestReadAdapterDeliversProgressiveFolderFacts(t *testing.T) {
	reader := &scriptedReader{
		folderUpdates: []readsession.FolderUpdate{
			{Started: true},
			{Messages: []message.Message{{Path: "/mail/INBOX/cur/a"}}, Err: errors.New("bad header")},
			{Messages: []message.Message{{Path: "/mail/INBOX/cur/a"}, {Path: "/mail/INBOX/cur/b"}}, Done: true, HadReadErrors: true},
		},
	}
	adapter := newReadAdapter(reader)

	start := adapter.startFolder("/mail/INBOX", false)
	first, ok := start().(folderReadFact)
	if !ok || !first.started || first.done || first.path != "/mail/INBOX" {
		t.Fatalf("first fact = %#v", first)
	}

	second, ok := adapter.nextFolder(first.path)().(folderReadFact)
	if !ok || second.done || second.err == nil || len(second.messages) != 1 {
		t.Fatalf("second fact = %#v", second)
	}
	third, ok := adapter.nextFolder(first.path)().(folderReadFact)
	if !ok || !third.done || !third.hadReadErrors || len(third.messages) != 2 {
		t.Fatalf("third fact = %#v", third)
	}
	if next := adapter.nextFolder(first.path); next != nil {
		t.Fatal("terminal folder read retained an active operation")
	}
}

func TestReadAdapterSuppressesStaleFolderFacts(t *testing.T) {
	reader := &scriptedReader{folderUpdates: []readsession.FolderUpdate{{Stale: true}, {Done: true}}}
	adapter := newReadAdapter(reader)
	first := adapter.startFolder("/mail/INBOX", false)
	second := adapter.startFolder("/mail/INBOX", true)
	if stale := first(); stale != nil {
		t.Fatalf("stale folder result reached UI: %#v", stale)
	}
	if fact, ok := second().(folderReadFact); !ok || !fact.done {
		t.Fatalf("current folder result = %#v", fact)
	}
}

func TestReadAdapterSuppressesStaleMessageFacts(t *testing.T) {
	reader := &scriptedReader{messageUpdates: []readsession.MessageUpdate{{Stale: true}, {Message: message.Message{Path: "/mail/cur/a"}}}}
	adapter := newReadAdapter(reader)
	summary := message.Message{Path: "/mail/cur/a"}

	first := adapter.startMessage(summary)
	second := adapter.startMessage(summary)
	if stale := first(); stale != nil {
		t.Fatalf("stale message result reached UI: %#v", stale)
	}
	if fact, ok := second().(messageReadFact); !ok || fact.path != summary.Path {
		t.Fatalf("current message result = %#v", fact)
	}
}

type scriptedReader struct {
	next           readsession.RequestID
	folderUpdates  []readsession.FolderUpdate
	messageUpdates []readsession.MessageUpdate
}

func (reader *scriptedReader) RequestFolder(path string, refresh bool) readsession.FolderRequest {
	reader.next++
	return readsession.FolderRequest{ID: reader.next, Path: path, Refresh: refresh}
}

func (reader *scriptedReader) ReadFolder(request readsession.FolderRequest) readsession.FolderUpdate {
	if len(reader.folderUpdates) == 0 {
		return readsession.FolderUpdate{Request: request, Done: true}
	}
	update := reader.folderUpdates[0]
	reader.folderUpdates = reader.folderUpdates[1:]
	update.Request = request
	return update
}

func (reader *scriptedReader) RequestMessage(summary message.Message) readsession.MessageRequest {
	reader.next++
	return readsession.MessageRequest{ID: reader.next, Path: summary.Path, Summary: summary}
}

func (reader *scriptedReader) ReadMessage(request readsession.MessageRequest) readsession.MessageUpdate {
	if len(reader.messageUpdates) == 0 {
		return readsession.MessageUpdate{Request: request, Message: request.Summary}
	}
	update := reader.messageUpdates[0]
	reader.messageUpdates = reader.messageUpdates[1:]
	update.Request = request
	return update
}
