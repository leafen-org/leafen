package app

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A database page takes an icon, a cover and a style as a page does, from
// the page itself: the same icons, covers, fonts, small text and full width,
// checked as a page's are, and its cover shown where its position says.

func setDatabaseRowPagePresentation(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	input := databaseRowPagePresentationInput{}
	if rowID == "" || event.BindBody(&input) != nil || !normalizeDatabaseRowPagePresentationInput(&input) {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the icon, cover and style details.")
	}
	response, err := updateDatabaseRowPagePresentationFromPage(event.App, user.Id, rowID, input)
	switch {
	case errors.Is(err, errDatabaseRowPageNotFound):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	case errors.Is(err, errDatabaseRowPageConflict):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageConflict, "The database row changed. Try again.")
	case isLockRefusal(err):
		return answerLockRefusal(event, err)
	case isDLPPolicyDecisionError(err):
		return writeDLPPolicyError(event, err, "database-row-page")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
			"Couldn't change the page's icon, cover or style.", "database_row_page", "presentation",
			errorCodeDatabaseRowPageFailed, err,
		)
	default:
		return event.JSON(http.StatusOK, response)
	}
}

// normalizeDatabaseRowPagePresentationInput checks an icon, cover and style
// change as a page's is.
func normalizeDatabaseRowPagePresentationInput(input *databaseRowPagePresentationInput) bool {
	if input.ExpectedAuthorityRevision < 1 ||
		input.Icon == nil && input.IconType == nil && input.Cover == nil && input.CoverPosition == nil &&
			input.Font == nil && input.SmallText == nil && input.FullWidth == nil && input.TableOfContentsDisplay == nil {
		return false
	}
	presentation := pageMetadataInput{
		Icon: input.Icon, IconType: input.IconType, Cover: input.Cover, CoverPosition: input.CoverPosition,
		Font: input.Font, SmallText: input.SmallText, FullWidth: input.FullWidth,
	}
	trimPagePresentationInput(&presentation)
	input.Icon, input.IconType, input.Cover = presentation.Icon, presentation.IconType, presentation.Cover
	return validPagePresentationInput(presentation) && validPageStyleMetadataInput(presentation) &&
		(input.TableOfContentsDisplay == nil || validPageTableOfContentsDisplay(*input.TableOfContentsDisplay))
}

func updateDatabaseRowPagePresentationFromPage(
	app core.App,
	principalID, rowID string,
	input databaseRowPagePresentationInput,
) (databaseRowPageActionResponse, error) {
	response := databaseRowPageActionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authority, err := authorizeDatabaseRowOrTemplatePage(txApp, principalID, rowID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		// A template's look is its page record's alone, set last-wins; its
		// icon is the template's own, changed with the template
		// (database_template_row_page.go).
		if authority.Template && input.Icon != nil {
			return errDatabaseRowPageConflict
		}
		if !authority.Template && authority.AuthorityRevision != input.ExpectedAuthorityRevision {
			return errDatabaseRowPageConflict
		}
		if locked, err := databaseRowPageLockedFor(txApp, authority, principalID); err != nil {
			return err
		} else if locked {
			return pageLockedError()
		}
		// A new icon or cover meets the DLP policy as a page's does (database_row_page_dlp.go),
		// and a file it shows is a picture (look_picture_files.go).
		if err := enforceDatabaseRowPageLookDLP(txApp, authority.WorkspaceID, rowID, input.Icon, input.Cover); err != nil {
			return err
		}
		if err := checkGivenLookFiles(txApp, authority.WorkspaceID, givenImageIcon("icon", input.Icon, input.IconType, authority.Icon),
			givenCover("cover", input.Cover, authority.Cover)); err != nil {
			return err
		}
		if input.Icon != nil {
			if err := updateDatabaseRowPagePresentation(
				txApp, rowID, authority.WorkspaceID, authority.ContainerPageID, principalID, *input.Icon, *input.IconType,
			); err != nil {
				return err
			}
		}
		changed := input.Icon != nil
		if err := setDatabaseRowPageStyle(txApp, rowID, authority.WorkspaceID, principalID, input.Font, input.SmallText, input.FullWidth); err != nil {
			return err
		}
		if input.TableOfContentsDisplay != nil {
			found, err := setRowPageRecordCustomization(txApp, rowID, authority.WorkspaceID, principalID, input.TableOfContentsDisplay, nil)
			if err != nil {
				return err
			}
			if !found {
				return errDatabaseRowPageConflict
			}
		}
		if input.Cover != nil || input.CoverPosition != nil {
			cover := authority.Cover
			if input.Cover != nil {
				cover = *input.Cover
			}
			found, err := setRowPageRecordCover(txApp, rowID, authority.WorkspaceID, principalID, cover, input.CoverPosition)
			if err != nil {
				return err
			}
			if !found {
				return errDatabaseRowPageConflict
			}
			// The cover's file is the page's, as a page's cover is.
			if err := replaceProductFilePageReferences(txApp, authority.WorkspaceID, rowID, "", "none", cover); err != nil {
				return err
			}
		}
		// Setting the icon moves the page's revision on; a cover or style
		// alone moves it here.
		if !changed && !authority.Template {
			if err := touchDatabaseRowPageAuthority(txApp, rowID, time.Now().UTC().UnixMilli()); err != nil {
				return err
			}
		}
		// The views draw its icon and cover, not its style.
		if !authority.Template && (input.Icon != nil || input.Cover != nil || input.CoverPosition != nil) {
			if err := announceDatabaseRowPageChange(txApp, rowID); err != nil {
				return err
			}
		}
		response = databaseRowPageActionResponse{
			RowID: rowID, ContainerPageID: authority.ContainerPageID,
			AuthorityRevision: authority.AuthorityRevision + 1,
		}
		if authority.Template {
			response.AuthorityRevision = authority.AuthorityRevision
		}
		return nil
	})
	return response, err
}

// setDatabaseRowPageStyle sets a database page's font, small text and full
// width, as its "..." menu does, each left as it is when nil: its page
// record's, which the page metadata writer doesn't take (authorizePage opens
// a database page only to read). The screen and MCP's page update both go
// through here; the views draw none of it.
func setDatabaseRowPageStyle(app core.App, rowID, workspaceID, actorID string, font *string, smallText, fullWidth *bool) error {
	if font == nil && smallText == nil && fullWidth == nil {
		return nil
	}
	found, err := setRowPageRecordStyle(app, rowID, workspaceID, actorID, font, smallText, fullWidth)
	if err != nil {
		return err
	}
	if !found {
		return errDatabaseRowPageConflict
	}
	return nil
}
