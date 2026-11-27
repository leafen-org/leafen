package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type databaseMutationCell struct {
	RowID        string `db:"row_id"`
	PropertyID   string `db:"property_id"`
	DataSourceID string `db:"data_source_id"`
}

type databaseMutationEntity struct {
	ID string `db:"id"`
}

func (p *pocketBaseYgoPersistence) advanceDatabaseMutationDeletion(
	ctx context.Context, jobID string,
) error {
	job, err := loadDatabaseMutationDeletionByID(p.app, jobID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != databaseMutationStatusPending) {
		return nil
	}
	if err != nil {
		return err
	}
	var cursor int
	var completed bool
	var counts graphMutationCounts
	interactionOrganizationID := ""
	interactionActorID := ""
	retentionRootBlockID := ""
	historySourceID := ""
	var activityTarget workspaceActivityDeletionTarget
	// What a row's turn changes of the page's reads: the row and the rows
	// naming it (database_row_changed_reads.go), which its receipt names.
	var changed graphChangedParts
	createdAt := time.Now().UTC().UnixMilli()
	err = p.mutateRoomDocumentContext(ctx, job.PageID,
		func(operationContext context.Context, txApp core.App, doc *crdt.Doc) (bool, error) {
			current, err := loadDatabaseMutationDeletionByID(txApp, job.ID)
			if err != nil || current.Status != databaseMutationStatusPending || current.Revision != job.Revision {
				return false, err
			}
			retentionRoot, retentionJob, err := databaseContainerTrashRetentionJob(
				txApp, current, createdAt,
			)
			if err != nil {
				return false, err
			}
			retentionRootBlockID = retentionRoot
			// A trashed database's retention, and a deletion the system
			// admitted, are the system's turns; the rest the person's who
			// asked (databaseMutationDeletionSubject).
			subject, err := databaseMutationDeletionSubject(txApp, current, retentionJob)
			if err != nil {
				return false, refusedUnlessStorage(errDatabaseMutationConflict, err)
			}
			authorizationActorID := subject.actorID()
			authorization, err := subject.authorizePageBody(txApp, current.PageID, permissionWorkspaceWrite)
			if err != nil || authorization.Workspace.OwnerID != current.OwnerID {
				return false, refusedUnlessStorage(errDatabaseMutationConflict, err)
			}
			// The data source a property or source deletion changes, read
			// before its last turn takes the property away.
			switch current.Phase {
			case databaseMutationPhasePropertyFinal:
				historySourceID, err = databaseMutationDeletionTargetSource(
					txApp, current.OwnerID, current.PageID, current.TargetKind, current.TargetID,
				)
				if err != nil {
					return false, err
				}
			case databaseMutationPhaseSourceFinal:
				historySourceID = current.TargetID
			}
			if !retentionJob && (current.Phase == databaseMutationPhasePropertyFinal || current.Phase == databaseMutationPhaseSourceFinal || current.Phase == databaseMutationPhaseRowFinal) {
				activityTarget, err = captureWorkspaceActivityDeletion(txApp, current)
				if err != nil {
					return false, err
				}
			}
			if current.TargetKind == databaseMutationTargetRow {
				if changed.OnlyRows, err = databaseRowsAsRead(txApp, []string{current.TargetID}); err != nil {
					return false, err
				}
			}
			turned, processed, nextPhase, terminal, err := applyDatabaseMutationDeletionTurn(
				txApp, doc, current,
			)
			if err != nil {
				return false, err
			}
			if !turned {
				return false, advanceDatabaseMutationDeletionState(txApp, current, nextPhase, processed)
			}
			if err := markSearchDirty(txApp, authorization.Page); err != nil {
				return false, err
			}
			if err := txApp.DB().NewQuery(`SELECT COALESCE(MAX([[cursor]]),0)+1
				FROM {{leafen_graph_changes}} WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}`,
			).Bind(dbx.Params{"owner": current.OwnerID, "pageId": current.PageID}).Row(&cursor); err != nil {
				return false, err
			}
			completed = terminal
			counts = databaseMutationDeletionCounts(current.TargetKind)
			if terminal {
				interactionOrganizationID = authorization.Workspace.Organization.OrganizationID
				interactionActorID = authorizationActorID
				return true, nil
			}
			return true, advanceDatabaseMutationDeletionState(txApp, current, nextPhase, processed)
		},
		func(operationContext context.Context, txApp core.App, update, stateVector []byte) error {
			turnMutationID := fmt.Sprintf("%s:%d", job.ID, job.Revision)
			publishedMutationID := turnMutationID
			publishedCounts := graphMutationCounts{}
			if completed {
				publishedMutationID = job.MutationID
				publishedCounts = counts
			}
			if err := insertStructuralGraphChangeContext(
				operationContext, txApp, job.OwnerID, job.PageID, publishedMutationID,
				job.RequestHash, cursor, update, createdAt,
			); err != nil {
				return err
			}
			stateVectorText := base64.StdEncoding.EncodeToString(stateVector)
			countsJSON, err := json.Marshal(publishedCounts)
			if err != nil {
				return err
			}
			receiptJSON, err := json.Marshal(storedGraphMutationReceipt{graphMutationCounts: publishedCounts, graphChangedParts: changed})
			if err != nil {
				return err
			}
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_graph_mutations}}
					([[owner]],[[page_id]],[[mutation_id]],[[request_hash]],[[cursor]],[[counts_json]],[[state_vector]],[[created]])
				VALUES ({:owner},{:pageId},{:mutationId},{:requestHash},{:cursor},{:counts},{:stateVector},{:created})
			`).Bind(dbx.Params{
				"owner": job.OwnerID, "pageId": job.PageID, "mutationId": publishedMutationID,
				"requestHash": job.RequestHash, "cursor": cursor, "counts": string(receiptJSON),
				"stateVector": stateVectorText, "created": createdAt,
			}).WithContext(operationContext).Execute(); err != nil {
				return err
			}
			if !completed {
				return nil
			}
			if err := recordNotionAdminLegalHoldPageInteraction(txApp, pageEditedInteraction(
				"content-graph", job.PageID+"\x00"+interactionActorID+"\x00"+job.MutationID,
				interactionOrganizationID, job.WorkspaceID,
				job.PageID, interactionActorID, createdAt,
			)); err != nil {
				return err
			}
			result, err := txApp.DB().NewQuery(`UPDATE {{leafen_database_mutation_jobs}}
				SET [[status]]='completed',[[phase]]='completed',[[revision]]=[[revision]]+1,
					[[processed_items]]=[[processed_items]]+1,[[cleanup_lane]]=0,[[receipt_cursor]]={:cursor},
					[[receipt_counts_json]]={:counts},[[receipt_state_vector]]={:stateVector},
					[[updated_at]]={:now},[[completed_at]]={:now}
				WHERE [[id]]={:jobId} AND [[status]]='pending' AND [[revision]]={:revision}`,
			).Bind(dbx.Params{
				"jobId": job.ID, "revision": job.Revision, "cursor": cursor,
				"counts": string(countsJSON), "stateVector": stateVectorText, "now": createdAt,
			}).WithContext(operationContext).Execute()
			if err := requireDatabaseMutationJobUpdate(result, err); err != nil {
				return err
			}
			if retentionRootBlockID != "" {
				return completeDatabaseContainerTrashRetentionJob(txApp, retentionRootBlockID, job)
			}
			// What the person took away shows in the page's history (a
			// trashed container's retention cleanup was shown when trashed).
			if err := recordDatabaseMutationDeletionHistory(txApp, job, historySourceID, update); err != nil {
				return err
			}
			if activityTarget.EntityType != "" {
				return recordWorkspaceActivityDeletion(txApp, job, activityTarget, createdAt)
			}
			return nil
		},
	)
	return err
}

func databaseMutationDeletionCounts(targetKind string) graphMutationCounts {
	counts := graphMutationCounts{}
	switch targetKind {
	case databaseMutationTargetSource:
		counts.DataSources = 1
	case databaseMutationTargetProperty:
		counts.Properties = 1
	case databaseMutationTargetRow:
		counts.Rows = 1
	}
	return counts
}

func applyDatabaseMutationDeletionTurn(
	app core.App, doc *crdt.Doc, job storedDatabaseMutationDeletion,
) (bool, int, string, bool, error) {
	switch job.Phase {
	case databaseMutationPhaseSourceCells:
		return deleteDatabaseMutationCells(app, job, databaseMutationPhaseSourceFiles)
	case databaseMutationPhaseSourceFiles:
		return deleteDatabaseMutationFileReferences(app, job, databaseMutationPhaseSourceRows)
	case databaseMutationPhasePropertyCells:
		return deleteDatabaseMutationCells(app, job, databaseMutationPhasePropertyFiles)
	case databaseMutationPhasePropertyFiles:
		return deleteDatabaseMutationFileReferences(app, job, databaseMutationPhasePropertyFinal)
	case databaseMutationPhaseRowIncoming:
		return rewriteDatabaseMutationIncomingCells(app, job)
	case databaseMutationPhaseRowCells:
		return deleteDatabaseMutationCells(app, job, databaseMutationPhaseRowFiles)
	case databaseMutationPhaseRowFiles:
		return deleteDatabaseMutationFileReferences(app, job, databaseMutationPhaseRowFinal)
	case databaseMutationPhaseSourceRows:
		return deleteDatabaseMutationRows(app, job)
	case databaseMutationPhaseSourceViews:
		return deleteDatabaseMutationEntities(app, job, "leafen_database_views", databaseMutationPhaseSourceProperties)
	case databaseMutationPhaseSourceProperties:
		return deleteDatabaseMutationEntities(app, job, "leafen_database_properties", databaseMutationPhaseSourceFinal)
	case databaseMutationPhasePropertyFinal:
		return deleteDatabaseMutationPropertyFinal(app, job)
	case databaseMutationPhaseRowFinal:
		return deleteDatabaseMutationRowFinal(app, job)
	case databaseMutationPhaseSourceFinal:
		return deleteDatabaseMutationSourceFinal(app, job)
	default:
		return false, 0, "", false, errDatabaseMutationConflict
	}
}

func deleteDatabaseMutationCells(
	app core.App, job storedDatabaseMutationDeletion, nextPhase string,
) (bool, int, string, bool, error) {
	where := "[[rows]].[[data_source_id]]={:targetId}"
	if job.TargetKind == databaseMutationTargetProperty {
		where = "[[values]].[[property_id]]={:targetId}"
	} else if job.TargetKind == databaseMutationTargetRow {
		where = "[[values]].[[row_id]]={:targetId}"
	}
	cells := []databaseMutationCell{}
	err := app.DB().NewQuery(`SELECT [[values]].[[row_id]],[[values]].[[property_id]],[[rows]].[[data_source_id]]
		FROM {{leafen_database_row_values}} AS [[values]]
		JOIN {{leafen_database_rows}} AS [[rows]] ON [[rows]].[[id]]=[[values]].[[row_id]]
		WHERE [[values]].[[owner]]={:owner} AND [[values]].[[page_id]]={:pageId} AND ` + where + `
		ORDER BY [[values]].[[row_id]],[[values]].[[property_id]] LIMIT {:limit}`,
	).Bind(dbx.Params{
		"owner": job.OwnerID, "pageId": job.PageID, "targetId": job.TargetID,
		"limit": databaseMutationItemsPerTurn,
	}).All(&cells)
	if err != nil {
		return false, 0, "", false, err
	}
	if len(cells) == 0 {
		return false, 0, nextPhase, false, nil
	}
	if err := deleteDatabaseMutationCellProjections(app, job, cells); err != nil {
		return false, 0, "", false, err
	}
	return true, len(cells), job.Phase, false, nil
}

func rewriteDatabaseMutationIncomingCells(
	app core.App, job storedDatabaseMutationDeletion,
) (bool, int, string, bool, error) {
	rawCells := []databaseMutationCell{}
	err := app.DB().NewQuery(`SELECT [[items]].[[row_id]],[[items]].[[property_id]],[[items]].[[data_source_id]]
		FROM {{leafen_database_relation_items}} AS [[items]]
		INDEXED BY [[idx_leafen_database_relation_items_target]]
		WHERE [[items]].[[owner]]={:owner} AND [[items]].[[page_id]]={:pageId} AND [[items]].[[target_row_id]]={:targetId}
		ORDER BY [[items]].[[row_id]],[[items]].[[property_id]],[[items]].[[ordinal]] LIMIT {:limit}`,
	).Bind(dbx.Params{
		"owner": job.OwnerID, "pageId": job.PageID, "targetId": job.TargetID,
		"limit": databaseMutationItemsPerTurn,
	}).All(&rawCells)
	if err != nil {
		return false, 0, "", false, err
	}
	if len(rawCells) == 0 {
		return false, 0, databaseMutationPhaseRowCells, false, nil
	}
	cells := deduplicateDatabaseMutationCells(rawCells)
	if err := rewriteDatabaseMutationIncomingCellBatch(app, job, cells); err != nil {
		return false, 0, "", false, err
	}
	return true, len(cells), job.Phase, false, nil
}

func deduplicateDatabaseMutationCells(cells []databaseMutationCell) []databaseMutationCell {
	unique := make([]databaseMutationCell, 0, len(cells))
	seen := make(map[databaseMutationCellKey]struct{}, len(cells))
	for _, cell := range cells {
		key := databaseMutationCellKey{RowID: cell.RowID, PropertyID: cell.PropertyID}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, cell)
	}
	return unique
}

func advanceDatabaseMutationDeletionState(
	app core.App, job storedDatabaseMutationDeletion, phase string, processed int,
) error {
	result, err := app.DB().NewQuery(`UPDATE {{leafen_database_mutation_jobs}}
		SET [[phase]]={:phase},[[revision]]=[[revision]]+1,
			[[processed_items]]=[[processed_items]]+{:processed},[[cleanup_lane]]=0,
			[[updated_at]]={:now},[[not_before]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='pending' AND [[revision]]={:revision}`,
	).Bind(dbx.Params{
		"phase": phase, "processed": processed, "now": time.Now().UTC().UnixMilli(),
		"jobId": job.ID, "revision": job.Revision,
	}).Execute()
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return refusedUnlessStorage(errDatabaseMutationConflict, err)
	}
	return nil
}

func requireDatabaseMutationJobUpdate(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return refusedUnlessStorage(errDatabaseMutationConflict, err)
	}
	return nil
}

func deleteDatabaseMutationRows(app core.App, job storedDatabaseMutationDeletion) (bool, int, string, bool, error) {
	if processed, err := deleteDatabaseMutationDependencyEdges(app, job, job.TargetID, ""); err != nil || processed > 0 {
		return processed > 0, processed, job.Phase, false, err
	}
	return deleteDatabaseMutationEntities(app, job, "leafen_database_rows", databaseMutationPhaseSourceViews)
}

func deleteDatabaseMutationEntities(
	app core.App, job storedDatabaseMutationDeletion, table, nextPhase string,
) (bool, int, string, bool, error) {
	items := []databaseMutationEntity{}
	err := app.DB().NewQuery(`SELECT [[id]] FROM {{` + table + `}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:targetId}
		ORDER BY [[position]],[[id]] LIMIT {:limit}`,
	).Bind(dbx.Params{
		"owner": job.OwnerID, "pageId": job.PageID, "targetId": job.TargetID,
		"limit": databaseMutationItemsPerTurn,
	}).All(&items)
	if err != nil {
		return false, 0, "", false, err
	}
	if len(items) == 0 {
		return false, 0, nextPhase, false, nil
	}
	if err := deleteDatabaseMutationEntityBatch(app, job, table, items); err != nil {
		return false, 0, "", false, err
	}
	return true, len(items), job.Phase, false, nil
}

func deleteDatabaseMutationDependencyEdges(
	app core.App, job storedDatabaseMutationDeletion, sourceID, rowID string,
) (int, error) {
	query := `SELECT [[id]] FROM {{leafen_database_dependency_edges}}
		INDEXED BY [[idx_leafen_database_dependency_edges_mutation_source]]
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId}
		ORDER BY [[id]] LIMIT {:limit}`
	if rowID != "" {
		query = `SELECT [[id]] FROM (
			SELECT [[id]] FROM {{leafen_database_dependency_edges}}
			INDEXED BY [[idx_leafen_database_dependency_edges_mutation_predecessor]]
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[predecessor_row_id]]={:rowId}
			ORDER BY [[id]] LIMIT {:limit}
		) UNION SELECT [[id]] FROM (
			SELECT [[id]] FROM {{leafen_database_dependency_edges}}
			INDEXED BY [[idx_leafen_database_dependency_edges_mutation_successor]]
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[successor_row_id]]={:rowId}
			ORDER BY [[id]] LIMIT {:limit}
		) ORDER BY [[id]] LIMIT {:limit}`
	}
	edges := []databaseMutationEntity{}
	err := app.DB().NewQuery(query).Bind(dbx.Params{
		"owner": job.OwnerID, "pageId": job.PageID, "sourceId": sourceID,
		"rowId": rowID, "limit": databaseMutationItemsPerTurn,
	}).All(&edges)
	if err != nil || len(edges) == 0 {
		return 0, err
	}
	ids := make([]string, 0, len(edges))
	for _, edge := range edges {
		ids = append(ids, edge.ID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return 0, err
	}
	result, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_dependency_edges}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
			AND [[id]] IN (SELECT [[value]] FROM json_each({:ids}))`,
	).Bind(dbx.Params{
		"owner": job.OwnerID, "pageId": job.PageID, "ids": string(encoded),
	}).Execute()
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != int64(len(ids)) {
		return 0, graphStructureAuthorityError()
	}
	return len(ids), nil
}
