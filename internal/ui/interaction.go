package ui

import "unicode/utf8"

type pane uint8

const (
	foldersPane pane = iota
	messagesPane
	readerPane
)

type interactionMode uint8

const (
	navigationMode interactionMode = iota
	searchMode
	attachmentsMode
)

type interactionState struct {
	mode             interactionMode
	focus            pane
	folderCursor     int
	messageCursor    int
	attachmentCursor int
	readerScroll     int
	query            string
	preferPlain      bool
	selectedKey      string
	attachmentKey    string
	searchRestore    searchSnapshot
}

type searchSnapshot struct {
	query         string
	focus         pane
	messageCursor int
	readerScroll  int
	selectedKey   string
}

type interactionInput struct {
	key  string
	text string
}

type interactionContext struct {
	folderCount        int
	messageCount       int
	selectedKey        string
	preservedIndex     int
	preserveSelection  bool
	hasFolder          bool
	folderLoading      bool
	canPickAttachments bool
	attachmentCount    int
	hasRichBody        bool
	readerBoundsValid  bool
	readerPageRows     int
	readerMaxScroll    int
}

type readTiming uint8

const (
	noRead readTiming = iota
	readImmediately
	readDeferred
)

type interactionNotice uint8

const (
	noNotice interactionNotice = iota
	noAttachmentsNotice
	noRichBodyNotice
	plainBodyNotice
	richBodyNotice
	noFolderNotice
	folderBusyNotice
)

type interactionOutcome struct {
	quit            bool
	folderRead      readTiming
	messageRead     readTiming
	refreshFolder   bool
	openAttachment  bool
	attachmentIndex int
	notice          interactionNotice
}

func (state *interactionState) Apply(input interactionInput, context interactionContext) interactionOutcome {
	switch state.mode {
	case searchMode:
		return state.applySearch(input)
	case attachmentsMode:
		return state.applyAttachments(input, context)
	default:
		return state.applyNavigation(input, context)
	}
}

func (state *interactionState) Reconcile(context interactionContext) bool {
	state.focus = pane(clamp(int(state.focus), int(foldersPane), int(readerPane)))
	state.folderCursor = clampCursor(state.folderCursor, context.folderCount)
	state.messageCursor = clampCursor(state.messageCursor, context.messageCount)
	state.attachmentCursor = clampCursor(state.attachmentCursor, context.attachmentCount)
	if state.selectedKey != "" && state.selectedKey != context.selectedKey && context.preserveSelection {
		state.messageCursor = context.preservedIndex
		return true
	}

	if state.selectedKey != context.selectedKey {
		state.readerScroll = 0
		state.selectedKey = context.selectedKey
	}
	if context.readerBoundsValid {
		state.readerScroll = clamp(state.readerScroll, 0, max(0, context.readerMaxScroll))
	}
	if state.mode == attachmentsMode && (!context.canPickAttachments || context.selectedKey == "" || context.selectedKey != state.attachmentKey) {
		state.closeAttachments()
	}
	return false
}

func (state *interactionState) applySearch(input interactionInput) interactionOutcome {
	outcome := interactionOutcome{}
	switch input.key {
	case "ctrl+c":
		outcome.quit = true
		return outcome
	case "esc":
		state.query = state.searchRestore.query
		state.focus = state.searchRestore.focus
		state.messageCursor = state.searchRestore.messageCursor
		state.readerScroll = state.searchRestore.readerScroll
		state.selectedKey = state.searchRestore.selectedKey
		state.mode = navigationMode
	case "enter":
		state.mode = navigationMode
	case "backspace":
		if len(state.query) > 0 {
			_, size := utf8.DecodeLastRuneInString(state.query)
			state.query = state.query[:len(state.query)-size]
			state.resetMessageSelection()
		}
	case "ctrl+u":
		state.query = ""
		state.resetMessageSelection()
	default:
		if input.text != "" {
			state.query += input.text
			state.resetMessageSelection()
		}
	}
	outcome.messageRead = readDeferred
	return outcome
}

func (state *interactionState) applyAttachments(input interactionInput, context interactionContext) interactionOutcome {
	outcome := interactionOutcome{}
	switch input.key {
	case "ctrl+c":
		outcome.quit = true
	case "esc", "q", "o":
		state.closeAttachments()
	case "up", "k":
		state.attachmentCursor = clampCursor(state.attachmentCursor-1, context.attachmentCount)
	case "down", "j":
		state.attachmentCursor = clampCursor(state.attachmentCursor+1, context.attachmentCount)
	case "enter":
		if context.canPickAttachments && context.attachmentCount > 0 {
			outcome.openAttachment = true
			outcome.attachmentIndex = state.attachmentCursor
		}
		state.closeAttachments()
	}
	return outcome
}

func (state *interactionState) applyNavigation(input interactionInput, context interactionContext) interactionOutcome {
	outcome := interactionOutcome{}
	switch input.key {
	case "ctrl+c", "q":
		outcome.quit = true
	case "/":
		state.searchRestore = searchSnapshot{
			query: state.query, focus: state.focus, messageCursor: state.messageCursor,
			readerScroll: state.readerScroll, selectedKey: state.selectedKey,
		}
		state.mode = searchMode
		if state.focus == foldersPane {
			state.focus = messagesPane
		}
	case "o":
		if context.canPickAttachments && context.attachmentCount > 0 {
			state.mode = attachmentsMode
			state.attachmentCursor = 0
			state.attachmentKey = context.selectedKey
			state.focus = readerPane
		} else {
			outcome.notice = noAttachmentsNotice
		}
	case "v":
		if !context.hasRichBody {
			outcome.notice = noRichBodyNotice
			break
		}
		state.preferPlain = !state.preferPlain
		state.readerScroll = 0
		if state.preferPlain {
			outcome.notice = plainBodyNotice
		} else {
			outcome.notice = richBodyNotice
		}
	case "r":
		switch {
		case !context.hasFolder:
			outcome.notice = noFolderNotice
		case context.folderLoading:
			outcome.notice = folderBusyNotice
		default:
			state.resetMessageSelection()
			outcome.refreshFolder = true
		}
	case "tab":
		state.focus = (state.focus + 1) % 3
		outcome.ensureFocusedData()
	case "shift+tab":
		state.focus = (state.focus + 2) % 3
		outcome.ensureFocusedData()
	case "left", "h":
		if state.focus > foldersPane {
			state.focus--
		}
	case "right", "l", "enter":
		if state.focus < readerPane {
			state.focus++
			outcome.ensureFocusedData()
		}
	case "esc", "backspace":
		if state.query != "" {
			state.query = ""
			state.resetMessageSelection()
			outcome.messageRead = readImmediately
		} else if state.focus > foldersPane {
			state.focus--
		}
	case "up", "k":
		state.move(-1, context, &outcome)
	case "down", "j":
		state.move(1, context, &outcome)
	case "pgup":
		if state.focus == readerPane {
			state.page(-1, context)
		}
	case "pgdown":
		if state.focus == readerPane {
			state.page(1, context)
		}
	case "home":
		state.moveToBoundary(false, context, &outcome)
	case "end":
		state.moveToBoundary(true, context, &outcome)
	}
	return outcome
}

func (state *interactionState) move(delta int, context interactionContext, outcome *interactionOutcome) {
	switch state.focus {
	case foldersPane:
		next := clampCursor(state.folderCursor+delta, context.folderCount)
		if next != state.folderCursor {
			state.folderCursor = next
			state.query = ""
			state.resetMessageSelection()
			outcome.folderRead = readDeferred
		}
	case messagesPane:
		next := clampCursor(state.messageCursor+delta, context.messageCount)
		if next != state.messageCursor {
			state.messageCursor = next
			state.readerScroll = 0
			state.selectedKey = ""
			outcome.messageRead = readDeferred
		}
	case readerPane:
		state.readerScroll = clamp(state.readerScroll+delta, 0, max(0, context.readerMaxScroll))
	}
}

func (state *interactionState) moveToBoundary(end bool, context interactionContext, outcome *interactionOutcome) {
	switch state.focus {
	case foldersPane:
		if context.folderCount == 0 {
			return
		}
		if end {
			state.folderCursor = context.folderCount - 1
		} else {
			state.folderCursor = 0
		}
		state.query = ""
		state.resetMessageSelection()
		outcome.folderRead = readDeferred
	case messagesPane:
		if end && context.messageCount > 0 {
			state.messageCursor = context.messageCount - 1
		} else {
			state.messageCursor = 0
		}
		state.readerScroll = 0
		state.selectedKey = ""
		outcome.messageRead = readDeferred
	}
}

func (state *interactionState) page(direction int, context interactionContext) {
	maximum := max(0, context.readerMaxScroll)
	state.readerScroll = clamp(state.readerScroll, 0, maximum)
	state.readerScroll = clamp(state.readerScroll+direction*max(1, context.readerPageRows), 0, maximum)
}

func (state *interactionState) resetMessageSelection() {
	state.messageCursor = 0
	state.readerScroll = 0
	state.selectedKey = ""
}

func (state *interactionState) closeAttachments() {
	state.mode = navigationMode
	state.attachmentCursor = 0
	state.attachmentKey = ""
}

func (outcome *interactionOutcome) ensureFocusedData() {
	if outcome == nil {
		return
	}
	// The caller decides whether the selected folder is already loaded.
	outcome.folderRead = readImmediately
	outcome.messageRead = readImmediately
}

func clampCursor(cursor, count int) int {
	if count <= 0 {
		return 0
	}
	return clamp(cursor, 0, count-1)
}
