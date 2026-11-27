package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A database row is a page, as Notion's API has it: PATCH /v1/pages/{id}
// sets a row's icon and cover as any page's (2026-03-11 API). Its page reads
// at GET /pages/{rowId}, so its metadata change goes there too: the icon,
// cover and style a row's page takes go through its own path
// (database_row_page_presentation.go), with the row's current revision, as
// the screen's page head sets them. Its lock, archive, trash and place go on
// to its row's own routes when the change sends the row's revision
// (page_metadata_row_page_paths.go); its title and other values have their
// own route. A change that sends them as they already are passes, as the
// screen sends title, parent and trash with every change and no revision,
// and one that changes them otherwise is refused with the way that does.

const rowPageMetadataRefusal = "A database row's page takes here its icon, cover and style, and with its row's revision " +
	"(expectedAuthorityRevision, as GET /pages/{id} reads it: authorityRevision) its lock, archive, trash, restore and move, one at a time. " +
	"Change its title and other values with POST /database-rows/{rowId}/page/values, and a wiki's page's place with POST /pages/{pageId}/wiki-page."

// updatePageOrRowPageMetadata answers PATCH /pages/{id}/metadata: a
// database row's page here, any other page as pages are
// (updatePageMetadataWithRooms), which reads the request again.
func updatePageOrRowPageMetadata(event *core.RequestEvent, rooms workspaceRoomInvalidator, ygo *pocketBaseYgoPersistence) error {
	if handled, err := updateRowPageMetadata(event, rooms, ygo); handled {
		return err
	}
	return updatePageMetadataWithRooms(event, rooms, ygo)
}

// updateRowPageMetadata answers a metadata change sent to a database row's
// page; handled is false for any other page, and for a request the page
// path refuses as it would any page's.
func updateRowPageMetadata(event *core.RequestEvent, rooms workspaceRoomInvalidator, ygo *pocketBaseYgoPersistence) (handled bool, err error) {
	user, ok := authenticatedUser(event)
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	if !ok || pageID == "" {
		return false, nil
	}
	page, findErr := event.App.FindRecordById(pagesCollection, pageID)
	if findErr != nil || !isDatabaseRowPageRecord(page) || rereadableRequestBody(event) != nil {
		return false, nil
	}
	var input pageMetadataInput
	var sent pageMetadataSentFields
	if event.BindBody(&input) != nil || event.BindBody(&sent) != nil {
		return false, nil
	}
	input = normalizePageMetadataInput(input)
	input, sent = pageMetadataInputSpellingTitle(input, sent)
	if !validPageMetadataMutationInput(pageID, input) {
		return false, nil
	}
	userID := user.Id
	// With the row's revision, what differs from the row as it is goes on to
	// its row's routes; what asks none of them, as without it, is the look's.
	if input.ExpectedAuthorityRevision > 0 {
		if changed, err := changeRowPageOnItsRoutes(event, userID, page, input, sent, rooms, ygo); changed {
			return true, err
		}
	} else if rowPageMetadataChangesOtherThanLook(page, rowPageInTrash(event.App, page), input, sent) {
		return true, apiError(event, http.StatusBadRequest, errorCodeInvalidInput, rowPageMetadataRefusal)
	}
	look := databaseRowPagePresentationInput{
		ExpectedAuthorityRevision: 1,
		Icon:                      input.Icon, IconType: input.IconType, Cover: input.Cover, CoverPosition: input.CoverPosition,
		Font: input.Font, SmallText: input.SmallText, FullWidth: input.FullWidth, TableOfContentsDisplay: input.TableOfContentsDisplay,
	}
	if page.GetInt("cursor") != input.BaseCursor {
		return true, apiError(event, http.StatusConflict, errorCodeCursorConflict, pageCursorConflictMessage)
	}
	// A change that sends nothing new leaves the page as it is, as a page's.
	changesLook := look.Icon != nil || look.Cover != nil || look.CoverPosition != nil || look.Font != nil ||
		look.SmallText != nil || look.FullWidth != nil || look.TableOfContentsDisplay != nil
	if changesLook && !normalizeDatabaseRowPagePresentationInput(&look) {
		return true, apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the icon, cover and style details.")
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if !changesLook {
			_, err := authorizeDatabaseRowOrTemplatePage(txApp, userID, pageID, permissionWorkspaceWrite)
			return err
		}
		authority, err := authorizeDatabaseRowOrTemplatePage(txApp, userID, pageID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		// A revision sent is held to the row's, as its own route holds it.
		if input.ExpectedAuthorityRevision > 0 && input.ExpectedAuthorityRevision != authority.AuthorityRevision {
			return errDatabaseRowPageConflict
		}
		look.ExpectedAuthorityRevision = authority.AuthorityRevision
		_, err = updateDatabaseRowPagePresentationFromPage(txApp, userID, pageID, look)
		return err
	})
	switch {
	case errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err):
		return true, answerPageMetadataMissing(event, userID, pageID)
	case errors.Is(err, errDatabaseRowPageConflict):
		return true, apiError(event, http.StatusConflict, errorCodePageMetadataConflict, rowPageRevisionConflictMessage)
	case errors.Is(err, errDatabaseRowPageInTrash):
		return true, apiError(event, http.StatusConflict, errorCodeDatabaseRowPageInTrash, "A row in Trash can be changed after it's restored.")
	case isLockRefusal(err):
		return true, answerLockRefusal(event, err)
	case isDLPPolicyDecisionError(err):
		return true, writeDLPPolicyError(event, err, "page-metadata")
	case err != nil:
		return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't save the page details.", err)
	}
	updated, err := event.App.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't save the page details.", err)
	}
	response, err := pageRecordResponseWithWiki(event.App, updated)
	if err != nil {
		return true, apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't save the page details.", err)
	}
	pageResponseAsRead(event.App, userID, &response)
	return true, event.JSON(http.StatusOK, pageMetadataResponse{
		Page:    response,
		Receipt: pageMetadataReceipt{MutationID: input.MutationID, Cursor: updated.GetInt("cursor")},
	})
}

// rowPageInTrash says whether a row's page is in the trash as a read of it
// says (pageResponseAsItsRow): its row's trash or its record's.
func rowPageInTrash(app core.App, page *core.Record) bool {
	if page.GetBool("in_trash") {
		return true
	}
	authority, err := loadDatabaseRowPageAuthority(app, page.Id)
	return err == nil && authority.TrashedAt != 0
}

// rowPageMetadataChangesOtherThanLook is a change that writes more of a
// row's page than its look: a title, place or trash state other than the
// page's, a teamspace, position, lock, archive or a display other than the
// table of contents.
func rowPageMetadataChangesOtherThanLook(page *core.Record, inTrash bool, input pageMetadataInput, sent pageMetadataSentFields) bool {
	return sent.Title != nil && input.Title != page.GetString("title") ||
		sent.ParentID != nil && input.ParentID != page.GetString("parent_id") ||
		sent.InTrash != nil && input.InTrash != inTrash ||
		input.TeamspaceID != nil || input.Position != nil || input.IsLocked != nil || input.IsArchived != nil ||
		input.BacklinksDisplay != nil || input.PageCommentsDisplay != nil || input.InlineCommentsDisplay != nil
}
