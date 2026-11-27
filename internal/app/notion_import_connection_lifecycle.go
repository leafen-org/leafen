package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionImportConnectionCursorVersion  = 1
	notionImportConnectionMaxCursorBytes = 1024
)

type notionImportConnectionCursor struct {
	Version     int    `json:"v"`
	PrincipalID string `json:"principalId"`
	WorkspaceID string `json:"workspaceId"`
	Created     int64  `json:"created"`
	ID          string `json:"id"`
}

func (r *durableJobRunner) listNotionImportConnections(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	cursor, err := decodeNotionImportConnectionCursor(
		event.Request.URL.Query().Get("cursor"), user.Id, workspaceID,
	)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "The connection list query position is invalid.")
	}
	page := notionImportConnectionPage{Items: []notionImportConnection{}}
	err = withAuthorizedNotionImportRead(
		event.Request.Context(), event.App, user.Id, workspaceID,
		func(readApp core.App) error {
			rows := []storedNotionImportConnection{}
			if err := readApp.DB().NewQuery(`
				SELECT [[id]], [[workspace_id]], [[label]], [[active]], [[created]]
				FROM {{leafen_notion_import_connections}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[owner]] = {:owner}
					-- Another tool's bundle has its own source, not a Notion connection.
					AND NOT EXISTS (SELECT 1 FROM {{leafen_bundle_import_sources}} AS [[bundle]] WHERE [[bundle]].[[connection_id]] = {{leafen_notion_import_connections}}.[[id]])
					AND ({:afterId} = '' OR [[created]] < {:afterCreated}
						OR ([[created]] = {:afterCreated} AND [[id]] < {:afterId}))
				ORDER BY [[created]] DESC, [[id]] DESC
				LIMIT {:limit}
			`).Bind(dbx.Params{
				"workspaceId": workspaceID, "owner": user.Id,
				"afterCreated": cursor.Created, "afterId": cursor.ID,
				"limit": notionImportConnectionPageItems + 1,
			}).All(&rows); err != nil {
				return err
			}
			page.HasMore = len(rows) > notionImportConnectionPageItems
			if page.HasMore {
				rows = rows[:notionImportConnectionPageItems]
			}
			for _, row := range rows {
				page.Items = append(page.Items, notionConnectionResponse(row))
			}
			if page.HasMore {
				page.NextCursor, err = encodeNotionImportConnectionCursor(rows[len(rows)-1], user.Id)
				return err
			}
			return nil
		},
	)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't load the connection list.", err)
	}
	relabelOldNotionConnections(event, user.Id, workspaceID, page.Items)
	return event.JSON(http.StatusOK, page)
}

// oldNotionConnectionLabel is the one name every connection had before
// connections were named as Notion names them (2026-10-26).
const oldNotionConnectionLabel = "Leafen Notion import"

// relabelOldNotionConnections names a connection still called the old way as
// Notion names it, once, and keeps the old name where Notion doesn't answer in
// a moment (the screen says it in the person's words then).
func relabelOldNotionConnections(event *core.RequestEvent, owner, workspaceID string, items []notionImportConnection) {
	for index := range items {
		if !items[index].Active || items[index].Label != oldNotionConnectionLabel {
			continue
		}
		connection, err := loadNotionConnection(event.App, items[index].ID, owner, workspaceID)
		if err != nil {
			continue
		}
		token, err := decryptNotionConnection(event.App, connection)
		if err != nil {
			continue
		}
		asked, cancel := context.WithTimeout(event.Request.Context(), 5*time.Second)
		name, err := newNotionProviderClient(token).connectionName(asked)
		cancel()
		if err != nil || name == "" {
			continue
		}
		if _, err := event.App.DB().NewQuery(`
			UPDATE {{leafen_notion_import_connections}} SET [[label]] = {:label}, [[updated]] = {:now}
			WHERE [[id]] = {:id} AND [[label]] = {:old}
		`).Bind(dbx.Params{"label": name, "now": time.Now().UTC().UnixMilli(), "id": connection.ID, "old": oldNotionConnectionLabel}).Execute(); err != nil {
			continue
		}
		items[index].Label = name
	}
}

func (r *durableJobRunner) revokeNotionImportConnection(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	connectionID := strings.TrimSpace(event.Request.PathValue("connectionId"))
	if workspaceID == "" || connectionID == "" {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the connection details.")
	}
	var connection storedNotionImportConnection
	err := event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeWorkspace(txApp, user.Id, workspaceID, permissionWorkspaceWrite); err != nil {
			return err
		}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]],
				[[label]], [[token_cipher]], [[active]], [[created]]
			FROM {{leafen_notion_import_connections}}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{
			"id": connectionID, "owner": user.Id, "workspaceId": workspaceID,
		}).One(&connection); err != nil {
			return err
		}
		if connection.Active == 0 && connection.TokenCipher == "" {
			return nil
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_notion_import_connections}}
			SET [[active]] = 0, [[token_cipher]] = '', [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{
			"id": connectionID, "owner": user.Id, "workspaceId": workspaceID, "updated": now,
		}).Execute(); err != nil {
			return err
		}
		if err := endNotionMigrationsOfConnection(txApp, connectionID, now); err != nil {
			return err
		}
		connection.Active = 0
		connection.TokenCipher = ""
		return nil
	})
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeNotionImportConnectionNotFound, "Connection not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeNotionImportConnectionFailed, "Couldn't disconnect.", err)
	}
	return event.JSON(http.StatusOK, notionConnectionResponse(connection))
}

func decodeNotionImportConnectionCursor(raw, principalID, workspaceID string) (notionImportConnectionCursor, error) {
	empty := notionImportConnectionCursor{
		Version: notionImportConnectionCursorVersion, PrincipalID: principalID, WorkspaceID: workspaceID,
	}
	if strings.TrimSpace(raw) == "" {
		return empty, nil
	}
	if len(raw) > notionImportConnectionMaxCursorBytes {
		return notionImportConnectionCursor{}, errors.New("Notion connection cursor is too large")
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > notionImportConnectionMaxCursorBytes {
		return notionImportConnectionCursor{}, errors.New("Notion connection cursor is malformed")
	}
	cursor := notionImportConnectionCursor{}
	if err := json.Unmarshal(data, &cursor); err != nil ||
		cursor.Version != notionImportConnectionCursorVersion || cursor.PrincipalID != principalID ||
		cursor.WorkspaceID != workspaceID || cursor.Created <= 0 || cursor.ID == "" || len(cursor.ID) > 255 {
		return notionImportConnectionCursor{}, errors.New("Notion connection cursor is invalid")
	}
	return cursor, nil
}

func encodeNotionImportConnectionCursor(connection storedNotionImportConnection, principalID string) (string, error) {
	data, err := json.Marshal(notionImportConnectionCursor{
		Version: notionImportConnectionCursorVersion, PrincipalID: principalID,
		WorkspaceID: connection.WorkspaceID, Created: connection.Created, ID: connection.ID,
	})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	if len(encoded) > notionImportConnectionMaxCursorBytes {
		return "", errors.New("Notion connection cursor is too large")
	}
	return encoded, nil
}

// notionConnectionTokensRefused asks Notion at once, for each connection of
// the ids not asked about yet, whether it still takes the connection's token,
// and marks in refused those it refuses (revoked, or replaced in Notion
// since it was saved); one Notion doesn't answer about in a moment, or that
// isn't the owner's, stays unmarked, as it may still work.
func notionConnectionTokensRefused(ctx context.Context, app core.App, owner, workspaceID string, ids []string, refused map[string]bool) {
	asking := []string{}
	for _, id := range ids {
		if _, asked := refused[id]; !asked {
			refused[id] = false
			asking = append(asking, id)
		}
	}
	answers := make([]bool, len(asking))
	var wait sync.WaitGroup
	for index, id := range asking {
		wait.Add(1)
		go func() {
			defer wait.Done()
			connection, err := loadNotionConnection(app, id, owner, workspaceID)
			if err != nil {
				return
			}
			token, err := decryptNotionConnection(app, connection)
			if err != nil {
				return
			}
			asked, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_, err = notionSourceClient(app, id, token).connectionName(asked)
			answers[index] = notionProviderTokenRejected(err)
		}()
	}
	wait.Wait()
	for index, id := range asking {
		refused[id] = answers[index]
	}
}
