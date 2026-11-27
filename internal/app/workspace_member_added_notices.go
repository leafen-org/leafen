package app

import (
	"context"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Someone with an account whom a workspace's owner adds hears of it in that
// workspace's inbox, as Notion's added member does (seen 2026-11-12: "〈사람〉
// 다음에 추가됨 〈워크스페이스〉" with a blue dot, a new line for each addition,
// the one from an earlier addition kept under it). It names who added them
// and the workspace, and opens the workspace. It travels as Leafen's own
// system notice, with "workspace:<id>" where a page would be named, and shows
// while the person is in the workspace; its email is the added email
// (workspace_member_added_emails.go), not an activity email.

const (
	workspaceNoticePagePrefix          = "workspace:"
	workspaceMemberAddedActivityPrefix = "workspace-member-added:"
)

// notifyWorkspaceMemberAdded tells the person the actor added of it, once
// for this addition.
func notifyWorkspaceMemberAdded(app core.App, workspaceID, actorID, memberID string, now time.Time) error {
	if memberID == "" || memberID == actorID {
		return nil
	}
	var name string
	if err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_workspaces}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": workspaceID}).Row(&name); err != nil {
		return err
	}
	at := now.UnixMilli()
	stamp := strconv.FormatInt(at, 10)
	_, err := upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
		ID: appNoticeID("workspace-member-added", workspaceID, memberID, stamp), EventID: appNoticeID("workspace-member-added-event", workspaceID, memberID, stamp),
		WorkspaceID: workspaceID, UserID: memberID, ActivityKey: workspaceMemberAddedActivityPrefix + stamp,
		Kind: "system", PageID: workspaceNoticePagePrefix + workspaceID, ActorID: actorID, Title: name,
		Target: "/", OccurredAt: at, Personal: true,
	}}, at)
	return err
}

// workspaceNoticeVisible reports whether a person still sees a notice about a
// workspace: while they are in it.
func workspaceNoticeVisible(app core.App, userID, workspaceID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_workspaces}} [[workspace]]
		WHERE [[workspace]].[[id]]={:workspaceId} AND [[workspace]].[[deletion_job_id]]=''
			AND ([[workspace]].[[workspace_owner]]={:userId} OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
				WHERE [[membership]].[[workspace_id]]=[[workspace]].[[id]] AND [[membership]].[[user_id]]={:userId}))
	`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": userID}).Row(&count)
	return count > 0, err
}
