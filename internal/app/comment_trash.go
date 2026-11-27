package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page, database page or template in Trash keeps its comments to read,
// as Notion's (seen 2026-10-31: the thread shows with its quote, no field
// for a reply, its "⋯" only copying the link and muting replies): nothing
// is added, changed, deleted, resolved or reacted to until it is restored.
// The routes people and their agents write through refuse; the lifecycle's
// own jobs (a permanent deletion, an import) don't pass through them.

// commentTargetInTrash reports a comment's page, row or template in Trash,
// or under a page in Trash.
func commentTargetInTrash(app core.App, pageID, rowID string) (bool, error) {
	if rowID == "" {
		return pageInTrashPath(app, pageID)
	}
	row, err := loadDatabaseRowPageAuthority(app, rowID)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		template, err := isDatabaseTemplatePageID(app, rowID)
		if err != nil || !template {
			return false, err
		}
		deletedAt := ""
		err = app.DB().NewQuery(`SELECT [[deleted_at]] FROM {{leafen_database_toolbar_template_state}} WHERE [[template_id]] = {:id}`).
			Bind(dbx.Params{"id": rowID}).Row(&deletedAt)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		if deletedAt != "" {
			return true, nil
		}
		page, err := app.FindRecordById(pagesCollection, rowID)
		if err != nil {
			return false, err
		}
		return pageInTrashPath(app, page.GetString("parent_id"))
	}
	if err != nil {
		return false, err
	}
	if row.TrashedAt != 0 {
		return true, nil
	}
	return pageInTrashPath(app, row.ContainerPageID)
}

// refuseCommentWritesInTrash wraps a comment write route: someone who may
// read the page or row it names is told it is in Trash; anyone else meets
// the route's own refusal.
func refuseCommentWritesInTrash(next func(*core.RequestEvent) error) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
		rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
		if !ok || pageID == "" && rowID == "" {
			return next(event)
		}
		var readable error
		if rowID != "" {
			_, readable = authorizeDatabaseRowOrTemplatePage(event.App, user.Id, rowID, permissionWorkspaceRead)
		} else {
			_, readable = authorizePage(event.App, user.Id, pageID, permissionWorkspaceRead)
		}
		if readable != nil {
			return next(event)
		}
		if trashed, err := commentTargetInTrash(event.App, pageID, rowID); err == nil && trashed {
			return apiError(event, http.StatusConflict, errorCodePageInTrash, "A page in Trash takes comments again once it's restored.")
		}
		return next(event)
	}
}

// notionArchivedCommentMessage is Notion's answer to a change on an archived block.
const notionArchivedCommentMessage = "The block is archived; restore it before editing it."

// notionCommentTargetInTrash reports a compatible comment's page or row in Trash.
func notionCommentTargetInTrash(event *core.RequestEvent, target notionCommentTarget) bool {
	rowID := ""
	if target.TargetKind == commentTargetDatabaseRow {
		rowID = target.PageID
	}
	trashed, err := commentTargetInTrash(event.App, target.StoragePageID, rowID)
	return err == nil && trashed
}

// mcpCommentTargetInTrash refuses an agent's comment change on a page or row in Trash.
func mcpCommentTargetInTrash(app core.App, target nativeCommentTarget, requirement permissionRequirement) error {
	if requirement == permissionWorkspaceRead {
		return nil
	}
	rowID := ""
	if target.Kind == commentTargetDatabaseRow {
		rowID = target.ID
	}
	if trashed, err := commentTargetInTrash(app, target.StoragePageID, rowID); err == nil && trashed {
		return mcpDenied("that page is in Trash; restore it to comment")
	}
	return nil
}
