package app

import (
	"context"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func readTeamspaceManagementState(
	ctx context.Context, app core.App, principalID, workspaceID, teamspaceID,
	memberRaw, requestRaw, principalRaw string,
) (teamspaceManagementResponse, error) {
	memberCursor, err := decodeTeamspaceCursor(memberRaw)
	if err != nil {
		return teamspaceManagementResponse{}, err
	}
	requestCursor, err := decodeTeamspaceCursor(requestRaw)
	if err != nil {
		return teamspaceManagementResponse{}, err
	}
	principalCursor, err := decodeTeamspaceCursor(principalRaw)
	if err != nil {
		return teamspaceManagementResponse{}, err
	}
	response := teamspaceManagementResponse{
		WorkspaceID: workspaceID, Members: []teamspaceMemberWire{},
		Requests: []teamspaceJoinRequestWire{}, PrincipalOptions: []teamspacePrincipalOption{},
	}
	err = runReadSnapshot(ctx, app, func(txApp core.App) error {
		// Who is in it is seen by everyone who sees the teamspace — its
		// members, and someone outside an open or closed one, come by the
		// library's "멤버 보기", as Notion's settings ask its server for the
		// team's members whoever opens them (useTeamMembersListSource); join
		// requests are for those who decide them (managesTeamspace), not a
		// restricted owner, who answers none (decision.MembersLocked). The
		// workspace's owner sees any teamspace's, a private one they are not
		// in too, as Notion's Enterprise owner's "멤버 보기" from its settings
		// table (readTeamspaceTableSnapshotState).
		authority, teamspace, decision, err := authorizeTeamspace(txApp, principalID, workspaceID, teamspaceID, false)
		if err != nil {
			return err
		}
		manages, err := managesTeamspace(txApp, authority, teamspaceID, decision)
		if err != nil {
			return err
		}
		if !manages && !decision.Joined && !decision.Visible && authority.Workspace.Role != workspaceRoleOwner {
			return errTeamspaceNotFound
		}
		directoryAuthority, err := readWorkspacePrincipalDirectoryAuthority(ctx, txApp, workspaceID)
		if err != nil {
			return err
		}
		revision := directoryAuthority.PermissionRevision
		groupRevision := directoryAuthority.GroupRevision
		if memberCursor.Version != 0 && memberCursor.Revision != revision ||
			requestCursor.Version != 0 && requestCursor.Revision != revision ||
			principalCursor.Version != 0 && (principalCursor.Revision != revision ||
				principalCursor.AuxRevision != groupRevision ||
				principalCursor.Updated != directoryAuthority.PersonRevision) {
			return errTeamspaceConflict
		}
		members := []storedTeamspaceMember{}
		err = txApp.DB().NewQuery(`
			SELECT [[id]],[[workspace_id]],[[teamspace_id]],[[principal_type]],[[principal_id]],
				[[workspace_member_id]],[[role]],[[revision]],[[created_by]],[[created_at]],[[updated_at]]
			FROM {{leafen_teamspace_members}} WHERE [[workspace_id]]={:workspaceId}
				AND [[teamspace_id]]={:teamspaceId} AND [[deleted_at]]=0 AND [[id]]>{:afterId}
			ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": workspaceID, "teamspaceId": teamspaceID,
			"afterId": memberCursor.ID, "limit": teamspaceManagementWindow + 1,
		}).All(&members)
		if err != nil {
			return err
		}
		// A request from an account that can't act now waits unlisted
		// (teamspace_join_request_respond.go).
		requests := []storedTeamspaceJoinRequest{}
		if manages && !decision.MembersLocked {
			err = txApp.DB().NewQuery(`
			SELECT ` + teamspaceJoinRequestColumns + `
			FROM {{leafen_teamspace_join_requests}} [[request]] WHERE [[workspace_id]]={:workspaceId}
				AND [[teamspace_id]]={:teamspaceId} AND [[status]]='pending' AND [[id]]>{:afterId}
				AND ` + accountMayActSQL("[[request]].[[user_id]]") + `
			ORDER BY [[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
				"workspaceId": workspaceID, "teamspaceId": teamspaceID,
				"afterId": requestCursor.ID, "limit": teamspaceManagementWindow + 1,
			}).All(&requests)
		}
		if err != nil {
			return err
		}
		options, err := loadWorkspacePrincipalOptions(
			ctx, txApp, workspaceID, principalID, principalCursor.ID, teamspaceManagementWindow+1,
		)
		if err != nil {
			if errors.Is(err, errWorkspacePrincipalDirectoryInvalid) {
				return errTeamspaceCursorInvalid
			}
			return err
		}
		response.SnapshotRevision, response.Teamspace, response.Decision = revision, projectTeamspace(teamspace), decision
		response.MembersHaveMore = len(members) > teamspaceManagementWindow
		response.RequestsHaveMore = len(requests) > teamspaceManagementWindow
		response.PrincipalOptionsHaveMore = len(options) > teamspaceManagementWindow
		if response.MembersHaveMore {
			members = members[:teamspaceManagementWindow]
		}
		if response.RequestsHaveMore {
			requests = requests[:teamspaceManagementWindow]
		}
		if response.PrincipalOptionsHaveMore {
			options = options[:teamspaceManagementWindow]
		}
		for _, member := range members {
			response.Members = append(response.Members, projectTeamspaceMember(member))
		}
		for _, request := range requests {
			response.Requests = append(response.Requests, projectTeamspaceJoinRequest(request))
		}
		// A restricted member is marked, as Notion's yellow "제한됨".
		restricted, err := workspaceRestrictedMembers(txApp, workspaceID)
		if err != nil {
			return err
		}
		for _, option := range options {
			response.PrincipalOptions = append(response.PrincipalOptions, teamspacePrincipalOption{
				PrincipalType: option.PrincipalType, PrincipalID: option.PrincipalID, Label: option.Label,
				Email: option.Email, WorkspaceOwner: option.WorkspaceOwner,
				Restricted: option.PrincipalType == "user" && restricted[option.PrincipalID],
			})
		}
		if response.MembersHaveMore {
			response.NextMemberCursor = encodeTeamspaceCursor(teamspaceCursor{Version: teamspaceSchemaVersion, Revision: revision, ID: members[len(members)-1].ID})
		}
		if response.RequestsHaveMore {
			response.NextRequestCursor = encodeTeamspaceCursor(teamspaceCursor{Version: teamspaceSchemaVersion, Revision: revision, ID: requests[len(requests)-1].ID})
		}
		if response.PrincipalOptionsHaveMore {
			last := options[len(options)-1]
			response.NextPrincipalCursor = encodeTeamspaceCursor(teamspaceCursor{
				Version: teamspaceSchemaVersion, Revision: revision,
				AuxRevision: groupRevision, Updated: directoryAuthority.PersonRevision,
				ID: last.PrincipalType + ":" + last.PrincipalID,
			})
		}
		return nil
	})
	return response, err
}
