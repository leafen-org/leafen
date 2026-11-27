package app

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A row moves to a database on another page of its workspace in one change
// of the target page, as in Notion. The target writes the row with the
// values its database can hold, as a move within a page does. In the same
// transaction the source page lets the row go: its dependency edges end,
// rows that related to it one way or two ways within its data source let it
// go, its sub-items stay behind as
// top-level rows, and the row leaves the source graph. The row keeps its
// identity, and what hangs on it moves with it to the target page: its page
// record with its content, icon and favorites, its uploaded files and its
// comments. Two-way relations to other data sources let it go through the
// relation pair runner. The source page records the row's leaving as a
// change of its own (announceSource).
// A row moves to a page in another place, a teamspace or private pages, only
// for someone with full access to its page, and its page record and the
// pages under it take the new page's place.

type databaseRowPageMoveLeave struct {
	authority databaseRowPageAuthority
	rows      []string
	pairWork  []databaseRelationPairWork
	// released are the rows staying on the source page that the leaving
	// changed: those whose relations let the rows go, and the item a moved
	// sub-item was under; edges is whether dependency edges went with them.
	released []string
	edges    bool
}

// databaseRowPageMoveAccess checks, inside the move, that the mover may
// still change the row, its page and the target page.
type databaseRowPageMoveAccess struct {
	row    func(core.App) (databaseRowPageAuthority, error)
	source func(core.App, string) error
	target func(core.App) (pageAuthorization, error)
}

type databaseRowPageMoveAcrossRequest struct {
	PrincipalID, RowID, MutationID, RequestHash    string
	SourcePageID, TargetPageID, TargetDataSourceID string
	// ExpectedRevision, when set, is the row authority the mover saw.
	ExpectedRevision int
	Accept           func(propertyID string) bool
}

func (p *pocketBaseYgoPersistence) applyDatabaseRowPageMoveAcrossRecord(
	ctx context.Context, request databaseRowPageMoveAcrossRequest, access databaseRowPageMoveAccess,
) (graphMutationReceipt, error) {
	leave := databaseRowPageMoveLeave{}
	return p.applyContentGraphMutationContext(ctx, request.PrincipalID, request.TargetPageID,
		graphMutationInput{MutationID: request.MutationID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				authority, err := access.row(txApp)
				if err != nil {
					return pageAuthorization{}, err
				}
				// A replay finds the row already on the target page.
				if authority.ContainerPageID != request.SourcePageID && authority.ContainerPageID != request.TargetPageID {
					return pageAuthorization{}, errDatabaseRowPageConflict
				}
				if err := access.source(txApp, request.SourcePageID); err != nil {
					return pageAuthorization{}, err
				}
				return access.target(txApp)
			},
			ResolveRequestHash: func(_ core.App, _ pageAuthorization, _ *crdt.Doc) (string, error) {
				return request.RequestHash, nil
			},
			Prepare: func(txApp core.App, authorization pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				authority, err := access.row(txApp)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if (request.ExpectedRevision != 0 && authority.AuthorityRevision != request.ExpectedRevision) ||
					authority.ContainerPageID != request.SourcePageID || authority.TrashedAt != 0 ||
					authority.OwnerID != authorization.Workspace.OwnerID ||
					authority.WorkspaceID != authorization.Workspace.WorkspaceID {
					return contentGraphMutationPreparation{}, errDatabaseRowPageConflict
				}
				if err := authorizeDatabaseRowPlaceMove(
					txApp, request.PrincipalID, authority, authorization.Page,
				); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				tree, err := databaseRowPageMoveTree(txApp, authority)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				noteDatabaseRowsMoving(txApp, authority.WorkspaceID, databaseRowPageMoveTreeIDs(tree)...)
				values, err := planDatabaseRowPageMoveValues(
					txApp, authority, request.TargetPageID, request.TargetDataSourceID, databaseRowPageMoveTreeIDs(tree),
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				for _, dropped := range values.Dropped {
					if !request.Accept(dropped.PropertyID) {
						return contentGraphMutationPreparation{}, errDatabaseRowPageMoveDropsValues
					}
				}
				operations := []graphOperation{}
				for index := range values.Added {
					operations = append(operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &values.Added[index]})
				}
				for _, propertyID := range workspaceExportSortedKeys(values.Options) {
					operation, err := databaseRowPageMovePropertyOperation(txApp, request.TargetPageID, propertyID, values.Options[propertyID])
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					operations = append(operations, operation)
				}
				tail, err := databaseRowTail(txApp, authority.OwnerID, request.TargetPageID, request.TargetDataSourceID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				moved, err := databaseRowPageMoveTreeOperations(tree, values, request.TargetDataSourceID, tail)
				if err == nil {
					err = ownRowsMovedIntoWiki(txApp, request.TargetDataSourceID, request.PrincipalID, moved)
				}
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				operations = append(operations, moved...)
				if len(tree) > 1 {
					if err := enableDatabaseRowPageMoveSubitems(
						txApp, authority, request.TargetPageID, request.TargetDataSourceID, values.TitleID,
					); err != nil {
						return contentGraphMutationPreparation{}, err
					}
				}
				leave, err = leaveDatabaseRowPageSource(txApp, authority, tree, request.TargetPageID, request.TargetDataSourceID, request.PrincipalID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if err := leave.finish(txApp, request.PrincipalID, request.MutationID, request.TargetPageID, request.TargetDataSourceID); err != nil {
					return err
				}
				recordDatabaseRowsMovedWebhookEvents(txApp, leave.authority, leave.rows, request.PrincipalID)
				return nil
			},
		},
	)
}

func (p *pocketBaseYgoPersistence) moveDatabaseRowPageAcross(
	event *core.RequestEvent,
	userID, rowID string,
	input databaseRowPageActionInput,
	preliminary databaseRowPageAuthority,
	targetPageID string,
	accepted map[string]struct{},
) error {
	receipt, err := p.applyDatabaseRowPageMoveAcross(event.Request.Context(), databaseRowPageMoveAcrossRequest{
		PrincipalID: userID, RowID: rowID,
		MutationID:   databaseRowPageActionIdentity("move", userID, rowID, input.RequestID),
		RequestHash:  databaseRowPageActionRequestHash("move", rowID, input),
		SourcePageID: preliminary.ContainerPageID, TargetPageID: targetPageID,
		TargetDataSourceID: input.TargetDataSourceID, ExpectedRevision: input.ExpectedAuthorityRevision,
		Accept: func(propertyID string) bool { _, ok := accepted[propertyID]; return ok },
	}, databaseRowPageMoveAccess{
		row: func(txApp core.App) (databaseRowPageAuthority, error) {
			return authorizeDatabaseRowPage(txApp, userID, rowID, permissionWorkspaceWrite)
		},
		// The row the mover changes is theirs to see: a page they can't
		// edit, here or where it goes, refuses the move with that reason.
		source: func(txApp core.App, pageID string) error {
			_, err := authorizePageBody(txApp, userID, pageID, permissionWorkspaceWrite)
			return databaseRowPageMoveRefusal(err)
		},
		target: func(txApp core.App) (pageAuthorization, error) {
			authorization, err := authorizePageBody(txApp, userID, targetPageID, permissionWorkspaceWrite)
			return authorization, databaseRowPageMoveRefusal(err)
		},
	})
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "move-across")
	}
	authority, err := authorizeDatabaseRowPage(event.App, userID, rowID, permissionWorkspaceRead)
	if err != nil {
		return writeDatabaseRowPageActionError(event, err, "move-across-response")
	}
	return event.JSON(http.StatusOK, databaseRowPageActionResponse{
		RowID: rowID, ContainerPageID: authority.ContainerPageID,
		AuthorityRevision: authority.AuthorityRevision, Duplicate: receipt.Duplicate,
	})
}

// leaveDatabaseRowPageSource takes the row out of its source page's graph
// and moves its record and what hangs on it to the target page, so the
// target writes the row it already owns.
func leaveDatabaseRowPageSource(
	app core.App,
	authority databaseRowPageAuthority,
	tree []databaseRowPageMoveTreeRow,
	targetPageID, targetDataSourceID, actorID string,
) (databaseRowPageMoveLeave, error) {
	leave := databaseRowPageMoveLeave{authority: authority, rows: databaseRowPageMoveTreeIDs(tree)}
	sourcePageID := authority.ContainerPageID
	edges, err := databaseRowPageMoveDependencyDeletions(app, authority, leave.rows)
	if err != nil {
		return leave, err
	}
	released, err := databaseRowPageMoveReleasedRows(app, authority, leave.rows)
	if err != nil {
		return leave, err
	}
	if leave.pairWork, err = databaseRowPageMovePairWork(app, authority.OwnerID, leave.rows); err != nil {
		return leave, err
	}
	if err := applyDatabaseGraphMutation(
		app, nil, authority.OwnerID, authority.WorkspaceID, sourcePageID, actorID, append(edges, released...),
	); err != nil {
		return leave, err
	}
	leave.edges = len(edges) > 0
	for _, operation := range released {
		if operation.Row != nil {
			leave.released = append(leave.released, operation.Row.ID)
		}
	}
	if authority.SubitemParentID != "" {
		leave.released = append(leave.released, authority.SubitemParentID)
	}

	encodedRows, err := json.Marshal(leave.rows)
	if err != nil {
		return leave, err
	}
	now := time.Now().UTC().UnixMilli()
	params := dbx.Params{
		"owner": authority.OwnerID, "workspaceId": authority.WorkspaceID, "sourcePageId": sourcePageID,
		"targetPageId": targetPageID, "targetSourceId": targetDataSourceID, "rowId": authority.RowID,
		"rowIds": string(encodedRows), "parentId": authority.SubitemParentID, "now": now,
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_rows}} SET [[page_id]]={:targetPageId}, [[data_source_id]]={:targetSourceId},
			[[subitem_parent_id]]=CASE WHEN [[id]]={:rowId} THEN '' ELSE [[subitem_parent_id]] END, [[updated]]={:now}
		WHERE [[owner]]={:owner} AND [[page_id]]={:sourcePageId} AND [[id]] IN (SELECT [[value]] FROM json_each({:rowIds}))
	`).Bind(params).Execute()
	if err != nil {
		return leave, err
	}
	if moved, err := result.RowsAffected(); err != nil || moved != int64(len(leave.rows)) {
		return leave, refusedUnlessStorage(errDatabaseRowPageConflict, err)
	}
	for _, statement := range []string{
		`UPDATE {{leafen_database_rows}} SET [[subitem_child_count]]=MAX(0, [[subitem_child_count]]-1)
			WHERE {:parentId}!='' AND [[id]]={:parentId} AND [[owner]]={:owner} AND [[page_id]]={:sourcePageId}`,
		`DELETE FROM {{leafen_database_row_values}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:sourcePageId} AND [[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))`,
		// The rows' own links leave with their values. Rows that stay let
		// them go above, except through two-way relations to other data
		// sources, which keep them until the relation pair runner lets go.
		`DELETE FROM {{leafen_database_relation_items}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:sourcePageId}
				AND [[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))`,
		`DELETE FROM {{leafen_reminders}} WHERE [[page_id]]={:sourcePageId} AND EXISTS (
			SELECT 1 FROM json_each({:rowIds}) [[moved]] WHERE
				substr([[reminder_key]], 1, 4)='row:' AND [[block_id]]=[[moved]].[[value]])`,
		`DELETE FROM {{leafen_deadline_notices}} WHERE [[page_id]]={:sourcePageId}
			AND [[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))`,
		// A row's body files are its page's and move with it; its values'
		// and icon's are the database page's.
		`UPDATE {{leafen_file_references}} SET [[page_id]]={:targetPageId}
			WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:sourcePageId}
				AND [[owner_kind]] IN ('` + productFileReferenceOwnerRow + `','` + productFileReferenceOwnerDatabaseRowPageIcon + `')
				AND [[owner_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))`,
		`UPDATE {{leafen_comments}} SET [[page_id]]={:targetPageId}
			WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:sourcePageId}
				AND [[target_kind]]='` + commentTargetDatabaseRow + `' AND [[target_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))`,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return leave, err
		}
	}
	return leave, markSearchDirtyByPageID(app, sourcePageID)
}

// finish lets two-way relations go, drops file references of the source's
// properties the rows no longer have, and reads the rows' search text and
// reminders again on their new page.
func (leave databaseRowPageMoveLeave) finish(app core.App, actorID, mutationID, targetPageID, targetDataSourceID string) error {
	if err := leave.announceSource(app, mutationID, targetPageID); err != nil {
		return err
	}
	encodedRows, err := json.Marshal(leave.rows)
	if err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_file_references}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[owner_kind]]={:ownerKind}
			AND [[owner_id]] IN (SELECT [[value]] FROM json_each({:rowIds})) AND [[property_id]] NOT IN (
				SELECT [[id]] FROM {{leafen_database_properties}} WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId}
			)
	`).Bind(dbx.Params{
		"workspaceId": leave.authority.WorkspaceID, "pageId": targetPageID, "ownerKind": productFileReferenceOwnerRow,
		"rowIds": string(encodedRows), "sourceId": targetDataSourceID,
	}).Execute(); err != nil {
		return err
	}
	if err := markDatabaseRowsSearchDirty(context.Background(), app, leave.authority.WorkspaceID, leave.rows); err != nil {
		return err
	}
	// The rows' page records and the pages under them take their new page's
	// place.
	target, err := app.FindRecordById(pagesCollection, targetPageID)
	if err != nil {
		return err
	}
	for _, rowID := range leave.rows {
		if err := movePageTreeToPlace(
			app, leave.authority.OwnerID, leave.authority.WorkspaceID, rowID, pagePlaceOf(target),
		); err != nil {
			return err
		}
		rowPage, err := app.FindRecordById(pagesCollection, rowID)
		if err != nil {
			return err
		}
		name, err := workspaceActivityPageName(app, rowPage)
		if err != nil {
			return err
		}
		if err = recordWorkspaceActivity(app, rowPage, actorID, "row-move:"+mutationID, "page", rowID, "move", workspaceActivityMetadata{Name: name, Kind: "database_row", LocationID: targetPageID, SourceID: targetDataSourceID}, time.Now().UTC().UnixMilli()); err != nil {
			return err
		}
	}
	return admitDatabaseRelationPairWork(
		app, leave.authority.OwnerID, leave.authority.WorkspaceID, actorID, "row-page-move:"+mutationID, leave.pairWork,
	)
}

// announceSource records the move on the page the rows left as a change of
// its rows, as a row put in the trash is (database_row_trash_actions.go): the
// move is the target page's change, and the source page's revision — what
// every kept read of its databases is judged by: SQL answers, rows and view
// queries' jobs, linked views on other pages, offline copies — stayed where
// it was, so the same SQL query of the source kept listing a row moved away
// (2026-11-26), and its open pages kept showing it. The change names the
// rows that left, which its readers find gone, and those it changed there,
// read again; one that took dependency edges, which rows show, or names more
// rows than a page reads as its changes has the page read whole.
func (leave databaseRowPageMoveLeave) announceSource(app core.App, mutationID, targetPageID string) error {
	sourcePageID := leave.authority.ContainerPageID
	if sourcePageID == "" || sourcePageID == targetPageID || len(leave.rows) == 0 {
		return nil
	}
	mutationID = "row-move-source:" + mutationID
	named := slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(leave.rows), leave.released...))))
	if leave.edges || len(named) > maxGraphChangedRows {
		return recordServerGraphChange(app, leave.authority.OwnerID, sourcePageID, mutationID,
			crdt.EncodeStateAsUpdateV1(crdt.New(), nil), graphMutationCounts{Rows: len(leave.rows)})
	}
	return recordServerGraphRowsChange(app, leave.authority.OwnerID, sourcePageID, mutationID, named)
}

// databaseRowPageMoveRefusal says a page's refusal of a row's move as the
// move's: the row is one the mover reads and changes.
func databaseRowPageMoveRefusal(err error) error {
	if isPermissionDenied(err) {
		return errDatabaseRowPageMoveDenied
	}
	return err
}
