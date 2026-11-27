package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database page becomes a page, or a page a database page, by changing the
// parent of its one record, as in Notion: its ID, body, sub-pages, databases,
// shares and favorites stay. The page holding the database records the change
// as one graph change in which its database lets the row go or takes it; the
// record's parent changes first, so the row's triggers keep what the record
// keeps. Comments and notification settings follow the record's kind. As a
// page move does, a change to another place, a teamspace or private pages,
// takes full access to the page, and the page and the pages under it take
// the new place.

var (
	// A move met a change to the page, its database or its parent meanwhile;
	// any place that refuses it says why (page_row_conversion_refusals.go).
	errPageRowConversionConflict = graphConflictError(errorCodeGraphReferenceInvalid, "The page, its database or its new parent changed while it moved. Read them again, then move it.")
	errPageRowConversionShared   = graphPreconditionError(errorCodeGraphReferenceInvalid, "Unpublish the page before moving it into a database.")
)

type databaseRowToPageRequest struct {
	PrincipalID, RowID, MutationID, RequestHash string
	// ContainerPageID is the page holding the row's database.
	ContainerPageID string
	// ParentID is the page the database page becomes a sub-page of; empty
	// puts it at the top of Place, or, with none, of the place its database
	// is in.
	ParentID string
	// Place is the teamspace or the private pages at whose top it goes, as
	// a page moved to the top of a place does (page_place_move.go).
	Place *pagePlace
	// FromTrashedSource takes a page out of its data source in the trash to
	// the person's private pages, as its restore from the trash does
	// (database_source_trash_rows.go); OutOfTrash takes one the trash holds,
	// as a cut leaves it, out of it as it moves (page_row_conversion_out_of_trash.go).
	FromTrashedSource, OutOfTrash bool
}

func (p *pocketBaseYgoPersistence) convertDatabaseRowToPage(
	ctx context.Context, request databaseRowToPageRequest,
) (graphMutationReceipt, error) {
	if receipt, within, err := p.moveWikiPageWithinWiki(ctx, request); within || err != nil {
		return receipt, err
	}
	var authority databaseRowPageAuthority
	receipt, err := p.convertDatabaseRowToPageRecord(ctx, request, &authority)
	if err != nil || authority.RowID == "" {
		return receipt, err
	}
	return receipt, p.settleDatabasePageLeft(ctx, request, authority)
}

func (p *pocketBaseYgoPersistence) convertDatabaseRowToPageRecord(
	ctx context.Context, request databaseRowToPageRequest, authorityOut *databaseRowPageAuthority,
) (graphMutationReceipt, error) {
	var authority databaseRowPageAuthority
	var pairWork []databaseRelationPairWork
	defer func() { *authorityOut = authority }()
	return p.applyContentGraphMutationContext(ctx, request.PrincipalID, request.ContainerPageID,
		graphMutationInput{MutationID: request.MutationID},
		contentGraphMutationHooks{
			// A page of a full-page database in the trash leaves the
			// database's page there (database_source_trash_rows.go).
			AllowTrashedPage: request.FromTrashedSource,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := rowToPageAuthorizer(request)(txApp, request.PrincipalID, request.RowID, permissionWorkspaceWrite)
				if errors.Is(err, errDatabaseRowPageNotFound) {
					// A replay finds the record already a page.
					if converted, checkErr := pageRecordConverted(txApp, request.RowID, "", request.ParentID); checkErr != nil || !converted {
						return pageAuthorization{}, errors.Join(err, checkErr)
					}
					return authorizePageBody(txApp, request.PrincipalID, request.ContainerPageID, permissionWorkspaceWrite)
				}
				if err != nil {
					return pageAuthorization{}, err
				}
				// A page sent to the trash before its data source comes out
				// of both (database_source_trash_rows.go).
				if current.ContainerPageID != request.ContainerPageID {
					return pageAuthorization{}, errDatabaseRowPageConflict
				} else if err := rowToPageTrashRefused(request, current); err != nil {
					return pageAuthorization{}, err
				}
				authority = current
				return authorizePageBody(txApp, request.PrincipalID, request.ContainerPageID, permissionWorkspaceWrite)
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return request.RequestHash, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				if err := checkDatabaseRowToPageParent(txApp, request, authority); err != nil {
					return contentGraphMutationPreparation{}, err
				} else if err := takeRowOutOfTrashToMove(txApp, request, &authority); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				noteDatabaseRowsMoving(txApp, authority.WorkspaceID, authority.RowID)
				operations, err := databaseRowLeaveOperations(txApp, authority)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if pairWork, err = planDatabaseRelationPairDeletion(
					txApp, authority.OwnerID, databaseMutationTargetRow, authority.RowID,
				); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
			BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
				// Its values wait for it, as Notion's do (database_row_kept_values.go).
				if err := keepDatabaseRowPageValues(txApp, authority); err != nil {
					return err
				}
				// The rows sent to the trash with it stay there on their own.
				if _, err := txApp.DB().NewQuery(`
					UPDATE {{leafen_database_row_trash}} SET [[root_row_id]] = [[row_id]]
					WHERE [[root_row_id]] = {:rowId} AND [[row_id]] != {:rowId}
				`).Bind(dbx.Params{"rowId": authority.RowID}).Execute(); err != nil {
					return err
				}
				return turnDatabaseRowIntoPage(txApp, authority, request.ParentID)
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if request.ParentID != request.ContainerPageID {
					if err := placeChildPageBlock(
						txApp, authority.OwnerID, authority.WorkspaceID, request.ParentID, authority.RowID,
						request.PrincipalID, pageBodyPlacement{Type: "end"},
					); err != nil {
						return err
					}
				}
				if request.FromTrashedSource {
					if err := movePageTreeToPlace(
						txApp, authority.OwnerID, authority.WorkspaceID, authority.RowID, pagePlace{PrivateOwnerID: request.PrincipalID},
					); err != nil {
						return err
					}
					// Its shares go, as Notion says they do.
					if err := unsharePrivatePage(txApp, authority.WorkspaceID, authority.RowID, request.PrincipalID, time.Now()); err != nil {
						return err
					}
				}
				if request.ParentID == "" && request.Place != nil {
					if err := moveDatabaseRowPageToPlace(txApp, authority, request.PrincipalID, *request.Place); err != nil {
						return err
					}
				}
				if request.ParentID != "" {
					parent, err := txApp.FindRecordById(pagesCollection, request.ParentID)
					if err != nil {
						return err
					}
					if err := movePageTreeToPlace(
						txApp, authority.OwnerID, authority.WorkspaceID, authority.RowID, pagePlaceOf(parent),
					); err != nil {
						return err
					}
				}
				page, err := txApp.FindRecordById(pagesCollection, authority.RowID)
				if err != nil {
					return err
				}
				if err := replaceProductFilePageReferences(
					txApp, authority.WorkspaceID, page.Id, page.GetString("icon"), page.GetString("icon_type"), page.GetString("cover"),
				); err != nil {
					return err
				}
				if err := markSearchDirtyByPageID(txApp, authority.RowID); err != nil {
					return err
				}
				recheckCollaborationAccessAfterCommit(txApp)
				return admitDatabaseRelationPairWork(
					txApp, authority.OwnerID, authority.WorkspaceID, request.PrincipalID,
					"row-to-page:"+request.MutationID, pairWork,
				)
			},
		})
}

// databaseRowLeaveOperations take a row out of its database: its dependency
// edges end, rows relating to it one way let it go, its sub-items stay as
// top-level rows, and the row goes.
func databaseRowLeaveOperations(app core.App, authority databaseRowPageAuthority) ([]graphOperation, error) {
	moved := []string{authority.RowID}
	operations, err := databaseRowPageMoveDependencyDeletions(app, authority, moved)
	if err != nil {
		return nil, err
	}
	released, err := databaseRowPageMoveReleasedRows(app, authority, moved)
	if err != nil {
		return nil, err
	}
	operations = append(operations, released...)
	children := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}}
		WHERE [[page_id]] = {:pageId} AND [[subitem_parent_id]] = {:rowId} ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"pageId": authority.ContainerPageID, "rowId": authority.RowID}).Column(&children); err != nil {
		return nil, err
	}
	for _, childID := range children {
		child, err := databaseRowRecordOperation(app, authority.ContainerPageID, childID)
		if err != nil {
			return nil, err
		}
		child.SubitemParentID, child.Values = nil, map[string]graphRowValue{}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &child})
	}
	operations = append(operations, graphOperation{
		Kind: graphOperationKindDeleteRow, RowDeletion: &graphDatabaseDelete{ID: authority.RowID},
	})
	if len(operations) > maxGraphMutationOperations {
		return nil, rowLeaveTooLarge(len(operations))
	}
	return operations, nil
}

// turnDatabaseRowIntoPage makes the record a page under its new parent, last
// among its sibling pages, with the comments and notification settings of a
// page; its title keeps the mentions and formatting its title cell held.
func turnDatabaseRowIntoPage(app core.App, authority databaseRowPageAuthority, parentID string) error {
	now := pageUpdatedNow()
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}}
		SET [[data_source_id]] = '', [[parent_id]] = {:parentId}, [[updated]] = {:now}, [[title_spans]] = {:titleSpans},
			[[position]] = (SELECT COALESCE(MAX([[sibling]].[[position]]), 0) + 1 FROM {{leafen_pages}} AS [[sibling]]
				WHERE [[sibling]].[[workspace]] = {:workspaceId} AND [[sibling]].[[parent_id]] = {:parentId}
					AND [[sibling]].[[data_source_id]] = '')
		WHERE [[id]] = {:id} AND [[data_source_id]] = {:sourceId}
	`).Bind(dbx.Params{
		"id": authority.RowID, "sourceId": authority.DataSourceID, "parentId": parentID,
		"workspaceId": authority.WorkspaceID, "now": now, "titleSpans": rowTitleSpansAsPageTitle(app, authority.RowID),
	}).Execute()
	if err := requireOneAffected(result, err); err != nil {
		return err
	}
	if err := keepPageRecordPublicIdentity(app, authority.WorkspaceID, authority.RowID, "page"); err != nil {
		return err
	}
	return retargetPageRecordKind(app, authority.WorkspaceID, authority.RowID, authority.RowID, true)
}

type pageToDatabaseRowRequest struct {
	PrincipalID, PageID, DataSourceID, MutationID, RequestHash string
	// ContainerPageID is the page holding the data source.
	ContainerPageID string
	// First puts the row before the source's first row, as a block moved
	// into the database is put; a page moved in goes after its last row,
	// as in Notion (seen 2026-11-10).
	First bool
	// Position, when given, is the place among its rows the page takes back,
	// as a cut row's paste taken back by ⌘Z (page_row_into_database_place.go).
	Position string
	// Checked is a to-do's check, which the row keeps in the source's
	// checkbox property Notion names for it (page_row_todo_check.go); it is
	// made, as CheckedPropertyID when one is given, when the source has none.
	Checked           *bool
	CheckedPropertyID string
}

func (p *pocketBaseYgoPersistence) convertPageToDatabaseRow(
	ctx context.Context, request pageToDatabaseRowRequest,
) (graphMutationReceipt, error) {
	var page pageAuthorization
	var container pageAuthorization
	return p.applyContentGraphMutationContext(ctx, request.PrincipalID, request.ContainerPageID,
		graphMutationInput{MutationID: request.MutationID},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				var err error
				container, err = authorizePageBody(txApp, request.PrincipalID, request.ContainerPageID, permissionWorkspaceWrite)
				if err != nil {
					return pageAuthorization{}, err
				}
				// A replay finds the record already the database's page.
				if converted, err := pageRecordConverted(txApp, request.PageID, request.DataSourceID, request.DataSourceID); err != nil || converted {
					return container, err
				}
				if page, err = authorizePage(txApp, request.PrincipalID, request.PageID, permissionWorkspaceWrite); err != nil {
					return pageAuthorization{}, err
				}
				return container, nil
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return request.RequestHash, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				if err := checkPageToDatabaseRow(txApp, request, page, container); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				noteDatabaseRowsMoving(txApp, page.Workspace.WorkspaceID, request.PageID)
				row, err := pageDatabaseRowOperation(
					txApp, request, container.Workspace.OwnerID, page.Page.GetString("title"), ordinaryPageTitleSpans(page.Page),
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				operations, err := keepToDoCheckInRow(ctx, txApp, request, row)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				// A page block in the body of the page holding the database
				// goes with this change; one elsewhere after it
				// (removeChildPageBlock). Either leaves the words the block
				// had as a line before it was a page in that body's text, so a
				// ⌘Z that turns it back into the line finds them there.
				if metadata, err := structuralMetadata(doc); err != nil {
					return contentGraphMutationPreparation{}, err
				} else if isChildPageBlock(request.PageID, metadata[request.PageID]) {
					operations = append(operations, graphOperation{
						Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: request.PageID, KeepText: true},
					})
				}
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
			BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
				return turnPageIntoDatabaseRow(txApp, page, request)
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if err := removeChildPageBlock(
					txApp, page.Workspace.OwnerID, page.Workspace.WorkspaceID, request.PageID, request.PrincipalID, false,
				); err != nil {
					return err
				} else if err := keepRowCreatedWhereItStood(txApp, request, page.Page); err != nil {
					return err
				}
				// The icon becomes the database page's; the cover stays the page's.
				record := page.Page
				if err := replaceProductFilePageReferences(
					txApp, page.Workspace.WorkspaceID, record.Id, "", "", record.GetString("cover"),
				); err != nil {
					return err
				}
				if err := replaceProductFileDatabaseRowPageIconReference(
					txApp, page.Workspace.WorkspaceID, request.ContainerPageID, record.Id,
					record.GetString("icon"), record.GetString("icon_type"),
				); err != nil {
					return err
				}
				// The page and the pages under it take the database's place.
				if err := movePageTreeToPlace(
					txApp, page.Workspace.OwnerID, page.Workspace.WorkspaceID, record.Id, pagePlaceOf(container.Page),
				); err != nil {
					return err
				}
				if err := markDatabaseRowsSearchDirty(context.Background(), txApp, page.Workspace.WorkspaceID, []string{record.Id}); err != nil {
					return err
				}
				if err := markSearchDirtyByPageID(txApp, record.Id); err != nil {
					return err
				}
				recheckCollaborationAccessAfterCommit(txApp)
				return nil
			},
		})
}

// pageDatabaseRowOperation writes the page's row: its title in the title
// property, with its mentions and formatting (page_title_spans.go), where
// pageIntoDatabasePosition puts it.
func pageDatabaseRowOperation(
	app core.App, request pageToDatabaseRowRequest, ownerID, title string, spans []graphTextSpan,
) (graphOperation, error) {
	var titleID string
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:sourceId} AND [[page_id]] = {:pageId} AND [[type]] = 'title' LIMIT 1
	`).Bind(dbx.Params{"sourceId": request.DataSourceID, "pageId": request.ContainerPageID}).Row(&titleID); err != nil {
		return graphOperation{}, errors.Join(err, errPageRowConversionConflict)
	}
	position, err := pageIntoDatabasePosition(app, request, ownerID)
	if err != nil {
		return graphOperation{}, err
	}
	value, err := json.Marshal(title)
	if err != nil {
		return graphOperation{}, err
	}
	// A page that was this data source's row takes back the values it kept.
	values, err := databaseRowPageKeptValues(app, request.PageID, request.DataSourceID, request.ContainerPageID)
	if err != nil {
		return graphOperation{}, err
	}
	values[titleID] = graphRowValue{Type: graphValueTypeTitle, Value: value, Spans: spans}
	return graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
		ID: request.PageID, DataSourceID: request.DataSourceID, Position: position, Values: values,
	}}, nil
}

// turnPageIntoDatabaseRow makes the record the database's page, with the
// comments and notification settings of a database page; its page block
// leaves its parent's body with the change.
func turnPageIntoDatabaseRow(app core.App, page pageAuthorization, request pageToDatabaseRowRequest) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}}
		SET [[data_source_id]] = {:sourceId}, [[parent_id]] = {:sourceId}, [[position]] = 0, [[updated]] = {:now},
			[[title_spans]] = ''
		WHERE [[id]] = {:id} AND [[data_source_id]] = ''
	`).Bind(dbx.Params{
		"id": request.PageID, "sourceId": request.DataSourceID, "now": pageUpdatedNow(),
	}).Execute()
	if err := requireOneAffected(result, err); err != nil {
		return err
	}
	if err := keepPageRecordPublicIdentity(app, page.Workspace.WorkspaceID, request.PageID, notionObjectIdentityRowPage); err != nil {
		return err
	}
	// The values it took back from this data source are no longer kept.
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_row_kept_values}} WHERE [[page_id]] = {:id} AND [[data_source_id]] = {:sourceId}`).
		Bind(dbx.Params{"id": request.PageID, "sourceId": request.DataSourceID}).Execute(); err != nil {
		return err
	}
	return retargetPageRecordKind(app, page.Workspace.WorkspaceID, request.PageID, request.ContainerPageID, false)
}

// retargetPageRecordKind moves the record's comments to where its kind
// keeps them (a page's on itself, a database page's on the page holding
// its database) and fits its followers' notification levels to its kind: a
// page has no property updates, so following all of a database page's
// updates becomes following its comments.
func retargetPageRecordKind(app core.App, workspaceID, recordID, storagePageID string, toPage bool) error {
	fromKind, toKind := commentTargetDatabaseRow, commentTargetPage
	if !toPage {
		fromKind, toKind = commentTargetPage, commentTargetDatabaseRow
	}
	params := dbx.Params{
		"workspaceId": workspaceID, "id": recordID, "storagePageId": storagePageID,
		"fromKind": fromKind, "toKind": toKind, "fromLevel": databaseRowNotificationAllUpdates, "toLevel": pageNotificationAllComments,
	}
	for _, statement := range []string{
		`UPDATE {{leafen_file_references}} SET [[page_id]] = {:storagePageId}
			WHERE [[workspace_id]] = {:workspaceId} AND [[owner_kind]] = 'comment' AND [[owner_id]] IN (
				SELECT [[id]] FROM {{leafen_comments}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[target_kind]] = {:fromKind} AND [[target_id]] = {:id})`,
		`UPDATE {{leafen_comments}} SET [[target_kind]] = {:toKind}, [[page_id]] = {:storagePageId}
			WHERE [[workspace_id]] = {:workspaceId} AND [[target_kind]] = {:fromKind} AND [[target_id]] = {:id}`,
		`UPDATE {{leafen_page_notification_preferences}} SET [[level]] = {:toLevel}
			WHERE [[page_id]] = {:id} AND [[level]] = {:fromLevel}`,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// pageRecordConverted reports whether the record already has the kind and
// parent a conversion gives it, as on a replay.
func pageRecordConverted(app core.App, recordID, dataSourceID, parentID string) (bool, error) {
	var found int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}}
		WHERE [[id]] = {:id} AND [[data_source_id]] = {:sourceId} AND [[parent_id]] = {:parentId}
	`).Bind(dbx.Params{"id": recordID, "sourceId": dataSourceID, "parentId": parentID}).Row(&found)
	return found == 1, err
}

// pageAncestryIncludes reports whether ancestorID is pageID or above it,
// through database pages to the pages holding their databases.
func pageAncestryIncludes(app core.App, pageID, ancestorID string) (bool, error) {
	var found int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[chain]] ([[id]]) AS (
			SELECT {:pageId}
			UNION
			SELECT ` + pageAncestryParentSQL("[[page]]") + `
			FROM {{leafen_pages}} AS [[page]] JOIN [[chain]] ON [[page]].[[id]] = [[chain]].[[id]]
			WHERE [[chain]].[[id]] != ''
		)
		SELECT COUNT(*) FROM [[chain]] WHERE [[id]] = {:ancestorId}
	`).Bind(dbx.Params{"pageId": pageID, "ancestorId": ancestorID}).Row(&found)
	return found > 0, err
}

func requireOneAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return errPageRowConversionConflict
	}
	return nil
}
