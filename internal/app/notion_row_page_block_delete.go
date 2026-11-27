package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// deleteNotionRowPageBlock deletes a database page asked for as a block, as
// Notion's API does (seen 2026-11-23 twice, Notion-Version 2026-03-11 and
// 2025-09-03, pages of a full-page and an inline database alike): it goes
// to the trash, with its sub-items, as its page's in_trash takes it there,
// and the answer is its child_page block in the trash. One in the trash is
// refused as an archived block, and one under its database or a page in the
// trash as having an archived ancestor (notion_trash_refusal.go).
func deleteNotionRowPageBlock(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	publicRowID string,
) error {
	admission, authorized, err := authorizeNotionDatabaseRowPageState(
		event, publicRowID, "pages:write", permissionWorkspaceWrite, notionRowAnyTrash,
	)
	if err != nil || !authorized {
		return err
	}
	if refused, err := refuseNotionTrashedEdit(event, admission.Authority.RowID, true, false, notionTrashedEdit); refused {
		return err
	}
	if err := setNotionDatabaseRowPageTrash(event, ygo, admission, true); err != nil {
		return notionDatabaseRowPageTrashError(event, err)
	}
	if err := recordConnectionStoredMark(
		event.App, admission.Identity, connectionAuthoredRow, admission.Authority.RowID, false,
		notionBlockEditMark(admission.Authority.UpdatedAt),
	); err != nil {
		return notionDatabaseRowPageMutationError(event, err)
	}
	response, err := notionRowPageBlockObject(event.App, admission.Identity, admission.Authority.RowID)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}
