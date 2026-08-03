package ui

import "testing"

func TestInteractionSearchCancelRestoresTransactionSnapshot(t *testing.T) {
	state := interactionState{focus: readerPane, messageCursor: 2, readerScroll: 17, query: "applied", selectedKey: "message-3"}
	context := interactionContext{folderCount: 1, messageCount: 4, selectedKey: "message-3", hasFolder: true, readerBoundsValid: true, readerPageRows: 8, readerMaxScroll: 40}

	state.Apply(interactionInput{key: "/", text: "/"}, context)
	state.Apply(interactionInput{key: "x", text: "x"}, context)
	state.Reconcile(interactionContext{folderCount: 1, messageCount: 1, selectedKey: "draft-result", hasFolder: true, readerBoundsValid: true, readerPageRows: 8, readerMaxScroll: 30})
	state.Apply(interactionInput{key: "esc"}, context)
	state.Reconcile(context)

	if state.mode != navigationMode || state.query != "applied" || state.focus != readerPane || state.messageCursor != 2 || state.readerScroll != 17 || state.selectedKey != "message-3" {
		t.Fatalf("search transaction was not restored: %#v", state)
	}
}

func TestInteractionModesAreExclusiveAndPickerOwnsKeys(t *testing.T) {
	state := interactionState{}
	context := interactionContext{folderCount: 1, messageCount: 1, selectedKey: "message", hasFolder: true, canPickAttachments: true, attachmentCount: 2}

	state.Apply(interactionInput{key: "/", text: "/"}, context)
	state.Apply(interactionInput{key: "o", text: "o"}, context)
	if state.mode != searchMode || state.query != "o" {
		t.Fatalf("search did not own text input: %#v", state)
	}
	state.Apply(interactionInput{key: "enter"}, context)
	state.Apply(interactionInput{key: "o", text: "o"}, context)
	if state.mode != attachmentsMode || state.focus != readerPane {
		t.Fatalf("attachment mode did not open exclusively: %#v", state)
	}
	state.Apply(interactionInput{key: "/", text: "/"}, context)
	if state.mode != attachmentsMode {
		t.Fatalf("attachment mode leaked into search: %#v", state)
	}
	state.Apply(interactionInput{key: "q", text: "q"}, context)
	if state.mode != navigationMode {
		t.Fatalf("picker q did not close the modal: %#v", state)
	}
}

func TestInteractionPagingClampsBeforeMoving(t *testing.T) {
	state := interactionState{focus: readerPane}
	context := interactionContext{readerBoundsValid: true, readerPageRows: 10, readerMaxScroll: 25}

	for range 5 {
		state.Apply(interactionInput{key: "pgdown"}, context)
	}
	if state.readerScroll != 25 {
		t.Fatalf("page down did not clamp at bottom: %d", state.readerScroll)
	}
	state.Apply(interactionInput{key: "pgup"}, context)
	if state.readerScroll != 15 {
		t.Fatalf("one page up from bottom = %d, want 15", state.readerScroll)
	}
}

func TestInteractionResizeReconcilesReaderBoundsBeforePaging(t *testing.T) {
	state := interactionState{focus: readerPane, readerScroll: 40}
	state.Reconcile(interactionContext{readerBoundsValid: true, readerPageRows: 5, readerMaxScroll: 18})
	if state.readerScroll != 18 {
		t.Fatalf("resize did not clamp reader offset: %d", state.readerScroll)
	}
	state.Apply(interactionInput{key: "pgup"}, interactionContext{readerBoundsValid: true, readerPageRows: 5, readerMaxScroll: 18})
	if state.readerScroll != 13 {
		t.Fatalf("paging did not consume resized geometry: %d", state.readerScroll)
	}
}

func TestInteractionClearingAppliedQueryResetsSelectionAndScroll(t *testing.T) {
	state := interactionState{focus: readerPane, query: "alice", messageCursor: 3, readerScroll: 22}
	outcome := state.Apply(interactionInput{key: "esc"}, interactionContext{messageCount: 5, readerMaxScroll: 40})
	if state.query != "" || state.messageCursor != 0 || state.readerScroll != 0 || outcome.messageRead != readImmediately {
		t.Fatalf("query clear did not reset dependent state: state=%#v outcome=%#v", state, outcome)
	}
}

func TestInteractionReconcileClosesInvalidPickerAndResetsChangedMessage(t *testing.T) {
	state := interactionState{
		mode: attachmentsMode, focus: readerPane, messageCursor: 4, attachmentCursor: 3,
		readerScroll: 19, selectedKey: "old", attachmentKey: "old",
	}
	state.Reconcile(interactionContext{
		folderCount: 1, messageCount: 2, selectedKey: "new", hasFolder: true,
		canPickAttachments: false, readerBoundsValid: true, readerPageRows: 8, readerMaxScroll: 12,
	})
	if state.mode != navigationMode || state.messageCursor != 1 || state.attachmentCursor != 0 || state.readerScroll != 0 || state.selectedKey != "new" {
		t.Fatalf("async data reconciliation left invalid interaction state: %#v", state)
	}
}

func TestInteractionReconcileClampsPickerForSameMessage(t *testing.T) {
	state := interactionState{
		mode: attachmentsMode, focus: readerPane, attachmentCursor: 4,
		selectedKey: "message", attachmentKey: "message",
	}
	state.Reconcile(interactionContext{
		messageCount: 1, selectedKey: "message", canPickAttachments: true, attachmentCount: 2,
	})
	if state.mode != attachmentsMode || state.attachmentCursor != 1 {
		t.Fatalf("picker was not clamped for the selected message: %#v", state)
	}
}

func TestInteractionFolderAndMessageMovesOwnResetRules(t *testing.T) {
	state := interactionState{query: "filter", messageCursor: 2, readerScroll: 9}
	outcome := state.Apply(interactionInput{key: "down"}, interactionContext{folderCount: 3, messageCount: 3})
	if state.folderCursor != 1 || state.query != "" || state.messageCursor != 0 || state.readerScroll != 0 || outcome.folderRead != readDeferred {
		t.Fatalf("folder move invariants = %#v / %#v", state, outcome)
	}

	state.focus = messagesPane
	state.readerScroll = 7
	outcome = state.Apply(interactionInput{key: "down"}, interactionContext{folderCount: 3, messageCount: 3})
	if state.messageCursor != 1 || state.readerScroll != 0 || outcome.messageRead != readDeferred {
		t.Fatalf("message move invariants = %#v / %#v", state, outcome)
	}
}

func TestInteractionPlainPreferencePersistsAcrossMessageReconcile(t *testing.T) {
	state := interactionState{focus: readerPane, selectedKey: "first"}
	outcome := state.Apply(interactionInput{key: "v"}, interactionContext{selectedKey: "first", hasRichBody: true})
	state.Reconcile(interactionContext{messageCount: 1, selectedKey: "second", hasRichBody: true, readerBoundsValid: true, readerMaxScroll: 10})
	if !state.preferPlain || outcome.notice != plainBodyNotice || state.readerScroll != 0 {
		t.Fatalf("plain preference did not survive selection change: %#v / %#v", state, outcome)
	}
}
