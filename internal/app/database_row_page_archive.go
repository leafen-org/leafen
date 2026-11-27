package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A database row page is archived by its own archive state, kept on its page
// record beside its icon, or with the archived page that holds its
// database. An archived row stays in its database: views, queries and
// relation pickers leave it out, and "View archived pages" lists it.

// applyDatabaseRowPageArchiveState is the single writer for a row page's own
// archive state. It keeps the first archive while the row stays archived and
// reports whether the state changed.
func applyDatabaseRowPageArchiveState(
	app core.App,
	rowID, workspaceID, containerPageID, actorID string,
	archived bool,
	transitionAt string,
) (bool, error) {
	archivedAt, archivedBy := "", ""
	if archived {
		archivedAt, archivedBy = transitionAt, actorID
	}
	changed, err := setRowPageRecordArchive(
		app, rowID, workspaceID, containerPageID, actorID, archivedAt, archivedBy, archived,
	)
	if err != nil || !changed {
		return false, err
	}
	return true, touchDatabaseRowPageAuthority(app, rowID, time.Now().UTC().UnixMilli())
}

// copyDatabaseRowPageArchiveState keeps a copied row archived, by whom and
// since when its source row is.
func copyDatabaseRowPageArchiveState(app core.App, sourceRowID, targetRowID, workspaceID string) error {
	return copyDatabaseRowPageArchiveStateFrom(app, sourceRowID, workspaceID, targetRowID, workspaceID)
}

// copyDatabaseRowPageArchiveStateFrom is copyDatabaseRowPageArchiveState
// from a source in another workspace.
func copyDatabaseRowPageArchiveStateFrom(app core.App, sourceRowID, sourceWorkspaceID, targetRowID, workspaceID string) error {
	_, err := copyRowPageRecordPresentationFrom(app, sourceRowID, sourceWorkspaceID, targetRowID, workspaceID, true)
	return err
}

// databaseRowPageArchived reports whether the row is archived itself or with
// the archived page that holds its database.
func databaseRowPageArchived(app core.App, authority databaseRowPageAuthority) (bool, error) {
	if strings.TrimSpace(authority.ArchivedAt) != "" {
		return true, nil
	}
	_, archived, err := loadPageArchiveEffect(app, authority.ContainerPageID)
	return archived, err
}

// databaseRowArchiveScopeSQL narrows a row query joined to its row page:
// a query reads the rows that are not archived unless it asks for archived
// ones, and views never show archived rows.
func databaseRowArchiveScopeSQL(scope, rowPage string) string {
	switch scope {
	case databaseArchiveScopeAll:
		return ""
	case databaseArchiveScopeArchived:
		return " AND COALESCE(" + rowPage + ".[[archived_at]], '') != ''"
	default:
		return " AND COALESCE(" + rowPage + ".[[archived_at]], '') = ''"
	}
}

// applyNotionDatabaseRowPageArchive applies a Public API is_archived to the
// row's own archive state; nil leaves it as it is.
func applyNotionDatabaseRowPageArchive(
	app core.App, authority databaseRowPageAuthority, actorID string, archived *bool,
) error {
	if archived == nil {
		return nil
	}
	changed, err := applyDatabaseRowPageArchiveState(
		app, authority.RowID, authority.WorkspaceID, authority.ContainerPageID, actorID,
		*archived, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil || !changed {
		return err
	}
	return announceDatabaseRowPageChange(app, authority.RowID)
}

func setDatabaseRowPageArchive(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	input := databaseRowPageArchiveInput{}
	if rowID == "" || event.BindBody(&input) != nil || input.ExpectedAuthorityRevision < 1 {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the archive change details.")
	}
	response, err := updateDatabaseRowPageArchive(event.App, user.Id, rowID, input)
	switch {
	case errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	case errors.Is(err, errDatabaseRowPageConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageConflict, "The database row changed. Try again.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
			"Couldn't change the archive state.", "database_row_page", "archive",
			errorCodeDatabaseRowPageFailed, err,
		)
	default:
		return event.JSON(http.StatusOK, response)
	}
}

func updateDatabaseRowPageArchive(
	app core.App,
	principalID, rowID string,
	input databaseRowPageArchiveInput,
) (databaseRowPageActionResponse, error) {
	response := databaseRowPageActionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authority, err := authorizeDatabaseRowPage(txApp, principalID, rowID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		if authority.AuthorityRevision != input.ExpectedAuthorityRevision {
			return errDatabaseRowPageConflict
		}
		changed, err := applyDatabaseRowPageArchiveState(
			txApp, rowID, authority.WorkspaceID, authority.ContainerPageID, principalID,
			input.Archived, time.Now().UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}
		response = databaseRowPageActionResponse{
			RowID: rowID, ContainerPageID: authority.ContainerPageID,
			AuthorityRevision: authority.AuthorityRevision,
		}
		if !changed {
			response.Duplicate = true
			return nil
		}
		response.AuthorityRevision++
		return announceDatabaseRowPageChange(txApp, rowID)
	})
	return response, err
}
