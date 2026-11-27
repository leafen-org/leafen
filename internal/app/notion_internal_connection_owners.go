package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// An internal connection's owners, as Notion's "소유자" (seen 2026-10-30):
// its maker first, marked "생성자", and the workspace's members added with
// "소유자 추가", each one removable; they and the workspace's owners manage
// the connection. It keeps acting within its maker's rights whoever owns it.

const (
	internalConnectionOwnersPath = internalConnectionItemPath + "/owners"
	internalConnectionOwnerPath  = internalConnectionOwnersPath + "/{userId}"
	internalConnectionMaxOwners  = 50
)

type internalConnectionOwner struct {
	ID      string `db:"user_id" json:"id"`
	Name    string `db:"name" json:"name"`
	Email   string `db:"email" json:"email"`
	Creator bool   `db:"creator" json:"creator"`
}

func registerInternalConnectionOwnerRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+internalConnectionOwnersPath, listInternalConnectionOwners)
	event.Router.POST(apiRoot+internalConnectionOwnersPath, addInternalConnectionOwner).Bind(apis.BodyLimit(internalConnectionBodyBytes))
	event.Router.DELETE(apiRoot+internalConnectionOwnerPath, removeInternalConnectionOwner)
}

func addInternalConnectionOwnerRow(app core.App, connectionID, workspaceID, userID, addedBy string) error {
	_, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_notion_internal_connection_owners}}
		([[connection_id]], [[workspace_id]], [[user_id]], [[added_by]], [[created_at]])
		VALUES ({:connectionId}, {:workspaceId}, {:userId}, {:addedBy}, {:now})`).Bind(dbx.Params{
		"connectionId": connectionID, "workspaceId": workspaceID, "userId": userID, "addedBy": addedBy, "now": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

// internalConnectionOwnedBy is whether the person is one of the connection's owners.
func internalConnectionOwnedBy(app core.App, connectionID, userID string) (bool, error) {
	owned := false
	err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_notion_internal_connection_owners}}
		WHERE [[connection_id]] = {:connectionId} AND [[user_id]] = {:userId})`).
		Bind(dbx.Params{"connectionId": connectionID, "userId": userID}).Row(&owned)
	return owned, err
}

func listInternalConnectionOwners(event *core.RequestEvent) error {
	stored, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "owners")
	}
	owners, err := loadInternalConnectionOwners(event.App, stored)
	if err != nil {
		return internalConnectionError(event, err, "owners")
	}
	return event.JSON(http.StatusOK, map[string]any{"items": owners})
}

func loadInternalConnectionOwners(app core.App, stored storedInternalConnection) ([]internalConnectionOwner, error) {
	owners := []internalConnectionOwner{}
	err := app.DB().NewQuery(`SELECT [[owner]].[[user_id]], COALESCE([[person]].[[name]], '') AS [[name]],
		COALESCE([[person]].[[email]], '') AS [[email]], [[owner]].[[user_id]] = {:creatorId} AS [[creator]]
		FROM {{leafen_notion_internal_connection_owners}} [[owner]]
		LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[owner]].[[user_id]]
		WHERE [[owner]].[[connection_id]] = {:connectionId}
		ORDER BY [[owner]].[[user_id]] = {:creatorId} DESC, [[owner]].[[created_at]], [[owner]].[[user_id]]`).
		Bind(dbx.Params{"connectionId": stored.ID, "creatorId": stored.CreatorID}).All(&owners)
	return owners, err
}

// addInternalConnectionOwner makes a member of the workspace (not a guest)
// one of its owners.
func addInternalConnectionOwner(event *core.RequestEvent) error {
	stored, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "owner-add")
	}
	user, _ := authenticatedUser(event)
	input := struct {
		UserID string `json:"userId"`
	}{}
	if err := event.BindBody(&input); err != nil {
		return internalConnectionError(event, bodyRefusal(errInternalConnectionInvalid, "connection owner", err), "owner-add-bind")
	}
	if strings.TrimSpace(input.UserID) == "" || len(input.UserID) > 255 {
		return internalConnectionError(event, refuse(errInternalConnectionInvalid, "userId names the member to make an owner, 1–255 characters"), "owner-add-bind")
	}
	if _, err := authorizeWorkspace(event.App, input.UserID, stored.WorkspaceID, permissionWorkspaceWrite); err != nil {
		if isPermissionDenied(err) {
			return internalConnectionError(event, refuse(errInternalConnectionInvalid, "userId names no one who may edit the workspace"), "owner-add-member")
		}
		return internalConnectionError(event, err, "owner-add-member")
	}
	count := 0
	if err := event.App.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_notion_internal_connection_owners}} WHERE [[connection_id]] = {:id}`).
		Bind(dbx.Params{"id": stored.ID}).Row(&count); err != nil {
		return internalConnectionError(event, err, "owner-add")
	}
	if count >= internalConnectionMaxOwners {
		return internalConnectionError(event, errInternalConnectionLimit, "owner-add-limit")
	}
	if err := addInternalConnectionOwnerRow(event.App, stored.ID, stored.WorkspaceID, input.UserID, user.Id); err != nil {
		return internalConnectionError(event, err, "owner-add")
	}
	owners, err := loadInternalConnectionOwners(event.App, stored)
	if err != nil {
		return internalConnectionError(event, err, "owner-add-read")
	}
	return event.JSON(http.StatusOK, map[string]any{"items": owners})
}

func removeInternalConnectionOwner(event *core.RequestEvent) error {
	stored, err := managedInternalConnection(event)
	if err != nil {
		return internalConnectionError(event, err, "owner-remove")
	}
	result, err := event.App.DB().NewQuery(`DELETE FROM {{leafen_notion_internal_connection_owners}}
		WHERE [[connection_id]] = {:connectionId} AND [[user_id]] = {:userId}`).Bind(dbx.Params{
		"connectionId": stored.ID, "userId": strings.TrimSpace(event.Request.PathValue("userId")),
	}).Execute()
	if err != nil {
		return internalConnectionError(event, err, "owner-remove")
	}
	if removed, _ := result.RowsAffected(); removed != 1 {
		return internalConnectionError(event, errInternalConnectionNotFound, "owner-remove")
	}
	owners, err := loadInternalConnectionOwners(event.App, stored)
	if err != nil {
		return internalConnectionError(event, err, "owner-remove-read")
	}
	return event.JSON(http.StatusOK, map[string]any{"items": owners})
}
