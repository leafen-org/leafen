package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Admitting a page creation: whether the person may make it where asked,
// told apart by its request ID and the hash of what it asks, and making it.

type pageCreationAdmissionStatus uint8

const (
	pageCreationApplied pageCreationAdmissionStatus = iota
	pageCreationWorkspaceMissing
	pageCreationParentConflict
	pageCreationIdentityReused
	// The person may not add pages under the parent, or at the teamspace's
	// top: they only view it, or don't reach it. A parent no one can add a
	// page to now (a full-page database, being changed or deleted for good —
	// pageCreationParentRefusal —, in another workspace or teamspace) is
	// pageCreationParentConflict.
	pageCreationParentDenied
	// The parent is in the trash, or under a page that is, and the person
	// could add pages to it once it is restored (page_creation_parent_trash.go).
	pageCreationParentInTrash
)

// pageCreationParentRefusal is a parent the person was refused: one an
// operation holds now (pageBusyReason: its blocks being moved, a wiki's
// change, its copy or its workspace moving, after which it takes a page
// again; its permanent deletion, after which it takes none, as a parent in
// the trash) is pageCreationParentConflict, told to a member of its
// workspace as a busy page is (pageBusyForMember); any other is
// pageCreationParentDenied (2026-11-19: a page turning into a wiki answered
// 403 "You can't add pages here."). Databases being copied into a row page
// refuse no one, so they don't count.
func pageCreationParentRefusal(app core.App, principalID, parentID string) pageCreationAdmissionStatus {
	if pageBusyForMemberOf(app, principalID, parentID, false) != "" {
		return pageCreationParentConflict
	}
	return pageCreationParentDenied
}

func admitPageCreation(
	app core.App,
	principalID string,
	input pageCreationAdmissionInput,
) (pageCreateResponse, pageCreationAdmissionStatus, error) {
	response := pageCreateResponse{}
	status := pageCreationApplied
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		response, status, err = admitPageCreationTransaction(txApp, principalID, input)
		return err
	})
	return response, status, err
}

// admitPageCreationTransaction creates a page from the app, which a locked
// parent refuses.
func admitPageCreationTransaction(
	app core.App,
	principalID string,
	input pageCreationAdmissionInput,
) (pageCreateResponse, pageCreationAdmissionStatus, error) {
	return admitPageCreationTransactionWithLock(app, principalID, input, pageLockEnforced)
}

func admitPageCreationTransactionWithLock(
	app core.App,
	principalID string,
	input pageCreationAdmissionInput,
	lock pageLockMode,
) (pageCreateResponse, pageCreationAdmissionStatus, error) {
	authorization, err := authorizeWorkspace(app, principalID, input.WorkspaceID, permissionWorkspaceWrite)
	if isPermissionDenied(err) && input.TeamspaceID == "" {
		// A guest waiting to be a member makes private pages meanwhile
		// (pending_guest_private_pages.go); a sub-page still needs its parent.
		authorization, err = pendingGuestPageCreationWorkspace(app, principalID, input.WorkspaceID)
	}
	if isPermissionDenied(err) && input.ParentID != "" {
		// A guest who may edit a page adds sub-pages to it, as Notion's
		// guests with edit access do (2026-11-06: "/페이지" in a shared page
		// failed for one); the parent's own check below holds them to it.
		authorization, err = pageGrantCreationWorkspace(app, principalID, input.WorkspaceID)
	}
	if isPermissionDenied(err) {
		return pageCreateResponse{}, pageCreationWorkspaceMissing, nil
	}
	if err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	requestHash := pageCreationAdmissionRequestHash(input)
	receipts, err := app.FindRecordsByFilter(
		receiptsCollection, "owner = {:owner} && mutation_id = {:mutationId}", "", 1, 0,
		dbx.Params{"owner": principalID, "mutationId": input.RequestID},
	)
	if err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	if len(receipts) == 1 {
		receipt := receipts[0]
		if receipt.GetString("request_hash") != requestHash {
			return pageCreateResponse{}, pageCreationIdentityReused, nil
		}
		page, err := app.FindRecordById(pagesCollection, receipt.GetString("page"))
		if err != nil {
			return pageCreateResponse{}, pageCreationApplied, err
		}
		if page.GetString("workspace") != authorization.WorkspaceID ||
			page.GetString("owner") != authorization.OwnerID {
			return pageCreateResponse{}, pageCreationIdentityReused, nil
		}
		pageResponse, err := pageRecordResponseWithWiki(app, page)
		if err != nil {
			return pageCreateResponse{}, pageCreationApplied, err
		}
		return pageCreateResponse{
			Page: pageResponse, RequestID: input.RequestID, Duplicate: true,
		}, pageCreationApplied, nil
	}
	teamspaceID := input.TeamspaceID
	if input.ParentID == "" && teamspaceID == "" {
		// A private page, which a restricted member makes only while the
		// organization lets them (organization_request_controls.go).
		if refused, err := restrictedPrivatePagesRefused(app, authorization); err != nil || refused {
			return pageCreateResponse{}, pageCreationParentDenied, err
		}
	}
	if input.ParentID != "" {
		// A sub-page is part of its parent's body, a database page's included.
		parent, err := authorizePageBody(app, principalID, input.ParentID, permissionWorkspaceWrite)
		if err == nil && parent.Workspace.WorkspaceID != authorization.WorkspaceID {
			return pageCreateResponse{}, pageCreationParentConflict, nil
		}
		// Never inside the trash, the parent's or a page's above it.
		if trashed, trashErr := pageCreationParentTrashed(
			app, principalID, authorization.WorkspaceID, input.ParentID, err,
		); trashErr != nil {
			return pageCreateResponse{}, pageCreationApplied, trashErr
		} else if trashed {
			return pageCreateResponse{}, pageCreationParentInTrash, nil
		}
		if isPermissionDenied(err) || isPageInTrashRefusal(err) {
			return pageCreateResponse{}, pageCreationParentRefusal(app, principalID, input.ParentID), nil
		}
		if err != nil {
			return pageCreateResponse{}, pageCreationApplied, err
		}
		fenced, err := wikiMutationFenceActive(app, input.ParentID)
		if err != nil {
			return pageCreateResponse{}, pageCreationApplied, err
		}
		// A locked page still takes new sub-pages, as Notion's sidebar "+" adds
		// them (seen 2026-10-31); its lock guards its own body and title. A
		// full-page database holds only its database; its pages are rows.
		if fenced || isFullPageDatabase(parent.Page) {
			return pageCreateResponse{}, pageCreationParentConflict, nil
		}
		parentTeamspaceID := parent.Page.GetString("teamspace_id")
		if teamspaceID != "" && teamspaceID != parentTeamspaceID {
			return pageCreateResponse{}, pageCreationParentConflict, nil
		}
		teamspaceID = parentTeamspaceID
	} else if teamspaceID != "" {
		teamspaceWorkspace, role, err := effectiveTeamspacePageAccessRole(
			app, principalID, authorization.WorkspaceID, teamspaceID,
		)
		if err != nil && !isPermissionDenied(err) {
			return pageCreateResponse{}, pageCreationApplied, err
		}
		if err != nil || role < pageAccessRoleEdit {
			return pageCreateResponse{}, pageCreationParentDenied, nil
		}
		if teamspaceWorkspace.WorkspaceID != authorization.WorkspaceID {
			return pageCreateResponse{}, pageCreationParentConflict, nil
		}
	}
	if err := enforceOrganizationDLPContent(
		app, authorization.Organization.OrganizationID,
		map[string]any{"title": input.Title, "content": input.Content},
	); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	if input.AnchorBlockID != "" {
		// It hangs on a line, table row or caption of the parent's own, or
		// the parent's title or cell (pageAnchorAdmitted); the server may not
		// have a new line yet, which comes with the parent's next change.
		// Anything else it can't hang on: it gets a block of its own at the
		// body's end, as a sub-page made elsewhere does.
		admitted, err := pageAnchorAdmitted(app, input.ParentID, input.AnchorBlockID)
		if err != nil {
			return pageCreateResponse{}, pageCreationApplied, err
		}
		if !admitted {
			input.AnchorBlockID = ""
		}
	}
	nextCursor, err := nextWorkspaceUserStateCursor(app, authorization.OwnerID)
	if err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	pages, err := app.FindCollectionByNameOrId(pagesCollection)
	if err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	position, err := nextPageSiblingPosition(app, input.WorkspaceID, input.ParentID)
	if err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	page := core.NewRecord(pages)
	page.Id = newPageID()
	if input.PreparedPageID != "" {
		page.Id = input.PreparedPageID
	}
	page.Set("owner", authorization.OwnerID)
	page.Set("workspace", authorization.WorkspaceID)
	page.Set("teamspace_id", teamspaceID)
	page.Set("title", input.Title)
	if input.Kind != pageKindDatabase {
		spans, _ := encodedPageTitleSpans(input.Title, input.TitleSpans)
		page.Set("title_spans", spans)
	}
	if input.ContentInBody {
		page.Set("content", "")
	} else {
		page.Set("content", input.Content)
	}
	page.Set("revision", 0)
	page.Set("cursor", nextCursor)
	page.Set("parent_id", input.ParentID)
	page.Set("position", position)
	if input.AnchorBlockID != "" {
		page.Set(pageAnchorBlockField, input.AnchorBlockID)
	}
	page.Set("kind", pageKindPage)
	if input.Kind == pageKindDatabase {
		page.Set("kind", pageKindDatabase)
	}
	applyPageTrashState(page, false, "")
	initializePageStyleMetadata(page)
	initializePageCustomizationMetadata(page)
	setNewPageCustodianProvenance(page, principalID)
	createdAt := time.Now().UTC().UnixMilli()
	if err := app.Save(page); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	if err := followCreatedPageComments(app, authorization.WorkspaceID, page.Id, principalID, createdAt); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	placement := pageBodyPlacement{Type: "end"}
	if input.BodyPlacement != nil {
		placement = *input.BodyPlacement
	}
	if placement.Type == "after" {
		// The page goes after a top-level block of its parent's body, a
		// sub-page's block included, which must still be there.
		var anchors int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_blocks}}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[page_id]] = {:parent} AND [[parent_id]] = ''
		`).Bind(dbx.Params{
			"id": placement.AfterID, "owner": authorization.OwnerID, "parent": input.ParentID,
		}).Row(&anchors); err != nil {
			return pageCreateResponse{}, pageCreationApplied, err
		}
		if anchors == 0 {
			return pageCreateResponse{}, pageCreationParentConflict,
				graphInputError(errorCodeGraphReferenceInvalid, pagePositionReferenceGone)
		}
	}
	if input.AnchorBlockID != "" {
		// It shows in the sidebar where its line stands, one on the
		// parent's title first (childPageBodyOrder); one on a cell, last
		// among the database page's sub-pages, where it was made.
		if !strings.HasPrefix(input.AnchorBlockID, pageAnchorCellPrefix) {
			if err := alignMentionAnchoredPages(app, input.ParentID, []string{page.Id}); err != nil {
				return pageCreateResponse{}, pageCreationApplied, err
			}
		}
	} else if !input.BodyBlockPlaced {
		if err := placeMadeChildPageBlock(app, authorization, input, page.Id, principalID, placement); err != nil {
			return pageCreateResponse{}, pageCreationApplied, err
		}
	}
	if err := ensureCreatedPageHistoryBaseline(app, page.Id); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	if !input.Imported {
		if err := expectPagePersonMentions(app, page.Id, createdAt); err != nil {
			return pageCreateResponse{}, pageCreationApplied, err
		}
	}
	if err := recordWorkspacePageCreated(app, page, principalID, createdAt); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	if err := recordNotionAdminLegalHoldPageInteraction(app, pageCreatedInteraction(
		authorization.Organization.OrganizationID,
		authorization.WorkspaceID, page.Id, principalID, createdAt,
	)); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	if err := markSearchDirty(app, page); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	receiptCollection, err := app.FindCollectionByNameOrId(receiptsCollection)
	if err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	receipt := core.NewRecord(receiptCollection)
	receipt.Set("owner", principalID)
	receipt.Set("page", page.Id)
	receipt.Set("mutation_id", input.RequestID)
	receipt.Set("request_hash", requestHash)
	receipt.Set("cursor", nextCursor)
	if err := app.Save(receipt); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	created := pageRecordResponse(page)
	// A page made in a button's draft is the button's template.
	if created.ButtonTemplate, err = isButtonTemplatePage(app, page.Id); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	// One hanging on a table row's cells or a caption stays out of the sidebar.
	if created.FieldAnchored, err = isFieldAnchoredPage(app, page); err != nil {
		return pageCreateResponse{}, pageCreationApplied, err
	}
	return pageCreateResponse{
		Page: created, RequestID: input.RequestID, Duplicate: false,
	}, pageCreationApplied, nil
}

func pageCreationAdmissionRequestHash(input pageCreationAdmissionInput) string {
	if input.AnchorBlockID != "" {
		named := input
		named.AnchorBlockID = ""
		sum := sha256.Sum256([]byte("page-create-anchor\x00" + pageCreateHashField(pageCreationAdmissionRequestHash(named)) +
			pageCreateHashField(input.AnchorBlockID)))
		return hex.EncodeToString(sum[:])
	}
	placement := ""
	if input.BodyPlacement != nil {
		placement = input.BodyPlacement.Type + "\x00" + input.BodyPlacement.AfterKind + "\x00" + input.BodyPlacement.AfterID
	}
	if placement != "" {
		payload := "page-create-v5\x00" + pageCreateHashField(input.WorkspaceID) +
			pageCreateHashField(input.TeamspaceID) + pageCreateHashField(input.ParentID) +
			pageCreateHashField(input.Title) + pageCreateHashField(input.Content) +
			pageCreateHashField(input.PreparedPageID) + pageCreateHashField(placement)
		sum := sha256.Sum256([]byte(payload))
		return hex.EncodeToString(sum[:])
	}
	if input.PreparedPageID != "" {
		payload := "page-create-v4\x00" + pageCreateHashField(input.WorkspaceID) +
			pageCreateHashField(input.TeamspaceID) + pageCreateHashField(input.ParentID) +
			pageCreateHashField(input.Title) + pageCreateHashField(input.Content) +
			pageCreateHashField(input.PreparedPageID)
		sum := sha256.Sum256([]byte(payload))
		return hex.EncodeToString(sum[:])
	}
	if input.TeamspaceID != "" {
		payload := "page-create-v3\x00" + pageCreateHashField(input.WorkspaceID) +
			pageCreateHashField(input.TeamspaceID) + pageCreateHashField(input.ParentID) +
			pageCreateHashField(input.Title) + pageCreateHashField(input.Content)
		sum := sha256.Sum256([]byte(payload))
		return hex.EncodeToString(sum[:])
	}
	if input.Content == "" {
		return pageCreateRequestHash(pageCreateInput{
			WorkspaceID: input.WorkspaceID, ParentID: input.ParentID, Title: input.Title,
		})
	}
	payload := "page-create-v2\x00" + pageCreateHashField(input.WorkspaceID) +
		pageCreateHashField(input.ParentID) + pageCreateHashField(input.Title) +
		pageCreateHashField(input.Content)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func pageCreateRequestHash(input pageCreateInput) string {
	if input.TeamspaceID != "" {
		payload := "page-create-v3\x00" + pageCreateHashField(input.WorkspaceID) +
			pageCreateHashField(input.TeamspaceID) + pageCreateHashField(input.ParentID) +
			pageCreateHashField(input.Title) + pageCreateHashField("")
		sum := sha256.Sum256([]byte(payload))
		return hex.EncodeToString(sum[:])
	}
	payload := "page-create-v1\x00" + pageCreateHashField(input.WorkspaceID) +
		pageCreateHashField(input.ParentID) + pageCreateHashField(input.Title)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func pageCreateHashField(value string) string {
	return strconv.Itoa(len(value)) + ":" + value + ";"
}
