package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageMutationStatus int

const (
	pageMutationApplied pageMutationStatus = iota
	pageMutationPageMissing
	pageMutationCursorConflict
	pageMutationIdentityReused
	pageMutationReplacementLocked
	pageMutationPageLocked
)

// applyPageMutation changes a page's scalar content for MCP, which the page
// lock does not stop.
func applyPageMutation(
	app core.App,
	principalID string,
	pageID string,
	input pageMutationInput,
) (pageMutationResponse, pageMutationStatus, error) {
	return applyPageMutationWithLock(app, principalID, pageID, input, pageLockIgnored)
}

func applyPageMutationWithLock(
	app core.App,
	principalID string,
	pageID string,
	input pageMutationInput,
	lock pageLockMode,
) (pageMutationResponse, pageMutationStatus, error) {
	response := pageMutationResponse{}
	status := pageMutationApplied
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizePage(txApp, principalID, pageID, permissionWorkspaceWrite)
		if isPermissionDenied(err) {
			status = pageMutationPageMissing
			return nil
		}
		if err != nil {
			return err
		}
		receipts, err := txApp.FindRecordsByFilter(
			receiptsCollection,
			"owner = {:owner} && mutation_id = {:mutationId}",
			"",
			1,
			0,
			dbx.Params{"owner": principalID, "mutationId": input.MutationID},
		)
		if err != nil {
			return err
		}
		requestHash := pageMutationRequestHash(pageID, input)
		if len(receipts) == 1 {
			receipt := receipts[0]
			if receipt.GetString("page") != pageID || receipt.GetString("request_hash") != requestHash {
				status = pageMutationIdentityReused
				return nil
			}
			pageResponse, responseErr := pageRecordResponseWithWiki(txApp, authorization.Page)
			if responseErr != nil {
				return responseErr
			}
			response = pageMutationResponse{
				Page: pageResponse,
				Receipt: mutationReceiptResponse{
					MutationID: input.MutationID,
					Cursor:     receipt.GetInt("cursor"),
					Duplicate:  true,
				},
			}
			return nil
		}
		leased, err := workspaceMarkdownReplacePageLeased(txApp, pageID, "")
		if err != nil {
			return err
		}
		if leased {
			status = pageMutationReplacementLocked
			return nil
		}
		if err := enforceOrganizationDLPContent(
			txApp, authorization.Workspace.Organization.OrganizationID, input.Content,
		); err != nil {
			return err
		}

		page := authorization.Page
		if page.GetInt("cursor") != input.BaseCursor {
			status = pageMutationCursorConflict
			return nil
		}
		if lock == pageLockEnforced {
			locked, err := pageLockedFor(txApp, page, principalID)
			if err != nil {
				return err
			}
			if locked {
				status = pageMutationPageLocked
				return nil
			}
		}
		if err := ensurePageHistoryBaseline(txApp, pageID); err != nil {
			return err
		}

		nextCursor, err := nextWorkspaceUserStateCursor(txApp, authorization.Workspace.OwnerID)
		if err != nil {
			return err
		}

		page.Set("content", input.Content)
		page.Set("revision", page.GetInt("revision")+1)
		page.Set("cursor", nextCursor)
		setPageLastEditor(page, principalID)
		editedAt := time.Now().UTC().UnixMilli()
		if err := txApp.Save(page); err != nil {
			return err
		}
		if err := recordNotionAdminLegalHoldPageInteraction(txApp, pageEditedInteraction(
			"page-mutation", pageID+"\x00"+principalID+"\x00"+input.MutationID,
			authorization.Workspace.Organization.OrganizationID,
			authorization.Workspace.WorkspaceID, pageID, principalID, editedAt,
		)); err != nil {
			return err
		}
		if err := markSearchDirty(txApp, page); err != nil {
			return err
		}
		if err := recordPageHistoryPageMutation(txApp, pageID, principalID); err != nil {
			return err
		}

		receiptCollection, err := txApp.FindCollectionByNameOrId(receiptsCollection)
		if err != nil {
			return err
		}
		receipt := core.NewRecord(receiptCollection)
		receipt.Set("owner", principalID)
		receipt.Set("page", page.Id)
		receipt.Set("mutation_id", input.MutationID)
		receipt.Set("request_hash", requestHash)
		receipt.Set("cursor", nextCursor)
		if err := txApp.Save(receipt); err != nil {
			return err
		}
		pageResponse, responseErr := pageRecordResponseWithWiki(txApp, page)
		if responseErr != nil {
			return responseErr
		}
		response = pageMutationResponse{
			Page: pageResponse,
			Receipt: mutationReceiptResponse{
				MutationID: input.MutationID,
				Cursor:     nextCursor,
				Duplicate:  false,
			},
		}
		return nil
	})
	return response, status, err
}

func pageMutationRequestHash(pageID string, input pageMutationInput) string {
	payload := pageID + "\x00" + strconv.Itoa(input.BaseCursor) + "\x00" + input.Content
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}
