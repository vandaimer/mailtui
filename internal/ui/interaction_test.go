package ui

import (
	"testing"

	"mailtui/internal/message"
)

func TestInteractionSearchCancelRestoresTransactionSnapshot(t *testing.T) {
	state := interactionState{focus: readerPane, readerScroll: 17, query: "applied", selectedPath: "message-3"}
	context := interactionContext{
		folderCount: 1, messages: projectionWithPaths("message-3", "message-1", "message-2", "message-3", "message-4"),
		hasFolder: true, readerBoundsValid: true, readerPageRows: 8, readerMaxScroll: 40,
	}

	state.Apply(interactionInput{key: "/", text: "/"}, context)
	state.Apply(interactionInput{key: "x", text: "x"}, context)
	state.Reconcile(interactionContext{
		folderCount: 1, messages: projectionWithPaths("draft-result", "draft-result"),
		hasFolder: true, readerBoundsValid: true, readerPageRows: 8, readerMaxScroll: 30,
	})
	state.Apply(interactionInput{key: "esc"}, context)
	state.Reconcile(context)

	if state.mode != navigationMode || state.query != "applied" || state.focus != readerPane || state.readerScroll != 17 || state.selectedPath != "message-3" {
		t.Fatalf("search transaction was not restored: %#v", state)
	}
}

func TestInteractionModesAreExclusiveAndPickerOwnsKeys(t *testing.T) {
	state := interactionState{}
	context := interactionContext{
		folderCount: 1, messages: projectionWithPaths("message", "message"), hasFolder: true,
		canPickAttachments: true, attachmentCount: 2,
	}

	state.Reconcile(context)
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

func TestInteractionClearingAppliedQueryPreservesSelectionAndScroll(t *testing.T) {
	state := interactionState{focus: readerPane, query: "alice", readerScroll: 22, selectedPath: "message"}
	context := interactionContext{messages: projectionWithPaths("message", "message"), readerMaxScroll: 40}
	outcome := state.Apply(interactionInput{key: "esc"}, context)
	if state.query != "" || state.selectedPath != "message" || state.readerScroll != 22 || outcome.messageRead != readImmediately {
		t.Fatalf("query clear changed selection: state=%#v outcome=%#v", state, outcome)
	}
}

func TestInteractionReconcileClosesInvalidPickerAndResetsChangedMessage(t *testing.T) {
	state := interactionState{
		mode: attachmentsMode, focus: readerPane, attachmentCursor: 3,
		readerScroll: 19, selectedPath: "old", attachmentKey: "old",
	}
	state.Reconcile(interactionContext{
		folderCount: 1, messages: projectionWithPaths("new", "first", "new"), hasFolder: true,
		canPickAttachments: false, readerBoundsValid: true, readerPageRows: 8, readerMaxScroll: 12,
	})
	if state.mode != navigationMode || state.attachmentCursor != 0 || state.readerScroll != 0 || state.selectedPath != "new" {
		t.Fatalf("async data reconciliation left invalid interaction state: %#v", state)
	}
}

func TestInteractionReconcileClampsPickerForSameMessage(t *testing.T) {
	state := interactionState{
		mode: attachmentsMode, focus: readerPane, attachmentCursor: 4,
		selectedPath: "message", attachmentKey: "message",
	}
	state.Reconcile(interactionContext{
		messages: projectionWithPaths("message", "message"), canPickAttachments: true, attachmentCount: 2,
	})
	if state.mode != attachmentsMode || state.attachmentCursor != 1 {
		t.Fatalf("picker was not clamped for the selected message: %#v", state)
	}
}

func TestInteractionFolderAndMessageMovesOwnResetRules(t *testing.T) {
	state := interactionState{query: "filter", readerScroll: 9, selectedPath: "message-3"}
	context := interactionContext{
		folderCount: 3,
		messages:    projectionWithPaths("message-3", "message-1", "message-2", "message-3"),
	}
	outcome := state.Apply(interactionInput{key: "down"}, context)
	if state.folderCursor != 1 || state.query != "" || state.selectedPath != "" || state.readerScroll != 0 || outcome.folderRead != readDeferred {
		t.Fatalf("folder move invariants = %#v / %#v", state, outcome)
	}

	state.focus = messagesPane
	state.readerScroll = 7
	context.messages = projectionWithPaths("message-1", "message-1", "message-2", "message-3")
	state.Reconcile(context)
	outcome = state.Apply(interactionInput{key: "down"}, context)
	if state.selectedPath != "message-2" || state.readerScroll != 0 || outcome.messageRead != readDeferred {
		t.Fatalf("message move invariants = %#v / %#v", state, outcome)
	}
}

func TestInteractionMessageNavigationClampsAtProjectionEdges(t *testing.T) {
	state := interactionState{focus: messagesPane, selectedPath: "last"}
	context := interactionContext{messages: projectionWithPaths("last", "first", "last")}
	outcome := state.Apply(interactionInput{key: "down"}, context)
	if state.selectedPath != "last" || outcome.messageRead != noRead {
		t.Fatalf("navigation moved past last message: %#v / %#v", state, outcome)
	}
}

func TestInteractionPlainPreferencePersistsAcrossMessageReconcile(t *testing.T) {
	state := interactionState{focus: readerPane, selectedPath: "first"}
	first := interactionContext{messages: projectionWithPaths("first", "first"), hasRichBody: true}
	outcome := state.Apply(interactionInput{key: "v"}, first)
	state.Reconcile(interactionContext{
		messages: projectionWithPaths("second", "second"), hasRichBody: true,
		readerBoundsValid: true, readerMaxScroll: 10,
	})
	if !state.preferPlain || outcome.notice != plainBodyNotice || state.readerScroll != 0 {
		t.Fatalf("plain preference did not survive selection change: %#v / %#v", state, outcome)
	}
}

func projectionWithPaths(selectedPath string, paths ...string) messageProjection {
	messages := make([]message.Message, len(paths))
	for index, path := range paths {
		messages[index].Path = path
	}
	return projectMessages(messages, "", selectedPath)
}
