package app

import (
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Asking to join a closed teamspace (action request): the request waits for
// the teamspace's owners, with why when the person says
// (schema_baseline.json).

// teamspaceJoinRequestColumns are a request's columns, as storedTeamspaceJoinRequest reads them.
const teamspaceJoinRequestColumns = `[[id]],[[workspace_id]],[[teamspace_id]],[[user_id]],[[workspace_member_id]],[[status]],
	[[revision]],[[created_by]],[[decided_by]],[[decided_at]],[[created_at]],[[updated_at]],[[message]]`

// teamspaceJoinMessage is a request's message as kept: trimmed, at most
// maxTeamspaceJoinMessageRunes long, and valid text.
func teamspaceJoinMessage(raw string) (string, bool) {
	message := strings.TrimSpace(raw)
	return message, utf8.ValidString(message) && utf8.RuneCountInString(message) <= maxTeamspaceJoinMessageRunes
}

// createTeamspaceJoinRequest asks to join, with why when the person says
// (one asked again after a refusal says the new message, or none).
func createTeamspaceJoinRequest(
	app core.App, authority teamspaceAuthority, teamspaceID, message string, now int64,
) (*teamspaceJoinRequestWire, error) {
	row := storedTeamspaceJoinRequest{
		ID: security.RandomString(15), WorkspaceID: authority.Workspace.WorkspaceID, TeamspaceID: teamspaceID,
		UserID: authority.Workspace.PrincipalID, WorkspaceMemberID: authority.Workspace.PrincipalID,
		Status: teamspaceRequestPending, Revision: 1, CreatedBy: authority.Workspace.PrincipalID,
		CreatedAt: now, UpdatedAt: now, Message: message,
	}
	err := app.DB().NewQuery(`
		INSERT INTO {{leafen_teamspace_join_requests}}
			([[id]],[[workspace_id]],[[teamspace_id]],[[user_id]],[[workspace_member_id]],[[status]],
			 [[revision]],[[created_by]],[[created_at]],[[updated_at]],[[message]])
		VALUES ({:id},{:workspaceId},{:teamspaceId},{:userId},{:memberId},'pending',1,{:userId},{:now},{:now},{:message})
		ON CONFLICT ([[teamspace_id]],[[user_id]]) DO UPDATE SET
			[[workspace_member_id]]=excluded.[[workspace_member_id]],[[status]]='pending',
			[[revision]]=[[revision]]+1,[[decided_by]]='',[[decided_at]]=0,[[updated_at]]=excluded.[[updated_at]],
			[[message]]=excluded.[[message]]
		WHERE [[status]]='denied'
		RETURNING ` + teamspaceJoinRequestColumns + `
	`).Bind(dbx.Params{
		"id": row.ID, "workspaceId": row.WorkspaceID, "teamspaceId": row.TeamspaceID,
		"userId": row.UserID, "memberId": row.WorkspaceMemberID, "now": now, "message": message,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, teamspaceStateRefused("your request to join this teamspace is waiting for its owners already, or was approved")
	}
	value := projectTeamspaceJoinRequest(row)
	return &value, err
}
