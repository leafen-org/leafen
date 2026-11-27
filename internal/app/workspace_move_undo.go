package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A move whose workspace didn't come in leaves nothing it made here: a
// failed import leaves the workspace empty, so once that is known the
// workspace takes the next move.

// workspaceMoveMade is what a move made here beside the workspace's pages:
// nothing now, but that a failed one was let go (moves before 2026-10-29
// brought bots, their desks, projects and chats too).
type workspaceMoveMade struct {
	// Undone says the move failed and was let go.
	Undone bool `json:"undone,omitzero"`
}

// workspaceMoveInKept is a move coming in as it is kept.
type workspaceMoveInKept struct {
	Made workspaceMoveMade `json:"made"`
}

// readWorkspaceMoveInKept reads a kept move.
func readWorkspaceMoveInKept(encoded string) workspaceMoveInKept {
	kept := workspaceMoveInKept{}
	_ = json.Unmarshal([]byte(encoded), &kept)
	return kept
}

// workspaceMoveJobFailed says whether a job's status ends the move it brings in.
func workspaceMoveJobFailed(status string) bool {
	return status == workspaceImportStatusFailed || status == workspaceImportStatusCancelled || status == durableJobStatusCanceled
}

// undoFailed lets a failed move go, once, and keeps that it did.
func (in *workspaceMoveIn) undoFailed(app core.App, slot *workspaceMoveInSlot) {
	in.mu.Lock()
	if slot.made.Undone {
		in.mu.Unlock()
		return
	}
	slot.made.Undone = true
	in.mu.Unlock()
	if err := keepWorkspaceMoveInSlot(app, slot); err != nil {
		app.Logger().Warn("workspace move: couldn't keep the failed move", "workspaceId", slot.workspaceID, "error", err)
	}
}
