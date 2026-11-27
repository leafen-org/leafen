package app

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace moving to another Leafen server (workspace_move_out.go) takes
// no more changes from the moment it starts to be sent, so the copy holds
// everything, and keeps none once it has moved: it stays, reading only,
// pointing to its new place, until its owner deletes it. Its owner may still
// do what owners do — delete it, export it, or move it again if sending
// failed. The fence is checked where every change is allowed
// (decideWorkspaceAccess), from what is kept in memory.

const (
	workspaceMoveSending = "sending"
	workspaceMoveMoved   = "moved"
	workspaceMoveFailed  = "failed"
)

// workspaceMoveFences are the workspaces sending or moved, by their new place.
var workspaceMoveFences = struct {
	sync.RWMutex
	loaded bool
	to     map[string]string
}{to: map[string]string{}}

// workspaceMovedTo is where a workspace that takes no changes went, or false.
func workspaceMovedTo(app core.App, workspaceID string) (string, bool) {
	workspaceMoveFences.RLock()
	if workspaceMoveFences.loaded {
		to, moved := workspaceMoveFences.to[workspaceID]
		workspaceMoveFences.RUnlock()
		return to, moved
	}
	workspaceMoveFences.RUnlock()
	workspaceMoveFences.Lock()
	defer workspaceMoveFences.Unlock()
	if !workspaceMoveFences.loaded {
		rows := []struct {
			WorkspaceID string `db:"workspace_id"`
			MovedTo     string `db:"moved_to"`
		}{}
		// Before the table is made (an old data folder starting), nothing is fenced.
		if err := app.DB().NewQuery(`SELECT [[workspace_id]], [[moved_to]] FROM {{leafen_workspace_moves_out}}
			WHERE [[state]] IN ('sending','moved')`).All(&rows); err != nil {
			return "", false
		}
		for _, row := range rows {
			workspaceMoveFences.to[row.WorkspaceID] = row.MovedTo
		}
		workspaceMoveFences.loaded = true
	}
	to, moved := workspaceMoveFences.to[workspaceID]
	return to, moved
}

// workspaceMoveFence refuses more than reading a page of a workspace that is
// moving or moved to another server, whatever the page grants: a workspace's
// writes, its pages' (comments and edits) and its rows' are decided here.
func workspaceMoveFence(app core.App, workspaceID string, minimum pageAccessRole) error {
	if minimum <= pageAccessRoleView {
		return nil
	}
	if _, moved := workspaceMovedTo(app, workspaceID); moved {
		return &permissionError{code: errorCodePermissionDenied, reason: permissionReasonWorkspaceMoved}
	}
	return nil
}

// setWorkspaceMove keeps a move's state, fencing the workspace while it is
// being sent and once it has moved, and lifting the fence when sending failed.
func setWorkspaceMove(app core.App, workspaceID, actorID, movedTo, state, step, errorCode string, report any) error {
	encoded := "{}"
	if report != nil {
		raw, err := json.Marshal(report)
		if err != nil {
			return err
		}
		encoded = string(raw)
	}
	now := time.Now().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_moves_out}} ([[workspace_id]], [[actor_id]], [[moved_to]], [[state]], [[step]], [[error_code]],
			[[report_json]], [[started_at]], [[updated_at]])
		VALUES ({:workspaceId}, {:actorId}, {:movedTo}, {:state}, {:step}, {:errorCode}, {:report}, {:now}, {:now})
		ON CONFLICT ([[workspace_id]]) DO UPDATE SET [[actor_id]] = excluded.[[actor_id]], [[moved_to]] = excluded.[[moved_to]],
			[[state]] = excluded.[[state]], [[step]] = excluded.[[step]], [[error_code]] = excluded.[[error_code]],
			[[report_json]] = excluded.[[report_json]],
			[[started_at]] = CASE WHEN excluded.[[state]] = 'sending' AND [[leafen_workspace_moves_out]].[[state]] != 'sending'
				THEN excluded.[[started_at]] ELSE [[leafen_workspace_moves_out]].[[started_at]] END,
			[[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "actorId": actorID, "movedTo": movedTo, "state": state, "step": step,
		"errorCode": errorCode, "report": encoded, "now": now,
	}).Execute(); err != nil {
		return err
	}
	// A failed move needs its code no more (workspace_move_resume.go); a
	// moved one keeps it to ask what is public there (workspace_move_follow.go).
	if state == workspaceMoveFailed {
		_ = forgetWorkspaceMoveOutCode(app, workspaceID)
	}
	workspaceMovedTo(app, workspaceID)
	workspaceMoveFences.Lock()
	if state == workspaceMoveFailed {
		delete(workspaceMoveFences.to, workspaceID)
	} else {
		workspaceMoveFences.to[workspaceID] = movedTo
	}
	workspaceMoveFences.Unlock()
	// Open editors of the workspace reopen reading only, or writing again.
	recheckCollaborationAccessAfterCommit(app)
	return nil
}
