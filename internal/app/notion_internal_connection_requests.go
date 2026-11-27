package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How an internal connection's request finds its connection and person, and
// how a refused one answers. The connections themselves and their routes are
// notion_internal_connections.go.

func internalConnectionRequest(event *core.RequestEvent, requirement permissionRequirement) (*core.Record, string, workspaceAuthorization, error) {
	user, ok := authenticatedUser(event)
	if !ok {
		return nil, "", workspaceAuthorization{}, permissionDenied()
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return nil, "", workspaceAuthorization{}, refuse(errInternalConnectionInvalid, "the workspace ID in the path is 1–%d characters", maxWorkspacePermissionIdentifierLength)
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, requirement)
	return user, workspaceID, authorization, err
}

// managedInternalConnection is the connection the request names, for its
// owners or the workspace's owner.
func managedInternalConnection(event *core.RequestEvent) (storedInternalConnection, error) {
	user, workspaceID, authorization, err := internalConnectionRequest(event, permissionWorkspaceRead)
	if err != nil {
		return storedInternalConnection{}, err
	}
	stored, err := loadInternalConnection(event.App, workspaceID, strings.TrimSpace(event.Request.PathValue("connectionId")))
	if err != nil {
		return storedInternalConnection{}, err
	}
	if authorization.Role == "owner" {
		return stored, nil
	}
	if owned, err := internalConnectionOwnedBy(event.App, stored.ID, user.Id); err != nil || !owned {
		if err != nil {
			return storedInternalConnection{}, err
		}
		return storedInternalConnection{}, errInternalConnectionNotFound
	}
	return stored, nil
}

func loadInternalConnection(app core.App, workspaceID, id string) (storedInternalConnection, error) {
	stored := storedInternalConnection{}
	err := app.DB().NewQuery(`SELECT ` + internalConnectionColumns + `
		FROM {{leafen_notion_internal_connections}} [[connection]]
		LEFT JOIN {{users}} [[person]] ON [[person]].[[id]] = [[connection]].[[creator_id]]
		WHERE [[connection]].[[id]] = {:id} AND [[connection]].[[workspace_id]] = {:workspaceId}`).
		Bind(dbx.Params{"id": id, "workspaceId": workspaceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, errInternalConnectionNotFound
	}
	return stored, err
}

// internalConnectionRefusalWords is what a refused connection request says
// to check before its reason, by the request its stage names: a token
// refresh, an owner, a webhook subscription, the connection's own fields —
// or, for any other (a path that names no workspace), the request.
func internalConnectionRefusalWords(stage string) string {
	head, _, _ := strings.Cut(stage, "-")
	switch head {
	case "create", "update":
		return "Check the connection's name, icon and capabilities."
	case "token":
		return "Choose when the old token stops."
	case "owner":
		return "Check the connection owner."
	case "webhook":
		return "Check the webhook subscription."
	}
	return "Check the connection request."
}

func internalConnectionError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusInternalServerError, errorCodeInternalConnectionFailed, "Couldn't complete the connection request."
	switch {
	case errors.Is(err, errInternalConnectionInvalid):
		status, code, message = http.StatusBadRequest, errorCodeInternalConnectionInvalid, refusalMessage(internalConnectionRefusalWords(stage), refusalReason(err))
	case errors.Is(err, errInternalConnectionNotFound), isPermissionDenied(err):
		status, code, message = http.StatusNotFound, errorCodeInternalConnectionNotFound, "Connection not found."
	case errors.Is(err, errInternalConnectionLimit):
		status, code, message = http.StatusConflict, errorCodeInternalConnectionLimit, "Delete a connection you no longer use before making another."
	case errors.Is(err, errPersonalAccessTokenOwners):
		status, code, message = http.StatusForbidden, errorCodePersonalAccessTokenOwners, "Only the workspace's owners can make connections."
	}
	return writeAPIError(event, status, code, message, "internal_connection", stage, code, err, diagnosticSourcePosition(1))
}
