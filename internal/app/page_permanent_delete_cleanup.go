package app

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const pagePermanentDeleteCleanupLaneCount = 52

var pagePermanentDeleteCleanupAuthorityKeys = map[string]struct{}{
	"leafen_ygo_documents:room:direct_delete":                                         {},
	"leafen_ygo_updates:room:direct_delete":                                           {},
	"leafen_ygo_structural_documents:room:direct_delete":                              {},
	"leafen_ygo_structural_updates:room:direct_delete":                                {},
	"leafen_ygo_structural_quarantine:room:direct_delete":                             {},
	"leafen_ygo_update_quarantine:room:direct_delete":                                 {},
	"leafen_blocks:page_id:direct_delete":                                             {},
	"leafen_block_trash_tombstones:page_id:direct_delete":                             {},
	"leafen_database_containers:page_id:direct_delete":                                {},
	"leafen_database_container_sources:page_id:linked_container_cleanup":              {},
	"leafen_database_container_views:page_id:linked_container_cleanup":                {},
	"leafen_data_sources:page_id:direct_delete":                                       {},
	"leafen_database_views:page_id:direct_delete":                                     {},
	"leafen_database_properties:page_id:direct_delete":                                {},
	"leafen_database_rows:page_id:direct_delete":                                      {},
	"leafen_database_row_values:page_id:direct_delete":                                {},
	"leafen_database_relation_items:page_id:direct_delete":                            {},
	"leafen_database_dependency_edges:page_id:bounded_page_child_delete":              {},
	"leafen_database_dependency_bindings:page_id:bounded_page_child_delete":           {},
	"leafen_database_query_dependency_values:job_id:bounded_job_child_delete":         {},
	"leafen_database_query_jobs:page_id:direct_delete":                                {},
	"leafen_database_query_results:job_id:job_cascade":                                {},
	"leafen_database_sql_query_jobs:id:bounded_source_owner_delete":                   {},
	"leafen_database_sql_query_sources:page_id:bounded_job_owner_delete":              {},
	"leafen_graph_mutations:page_id:direct_delete":                                    {},
	"leafen_graph_changes:page_id:direct_delete":                                      {},
	"leafen_page_history_restore_receipts:page_id:direct_delete":                      {},
	"leafen_page_history_snapshot_blocks:materialization_id:bounded_job_child_delete": {},
	"leafen_page_history_snapshot_jobs:page_id:direct_delete":                         {},
	"leafen_page_history_block_spans:page_id:direct_delete":                           {},
	"leafen_page_history_text_spans:page_id:direct_delete":                            {},
	"leafen_page_history_checkpoints:page_id:direct_delete":                           {},
	"leafen_page_history_events:page_id:direct_delete":                                {},
	"leafen_page_history_versions:page_id:direct_delete":                              {},
	"leafen_search_pages:page_id:direct_delete":                                       {},
	"leafen_search_pages_fts:page_id:direct_delete":                                   {},
	"leafen_search_dirty:page_id:direct_delete":                                       {},
	"leafen_search_chunks:page_id:direct_delete":                                      {},
	"leafen_jobs:page_id:cancel_then_delete":                                          {},
	"leafen_job_effects:page_id:direct_delete":                                        {},
	"leafen_job_chunks:job_id:job_cascade":                                            {},
	"leafen_interactive_automation_executions:page_id:direct_delete":                  {},
	"leafen_database_automation_events:page_id:direct_delete":                         {},
	"leafen_database_automation_outcomes:page_id:direct_delete":                       {},
	"leafen_database_automation_deliveries:page_id:direct_delete":                     {},
	"leafen_database_automation_schedule_occurrences:page_id:direct_delete":           {},
	"leafen_database_automation_notifications:page_id:direct_delete":                  {},
	"leafen_database_automation_mutations:page_id:direct_delete":                      {},
	"leafen_database_automation_frontiers:page_id:direct_delete":                      {},
	"leafen_database_automation_definitions:page_id:direct_delete":                    {},
	"leafen_notifications:page_id:bounded_page_child_delete":                          {},
	"leafen_workspace_creation_receipts:page_id:clear_reference":                      {},
	"leafen_notion_import_mappings:native_id:semantic_delete":                         {},
}

func advancePagePermanentDeleteStep(
	app core.App,
	job storedPagePermanentDeleteJob,
) error {
	if err := validatePagePermanentDeleteResidueInventory(); err != nil {
		return err
	}
	switch job.Phase {
	case pagePermanentDeletePhaseDiscovering:
		return advancePagePermanentDeleteDiscovery(app, job)
	case pagePermanentDeletePhaseAuthorizing:
		return advancePagePermanentDeleteAuthorization(app, job)
	case pagePermanentDeletePhaseCleaning:
		return advancePagePermanentDeleteChildren(app, job)
	case pagePermanentDeletePhasePublishing:
		return advancePagePermanentDeleteRoot(app, job)
	case pagePermanentDeletePhaseCompleted:
		return nil
	default:
		return errPagePermanentDeleteUnavailable
	}
}

func advancePagePermanentDeleteDiscovery(
	app core.App,
	job storedPagePermanentDeleteJob,
) error {
	item, err := loadNextPagePermanentDeleteItem(app, job.ID, "[[discovered]] = 0", "[[depth]], [[page_id]]")
	if errors.Is(err, sql.ErrNoRows) {
		return touchPagePermanentDeleteJob(
			app, job.ID, pagePermanentDeletePhaseAuthorizing, pagePermanentDeleteStatusPending,
		)
	}
	if err != nil {
		return err
	}
	child := struct {
		ID            string `db:"id"`
		Owner         string `db:"owner"`
		DeletionJobID string `db:"deletion_job_id"`
	}{}
	// A page's sub-pages include those of the database pages in its
	// databases, which go with it. The parents are one list, so each is
	// looked up by key, and the rows by their page (+ keeps the workspace
	// out): "parent_id = page OR parent_id IN (its rows)" left SQLite no key,
	// and each step read the workspace's pages from the cursor on and every
	// row of the workspace (2026-11-18).
	err = app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[deletion_job_id]] FROM {{leafen_pages}}
		WHERE [[parent_id]] IN (
				SELECT {:parentId}
				UNION ALL
				SELECT [[row]].[[id]] FROM {{leafen_database_rows}} AS [[row]]
				WHERE [[row]].[[page_id]] = {:parentId} AND +[[row]].[[workspace_id]] = {:workspaceId})
			AND +[[workspace]] = {:workspaceId} AND +[[data_source_id]] = '' AND +[[id]] > {:cursor}
		ORDER BY [[id]] LIMIT 1
	`).Bind(dbx.Params{
		"workspaceId": job.WorkspaceID, "parentId": item.PageID,
		"cursor": item.DiscoveryCursor,
	}).One(&child)
	now := time.Now().UTC().UnixMilli()
	if errors.Is(err, sql.ErrNoRows) {
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_page_permanent_delete_items}}
			SET [[discovered]] = 1, [[updated_at]] = {:now}
			WHERE [[job_id]] = {:jobId} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"now": now, "jobId": job.ID, "pageId": item.PageID}).Execute()
		if err != nil {
			return err
		}
		return touchPagePermanentDeleteJob(app, job.ID, job.Phase, job.Status)
	}
	if err != nil {
		return err
	}
	if pageDeletionHoldFence(child.DeletionJobID) {
		// A sub-page deleted before the page it was under goes with it.
		if err := endPageDeletionHold(app, job.WorkspaceID, child.ID); err != nil {
			return err
		}
	} else if child.DeletionJobID != "" && child.DeletionJobID != job.ID {
		return errPagePermanentDeleteConflict
	}
	if _, err := app.DB().Insert(pagePermanentDeleteItemsTable, dbx.Params{
		"job_id": job.ID, "page_id": child.ID, "parent_page_id": item.PageID,
		"custodian_id": child.Owner, "depth": item.Depth + 1, "is_root": false,
		"discovery_cursor": "", "discovered": false, "authorized": false,
		"cleanup_lane": 0, "cleaned": false, "created_at": now, "updated_at": now,
	}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_permanent_delete_items}}
		SET [[discovery_cursor]] = {:cursor}, [[updated_at]] = {:now}
		WHERE [[job_id]] = {:jobId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{
		"cursor": child.ID, "now": now, "jobId": job.ID, "pageId": item.PageID,
	}).Execute(); err != nil {
		return err
	}
	return touchPagePermanentDeleteJob(app, job.ID, job.Phase, job.Status)
}

func advancePagePermanentDeleteAuthorization(
	app core.App,
	job storedPagePermanentDeleteJob,
) error {
	item, err := loadNextPagePermanentDeleteItem(app, job.ID, "[[authorized]] = 0", "[[depth]], [[page_id]]")
	if errors.Is(err, sql.ErrNoRows) {
		return touchPagePermanentDeleteJob(
			app, job.ID, pagePermanentDeletePhaseCleaning, pagePermanentDeleteStatusPending,
		)
	}
	if err != nil {
		return err
	}
	page, err := app.FindRecordById(pagesCollection, item.PageID)
	if errors.Is(err, sql.ErrNoRows) {
		return errPagePermanentDeleteConflict
	}
	if err != nil {
		return err
	}
	if page.GetString("workspace") != job.WorkspaceID || page.GetString("owner") != item.CustodianID {
		return errPagePermanentDeleteConflict
	}
	if err := assertNoActiveLegalHoldForPermanentDelete(
		app, job.OrganizationID,
		legalHoldDeletionTarget{
			WorkspaceID: job.WorkspaceID, PageIDs: []string{item.PageID},
			CustodianUserIDs: []string{item.CustodianID},
		},
	); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_permanent_delete_items}}
		SET [[authorized]] = 1, [[updated_at]] = {:now}
		WHERE [[job_id]] = {:jobId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{
		"now": time.Now().UTC().UnixMilli(), "jobId": job.ID, "pageId": item.PageID,
	}).Execute()
	if err != nil {
		return err
	}
	return touchPagePermanentDeleteJob(app, job.ID, job.Phase, job.Status)
}

func advancePagePermanentDeleteChildren(
	app core.App,
	job storedPagePermanentDeleteJob,
) error {
	item, err := loadNextPagePermanentDeleteItem(
		app, job.ID,
		"[[authorized]] = 1 AND [[cleaned]] = 0 AND [[is_root]] = 0 AND NOT EXISTS ("+
			"SELECT 1 FROM {{leafen_page_permanent_delete_items}} [[child]] "+
			"WHERE [[child]].[[job_id]] = [[item]].[[job_id]] "+
			"AND [[child]].[[parent_page_id]] = [[item]].[[page_id]] AND [[child]].[[cleaned]] = 0)",
		"[[depth]] DESC, [[page_id]]",
	)
	if errors.Is(err, sql.ErrNoRows) {
		return touchPagePermanentDeleteJob(
			app, job.ID, pagePermanentDeletePhasePublishing, pagePermanentDeleteStatusPending,
		)
	}
	if err != nil {
		return err
	}
	return cleanPagePermanentDeleteItem(app, job, item, false)
}

func advancePagePermanentDeleteRoot(
	app core.App,
	job storedPagePermanentDeleteJob,
) error {
	item, err := loadNextPagePermanentDeleteItem(
		app, job.ID, "[[is_root]] = 1 AND [[cleaned]] = 0", "[[page_id]]",
	)
	if errors.Is(err, sql.ErrNoRows) {
		return touchPagePermanentDeleteJob(
			app, job.ID, pagePermanentDeletePhaseCompleted, pagePermanentDeleteStatusCompleted,
		)
	}
	if err != nil {
		return err
	}
	return cleanPagePermanentDeleteItem(app, job, item, true)
}

func cleanPagePermanentDeleteItem(
	app core.App,
	job storedPagePermanentDeleteJob,
	item storedPagePermanentDeleteItem,
	root bool,
) error {
	if item.CleanupLane < pagePermanentDeleteCleanupLaneCount {
		if item.CleanupLane == pagePermanentDeleteRowsLane {
			if err := admitPagePermanentDeleteGoneRows(app, job, item.PageID); err != nil {
				return err
			}
		}
		more, err := cleanPagePermanentDeleteResidueLane(app, item.PageID, item.CleanupLane)
		if err != nil {
			return err
		}
		if !more {
			_, err = app.DB().NewQuery(`
				UPDATE {{leafen_page_permanent_delete_items}}
				SET [[cleanup_lane]] = [[cleanup_lane]] + 1, [[updated_at]] = {:now}
				WHERE [[job_id]] = {:jobId} AND [[page_id]] = {:pageId}
			`).Bind(dbx.Params{
				"now": time.Now().UTC().UnixMilli(), "jobId": job.ID, "pageId": item.PageID,
			}).Execute()
			if err != nil {
				return err
			}
		}
		return touchPagePermanentDeleteJob(app, job.ID, job.Phase, job.Status)
	}
	page, err := app.FindRecordById(pagesCollection, item.PageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if root {
			// A page's block leaves its parent's body with the page.
			if err := removeChildPageBlock(app, job.OwnerID, job.WorkspaceID, page.Id, job.ActorID, false); err != nil {
				return err
			}
		}
		page.Set("icon", "")
		page.Set("icon_type", "none")
		page.Set("cover", "")
		if err := app.Save(page); err != nil {
			return err
		}
		if err := replaceProductFilePageReferences(
			app, page.GetString("workspace"), page.Id, "", "none", "",
		); err != nil {
			return err
		}
		if err := app.Delete(page); err != nil {
			return err
		}
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_permanent_delete_items}}
		SET [[cleaned]] = 1, [[updated_at]] = {:now}
		WHERE [[job_id]] = {:jobId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{
		"now": time.Now().UTC().UnixMilli(), "jobId": job.ID, "pageId": item.PageID,
	}).Execute(); err != nil {
		return err
	}
	if root {
		return touchPagePermanentDeleteJob(
			app, job.ID, pagePermanentDeletePhaseCompleted, pagePermanentDeleteStatusCompleted,
		)
	}
	return touchPagePermanentDeleteJob(app, job.ID, job.Phase, job.Status)
}

// admitPagePermanentDeleteGoneRows has the rows the rows lane deletes next
// leave other pages' relations; pages deleted with them keep theirs.
func admitPagePermanentDeleteGoneRows(app core.App, job storedPagePermanentDeleteJob, pageID string) error {
	rowIDs := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}} WHERE [[page_id]] = {:pageId}
		ORDER BY [[rowid]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"pageId": pageID, "limit": maxPagePermanentDeleteItemsPerRequest,
	}).Column(&rowIDs); err != nil || len(rowIDs) == 0 {
		return err
	}
	deleted := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_page_permanent_delete_items}} WHERE [[job_id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).Column(&deleted); err != nil {
		return err
	}
	return admitGoneRelationTargetWork(
		app, job.OwnerID, job.WorkspaceID, job.ActorID, "gone:"+job.ID+":"+rowIDs[0], rowIDs, deleted,
	)
}

// deleteWorkspacePageThroughPermanentDeleteOwner keeps the page-root delete
// primitive and presentation-reference cleanup inside the common permanent
// page deletion owner. The workspace lifecycle supplies its own durable fence.
func deleteWorkspacePageThroughPermanentDeleteOwner(
	app core.App,
	workspaceID, pageID string,
) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	if page.GetString("workspace") != workspaceID {
		return errPagePermanentDeleteConflict
	}
	page.Set("icon", "")
	page.Set("icon_type", "none")
	page.Set("cover", "")
	if err := app.Save(page); err != nil {
		return err
	}
	if err := replaceProductFilePageReferences(
		app, workspaceID, page.Id, "", "none", "",
	); err != nil {
		return err
	}
	return app.Delete(page)
}

// cleanCompletedPagePermanentDeleteReceiptsForWorkspace retains the terminal
// receipt deletion decision in the page lifecycle owner while allowing a
// bounded workspace-root cleanup consumer. Admission has already denied any
// pending page deletion for this workspace. A batch goes in the order of the
// workspace's index, which reads only its own rows.
func cleanCompletedPagePermanentDeleteReceiptsForWorkspace(
	app core.App,
	workspaceID string,
	limit int,
) (bool, error) {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_permanent_delete_jobs}} WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_page_permanent_delete_jobs}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[status]] = 'completed'
			ORDER BY [[updated_at]], [[id]] LIMIT {:limit}
		)
	`).Bind(dbx.Params{"workspaceId": workspaceID, "limit": limit}).Execute()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows >= int64(limit), nil
}

func loadNextPagePermanentDeleteItem(
	app core.App,
	jobID, predicate, order string,
) (storedPagePermanentDeleteItem, error) {
	item := storedPagePermanentDeleteItem{}
	err := app.DB().NewQuery(`
		SELECT [[item]].[[job_id]], [[item]].[[page_id]], [[item]].[[parent_page_id]],
			[[item]].[[custodian_id]], [[item]].[[depth]], [[item]].[[is_root]],
			[[item]].[[discovery_cursor]], [[item]].[[discovered]], [[item]].[[authorized]],
			[[item]].[[cleanup_lane]], [[item]].[[cleaned]]
		FROM {{leafen_page_permanent_delete_items}} [[item]]
		WHERE [[item]].[[job_id]] = {:jobId} AND ` + predicate + `
		ORDER BY ` + order + ` LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&item)
	return item, err
}

func validatePagePermanentDeleteResidueInventory() error {
	handledCount := 0
	for _, authority := range pagePermanentDeleteResidueAuthorities {
		key := fmt.Sprintf("%s:%s:%s", authority.Table, authority.Key, authority.Disposition)
		if _, handled := pagePermanentDeleteCleanupAuthorityKeys[key]; handled {
			handledCount++
			continue
		}
		switch authority.Disposition {
		case "root_last_delete", "pocketbase_cascade", "sqlite_cascade":
			continue
		default:
			return errPagePermanentDeleteUnavailable
		}
	}
	if handledCount != len(pagePermanentDeleteCleanupAuthorityKeys) {
		return errPagePermanentDeleteUnavailable
	}
	return nil
}
