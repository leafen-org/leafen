package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageCreationAdmissionInput struct {
	RequestID   string
	WorkspaceID string
	TeamspaceID string
	ParentID    string
	Title       string
	// TitleSpans are a title's mentions, links and formatting
	// (page_title_spans.go); none for its plain words.
	TitleSpans []graphTextSpan
	Content    string
	// ContentInBody tells that the caller writes Content into the page's
	// body itself; the request is still told apart by it, but the page
	// record keeps no copy.
	ContentInBody  bool
	PreparedPageID string
	BodyPlacement  *pageBodyPlacement
	// BodyBlockPlaced tells that the caller puts the page's block in its
	// parent's body itself, as turning a block into a page does.
	BodyBlockPlaced bool
	// AnchorBlockID is the parent's line, table row or captioned block the
	// page is made from, by its mention there, or "title" or "cell:" and a
	// property for the parent's title or cell: it hangs on that, with no
	// block of its own (mention_anchored_pages.go, mention_anchored_fields.go).
	AnchorBlockID string
	// Imported is a page coming with its text: what it mentions notifies nobody.
	Imported bool
	// Kind is pageKindDatabase for a full-page database; empty is a page.
	Kind string
}

// pagePositionReferenceGone refuses a new page placed after a block its
// parent's body doesn't hold at its top.
const pagePositionReferenceGone = "The page position reference is no longer available."

// createPage makes a page; one made under a wiki's page, or under a page of
// a wiki, becomes the wiki database's page.
func createPage(ygo *pocketBaseYgoPersistence) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		return createPageIn(event, ygo)
	}
}

func createPageIn(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	var input pageCreateInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput,
			refusalMessage("Check the page details.", bodyRefusalReason("page request", err)))
	}
	admissionInput := normalizePageCreationAdmissionInput(pageCreationAdmissionInput{
		RequestID: input.RequestID, WorkspaceID: input.WorkspaceID,
		TeamspaceID: input.TeamspaceID, ParentID: input.ParentID, Title: input.Title,
		// A page made on a device first comes with the ID it has there, which
		// the changes kept for it on that device name.
		PreparedPageID: input.PageID, AnchorBlockID: input.AnchorBlockID,
	})
	if !validPageCreationAdmissionInput(admissionInput) {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput,
			refusalMessage("Check the page details.", pageCreationInputProblem(admissionInput)))
	}
	response, status, err := admitPageCreation(event.App, user.Id, admissionInput)
	if status == pageCreationWorkspaceMissing {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if status == pageCreationParentConflict {
		return apiErrorFrom(event, http.StatusConflict, errorCodePageCreateParentConflict, "A page can't be created here.",
			pageCreationPlaceRefused(event.App, user.Id, admissionInput))
	}
	if status == pageCreationParentInTrash {
		return apiError(event, http.StatusConflict, errorCodePageInTrash, pageInTrashMessage)
	}
	if status == pageCreationParentDenied {
		return apiErrorFrom(event, http.StatusForbidden, errorCodePermissionDenied, "You can't add pages here.",
			pageCreationDeniedRefused(event.App, user.Id, admissionInput))
	}
	if status == pageCreationIdentityReused {
		return apiError(event, http.StatusConflict, errorCodePageCreateIdentityReused, "This ID was used for a different page creation.")
	}
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "page-create")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageCreateFailed, "Couldn't create the page.", err)
	}
	if ygo != nil {
		adopted, err := ygo.adoptPageTreeIntoWiki(event.Request.Context(), user.Id, response.Page.ID)
		if err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePageCreateFailed,
				"Couldn't create the page.", "page_create", "wiki", errorCodePageCreateFailed, err)
		}
		if adopted {
			// It goes last under the page above it in the wiki (wiki_page_links.go).
			err := placeAddedWikiPagesLast(event.App, []string{response.Page.ID})
			page, findErr := event.App.FindRecordById(pagesCollection, response.Page.ID)
			if err == nil && findErr == nil {
				response.Page, err = pageRecordResponseWithWiki(event.App, page)
			}
			err = errors.Join(err, findErr)
			if err != nil {
				return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageCreateFailed, "Couldn't create the page.", err)
			}
		}
	}
	return event.JSON(http.StatusCreated, response)
}

func normalizePageCreationAdmissionInput(input pageCreationAdmissionInput) pageCreationAdmissionInput {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.TeamspaceID = strings.TrimSpace(input.TeamspaceID)
	input.ParentID = strings.TrimSpace(input.ParentID)
	// A title is kept as typed, the spaces at its ends too, as Notion keeps
	// one typed in its app (seen 2026-11-13: " W886끝 뒤 " and "   ").
	input.PreparedPageID = strings.TrimSpace(input.PreparedPageID)
	input.AnchorBlockID = strings.TrimSpace(input.AnchorBlockID)
	return input
}

func validPageCreationAdmissionInput(input pageCreationAdmissionInput) bool {
	return input.RequestID != "" && len([]rune(input.RequestID)) <= maxPageCreationRequestIDLength &&
		input.WorkspaceID != "" && len(input.WorkspaceID) <= maxWorkspacePermissionIdentifierLength &&
		len(input.TeamspaceID) <= maxWorkspacePermissionIdentifierLength &&
		len(input.ParentID) <= maxWorkspacePermissionIdentifierLength &&
		(input.PreparedPageID == "" || validPageRecordID(input.PreparedPageID)) &&
		len(input.AnchorBlockID) <= maxWorkspacePermissionIdentifierLength &&
		(input.AnchorBlockID == "" || input.ParentID != "" && input.BodyPlacement == nil) &&
		len([]rune(input.Title)) <= maxGraphTitleRunes &&
		validPageBodyPlacement(input.BodyPlacement)
}

func validPageBodyPlacement(placement *pageBodyPlacement) bool {
	if placement == nil {
		return true
	}
	switch placement.Type {
	case "start", "end":
		return placement.AfterKind == "" && placement.AfterID == ""
	case "after":
		return (placement.AfterKind == pageBodyItemPage || placement.AfterKind == pageBodyItemBlock) &&
			placement.AfterID != ""
	default:
		return false
	}
}

// pageCreationPlaceRefused says why the place a new page was asked for takes
// none, read again as the creation read it (2026-11-26: each was 409 "A page
// can't be created here."): a parent of another workspace as such
// (other_workspace_refusal.go), a full-page database parent, or a teamspace
// other than its parent's, each with its field; nil leaves a parent being
// changed the conflict it is. (A teamspace of another workspace is refused
// before, as one the person can't add pages to.)
func pageCreationPlaceRefused(app core.App, principalID string, input pageCreationAdmissionInput) error {
	crossing := workspaceCrossing{subject: "the new page", from: input.WorkspaceID, made: true}
	if to := pageInOtherWorkspace(app, principalID, input.ParentID, input.WorkspaceID); to != "" {
		crossing.place, crossing.to = "its parent "+input.ParentID, to
		return otherWorkspaceRefused(app, "parentId", crossing)
	}
	parent, err := authorizePageBody(app, principalID, input.ParentID, permissionWorkspaceWrite)
	switch {
	case err != nil:
		return nil
	case isFullPageDatabase(parent.Page):
		return refusedFor(http.StatusBadRequest, errorCodePageCreateParentConflict, "Check parentId: that page is a full-page database, "+
			"which holds only its database; add the page to it as a row, its data source as the parent.",
			apiErrorField{Name: "parentId", Problem: "takes no page"})
	case input.TeamspaceID != "" && input.TeamspaceID != parent.Page.GetString("teamspace_id"):
		return refusedFor(http.StatusBadRequest, errorCodePageCreateParentConflict, "Check teamspaceId: a page made under a parent goes "+
			"in its parent's teamspace; leave teamspaceId out, or name the parent's.", apiErrorField{Name: "teamspaceId", Problem: "not the parent's"})
	}
	return nil
}

// pageCreationDeniedRefused says why the person may not add the new page
// where it was asked for (2026-11-26: each was 403 "You can't add pages
// here."): a teamspace of another workspace as such (other_workspace_refusal.go);
// a teamspace or a parent they may only view, or don't reach, with its field;
// else the organization that keeps its restricted members from making
// private pages.
func pageCreationDeniedRefused(app core.App, principalID string, input pageCreationAdmissionInput) error {
	if to := teamspaceInOtherWorkspace(app, principalID, input.WorkspaceID, input.TeamspaceID); to != "" {
		return otherWorkspaceRefused(app, "teamspaceId", workspaceCrossing{
			subject: "the new page", from: input.WorkspaceID, place: "the teamspace " + input.TeamspaceID, to: to, made: true,
		})
	}
	switch {
	case input.ParentID != "":
		return refusedFor(http.StatusForbidden, errorCodePermissionDenied, "Check parentId: you can't add pages under that page; "+
			"you may only view it, or don't reach it. Someone who edits it can, or can let you.",
			apiErrorField{Name: "parentId", Problem: "not one you can add to"})
	case input.TeamspaceID != "":
		return refusedFor(http.StatusForbidden, errorCodePermissionDenied, "Check teamspaceId: you can't add pages to that teamspace; "+
			"you may only view it, or aren't in it. Its owners can let you, or name one you edit in.",
			apiErrorField{Name: "teamspaceId", Problem: "not one you can add to"})
	}
	return refusedFor(http.StatusForbidden, errorCodePermissionDenied, "The organization keeps its restricted members from making "+
		"private pages: name a teamspace (teamspaceId) or a page (parentId) to make it in.")
}

// teamspaceInOtherWorkspace is the workspace of a teamspace the person
// reaches there, when it isn't here; "" for one of here, or one they don't reach.
func teamspaceInOtherWorkspace(app core.App, principalID, here, teamspaceID string) string {
	workspaceID := ""
	if err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_teamspaces}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": strings.TrimSpace(teamspaceID)}).Row(&workspaceID); err != nil || workspaceID == "" || workspaceID == here {
		return ""
	}
	if _, _, err := effectiveTeamspacePageAccessRole(app, principalID, workspaceID, teamspaceID); err != nil {
		return ""
	}
	return workspaceID
}
