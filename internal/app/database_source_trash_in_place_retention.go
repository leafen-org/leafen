package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A source kept in the trash where it stands goes for good when its 30 days
// end, or when a person deletes it from the trash for good: the trash's
// retention deletes it with its pages. Its database lets it go first, in the
// trash's own forms. One whose database keeps another source of its own
// leaves it as a database of its own in the trash, due at once
// (notion_data_source_trash.go); one that was all its
// database had takes the database with it — an inline database's block goes
// to the trash of deleted blocks, due at once, and a full-page database's
// page is deleted for good as a page from the trash is. The trash's runner
// ends them a few a turn (block_trash_retention_runner.go); one whose page is
// in the trash or that a legal hold keeps waits, and one that can't go now
// waits for the runner's next turn.

const databaseSourcesTrashedInPlaceRetiredPerTurn = 4

type databaseSourceTrashedInPlaceDue struct {
	DataSourceID string `db:"data_source_id"`
	ContainerID  string `db:"container_id"`
	OwnerID      string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
}

// retireDatabaseSourcesTrashedInPlace ends the trash of the sources whose
// time is over, and says whether more wait now and when the next one is due.
func retireDatabaseSourcesTrashedInPlace(
	ctx context.Context, app core.App, rooms workspaceRoomInvalidator, now time.Time,
) (bool, sql.NullInt64, error) {
	nowMillis := now.UTC().UnixMilli()
	// A source deleted for good with its database needs no record.
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_data_source_trash_in_place}} WHERE NOT EXISTS (
			SELECT 1 FROM {{leafen_data_sources}} AS [[source]]
			WHERE [[source]].[[id]] = {{leafen_data_source_trash_in_place}}.[[data_source_id]]
		)
	`).WithContext(ctx).Execute(); err != nil {
		return false, sql.NullInt64{}, err
	}
	due := []databaseSourceTrashedInPlaceDue{}
	if err := app.DB().NewQuery(`
		SELECT [[kept]].[[data_source_id]], [[container]].[[id]] AS [[container_id]], [[container]].[[owner]],
			[[container]].[[workspace_id]], [[container]].[[page_id]]
		FROM {{leafen_data_source_trash_in_place}} AS [[kept]]
		JOIN {{leafen_database_containers}} AS [[container]]
			ON [[container]].[[id]] = [[kept]].[[container_id]] AND [[container]].[[kind]] = 'source'
		WHERE [[kept]].[[expires_at]] <= {:now}
			AND ` + databaseSourceTrashedInPlaceSQL("[[kept]].[[data_source_id]]") + `
			AND NOT ` + legalHoldRetainsPageSQL("[[container]].[[page_id]]", "[[container]].[[workspace_id]]") + `
		ORDER BY [[kept]].[[expires_at]], [[kept]].[[data_source_id]]
	`).Bind(dbx.Params{"now": nowMillis}).WithContext(ctx).All(&due); err != nil {
		return false, sql.NullInt64{}, err
	}
	ygo, _ := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	retired := 0
	for _, source := range due {
		if ygo == nil || retired == databaseSourcesTrashedInPlaceRetiredPerTurn {
			break
		}
		done, err := retireDatabaseSourceTrashedInPlace(ctx, app, rooms, ygo, source)
		if err != nil {
			if ctx.Err() != nil {
				return false, sql.NullInt64{}, ctx.Err()
			}
			logDurableJobDiagnostic(app, errorCodeJobRunnerFailed, newDiagnosticReference(), source.DataSourceID,
				"data-source.trash-in-place", "retire", err)
			continue
		}
		if done {
			retired++
		}
	}
	next := sql.NullInt64{}
	err := app.DB().NewQuery(`
		SELECT MIN([[expires_at]]) FROM {{leafen_data_source_trash_in_place}} WHERE [[expires_at]] > {:now}
	`).Bind(dbx.Params{"now": nowMillis}).WithContext(ctx).Row(&next)
	return retired == databaseSourcesTrashedInPlaceRetiredPerTurn, next, err
}

// retireDatabaseSourceTrashedInPlace lets one source's database let it go,
// due at once; false is one that waits.
func retireDatabaseSourceTrashedInPlace(
	ctx context.Context, app core.App, rooms workspaceRoomInvalidator, ygo *pocketBaseYgoPersistence,
	due databaseSourceTrashedInPlaceDue,
) (bool, error) {
	if trashed, err := pageInTrashPath(app, due.PageID); err != nil || trashed {
		return false, err
	}
	workspace, err := app.FindRecordById(workspacesCollection, due.WorkspaceID)
	if err != nil {
		return false, err
	}
	page, err := app.FindRecordById(pagesCollection, due.PageID)
	if err != nil {
		return false, err
	}
	stored := ""
	if err := app.DB().NewQuery(`SELECT [[content]] FROM {{leafen_blocks}} WHERE [[id]] = {:id} AND [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"id": due.ContainerID, "pageId": due.PageID}).Row(&stored); err != nil {
		return false, err
	}
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(stored))
	if err != nil || !content.trashedInPlace(due.DataSourceID) {
		return false, err
	}
	// The trash's retention works as the system (system_job_access.go), as a
	// trashed database's does.
	system := systemJobSubjectFor(workspace)
	others := slices.DeleteFunc(content.ownDataSourceIDs(), func(id string) bool { return id == due.DataSourceID })
	switch {
	case len(others) > 0:
		return true, detachDatabaseSourceTrashedInPlace(ctx, ygo, system, due)
	case isFullPageDatabase(page):
		if err := trashSourceDatabasePage(app, system, due.PageID); err != nil {
			return false, err
		}
		// Its time in the trash is its source's, already over: it goes as a
		// page deleted for good from the trash does.
		hold, _, err := holdPageForPermanentDeletion(app, system, due.PageID,
			"data-source-trash-retention:"+due.DataSourceID)
		if err != nil {
			return false, err
		}
		if rooms != nil {
			return true, invalidatePermanentPageRooms(rooms, hold.WorkspaceID)
		}
		return true, nil
	}
	_, err = ygo.applyContentGraphMutationContext(ctx, system.actorID(), due.PageID,
		graphMutationInput{
			MutationID: "data-source-trash-retention:" + newDiagnosticReference(),
			Operations: []graphOperation{{
				Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: due.ContainerID},
			}},
		},
		contentGraphMutationHooks{
			Authorize:             systemPageBodyWrite(system, due.PageID),
			BeforeApplyOperations: captureRecoverableBlockTrash(due.ContainerID, system.actorID()),
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				return deleteTrashedDatabaseContainerForever(txApp, trashedDatabaseContainer{RootBlockID: due.ContainerID})
			},
		},
	)
	return err == nil, err
}

// systemPageBodyWrite lets the system write the page's body.
func systemPageBodyWrite(system systemJobSubject, pageID string) func(core.App) (pageAuthorization, error) {
	return func(txApp core.App) (pageAuthorization, error) {
		return system.authorizePageBody(txApp, pageID, permissionWorkspaceWrite)
	}
}

// trashSourceDatabasePage puts a full-page database whose last source's 30
// days are over into the trash, as the system's own change of its page.
func trashSourceDatabasePage(app core.App, system systemJobSubject, pageID string) error {
	status := pageMetadataApplied
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		input := pageMetadataInput{
			Title: current.GetString("title"), ParentID: current.GetString("parent_id"), InTrash: true,
			MutationID: "data-source-trash-retention:" + newDiagnosticReference(), BaseCursor: current.GetInt("cursor"),
		}
		_, status, err = applyPageMetadataTransactionWithLock(
			txApp, system, pageID, input, pageMetadataRequestHash(pageID, input), pageLockIgnored,
		)
		return err
	})
	if err == nil && status != pageMetadataApplied {
		return errDatabaseRowPageConflict
	}
	return err
}

// detachDatabaseSourceTrashedInPlace takes the source out of its database,
// which keeps its other sources, as a database of its own in the trash, due
// at once.
func detachDatabaseSourceTrashedInPlace(
	ctx context.Context, ygo *pocketBaseYgoPersistence, system systemJobSubject, due databaseSourceTrashedInPlaceDue,
) error {
	var entry structuralBlockMetadata
	view := ""
	_, err := ygo.applyContentGraphMutationContext(ctx, system.actorID(), due.PageID,
		graphMutationInput{MutationID: "data-source-trash-retention:" + newDiagnosticReference()},
		contentGraphMutationHooks{
			Authorize: systemPageBodyWrite(system, due.PageID),
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, content, err := notionDataSourceContainerBlock(doc, due.ContainerID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if !content.trashedInPlace(due.DataSourceID) {
					return contentGraphMutationPreparation{}, errDatabaseRowPageConflict
				}
				if err := rejectLinkedDataSourceDetach(txApp, due.ContainerID, due.DataSourceID); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				remaining := slices.DeleteFunc(content.dataSourceIDs(), func(id string) bool { return id == due.DataSourceID })
				block, err := notionDataSourceMembershipBlock(txApp, current, due.ContainerID, due.PageID, due.OwnerID, remaining)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				err = txApp.DB().NewQuery(`
					SELECT [[id]] FROM {{leafen_database_views}}
					WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId} AND [[container_id]]=''
					ORDER BY [[position]], [[id]] LIMIT 1
				`).Bind(dbx.Params{"owner": due.OwnerID, "pageId": due.PageID, "sourceId": due.DataSourceID}).Row(&view)
				if errors.Is(err, sql.ErrNoRows) {
					return contentGraphMutationPreparation{}, errDatabaseRowPageConflict
				}
				entry = current
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertBlock, Block: block,
				}}}, err
			},
			AfterApply: func(txApp core.App, page pageAuthorization, doc *crdt.Doc) error {
				order, err := structuralOrder(doc, entry.ParentID)
				if err != nil {
					return err
				}
				successorID := ""
				if index := structuralIndex(order, due.ContainerID); index >= 0 && index+1 < len(order) {
					successorID = order[index+1]
				}
				rootID, err := persistDetachedDataSourceTrash(
					txApp, page, system.actorID(), due.ContainerID, successorID, entry.ParentID, due.DataSourceID, view,
				)
				if err != nil {
					return err
				}
				return deleteTrashedDatabaseContainerForever(txApp, trashedDatabaseContainer{RootBlockID: rootID})
			},
		},
	)
	return err
}
