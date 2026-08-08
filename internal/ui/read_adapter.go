package ui

import (
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"mailtui/internal/message"
	"mailtui/internal/readsession"
)

const (
	folderDebounce  = 180 * time.Millisecond
	messageDebounce = 120 * time.Millisecond
)

// folderReadFact is the UI-facing result of one progressive folder read
// step. The read-session request protocol is deliberately kept behind the
// adapter; Model.Update only consumes these visible facts.
type folderReadFact struct {
	path          string
	refresh       bool
	messages      []message.Message
	started       bool
	done          bool
	fatal         bool
	err           error
	hadReadErrors bool
	cacheErr      error
}

// messageReadFact is the UI-facing result of a full-message hydration.
type messageReadFact struct {
	path    string
	message message.Message
}

type folderReadOperation struct {
	request readsession.FolderRequest
}

type messageReadOperation struct {
	request readsession.MessageRequest
}

// readAdapter owns the Bubble Tea scheduling seam for readsession. Commands
// perform one read step at a time, allowing progressive batches to reach the
// UI without blocking Update on a complete folder scan. The maps are guarded
// because command functions execute concurrently with Update.
type readAdapter struct {
	reader readsession.Reader

	mu       sync.Mutex
	folders  map[string]folderReadOperation
	messages map[string]messageReadOperation
}

func newReadAdapter(reader readsession.Reader) *readAdapter {
	return &readAdapter{
		reader:   reader,
		folders:  make(map[string]folderReadOperation),
		messages: make(map[string]messageReadOperation),
	}
}

func (adapter *readAdapter) queueFolder(path string, refresh bool, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return folderReadDue{path: path, refresh: refresh}
	})
}

func (adapter *readAdapter) queueMessage(summary message.Message, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return messageReadDue{summary: summary}
	})
}

// startFolder allocates a generation and performs its first read step in an
// asynchronous command. RequestFolder is in-memory; ReadFolder is the only
// operation here that may touch the Maildir or metadata cache.
func (adapter *readAdapter) startFolder(path string, refresh bool) tea.Cmd {
	return func() tea.Msg {
		request := adapter.reader.RequestFolder(path, refresh)
		adapter.mu.Lock()
		adapter.folders[path] = folderReadOperation{request: request}
		adapter.mu.Unlock()
		return adapter.readFolder(request)
	}
}

// nextFolder continues exactly one progressive read step. The caller invokes
// it only after a non-terminal folderReadFact has reached Update.
func (adapter *readAdapter) nextFolder(path string) tea.Cmd {
	adapter.mu.Lock()
	operation, found := adapter.folders[path]
	adapter.mu.Unlock()
	if !found {
		return nil
	}
	return func() tea.Msg { return adapter.readFolder(operation.request) }
}

func (adapter *readAdapter) readFolder(request readsession.FolderRequest) tea.Msg {
	update := adapter.reader.ReadFolder(request)
	if !adapter.folderCurrent(request) || update.Stale {
		adapter.finishFolder(request)
		return nil
	}
	if update.Done {
		adapter.finishFolder(request)
	}
	return folderReadFact{
		path:          request.Path,
		refresh:       request.Refresh,
		messages:      update.Messages,
		started:       update.Started,
		done:          update.Done,
		fatal:         update.Fatal,
		err:           update.Err,
		hadReadErrors: update.HadReadErrors,
		cacheErr:      update.CacheErr,
	}
}

func (adapter *readAdapter) folderCurrent(request readsession.FolderRequest) bool {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	operation, found := adapter.folders[request.Path]
	return found && operation.request.ID == request.ID
}

func (adapter *readAdapter) finishFolder(request readsession.FolderRequest) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if operation, found := adapter.folders[request.Path]; found && operation.request.ID == request.ID {
		delete(adapter.folders, request.Path)
	}
}

// startMessage allocates a generation and hydrates one selected message in
// an asynchronous command. Stale generations are suppressed before they can
// reach the UI.
func (adapter *readAdapter) startMessage(summary message.Message) tea.Cmd {
	return func() tea.Msg {
		request := adapter.reader.RequestMessage(summary)
		adapter.mu.Lock()
		adapter.messages[request.Path] = messageReadOperation{request: request}
		adapter.mu.Unlock()
		update := adapter.reader.ReadMessage(request)
		if !adapter.messageCurrent(request) || update.Stale {
			adapter.finishMessage(request)
			return nil
		}
		adapter.finishMessage(request)
		return messageReadFact{path: request.Path, message: update.Message}
	}
}

func (adapter *readAdapter) messageCurrent(request readsession.MessageRequest) bool {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	operation, found := adapter.messages[request.Path]
	return found && operation.request.ID == request.ID
}

func (adapter *readAdapter) finishMessage(request readsession.MessageRequest) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if operation, found := adapter.messages[request.Path]; found && operation.request.ID == request.ID {
		delete(adapter.messages, request.Path)
	}
}
