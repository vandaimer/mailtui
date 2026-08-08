package ui

import "mailtui/internal/message"

// loadedFolderPhase describes the in-memory lifecycle of one discovered
// Maildir folder. It deliberately has no knowledge of reads, cache files,
// scheduling, filtering, navigation, or rendering.
type loadedFolderPhase uint8

const (
	folderUnloaded loadedFolderPhase = iota
	folderLoading
	folderLoaded
	folderFailed
)

// loadedFolderStates owns mutable message snapshots separately from immutable
// maildir.Folder discovery identity. Each update replaces the complete snapshot
// supplied by the read session, preserving that session's order.
type loadedFolderStates struct {
	byFolder map[string]loadedFolder
}

type loadedFolder struct {
	phase    loadedFolderPhase
	messages []message.Message
	previous []message.Message
	failure  error
}

func newLoadedFolderStates() loadedFolderStates {
	return loadedFolderStates{byFolder: make(map[string]loadedFolder)}
}

func (states *loadedFolderStates) begin(path string, refresh bool) {
	state := states.folder(path)
	if refresh && state.messages != nil {
		state.previous = cloneFolderMessages(state.messages)
	} else {
		state.messages = nil
		state.previous = nil
	}
	state.phase = folderLoading
	state.failure = nil
	states.put(path, state)
}

// replace stores a progressive complete snapshot. The caller supplies the
// ordering; this state module must never sort it.
func (states *loadedFolderStates) replace(path string, messages []message.Message) {
	state := states.folder(path)
	state.messages = cloneFolderMessages(messages)
	if state.phase == folderUnloaded || state.phase == folderFailed {
		state.phase = folderLoading
	}
	state.failure = nil
	states.put(path, state)
}

func (states *loadedFolderStates) complete(path string) {
	state := states.folder(path)
	state.phase = folderLoaded
	state.previous = nil
	state.failure = nil
	states.put(path, state)
}

// fail preserves the last completed snapshot when a refresh cannot start or
// complete. An initial failure remains a failed state without a snapshot.
func (states *loadedFolderStates) fail(path string, failure error) {
	state := states.folder(path)
	if state.previous != nil {
		state.messages = state.previous
		state.previous = nil
		state.phase = folderLoaded
	} else {
		state.messages = nil
		state.phase = folderFailed
	}
	state.failure = failure
	states.put(path, state)
}

// replaceMessage updates a hydrated message by its stable Maildir path. It
// returns false when no current folder snapshot contains that path.
func (states *loadedFolderStates) replaceMessage(path string, replacement message.Message) bool {
	for folderPath, state := range states.byFolder {
		for index := range state.messages {
			if state.messages[index].Path != path {
				continue
			}
			state.messages[index] = replacement
			states.byFolder[folderPath] = state
			return true
		}
	}
	return false
}

func (states *loadedFolderStates) hasSnapshot(path string) bool {
	return states.folder(path).messages != nil
}

func (states *loadedFolderStates) messages(path string) []message.Message {
	return cloneFolderMessages(states.folder(path).messages)
}

func (states *loadedFolderStates) phase(path string) loadedFolderPhase {
	return states.folder(path).phase
}

func (states *loadedFolderStates) failure(path string) error {
	return states.folder(path).failure
}

func (states *loadedFolderStates) folder(path string) loadedFolder {
	if states.byFolder == nil {
		return loadedFolder{phase: folderUnloaded}
	}
	return states.byFolder[path]
}

func (states *loadedFolderStates) put(path string, state loadedFolder) {
	if states.byFolder == nil {
		states.byFolder = make(map[string]loadedFolder)
	}
	states.byFolder[path] = state
}

func cloneFolderMessages(messages []message.Message) []message.Message {
	if messages == nil {
		return nil
	}
	return append([]message.Message(nil), messages...)
}
