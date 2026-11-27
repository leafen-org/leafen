package app

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's move goes on when a server it goes between starts again
// (2026-10-23: the sending server started again while the new one brought
// the workspace in, and the workspace stayed sending and reading only, the
// move done there). Once the move is handed over, the old server keeps its
// code, so it asks the new one again how bringing it in goes and ends the
// move moved or failed; the new server keeps the move it brings in — its
// code's secret by its SHA-256 alone — so it still answers. A move that
// stopped before it was handed over fails, its fence lifted: the new server
// brought nothing in. The old server's code goes when its move fails, and
// stays once it is moved, to ask what is public there
// (workspace_move_follow.go); neither goes in an instance's export.

// keepWorkspaceMoveOutCode keeps a move's code as it is handed over.
func keepWorkspaceMoveOutCode(app core.App, workspaceID string, code serverMoveCode, origin string) error {
	encoded, err := json.Marshal(code)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`INSERT OR REPLACE INTO {{leafen_workspace_move_out_codes}}
		([[workspace_id]], [[code_json]], [[origin]], [[created_at]]) VALUES ({:workspaceId}, {:code}, {:origin}, {:now})`).
		Bind(dbx.Params{"workspaceId": workspaceID, "code": string(encoded), "origin": origin, "now": time.Now().UnixMilli()}).Execute()
	return err
}

func forgetWorkspaceMoveOutCode(app core.App, workspaceID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_workspace_move_out_codes}} WHERE [[workspace_id]] = {:workspaceId}`).
		Bind(dbx.Params{"workspaceId": workspaceID}).Execute()
	return err
}

// resume ends each move that was being sent when this server stopped: one
// handed over asks the new server again, one that wasn't fails.
func (out *workspaceMoveOut) resume(context.Context) {
	app := out.app
	rows := []struct {
		WorkspaceID string         `db:"workspace_id"`
		ActorID     string         `db:"actor_id"`
		MovedTo     string         `db:"moved_to"`
		Step        string         `db:"step"`
		Code        sql.NullString `db:"code_json"`
		Origin      sql.NullString `db:"origin"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[move]].[[workspace_id]], [[move]].[[actor_id]], [[move]].[[moved_to]], [[move]].[[step]],
			[[code]].[[code_json]], [[code]].[[origin]]
		FROM {{leafen_workspace_moves_out}} [[move]]
		LEFT JOIN {{leafen_workspace_move_out_codes}} [[code]] ON [[code]].[[workspace_id]] = [[move]].[[workspace_id]]
		WHERE [[move]].[[state]] = {:sending}`).Bind(dbx.Params{"sending": workspaceMoveSending}).All(&rows); err != nil {
		app.Logger().Warn("workspace move: couldn't read the moves under way", "error", err)
		return
	}
	for _, row := range rows {
		code := serverMoveCode{}
		if row.Step != "bring-in" || !row.Code.Valid || !row.Origin.Valid || json.Unmarshal([]byte(row.Code.String), &code) != nil {
			app.Logger().Warn("workspace move: it stopped as this server did", "workspaceId", row.WorkspaceID, "step", row.Step)
			_ = setWorkspaceMove(app, row.WorkspaceID, row.ActorID, row.MovedTo, workspaceMoveFailed, row.Step, errorCodeWorkspaceMoveFailed, nil)
			continue
		}
		out.mu.Lock()
		out.busy[row.WorkspaceID] = true
		out.mu.Unlock()
		workspaceID, actorID, origin := row.WorkspaceID, row.ActorID, row.Origin.String
		if !serverBackgroundOf(app).run("workspace move", func(ctx context.Context) {
			defer out.done(workspaceID)
			out.awaitBringIn(ctx, workspaceID, actorID, code, origin)
		}) {
			out.done(workspaceID)
		}
	}
}

func workspaceMoveSecretSHA256(secret string) string {
	digest := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(digest[:])
}

// holdsSecret says whether a secret is the slot's code's.
func (slot *workspaceMoveInSlot) holdsSecret(secret string) bool {
	return subtle.ConstantTimeCompare([]byte(workspaceMoveSecretSHA256(secret)), []byte(slot.secretSHA256)) == 1
}

// keepWorkspaceMoveInSlot keeps a move this server brings in.
func keepWorkspaceMoveInSlot(app core.App, slot *workspaceMoveInSlot) error {
	spaces, err := json.Marshal(workspaceMoveInKept{Made: slot.made})
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`INSERT OR REPLACE INTO {{leafen_workspace_move_in_slots}}
		([[workspace_id]], [[code_id]], [[secret_sha256]], [[expires]], [[owner_id]], [[job_id]], [[spaces_json]], [[created_at]])
		VALUES ({:workspaceId}, {:codeId}, {:secret}, {:expires}, {:ownerId}, {:jobId}, {:spaces}, {:now})`).
		Bind(dbx.Params{"workspaceId": slot.workspaceID, "codeId": slot.code.ID, "secret": slot.secretSHA256, "expires": slot.code.Expires,
			"ownerId": slot.ownerID, "jobId": slot.jobID, "spaces": string(spaces), "now": time.Now().UnixMilli()}).Execute()
	return err
}

func forgetWorkspaceMoveInSlot(app core.App, workspaceID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_workspace_move_in_slots}} WHERE [[workspace_id]] = {:workspaceId}`).
		Bind(dbx.Params{"workspaceId": workspaceID}).Execute()
	return err
}

// restore takes back the moves this server was bringing in when it stopped.
func (in *workspaceMoveIn) restore() {
	rows := []struct {
		WorkspaceID string `db:"workspace_id"`
		CodeID      string `db:"code_id"`
		Secret      string `db:"secret_sha256"`
		Expires     int64  `db:"expires"`
		OwnerID     string `db:"owner_id"`
		JobID       string `db:"job_id"`
		Spaces      string `db:"spaces_json"`
	}{}
	if err := in.app.DB().NewQuery(`SELECT [[workspace_id]], [[code_id]], [[secret_sha256]], [[expires]], [[owner_id]], [[job_id]], [[spaces_json]]
		FROM {{leafen_workspace_move_in_slots}}`).All(&rows); err != nil {
		in.app.Logger().Warn("workspace move: couldn't read the moves coming in", "error", err)
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, row := range rows {
		kept := readWorkspaceMoveInKept(row.Spaces)
		in.slots[row.WorkspaceID] = &workspaceMoveInSlot{code: serverMoveCode{ID: row.CodeID, Expires: row.Expires}, secretSHA256: row.Secret,
			workspaceID: row.WorkspaceID, ownerID: row.OwnerID, state: workspaceMoveInImporting, jobID: row.JobID, made: kept.Made}
	}
}
