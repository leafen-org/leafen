package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func bindDatabaseRowPageAction(event *core.RequestEvent) (string, databaseRowPageActionInput, error) {
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	input := databaseRowPageActionInput{}
	if rowID == "" || event.BindBody(&input) != nil {
		return "", input, errDatabaseRowPageConflict
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.TargetDataSourceID = strings.TrimSpace(input.TargetDataSourceID)
	input.TargetPageID = strings.TrimSpace(input.TargetPageID)
	if input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > 1024 ||
		input.ExpectedAuthorityRevision < 1 || input.AsHeld || len(input.Position) > maxGraphPositionLength {
		return "", input, errDatabaseRowPageConflict
	}
	return rowID, input, nil
}

// bindDatabaseRowActionAsHeld binds a row's trash, restore or move, which
// alone may go as the server holds the row (asHeld) with no revision: a
// change kept on a device and sent as its page goes, with no time to read the
// row first (database_row_trash_actions.go, rowMoveKept.ts).
func bindDatabaseRowActionAsHeld(event *core.RequestEvent) (string, databaseRowPageActionInput, error) {
	rowID := strings.TrimSpace(event.Request.PathValue("rowId"))
	input := databaseRowPageActionInput{}
	if rowID == "" || event.BindBody(&input) != nil {
		return "", input, errDatabaseRowPageConflict
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.TargetDataSourceID = strings.TrimSpace(input.TargetDataSourceID)
	input.TargetPageID = strings.TrimSpace(input.TargetPageID)
	if input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > 1024 ||
		input.ExpectedAuthorityRevision < 1 && !input.AsHeld || len(input.Position) > maxGraphPositionLength {
		return "", input, errDatabaseRowPageConflict
	}
	return rowID, input, nil
}

func databaseRowPageActionIdentity(action, principalID, rowID, requestID string) string {
	digest := sha256.Sum256([]byte(action + "\x00" + principalID + "\x00" + rowID + "\x00" + requestID))
	return "row-page-" + action + "-" + hex.EncodeToString(digest[:])
}

func databaseRowPageActionRequestHash(action, rowID string, input databaseRowPageActionInput) string {
	accepted := append([]string{}, input.AcceptDroppedPropertyIDs...)
	sort.Strings(accepted)
	request := action + "\x00" + rowID + "\x00" + strconv.Itoa(input.ExpectedAuthorityRevision) +
		"\x00" + input.TargetDataSourceID + "\x00" + input.TargetPageID + "\x00" + strconv.FormatBool(input.TargetTopLevel) +
		"\x00" + strings.Join(accepted, "\x00")
	// The place at whose top it goes is part of its request.
	if input.TargetTeamspaceID != nil {
		request += "\x00place\x00" + *input.TargetTeamspaceID
	}
	// A copy's position is part of its request, as a move's taking it out of
	// the trash is, and a trash's going as the row is held.
	if input.Position != "" {
		request += "\x00position\x00" + input.Position
	}
	if input.FromTrash {
		request += "\x00fromTrash"
	}
	if input.AsHeld {
		request += "\x00asHeld"
	}
	digest := sha256.Sum256([]byte(request))
	return hex.EncodeToString(digest[:])
}

// databaseRowPageDuplicateRowID names a person's copy of a row by the request,
// so a retry makes the same copy; the web client works out the same ID before
// it asks (duplicateRowCopyId, web/src/databaseRowPageApi.ts) to draw the copy
// in its place at the press, before it comes.
func databaseRowPageDuplicateRowID(principalID, rowID, requestID string) string {
	return derivedPageID("duplicate", principalID, rowID, requestID)
}

func optionalDatabaseRowParent(parentID string) *string {
	if parentID == "" {
		return nil
	}
	value := parentID
	return &value
}

func (p *pocketBaseYgoPersistence) duplicateDatabaseRowPage(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID, input, err := bindDatabaseRowPageAction(event)
	if err != nil || input.TargetDataSourceID != "" || input.Preview || len(input.AcceptDroppedPropertyIDs) > 0 {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the duplication details.")
	}
	preliminary, err := authorizeDatabaseRowPage(event.App, user.Id, rowID, permissionWorkspaceWrite)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "duplicate-authorize")
	}
	duplicateRowID := databaseRowPageDuplicateRowID(user.Id, rowID, input.RequestID)
	first, stage, err := p.copyDatabaseRowPage(event.Request.Context(), event.App, user.Id, rowID, input, preliminary)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, stage)
	}
	authority, err := authorizeDatabaseRowPage(event.App, user.Id, duplicateRowID, permissionWorkspaceRead)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "duplicate-response")
	}
	status := http.StatusCreated
	if first.Duplicate {
		status = http.StatusOK
	}
	// Its body's own databases come after it (database_row_page_database_copies.go).
	copying, err := rowPageDatabasesCopying(event.App, duplicateRowID)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "duplicate-response")
	}
	return event.JSON(status, databaseRowPageActionResponse{
		RowID: duplicateRowID, ContainerPageID: authority.ContainerPageID,
		AuthorityRevision: authority.AuthorityRevision, Duplicate: first.Duplicate, CopyingDatabases: copying,
	})
}

// copyDatabaseRowPage copies a database page and its sub-items as the
// person, the app's duplicate and MCP's alike, the copy's ID
// databaseRowPageDuplicateRowID's; stage names the step an error came from.
// The tree is copied a change at a time, parents first. Each change is named
// by the rows it copies and the copies' IDs come from their sources, so a
// retry of the request makes the same copies and finishes the rest.
func (p *pocketBaseYgoPersistence) copyDatabaseRowPage(
	ctx context.Context, app core.App, principalID, rowID string,
	input databaseRowPageActionInput, preliminary databaseRowPageAuthority,
) (graphMutationReceipt, string, error) {
	mutationID := databaseRowPageActionIdentity("duplicate", principalID, rowID, input.RequestID)
	tree, err := readDatabaseRowDuplicateTree(app, preliminary, principalID, input.RequestID)
	if err != nil {
		return graphMutationReceipt{}, "duplicate-tree", err
	}
	batches, err := tree.partition(app, preliminary.OwnerID, preliminary.ContainerPageID)
	if err != nil {
		return graphMutationReceipt{}, "duplicate-tree", err
	}
	var first graphMutationReceipt
	for index, batch := range batches {
		id := mutationID
		if index > 0 {
			id = fmt.Sprintf("%s:%s:%s", mutationID, tree.Rows[batch.From].ID, tree.Rows[batch.To-1].ID)
		}
		receipt, err := p.applyDatabaseRowDuplicateBatch(ctx, principalID, rowID, input, preliminary, id, tree, batch, index == 0)
		if err != nil {
			return graphMutationReceipt{}, "duplicate", err
		}
		if index == 0 {
			first = receipt
		}
	}
	return first, "", nil
}

// applyDatabaseRowDuplicateBatch copies one change's rows; the first change
// holds the item and checks the revision the request named.
func (p *pocketBaseYgoPersistence) applyDatabaseRowDuplicateBatch(
	ctx context.Context, principalID, rowID string, input databaseRowPageActionInput,
	preliminary databaseRowPageAuthority, mutationID string, tree databaseRowDuplicateTree,
	batch databaseRowDuplicateBatch, first bool,
) (graphMutationReceipt, error) {
	duplicates := []databaseRowDuplicate{}
	return p.applyContentGraphMutationContext(
		ctx, principalID, preliminary.ContainerPageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				authority, authErr := authorizeDatabaseRowPage(
					txApp, principalID, rowID, permissionWorkspaceWrite,
				)
				if authErr != nil {
					return pageAuthorization{}, authErr
				}
				if authority.ContainerPageID != preliminary.ContainerPageID {
					return pageAuthorization{}, errDatabaseRowPageConflict
				}
				return authorizePageBody(txApp, principalID, authority.ContainerPageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(_ core.App, _ pageAuthorization, _ *crdt.Doc) (string, error) {
				return databaseRowPageActionRequestHash("duplicate", rowID, input), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				authority, loadErr := authorizeDatabaseRowPage(
					txApp, principalID, rowID, permissionWorkspaceWrite,
				)
				if loadErr != nil {
					return contentGraphMutationPreparation{}, loadErr
				}
				if authority.ContainerPageID != preliminary.ContainerPageID ||
					first && authority.AuthorityRevision != input.ExpectedAuthorityRevision {
					return contentGraphMutationPreparation{}, errDatabaseRowPageConflict
				}
				operations, planned, planErr := planDatabaseRowDuplicate(txApp, authority, principalID, tree, batch, input.Position)
				if planErr != nil {
					return contentGraphMutationPreparation{}, planErr
				}
				duplicates = planned
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
			AfterApply: func(txApp core.App, authorization pageAuthorization, _ *crdt.Doc) error {
				for _, duplicate := range duplicates {
					// A copy made in the app tells the people its text mentions, as
					// a page's does.
					if pageHistoryConnectionOf(txApp).Name == "" {
						if err := expectPagePersonMentions(txApp, duplicate.TargetID, time.Now().UTC().UnixMilli()); err != nil {
							return err
						}
					}
					if err := duplicateDatabaseRowPagePresentationAndBody(
						txApp, duplicate.SourceID, duplicate.TargetID, authorization.Workspace.WorkspaceID, principalID,
						pointRowPageBodySyncedCopiesAtRows(txApp, tree.Targets),
					); err != nil {
						return err
					}
				}
				return nil
			},
		},
	)
}

func (p *pocketBaseYgoPersistence) moveDatabaseRowPage(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID, input, err := bindDatabaseRowActionAsHeld(event)
	toPage := input.TargetPageID != "" || input.TargetTopLevel
	if err != nil || (input.TargetDataSourceID == "") == !toPage || input.TargetPageID != "" && input.TargetTopLevel || input.Position != "" ||
		input.TargetTeamspaceID != nil && !input.TargetTopLevel || input.FromTrash && !toPage || input.AsHeld && input.Preview {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the move details: send requestId (1 to 1,024 "+
			"characters), expectedAuthorityRevision as the row was read, and targetDataSourceId to move it to another database, or "+
			"targetPageId or targetTopLevel (with targetTeamspaceId, or none for private pages) to move it out as a page, with fromTrash "+
			"to take it out of the Trash as it goes (a cut row pasted on a page); a move takes no position.")
	}
	// One in the trash is read as such when the move takes it out of it (page_row_conversion_out_of_trash.go).
	authorizeRow := authorizeDatabaseRowPage
	if input.FromTrash {
		authorizeRow = authorizeDatabaseRowPageInTrash
	}
	preliminary, err := authorizeRow(event.App, user.Id, rowID, permissionWorkspaceWrite)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "move-authorize")
	}
	if toPage {
		return p.moveDatabaseRowPageOut(event, user.Id, rowID, input, preliminary)
	}
	named := input.TargetDataSourceID
	input.TargetDataSourceID = databaseRowPageMoveSource(event.App, named)
	targetPageID, err := databaseRowPageMoveTargetPage(event.App, user.Id, preliminary, input.TargetDataSourceID, named)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "move-target")
	}
	if input.Preview {
		if preliminary.AuthorityRevision != input.ExpectedAuthorityRevision {
			return writeDatabaseRowPageActionError(event, errDatabaseRowPageConflict, "move-preview")
		}
		// A move to another page it can't make is refused, with its reason,
		// before the values it would lose are counted.
		if targetPageID != preliminary.ContainerPageID {
			if _, err := authorizePageBody(event.App, user.Id, targetPageID, permissionWorkspaceWrite); err != nil {
				return writeDatabaseRowPageActionError(event, databaseRowPageMoveRefusal(err), "move-preview")
			}
			if err := authorizeDatabaseRowPlaceMove(event.App, user.Id, preliminary, nil); err != nil {
				return writeDatabaseRowPageActionError(event, err, "move-preview")
			}
		}
		tree, err := databaseRowPageMoveTree(event.App, preliminary)
		if err != nil {
			return writeDatabaseRowPageActionError(event, err, "move-preview")
		}
		values, err := planDatabaseRowPageMoveValues(
			event.App, preliminary, targetPageID, input.TargetDataSourceID, databaseRowPageMoveTreeIDs(tree),
		)
		if err != nil {
			return writeDatabaseRowPageActionError(event, err, "move-preview")
		}
		return event.JSON(http.StatusOK, databaseRowPageActionResponse{
			RowID: rowID, ContainerPageID: preliminary.ContainerPageID,
			AuthorityRevision: preliminary.AuthorityRevision, DroppedProperties: values.Dropped,
		})
	}
	accepted := map[string]struct{}{}
	for _, propertyID := range input.AcceptDroppedPropertyIDs {
		accepted[strings.TrimSpace(propertyID)] = struct{}{}
	}
	if targetPageID != preliminary.ContainerPageID {
		return p.moveDatabaseRowPageAcross(event, user.Id, rowID, input, preliminary, targetPageID, accepted)
	}
	mutationID := databaseRowPageActionIdentity("move", user.Id, rowID, input.RequestID)
	plan := databaseRowPageMovePlan{}
	receipt, err := p.applyContentGraphMutationContext(
		event.Request.Context(), user.Id, preliminary.ContainerPageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				authority, authErr := authorizeDatabaseRowPage(
					txApp, user.Id, rowID, permissionWorkspaceWrite,
				)
				if authErr != nil {
					return pageAuthorization{}, authErr
				}
				if authority.ContainerPageID != preliminary.ContainerPageID {
					return pageAuthorization{}, errDatabaseRowPageConflict
				}
				return authorizePageBody(txApp, user.Id, authority.ContainerPageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(_ core.App, _ pageAuthorization, _ *crdt.Doc) (string, error) {
				return databaseRowPageActionRequestHash("move", rowID, input), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				authority, loadErr := authorizeDatabaseRowPage(
					txApp, user.Id, rowID, permissionWorkspaceWrite,
				)
				if loadErr != nil {
					return contentGraphMutationPreparation{}, loadErr
				}
				// Sent as the row is held (asHeld), it isn't held to a revision.
				if !input.AsHeld && authority.AuthorityRevision != input.ExpectedAuthorityRevision ||
					authority.ContainerPageID != preliminary.ContainerPageID {
					return contentGraphMutationPreparation{}, errDatabaseRowPageConflict
				}
				plan, loadErr = planDatabaseRowPageMove(
					txApp, authority, input.TargetDataSourceID,
					func(propertyID string) bool { _, ok := accepted[propertyID]; return ok },
				)
				if loadErr != nil {
					return contentGraphMutationPreparation{}, loadErr
				}
				return contentGraphMutationPreparation{Operations: plan.Operations}, nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				return plan.finish(txApp, user.Id, mutationID)
			},
		},
	)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "move")
	}
	authority, err := authorizeDatabaseRowPage(event.App, user.Id, rowID, permissionWorkspaceRead)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "move-response")
	}
	return event.JSON(http.StatusOK, databaseRowPageActionResponse{
		RowID: rowID, ContainerPageID: authority.ContainerPageID,
		AuthorityRevision: authority.AuthorityRevision, Duplicate: receipt.Duplicate,
	})
}

func writeDatabaseRowPageActionError(event *core.RequestEvent, err error, stage string) error {
	var requestError *graphRequestError
	switch {
	case errors.Is(err, errDatabaseRowPageReadOnly):
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You can view this database row but not change it.")
	case errors.Is(err, errDatabaseRowPageMoveDenied):
		return apiError(event, http.StatusForbidden, errorCodeDatabaseRowPageMoveDenied,
			"Moving it to another page needs full access to it and edit access to where it goes.")
	case errors.Is(err, errDatabaseRowPageNotFound), errors.Is(err, errDatabaseMutationNotFound), isPermissionDenied(err):
		return apiError(event, http.StatusNotFound, errorCodeDatabaseRowPageNotFound, "Database row not found.")
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		return apiError(event, http.StatusPreconditionFailed, errorCodeLegalHoldPreventsPermanentDelete, "Release the active legal hold, then permanently delete.")
	case databaseRowTrashRefused(err):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageInTrash, "A row in Trash can be changed after it's restored.")
	case errors.Is(err, errDatabaseRowPageMoveDropsValues):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageMoveDropsValues, "The property values lost by the move changed. Check them again.")
	case errors.Is(err, errDatabaseRowPageValueChanged):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageValueChanged, "The value changed since it was read.")
	case errors.Is(err, errDatabaseRowPageNotTrashed):
		// As a page's permanent deletion says it (page_permanent_delete_route.go).
		return apiError(event, http.StatusConflict, errorCodePagePermanentDeleteNotTrashed, "Move the page to Trash first.")
	case errors.Is(err, errDatabaseRowPageNotInTrash):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageConflict, "The database row isn't in Trash.")
	case errors.Is(err, errDatabaseRowPageConflict), errors.Is(err, errDatabaseMutationConflict),
		errors.Is(err, errDatabaseMutationRequestIDReused):
		return apiError(event, http.StatusConflict, errorCodeDatabaseRowPageConflict, "The database row changed. Try again.")
	case errors.As(err, &requestError):
		return apiError(event, requestError.status, requestError.code, requestError.message)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
			"Couldn't complete the database row operation.", "database_row_page", stage,
			errorCodeDatabaseRowPageFailed, err,
		)
	}
}

// databaseRowPageMoveTargetPage is the page that holds the target data
// source, in the row's workspace. One of another workspace is refused as such
// (other_workspace_refusal.go), named as the request named it, and one the
// move can't find with its field — never as the row changed meanwhile
// (2026-11-26).
func databaseRowPageMoveTargetPage(
	app core.App, principalID string, authority databaseRowPageAuthority, targetDataSourceID, named string,
) (string, error) {
	pageID := ""
	err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_data_sources}}
		WHERE [[id]]={:targetId} AND [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{
		"targetId": targetDataSourceID, "ownerId": authority.OwnerID, "workspaceId": authority.WorkspaceID,
	}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) || pageID == "" {
		if workspaceID, _ := dataSourceInOtherWorkspace(app, principalID, targetDataSourceID, authority.WorkspaceID); workspaceID != "" {
			return "", otherWorkspaceRefused(app, "targetDataSourceId", workspaceCrossing{
				subject: "this row", from: authority.WorkspaceID, place: "the data source " + named, to: workspaceID,
				whole: authority.ContainerPageID,
			})
		}
		return "", refusedFor(http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check targetDataSourceId: no database this row "+
			"can move to has that data source; notion-fetch of a database gives its data sources (collection://…).",
			apiErrorField{Name: "targetDataSourceId", Problem: "not found"})
	}
	return pageID, err
}

// databaseRowPageMoveSource is the data source a move names: by its ID, or
// by the public API's, either as collection://, as an agent's tools give it
// (2026-11-26: an ID from notion-fetch was answered as the row changed).
func databaseRowPageMoveSource(app core.App, named string) string {
	id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(named), mcpDataSourceURLPrefix))
	if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDataSource, id); err == nil && identity.InternalID != "" {
		return identity.InternalID
	}
	return id
}
