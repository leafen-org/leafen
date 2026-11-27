package app

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A row page's in_trash, alone or with the rest of a page update, as
// Notion's API takes it (seen 2026-11-12): with values, an icon or a cover,
// the row comes out of the trash before they are written and goes into it
// after, and the answer is the page as it then is.

// changeNotionDatabaseRowPageTrash moves a row page to the trash or back,
// with its sub-items, through the same trash event as the Leafen web client,
// and answers with its page.
func changeNotionDatabaseRowPageTrash(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	admission notionDatabaseRowPageAuthorization,
	trash bool,
) error {
	if err := setNotionDatabaseRowPageTrash(event, ygo, admission, trash); err != nil {
		return notionDatabaseRowPageTrashError(event, err)
	}
	if err := recordConnectionStoredMark(event.App, admission.Identity, connectionAuthoredRow, admission.Authority.RowID, false, notionBlockEditMark(admission.Authority.UpdatedAt)); err != nil {
		return notionDatabaseRowPageMutationError(event, err)
	}
	return answerNotionDatabaseRowPage(event, admission)
}

// setNotionDatabaseRowPageTrash moves a row page to the trash or back as it
// was admitted, its authority unchanged since.
func setNotionDatabaseRowPageTrash(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	admission notionDatabaseRowPageAuthorization,
	trash bool,
) error {
	_, err := ygo.changeDatabaseRowTrash(
		event.Request.Context(), admission.Identity.PrincipalID, admission.Authority.RowID,
		databaseRowPageActionInput{
			RequestID:                 "notion-public-row-trash:" + newDiagnosticReference(),
			ExpectedAuthorityRevision: admission.Authority.AuthorityRevision,
		}, trash,
	)
	return err
}

// trashNotionDatabaseRowPageLast moves a row page to the trash after the
// rest of its update was written, as it now is.
func trashNotionDatabaseRowPageLast(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	admission notionDatabaseRowPageAuthorization,
) error {
	current, err := authorizeNotionDatabaseRowPageCurrent(
		event.App, admission.Identity, admission.Authority.RowID, permissionWorkspaceWrite,
	)
	if err != nil {
		return err
	}
	return setNotionDatabaseRowPageTrash(event, ygo, current, true)
}

func notionDatabaseRowPageTrashError(event *core.RequestEvent, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return notionPageNotFound(event)
	}
	return notionDatabaseRowPageMutationError(event, err)
}

// answerNotionDatabaseRowPage answers with a row's page as it now is.
func answerNotionDatabaseRowPage(event *core.RequestEvent, admission notionDatabaseRowPageAuthorization) error {
	response, err := loadNotionDatabaseRowPageResponse(
		event, admission.Identity, admission.Authority.RowID, permissionWorkspaceRead,
	)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}
