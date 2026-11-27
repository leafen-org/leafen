package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
)

// A locked page or database, as in Notion, keeps everyone's access but stops
// changes made in the app: a page's content, title, icon and cover, and a
// database's views and properties, while database rows stay editable. Anyone
// who can edit may unlock it for everyone, or only for themselves; locking it
// again, or unlocking it for everyone, ends every personal unlock. The
// public API and MCP edit by access alone, as Notion's API does (a locked
// database there took a row's value with a new select option and a new
// property, seen 2026-11-18). What a refusal says is lock_refusal.go's.

type pageLockMode uint8

const (
	// pageLockEnforced is the app's own editing path.
	pageLockEnforced pageLockMode = iota
	// pageLockIgnored is the public API, MCP and the server's own work.
	pageLockIgnored
)

type pageLockStateResponse struct {
	Locked        bool `json:"locked"`
	UnlockedForMe bool `json:"unlockedForMe"`
}

// pageLockReadResponse is a page's lock as GET /pages/{id}/lock reads it,
// with what a change of it sends to PATCH /pages/{id}/metadata: the page's
// cursor (baseCursor) and, for a database row's page, its row's revision
// (expectedAuthorityRevision), so that locking it is this read and that
// change (page_metadata_row_page_paths.go).
type pageLockReadResponse struct {
	Locked            bool `json:"locked"`
	UnlockedForMe     bool `json:"unlockedForMe"`
	Cursor            int  `json:"cursor"`
	DatabaseRow       bool `json:"databaseRow,omitzero"`
	AuthorityRevision int  `json:"authorityRevision,omitzero"`
}

func pageUnlockedFor(app core.App, pageID string, principalID string) (bool, error) {
	var exists int
	err := app.DB().NewQuery(`
		SELECT 1 FROM {{leafen_page_lock_unlocks}}
		WHERE [[page_id]] = {:pageId} AND [[principal_id]] = {:principalId}
	`).Bind(dbx.Params{"pageId": pageID, "principalId": principalID}).Row(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// markRowLocksUnlockedFor takes the lock off the graph rows whose database
// pages the reader unlocked for themselves, so the rows say whether their
// values take this reader's changes.
func markRowLocksUnlockedFor(app core.App, principalID string, response *graphReadResponse) error {
	locked := map[string]map[string]any{}
	ids := []any{}
	for _, item := range response.Items {
		id, _ := item.Value["id"].(string)
		if item.Kind == graphReadKindRow && item.Value["lockedForMe"] == true && id != "" {
			locked[id] = item.Value
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	unlocked := []string{}
	if err := app.DB().Select("page_id").From("leafen_page_lock_unlocks").
		Where(dbx.HashExp{"principal_id": principalID}).AndWhere(dbx.In("page_id", ids...)).
		Column(&unlocked); err != nil {
		return err
	}
	for _, id := range unlocked {
		delete(locked[id], "lockedForMe")
	}
	return nil
}

// pageLockedFor reports whether the page's lock stops this person's changes
// in the app: the page is locked and they have not unlocked it for
// themselves.
func pageLockedFor(app core.App, page *core.Record, principalID string) (bool, error) {
	if page == nil || !page.GetBool("is_locked") {
		return false, nil
	}
	unlocked, err := pageUnlockedFor(app, page.Id, principalID)
	return !unlocked, err
}

// clearPageLockUnlocks ends every personal unlock of a page when its lock
// changes for everyone.
func clearPageLockUnlocks(app core.App, pageID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_page_lock_unlocks}} WHERE [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"pageId": pageID}).Execute()
	return err
}

// graphOperationsChangeLockedContent reports whether a content change
// reaches what a lock protects: blocks, data sources, views, properties and
// dependency settings. Rows and the dependencies between them stay editable,
// and so does whether a database's description shows (databaseDescriptionShowingAlone).
func graphOperationsChangeLockedContent(operations []graphOperation) bool {
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow, operation.Kind == graphOperationKindDeleteRow,
			operation.Kind == graphOperationKindUpsertDependencyEdge, operation.Kind == graphOperationKindDeleteDependencyEdge,
			operation.Kind == graphOperationKindUpsertDataSource && databaseDescriptionShowingAlone(operation.DataSource):
			continue
		default:
			return true
		}
	}
	return false
}

// pageMetadataChangesLockedContent reports whether a metadata change reaches
// what a lock protects: the title, icon, cover and page style. Locking,
// trashing, archiving and moving follow their own rules.
func pageMetadataChangesLockedContent(page *core.Record, input pageMetadataInput) bool {
	if input.Title != page.GetString("title") || pageTitleSpansChange(page, input) {
		return true
	}
	if input.Icon != nil && (*input.Icon != page.GetString("icon") ||
		(input.IconType != nil && *input.IconType != page.GetString("icon_type"))) {
		return true
	}
	if input.Cover != nil && *input.Cover != page.GetString("cover") {
		return true
	}
	if input.CoverPosition != nil && *input.CoverPosition != page.GetInt("cover_position") {
		return true
	}
	preview := core.NewRecord(page.Collection())
	for _, field := range []string{"font", "small_text", "full_width", "backlinks_display", "page_comments_display", "table_of_contents_display", "inline_comments_display"} {
		preview.Set(field, page.Get(field))
	}
	applyPageStyleMetadata(preview, input)
	applyPageCustomizationMetadata(preview, input)
	for _, field := range []string{"font", "small_text", "full_width", "backlinks_display", "page_comments_display", "table_of_contents_display", "inline_comments_display"} {
		if preview.GetString(field) != page.GetString(field) {
			return true
		}
	}
	return false
}

// pageRoomInvalidator closes a page's collaboration room so connected
// editors reconnect and are authorized again, as after a lock change.
type pageRoomInvalidator interface {
	InvalidatePageRoom(pageID string) error
}

func (a *ygoRoomAuthorization) InvalidatePageRoom(pageID string) error {
	err := a.server.CloseRoom(pageID, true)
	if errors.Is(err, ygowebsocket.ErrRoomNotFound) {
		return nil
	}
	if err != nil {
		a.closeRoomSockets(pageID)
		a.app.Logger().Warn("leafen page collaboration room not closed; its sockets were cut", "page", pageID, "error", err)
	}
	return err
}

func invalidateLockedPageRoom(rooms workspaceRoomInvalidator, pageID string) {
	if invalidator, ok := rooms.(pageRoomInvalidator); ok {
		_ = invalidator.InvalidatePageRoom(pageID)
	}
}

func registerPageLockRoutes(event *core.ServeEvent, rooms workspaceRoomInvalidator, ygo *pocketBaseYgoPersistence) {
	event.Router.PATCH(apiRoot+"/pages/{id}/metadata", func(request *core.RequestEvent) error {
		return updatePageOrRowPageMetadata(request, rooms, ygo)
	})
	event.Router.GET(apiRoot+"/pages/{id}/lock", readPageLockState)
	event.Router.PUT(apiRoot+"/pages/{id}/lock/me", func(request *core.RequestEvent) error {
		return changePageLockForMe(request, rooms)
	})
}

func readPageLockState(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	authorization, err := authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't read the page lock.", err)
	}
	state, err := pageLockState(event.App, authorization.Page, user.Id)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't read the page lock.", err)
	}
	read := pageLockReadResponse{Locked: state.Locked, UnlockedForMe: state.UnlockedForMe,
		Cursor: authorization.Page.GetInt("cursor"), DatabaseRow: isDatabaseRowPageRecord(authorization.Page)}
	if read.DatabaseRow {
		read.AuthorityRevision = rowPageAuthorityRevision(event.App, user.Id, pageID)
	}
	return event.JSON(http.StatusOK, read)
}

func pageLockState(app core.App, page *core.Record, principalID string) (pageLockStateResponse, error) {
	if !page.GetBool("is_locked") {
		return pageLockStateResponse{}, nil
	}
	unlocked, err := pageUnlockedFor(app, page.Id, principalID)
	return pageLockStateResponse{Locked: true, UnlockedForMe: unlocked}, err
}

// changePageLockForMe unlocks a locked page for the person asking, or locks
// it for them again. Their open editors reconnect under the new state.
func changePageLockForMe(event *core.RequestEvent, rooms workspaceRoomInvalidator) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	unlocked, ok := bindPageLockForMe(event)
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, pageLockForMeRefusal)
	}
	var state pageLockStateResponse
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizePage(txApp, user.Id, pageID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		page := authorization.Page
		if unlocked && page.GetBool("is_locked") {
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_page_lock_unlocks}} ([[page_id]], [[principal_id]], [[workspace_id]], [[created_at]])
				VALUES ({:pageId}, {:principalId}, {:workspaceId}, {:now})
				ON CONFLICT ([[page_id]], [[principal_id]]) DO NOTHING
			`).Bind(dbx.Params{
				"pageId": page.Id, "principalId": user.Id,
				"workspaceId": authorization.Workspace.WorkspaceID, "now": time.Now().UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
		} else if !unlocked {
			if _, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_page_lock_unlocks}} WHERE [[page_id]] = {:pageId} AND [[principal_id]] = {:principalId}
			`).Bind(dbx.Params{"pageId": page.Id, "principalId": user.Id}).Execute(); err != nil {
				return err
			}
		}
		state, err = pageLockState(txApp, page, user.Id)
		return err
	})
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't change the page lock.", err)
	}
	invalidateLockedPageRoom(rooms, pageID)
	return event.JSON(http.StatusOK, state)
}
