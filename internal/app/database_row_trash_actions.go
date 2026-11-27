package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Trash and restore are content graph mutations of the row's container page
// without graph operations: the page revision moves, so query results and
// other open pages read the database again.
func (p *pocketBaseYgoPersistence) changeDatabaseRowTrash(
	ctx context.Context,
	principalID, rowID string,
	input databaseRowPageActionInput,
	trash bool,
) (databaseRowPageActionResponse, error) {
	action := "restore"
	if trash {
		action = "trash"
	}
	historyChange := pageHistoryChangeRowRestored
	if trash {
		historyChange = pageHistoryChangeRowTrashed
	}
	preliminary, err := authorizeDatabaseRowPageInTrash(p.app, principalID, rowID, permissionWorkspaceWrite)
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	current := databaseRowPageAuthority{}
	// The rows the event puts in the trash or brings back, and those naming
	// them, which the change names (database_row_changed_reads.go).
	var changedRows []string
	receipt, err := p.applyContentGraphMutationContext(
		ctx, principalID, preliminary.ContainerPageID,
		graphMutationInput{MutationID: databaseRowPageActionIdentity(action, principalID, rowID, input.RequestID)},
		contentGraphMutationHooks{
			HistoryRows: []pageHistoryRowChange{{ID: rowID, Change: historyChange}},
			ChangedRows: func() []string { return changedRows },
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				authority, err := authorizeDatabaseRowPageInTrash(txApp, principalID, rowID, permissionWorkspaceWrite)
				if err != nil {
					return pageAuthorization{}, err
				}
				if authority.ContainerPageID != preliminary.ContainerPageID {
					return pageAuthorization{}, errDatabaseRowPageConflict
				}
				return authorizePageBody(txApp, principalID, authority.ContainerPageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(_ core.App, _ pageAuthorization, _ *crdt.Doc) (string, error) {
				return databaseRowPageActionRequestHash(action, rowID, input), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				authority, err := authorizeDatabaseRowPageInTrash(txApp, principalID, rowID, permissionWorkspaceWrite)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				switch {
				case input.AsHeld && (authority.TrashedAt != 0) == trash:
					// Sent as the row is held, one already where it goes is left as it is.
					return contentGraphMutationPreparation{Deferred: true}, nil
				case trash && authority.TrashedAt != 0:
					return contentGraphMutationPreparation{}, errDatabaseRowPageInTrash
				case !trash && authority.TrashedAt == 0:
					return contentGraphMutationPreparation{}, errDatabaseRowPageNotInTrash
				case !input.AsHeld && authority.AuthorityRevision != input.ExpectedAuthorityRevision ||
					authority.ContainerPageID != preliminary.ContainerPageID:
					return contentGraphMutationPreparation{}, errDatabaseRowPageConflict
				}
				current = authority
				return contentGraphMutationPreparation{}, nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if trash {
					_, err := trashDatabaseRowEvent(txApp, current, principalID, time.Now().UTC())
					if err == nil {
						changedRows, err = databaseRowTrashEventAsRead(txApp, current.RowID)
					}
					return err
				}
				root, err := databaseRowTrashEventRoot(current)
				if err != nil {
					return err
				}
				if changedRows, err = databaseRowTrashEventAsRead(txApp, root); err != nil {
					return err
				}
				_, err = restoreDatabaseRowEvent(txApp, root, principalID, time.Now().UTC())
				return err
			},
		},
	)
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	if !receipt.Duplicate && current.RowID != "" {
		// A wiki's page in the trash leaves the body above it, and comes back at its end.
		if err := p.wikiPageLinkFollowsTrash(ctx, principalID, current, trash); err != nil {
			logDurableJobDiagnostic(p.app, errorCodeGraphInvalid, newDiagnosticReference(), "", "wiki.page-link", "trash", err)
		}
	}
	if !trash && !receipt.Duplicate && current.DataSourceID != "" {
		// A row whose Parent item names a row back from the trash sits under it again.
		if err := p.renestDatabaseSubitems(ctx, principalID, current.ContainerPageID, current.DataSourceID); err != nil {
			logDurableJobDiagnostic(p.app, errorCodeGraphInvalid, newDiagnosticReference(), "", "database.subitems", "renest", err)
		}
	}
	authority, err := authorizeDatabaseRowPageInTrash(p.app, principalID, rowID, permissionWorkspaceRead)
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	return databaseRowPageActionResponse{
		RowID: rowID, ContainerPageID: authority.ContainerPageID,
		AuthorityRevision: authority.AuthorityRevision, Duplicate: receipt.Duplicate,
	}, nil
}

func (p *pocketBaseYgoPersistence) trashDatabaseRowPage(event *core.RequestEvent) error {
	return p.serveDatabaseRowTrashChange(event, true)
}

func (p *pocketBaseYgoPersistence) restoreDatabaseRowPage(event *core.RequestEvent) error {
	return p.serveDatabaseRowTrashChange(event, false)
}

func (p *pocketBaseYgoPersistence) serveDatabaseRowTrashChange(event *core.RequestEvent, trash bool) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	// A change sent as its page goes may go as the row is held (asHeld): it
	// had no time to read the row's revision first.
	rowID, input, err := bindDatabaseRowActionAsHeld(event)
	if err != nil || input.TargetDataSourceID != "" || input.Preview || len(input.AcceptDroppedPropertyIDs) > 0 || input.Position != "" {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the trash details.")
	}
	fromSource, err := trashedSourceRow(event.App, user.Id, rowID)
	var response databaseRowPageActionResponse
	switch {
	case err != nil:
	case !trash && fromSource:
		// A page of a data source in the trash comes out of it to private pages.
		response, err = p.restoreTrashedSourceRow(event, user.Id, rowID, input)
	default:
		response, err = p.changeDatabaseRowTrash(event.Request.Context(), user.Id, rowID, input, trash)
	}
	if !trash && errors.Is(err, errDatabaseRowPageNotFound) {
		// A deleted template comes back from the trash as a row does.
		response, err = changeDatabaseTemplateTrash(event.App, user.Id, rowID, input.ExpectedAuthorityRevision, false)
	}
	if err != nil {
		stage := "restore"
		if trash {
			stage = "trash"
		}
		return writeDatabaseRowPageActionError(event, err, stage)
	}
	return event.JSON(http.StatusOK, response)
}

// deleteDatabaseRowPagePermanently deletes a row's trash event for good. Like
// deleting a page from the trash, it needs the workspace owner's authority.
// A key's call asks its person only for a deletion that would happen
// (user_key_acting_ask.go).
func deleteDatabaseRowPagePermanently(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	rowID, input, err := bindDatabaseRowPageAction(event)
	if err != nil || input.TargetDataSourceID != "" || input.Preview || len(input.AcceptDroppedPropertyIDs) > 0 || input.Position != "" {
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Check the permanent deletion details.")
	}
	if err := rehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := deleteDatabaseRowPageForever(txApp, user.Id, rowID, input)
		return err
	}); err != nil {
		return writeDatabaseRowPageActionError(event, err, "delete-permanently")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	response, err := deleteDatabaseRowPageForever(event.App, user.Id, rowID, input)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "delete-permanently")
	}
	// Gone from everyone's sight at once, as a page deleted for good is; a
	// deletion still going names its job, followed at GET /jobs/{id}.
	if response.Status == databaseRowPageDeletionPending {
		return event.JSON(http.StatusAccepted, response)
	}
	return event.JSON(http.StatusOK, response)
}

// The states a row's permanent deletion answers with.
const (
	databaseRowPageDeletionPending   = "pending"
	databaseRowPageDeletionCompleted = "completed"
)

// deleteDatabaseRowPageForever deletes a row's page in the trash for good: a
// page of a data source in the trash, a row's trash event, or a deleted
// template.
func deleteDatabaseRowPageForever(
	app core.App, userID, rowID string, input databaseRowPageActionInput,
) (databaseRowPageActionResponse, error) {
	// So does a page of a data source in the trash go for good.
	if fromSource, err := trashedSourceRow(app, userID, rowID); err != nil || fromSource {
		if err != nil {
			return databaseRowPageActionResponse{}, err
		}
		return deleteTrashedSourceRowForever(app, userID, rowID, input)
	}
	response := databaseRowPageActionResponse{RowID: rowID, Status: databaseRowPageDeletionCompleted}
	err := app.RunInTransaction(func(txApp core.App) error {
		authority, err := authorizeDatabaseRowPageInTrash(txApp, userID, rowID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		root, err := databaseRowTrashEventRoot(authority)
		if err != nil {
			return err
		}
		if authority.AuthorityRevision != input.ExpectedAuthorityRevision {
			return errDatabaseRowPageConflict
		}
		// Its 30 days are a page's (page_deletion_hold.go), named as one's are.
		response.ContainerPageID, response.AuthorityRevision = authority.ContainerPageID, authority.AuthorityRevision
		response.JobID = pageDeletionHoldFencePrefix + root
		return holdDatabaseRowTrashEvent(txApp, root, userID, authority.WorkspaceID, "row-trash-delete:"+root)
	})
	if errors.Is(err, errDatabaseRowPageNotFound) {
		// So does a deleted template go for good, at once.
		response, err = changeDatabaseTemplateTrash(app, userID, rowID, input.ExpectedAuthorityRevision, true)
		response.Status = databaseRowPageDeletionCompleted
	}
	return response, err
}

func readDatabaseRowTrash(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	items, next, err := listDatabaseRowTrashFor(event.Request.Context(), event.App, user.Id, workspaceID, event.Request.URL.Query().Get("cursor"))
	switch {
	case errors.Is(err, errDatabaseRowTrashCursor):
		return apiError(event, http.StatusBadRequest, errorCodeDatabaseRowPageInvalid, "Give the nextCursor of the last list, or none to start.")
	case isPermissionDenied(err) || errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	case err != nil:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeDatabaseRowPageFailed,
			"Couldn't load the database rows in Trash.", "database_row_page", "trash-list",
			errorCodeDatabaseRowPageFailed, err,
		)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, databaseRowTrashListResponse{Items: items, NextCursor: next})
}
