package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A private page whose person has left the workspace keeps the shares it
// had, so no one may hold it in full any more and no one can change who has
// it. Notion's owners take such pages over from their content search; here
// a server administrator gives the workspace's owner full access to it, as
// the owner's own act, kept in the workspace's record of what server
// administrators did. Nobody else's access changes.

// administratorContentTakeOverSQL marks such a page in the content list: a
// private page of someone who is no longer a member, which the owner does
// not hold in full yet.
const administratorContentTakeOverSQL = `(` + administratorContentPrivateOwnerSQL + ` != ''
	AND [[page]].[[private_owner_id]] != {:personId}
	AND NOT EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
		WHERE [[member]].[[workspace_id]] = {:workspaceId} AND [[member]].[[user_id]] = [[page]].[[private_owner_id]])
	AND NOT EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[held]]
		WHERE [[held]].[[page_id]] = [[page]].[[id]] AND [[held]].[[principal_type]] = 'user'
			AND [[held]].[[principal_id]] = {:personId} AND [[held]].[[role]] = 'full_access'))`

// administratorContentPrivateOwnerSQL is whose private page a page is in the
// content list. People's database and its people's pages are the server's own
// (people_database.go), private to Leafen's author: the workspace's, no one's
// private page, so no one left them to be taken over (2026-11-16: People
// stood under "알 수 없는 사용자의 개인 페이지").
const administratorContentPrivateOwnerSQL = `(CASE WHEN [[page]].[[private_owner_id]] = '` + leafenSystemAuthorID +
	`' THEN '' ELSE COALESCE([[page]].[[private_owner_id]], '') END)`

var errAdministratorContentTakeOverRefused = errors.New("administrator content take-over refused")

// takeOverAdministratorWorkspacePage gives the workspace's owner full access
// to a private page whose person has left.
func takeOverAdministratorWorkspacePage(event *core.RequestEvent) error {
	actorID, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	page, ok := administratorContentPage(event, workspace)
	if !ok {
		return nil
	}
	change := func(txApp core.App) error {
		eligible := false
		if err := txApp.DB().NewQuery(`SELECT ` + administratorContentTakeOverSQL + ` FROM {{leafen_pages}} [[page]]
			WHERE [[page]].[[id]] = {:pageId} AND [[page]].[[in_trash]] = FALSE`).Bind(dbx.Params{
			"pageId": page.Id, "workspaceId": workspace.ID, "personId": workspace.PersonID,
		}).Row(&eligible); err != nil {
			return err
		}
		if !eligible {
			return errAdministratorContentTakeOverRefused
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_page_permissions}}
				([[id]],[[workspace_id]],[[page_id]],[[principal_type]],[[principal_id]],[[role]],
				 [[created_by]],[[created_at]],[[updated_at]],[[invite_message]],[[notify_on_access]])
			VALUES ({:id},{:workspaceId},{:pageId},{:principalType},{:personId},{:role},{:personId},{:now},{:now},'',FALSE)
			ON CONFLICT ([[page_id]],[[principal_type]],[[principal_id]]) DO UPDATE SET
				[[role]]=excluded.[[role]],[[updated_at]]=excluded.[[updated_at]],[[rule_id]]=''
		`).Bind(dbx.Params{
			"id": securityRandomID(), "workspaceId": workspace.ID, "pageId": page.Id,
			"principalType": publicMutationPrincipalUser, "personId": workspace.PersonID,
			"role": publicMutationRoleFullAccess, "now": now,
		}).Execute(); err != nil {
			return err
		}
		return recordAdministratorContentEvent(txApp, workspace.ID, actorID, administratorContentTakeOverPage,
			"page", page.Id, workspace.titleAsRead(txApp, page.Id, page.GetString("title")))
	}
	// A key's call asks its person only for a take-over that would be made
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if errors.Is(err, errAdministratorContentTakeOverRefused) {
		return apiError(event, http.StatusConflict, errorCodeInvalidInput,
			"Only a private page of someone who left the workspace is taken over.")
	}
	if err != nil {
		return administratorContentError(event, err, "take-over")
	}
	return event.JSON(http.StatusOK, administratorWorkspaceActionResult{Status: "takenOver"})
}
