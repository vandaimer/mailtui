// Package readsession owns the asynchronous read policy for a Maildir browsing
// session. It keeps cache validation, progressive header reads, metadata
// persistence, and full-message hydration behind one interface while leaving
// scheduling to the caller.
package readsession

import (
	"sync"

	"mailtui/internal/maildir"
	"mailtui/internal/message"
	"mailtui/internal/metadata"
)

const headerBatchSize = 64

// RequestID identifies one generation of an asynchronous read. A newer read
// for the same folder or selected message makes older work stale.
type RequestID uint64

type FolderRequest struct {
	ID      RequestID
	Path    string
	Refresh bool
}

// FolderUpdate is a stable snapshot of a progressive folder read. Started is
// emitted before the first batch so callers can clear an earlier snapshot.
// Done is true for cache hits, terminal failures, and the final scanned batch.
type FolderUpdate struct {
	Request       FolderRequest
	Messages      []message.Message
	Started       bool
	Done          bool
	Stale         bool
	Fatal         bool
	Err           error
	HadReadErrors bool
	CacheErr      error
}

type MessageRequest struct {
	ID      RequestID
	Path    string
	Summary message.Message
}

type MessageUpdate struct {
	Request MessageRequest
	Message message.Message
	Stale   bool
}

// Reader is the narrow seam used by the UI. Request methods only allocate
// in-memory state. Read methods perform filesystem I/O and must be called from
// asynchronous commands, never from a Bubble Tea Update path.
type Reader interface {
	RequestFolder(path string, refresh bool) FolderRequest
	ReadFolder(FolderRequest) FolderUpdate
	RequestMessage(summary message.Message) MessageRequest
	ReadMessage(MessageRequest) MessageUpdate
}

type cacheStore interface {
	Load(folder, fingerprint string) ([]message.Message, bool)
	Save(folder, fingerprint string, messages []message.Message) error
}

type dependencies struct {
	list     func(string) ([]string, string, error)
	scan     func([]string, int) <-chan maildir.HeaderBatch
	hydrate  func(string) (message.Message, error)
	metadata cacheStore
}

// Session coordinates reads for one application run.
type Session struct {
	mu sync.Mutex

	nextID         RequestID
	folders        map[RequestID]*folderRead
	latestFolders  map[string]RequestID
	pendingFolders map[string]FolderRequest
	latestMessage  RequestID
	pendingMessage *MessageRequest
	deps           dependencies
}

type folderRead struct {
	mu            sync.Mutex
	request       FolderRequest
	started       bool
	done          bool
	fingerprint   string
	batches       <-chan maildir.HeaderBatch
	messages      []message.Message
	hadReadErrors bool
	draining      bool
}

func New(root string) *Session {
	return newSession(dependencies{
		list:     maildir.ListMessagePaths,
		scan:     maildir.ScanHeaderBatches,
		hydrate:  message.ParseFile,
		metadata: metadata.New(root),
	})
}

// NewAt is intended for deterministic callers such as tests and tools that
// need to choose an external cache directory explicitly.
func NewAt(cacheDir string) *Session {
	return newSession(dependencies{
		list:     maildir.ListMessagePaths,
		scan:     maildir.ScanHeaderBatches,
		hydrate:  message.ParseFile,
		metadata: metadata.NewAt(cacheDir),
	})
}

func newSession(deps dependencies) *Session {
	return &Session{
		folders:        make(map[RequestID]*folderRead),
		latestFolders:  make(map[string]RequestID),
		pendingFolders: make(map[string]FolderRequest),
		deps:           deps,
	}
}

func (session *Session) RequestFolder(path string, refresh bool) FolderRequest {
	session.mu.Lock()
	defer session.mu.Unlock()

	if request, found := session.pendingFolders[path]; found {
		return request
	}
	request := FolderRequest{ID: session.allocateID(), Path: path, Refresh: refresh}
	session.latestFolders[path] = request.ID
	session.pendingFolders[path] = request
	return request
}

func (session *Session) ReadFolder(request FolderRequest) FolderUpdate {
	session.mu.Lock()
	read, found := session.folders[request.ID]
	if session.latestFolders[request.Path] != request.ID {
		delete(session.folders, request.ID)
		if pending, pendingFound := session.pendingFolders[request.Path]; pendingFound && pending.ID == request.ID {
			delete(session.pendingFolders, request.Path)
		}
		session.mu.Unlock()
		if found {
			read.drainBatches()
		}
		return FolderUpdate{Request: request, Done: true, Stale: true}
	}
	if !found {
		read = &folderRead{request: request}
		session.folders[request.ID] = read
	} else if read.request != request {
		delete(session.folders, request.ID)
		session.mu.Unlock()
		read.drainBatches()
		session.forgetFolder(read.request)
		return FolderUpdate{Request: request, Done: true, Stale: true}
	}
	session.mu.Unlock()

	read.mu.Lock()
	defer read.mu.Unlock()
	if !session.folderIsCurrent(request) {
		read.startDrainLocked()
		session.forgetFolder(request)
		return FolderUpdate{Request: request, Done: true, Stale: true}
	}
	if read.done {
		session.forgetFolder(request)
		return FolderUpdate{Request: request, Done: true, Messages: cloneMessages(read.messages), HadReadErrors: read.hadReadErrors}
	}
	if !read.started {
		paths, fingerprint, err := session.deps.list(request.Path)
		if !session.folderIsCurrent(request) {
			session.forgetFolder(request)
			return FolderUpdate{Request: request, Done: true, Stale: true}
		}
		if err != nil {
			read.done = true
			session.forgetFolder(request)
			return FolderUpdate{Request: request, Done: true, Fatal: true, Err: err}
		}
		if !request.Refresh {
			if messages, found := session.deps.metadata.Load(request.Path, fingerprint); found {
				if !session.folderIsCurrent(request) {
					session.forgetFolder(request)
					return FolderUpdate{Request: request, Done: true, Stale: true}
				}
				maildir.SortMessages(messages)
				read.messages = messages
				read.done = true
				session.forgetFolder(request)
				return FolderUpdate{Request: request, Messages: cloneMessages(messages), Done: true}
			}
		}
		read.started = true
		read.fingerprint = fingerprint
		read.batches = session.deps.scan(paths, headerBatchSize)
		return FolderUpdate{Request: request, Started: true}
	}

	batch, open := <-read.batches
	if !open {
		batch = maildir.HeaderBatch{Done: true}
	}
	if !session.folderIsCurrent(request) {
		read.startDrainLocked()
		session.forgetFolder(request)
		return FolderUpdate{Request: request, Done: true, Stale: true}
	}
	read.messages = append(read.messages, batch.Messages...)
	maildir.SortMessages(read.messages)
	if batch.Err != nil {
		read.hadReadErrors = true
	}
	update := FolderUpdate{
		Request:       request,
		Messages:      cloneMessages(read.messages),
		Done:          batch.Done,
		Err:           batch.Err,
		HadReadErrors: read.hadReadErrors,
	}
	if batch.Done {
		read.done = true
		update.CacheErr = session.deps.metadata.Save(request.Path, read.fingerprint, read.messages)
		if !session.folderIsCurrent(request) {
			update.Stale = true
		}
		session.forgetFolder(request)
	}
	return update
}

func (session *Session) RequestMessage(summary message.Message) MessageRequest {
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.pendingMessage != nil && session.pendingMessage.Path == summary.Path {
		return *session.pendingMessage
	}
	request := MessageRequest{ID: session.allocateID(), Path: summary.Path, Summary: summary}
	session.latestMessage = request.ID
	session.pendingMessage = &request
	return request
}

func (session *Session) ReadMessage(request MessageRequest) MessageUpdate {
	session.mu.Lock()
	if session.latestMessage != request.ID {
		session.mu.Unlock()
		return MessageUpdate{Request: request, Stale: true}
	}
	session.mu.Unlock()

	parsed, err := session.deps.hydrate(request.Path)
	if err != nil {
		parsed = request.Summary.MarkContentUnavailable(err)
	}

	session.mu.Lock()
	stale := session.latestMessage != request.ID
	if session.pendingMessage != nil && session.pendingMessage.ID == request.ID {
		session.pendingMessage = nil
	}
	session.mu.Unlock()
	return MessageUpdate{Request: request, Message: parsed, Stale: stale}
}

func (session *Session) allocateID() RequestID {
	session.nextID++
	return session.nextID
}

func (session *Session) folderIsCurrent(request FolderRequest) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.latestFolders[request.Path] == request.ID
}

func (session *Session) forgetFolder(request FolderRequest) {
	session.mu.Lock()
	delete(session.folders, request.ID)
	if pending, found := session.pendingFolders[request.Path]; found && pending.ID == request.ID {
		delete(session.pendingFolders, request.Path)
	}
	session.mu.Unlock()
}

func (read *folderRead) drainBatches() {
	read.mu.Lock()
	defer read.mu.Unlock()
	read.startDrainLocked()
}

func (read *folderRead) startDrainLocked() {
	if read.draining || read.batches == nil {
		return
	}
	read.draining = true
	go func(batches <-chan maildir.HeaderBatch) {
		for range batches {
		}
	}(read.batches)
}

func cloneMessages(messages []message.Message) []message.Message {
	return append([]message.Message(nil), messages...)
}
