package app

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A closed teamspace's join request reaches those who answer it — its
// owners — in their inbox and by mail, as a page's access request reaches its
// approvers (2026-10-16: it showed only in the teamspace's settings, on the
// members tab). The notice opens those settings at the members tab, where it
// is answered. The person who asked hears when it is approved. They travel as
// Leafen's own system notices, with "teamspace:<id>" where a page would be
// named, and show while the teamspace is there for the person.

const (
	teamspaceNoticePagePrefix           = "teamspace:"
	teamspaceJoinRequestActivityPrefix  = "teamspace-join-request:"
	teamspaceJoinApprovedActivityPrefix = "teamspace-join-approved:"
)

// teamspaceSettingsTarget opens a teamspace's settings, at a tab when named
// (the web's sidebar reads it).
func teamspaceSettingsTarget(teamspaceID, tab string) string {
	values := url.Values{"teamspace-settings": {teamspaceID}}
	if tab != "" {
		values.Set("teamspace-tab", tab)
	}
	return "/?" + values.Encode()
}

// managesTeamspace says whether someone changes a teamspace's settings,
// members and roles and answers its join requests: its owners, as Notion's
// settings give a workspace owner who is only its member its member's window
// (seen 2026-11-18); a workspace owner stands in only for a teamspace with no
// owner who can answer — none at all, or only restricted ones, who move no
// one (decision.MembersLocked), or ones whose accounts can't act now — as
// its join requests reach them then (teamspaceJoinRequestApprovers), as
// Notion's workspace owners "may assign a new teamspace owner" to one with
// none (its help, Manage teamspaces).
func managesTeamspace(
	app core.App, authority teamspaceAuthority, teamspaceID string, decision teamspaceAccessDecisionWire,
) (bool, error) {
	if decision.MembershipRole == teamspaceMemberRoleOwner {
		return true, nil
	}
	if authority.Workspace.Role != workspaceRoleOwner {
		return false, nil
	}
	approvers, err := teamspaceJoinRequestApprovers(app, authority.Workspace.WorkspaceID, teamspaceID, "")
	return err == nil && slices.Contains(approvers, authority.Workspace.PrincipalID), err
}

// requireTeamspaceManager refuses, as a teamspace not found, someone who
// doesn't manage it (managesTeamspace).
func requireTeamspaceManager(
	app core.App, authority teamspaceAuthority, teamspaceID string, decision teamspaceAccessDecisionWire,
) error {
	manages, err := managesTeamspace(app, authority, teamspaceID, decision)
	if err == nil && !manages {
		err = errTeamspaceNotFound
	}
	return err
}

// teamspaceJoinRequestApprovers are those who answer the teamspace's join
// requests (teamspaceAnsweringOwnersSQL), other than the person asking; with
// none, the workspace's owner, who may answer too (managesTeamspace). A
// restricted owner, whom the answer is refused (decision.MembersLocked),
// hears of none.
func teamspaceJoinRequestApprovers(app core.App, workspaceID, teamspaceID, requesterID string) ([]string, error) {
	owners := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[user_id]] FROM (` + teamspaceAnsweringOwnersSQL("={:teamspaceId}") + `)
		WHERE [[user_id]]!={:requester} ORDER BY 1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "teamspaceId": teamspaceID, "requester": requesterID}).Column(&owners); err != nil {
		return nil, err
	}
	if len(owners) > 0 {
		return owners, nil
	}
	if err := app.DB().NewQuery(`SELECT [[workspace_owner]] FROM {{leafen_workspaces}} WHERE [[id]]={:workspaceId} AND [[workspace_owner]]!={:requester}`).
		Bind(dbx.Params{"workspaceId": workspaceID, "requester": requesterID}).Column(&owners); err != nil {
		return nil, err
	}
	return owners, nil
}

// teamspaceAnsweringOwnersSQL lists, as (teamspace_id, user_id), those who
// answer join requests to the teamspaces of the workspace {:workspaceId}
// whose id meets the condition ("={:teamspaceId}", "IN (…)"), and move
// people in and out of them: their owners, by account or through a group,
// but a restricted member (decision.MembersLocked) and an account that
// can't act now (accountMayAct). The screens read their count
// (teamspaceSummaryWire.AnsweringOwnerCount) for where the workspace's
// owner stands in.
func teamspaceAnsweringOwnersSQL(teamspaceCondition string) string {
	return `
		SELECT [[member]].[[teamspace_id]] AS [[teamspace_id]],[[member]].[[principal_id]] AS [[user_id]]
		FROM {{leafen_teamspace_members}} [[member]]
		WHERE [[member]].[[teamspace_id]]` + teamspaceCondition + ` AND [[member]].[[principal_type]]='user'
			AND [[member]].[[role]]='owner' AND [[member]].[[deleted_at]]=0
			AND NOT ` + workspaceRestrictedMemberSQL("{:workspaceId}", "[[member]].[[principal_id]]") + `
			AND ` + accountMayActSQL("[[member]].[[principal_id]]") + `
		UNION
		SELECT [[member]].[[teamspace_id]],[[group_member]].[[user_id]]
		FROM {{leafen_teamspace_members}} [[member]]
		JOIN {{leafen_workspace_permission_groups}} [[group]]
			ON [[group]].[[id]]=[[member]].[[principal_id]] AND [[group]].[[workspace_id]]={:workspaceId} AND [[group]].[[deleted_at]] IS NULL
		JOIN {{leafen_workspace_permission_group_members}} [[group_member]] ON [[group_member]].[[group_id]]=[[group]].[[id]]
		WHERE [[member]].[[teamspace_id]]` + teamspaceCondition + ` AND [[member]].[[principal_type]]='group'
			AND [[member]].[[role]]='owner' AND [[member]].[[deleted_at]]=0
			AND NOT ` + workspaceRestrictedMemberSQL("{:workspaceId}", "[[group_member]].[[user_id]]") + `
			AND ` + accountMayActSQL("[[group_member]].[[user_id]]")
}

// notifyTeamspaceJoinRequest tells the teamspace's owners of a request, with
// its message under it in gray when it says why; one asked again after a
// refusal tells them again.
func notifyTeamspaceJoinRequest(app core.App, teamspace storedTeamspace, request teamspaceJoinRequestWire) error {
	approvers, err := teamspaceJoinRequestApprovers(app, teamspace.WorkspaceID, teamspace.ID, request.UserID)
	if err != nil || len(approvers) == 0 {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	revision := strconv.Itoa(request.Revision)
	projections := make([]productNotificationProjection, 0, len(approvers))
	for _, userID := range approvers {
		projections = append(projections, productNotificationProjection{
			ID: appNoticeID("teamspace-join-request", request.ID, revision, userID), EventID: appNoticeID("teamspace-join-request-event", request.ID, revision),
			WorkspaceID: teamspace.WorkspaceID, UserID: userID, ActivityKey: teamspaceJoinRequestActivityPrefix + request.ID,
			Kind: "system", PageID: teamspaceNoticePagePrefix + teamspace.ID, ActorID: request.UserID, Title: teamspace.Name,
			Preview: request.Message, Target: teamspaceSettingsTarget(teamspace.ID, "members"), OccurredAt: now, Personal: true,
		})
	}
	for start := 0; start < len(projections); start += databaseRowNotificationBatch {
		if _, err := upsertProductNotifications(context.Background(), app, projections[start:min(start+databaseRowNotificationBatch, len(projections))], now); err != nil {
			return err
		}
	}
	return nil
}

// notifyTeamspaceJoinApproved tells the person who asked that they are in.
func notifyTeamspaceJoinApproved(app core.App, teamspace storedTeamspace, request storedTeamspaceJoinRequest, deciderID string) error {
	if request.UserID == "" || request.UserID == deciderID {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	revision := strconv.Itoa(request.Revision)
	_, err := upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID: appNoticeID("teamspace-join-approved", request.ID, revision), EventID: appNoticeID("teamspace-join-approved-event", request.ID, revision),
		WorkspaceID: teamspace.WorkspaceID, UserID: request.UserID, ActivityKey: teamspaceJoinApprovedActivityPrefix + request.ID,
		Kind: "system", PageID: teamspaceNoticePagePrefix + teamspace.ID, ActorID: deciderID, Title: teamspace.Name,
		Target: teamspaceSettingsTarget(teamspace.ID, ""), OccurredAt: now, Personal: true,
	}}, now)
	return err
}

// teamspaceNoticeVisible reports whether a person still sees notices about a
// teamspace: while it is there and they belong to its workspace.
func teamspaceNoticeVisible(app core.App, userID, teamspaceID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_teamspaces}} [[teamspace]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]=[[teamspace]].[[workspace_id]]
		WHERE [[teamspace]].[[id]]={:teamspaceId} AND [[teamspace]].[[deleted_at]]=0
			AND ([[workspace]].[[workspace_owner]]={:userId} OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
				WHERE [[membership]].[[workspace_id]]=[[workspace]].[[id]] AND [[membership]].[[user_id]]={:userId}))
	`).Bind(dbx.Params{"teamspaceId": teamspaceID, "userId": userID}).Row(&count)
	return count > 0, err
}
