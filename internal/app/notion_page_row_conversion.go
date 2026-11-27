package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The public move endpoint turns a page into a database page when its parent
// is a data source, and a database page into a page when its parent is a
// page, as Notion's does. The record keeps its ID; like Notion's, the API
// has no confirmation step, so a database page's values go with the move.

func notionPageRowConversionHash(kind, recordID, parentID string) string {
	digest := sha256.Sum256([]byte("notion-public-" + kind + "\x00" + recordID + "\x00" + parentID))
	return hex.EncodeToString(digest[:])
}

// serveNotionPageMoveIntoDataSource moves a page into a database.
func serveNotionPageMoveIntoDataSource(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	authorization notionPageAuthorization,
	publicDataSourceID string,
) error {
	target, authorized, err := authorizeNotionDataSourceParent(
		event, strings.TrimSpace(publicDataSourceID), "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	if target.Container.WorkspaceID != authorization.Page.Workspace.WorkspaceID {
		return notionPageNotFound(event)
	}
	pageID := authorization.Page.Page.Id
	if _, err := ygo.movePageIntoDatabase(event.Request.Context(), pageToDatabaseRowRequest{
		PrincipalID: authorization.Identity.PrincipalID, PageID: pageID,
		DataSourceID: target.Container.DataSource, ContainerPageID: target.Container.PageID,
		MutationID:  "notion-public-page-to-row:" + newDiagnosticReference(),
		RequestHash: notionPageRowConversionHash("page-to-row", pageID, target.Container.DataSource),
	}); err != nil {
		return notionPageRowConversionError(event, err)
	}
	response, err := loadNotionDatabaseRowPageResponse(event, authorization.Identity, pageID, permissionWorkspaceRead)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// serveNotionDatabaseRowPageMoveOut moves a database page out of its
// database, under a page.
func serveNotionDatabaseRowPageMoveOut(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	admission notionDatabaseRowPageAuthorization,
	publicParentID string,
) error {
	parent, authorized, err := authorizeNotionParentPage(event, publicParentID, notionPageMoveParentAnswers)
	if err != nil || !authorized {
		return err
	}
	if parent.Page.Workspace.WorkspaceID != admission.Authority.WorkspaceID {
		return notionPageNotFound(event)
	}
	rowID, parentID := admission.Authority.RowID, parent.Page.Page.Id
	if refused, err := refuseNotionMoveIntoItself(event, rowID, parentID); refused {
		return err
	}
	if _, err := ygo.convertDatabaseRowToPage(event.Request.Context(), databaseRowToPageRequest{
		PrincipalID: admission.Identity.PrincipalID, RowID: rowID,
		ContainerPageID: admission.Authority.ContainerPageID, ParentID: parentID,
		MutationID:  "notion-public-row-to-page:" + newDiagnosticReference(),
		RequestHash: notionPageRowConversionHash("row-to-page", rowID, parentID),
	}); err != nil {
		return notionPageRowConversionError(event, err)
	}
	moved, err := event.App.FindRecordById(pagesCollection, rowID)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	// One moved under a wiki is the wiki's page now (settleDatabasePageLeft).
	response, err := notionWrittenPageObject(event, admission.Identity, moved)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// notionPageRowConversionError answers a refused conversion as Notion
// answers a move to a parent that can't take the page.
func notionPageRowConversionError(event *core.RequestEvent, err error) error {
	if words, refused := refusalWords(err); refused {
		return notionPageValidationError(event, words)
	}
	var shared *graphRequestError
	if errors.Is(err, errPageRowConversionShared) && errors.As(err, &shared) {
		return notionPageValidationError(event, shared.message)
	}
	// What is left is a change met meanwhile (page_row_conversion_refusals.go).
	if errors.Is(err, errDatabaseRowPageConflict) || errors.Is(err, errPageRowConversionConflict) {
		return notionCompatibilityError(event, http.StatusConflict, "conflict_error", "The page could not be moved to that parent.")
	}
	return notionDatabaseRowPageMutationError(event, err)
}
