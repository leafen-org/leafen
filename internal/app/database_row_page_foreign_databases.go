package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's body copied before 2026-11-14 — a row's duplicate, a
// page made from a template — took the body's own database as its block
// alone: the copy's block named the source's database, on another page. The
// copy showed "데이터베이스를 사용할 수 없습니다" there, a block nobody could
// select, move or delete. Each such block now takes a copy of the database
// it names, as today's copy of the body does (database_template_body_
// databases.go): the copy job places it after the block, then takes the
// block away. A block naming a database that no page holds any more stays
// as it is. A body copied into another workspace before 2026-11-15 kept its
// block naming the database in the workspace it came from: that block takes
// a copy of the database from there (block_copy_workspaces.go), once the
// person who wrote it may still read it.

// writeRowPageDatabaseCopyJob writes the job that copies the databases into
// the page, from sourcePageID or each one's own page.
func writeRowPageDatabaseCopyJob(
	app core.App, actorID, sourcePageID, workspaceID, targetPageID string, databases []rowPageOwnDatabase, subPages []rowPageSubPageCopy,
) error {
	payload, err := json.Marshal(rowPageDatabaseCopyPayload{
		ActorID: actorID, SourcePageID: sourcePageID, Databases: databases, Copies: map[string]string{}, SubPages: subPages,
	})
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	requestID := "row-page-database-copy:" + newDiagnosticReference()
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_jobs}}
			([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
			 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]], [[cursor]],
			 [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
		VALUES ({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, {:pageId},
			'queued', '', {:now}, {:now}, {:now}, {:payload}, '', 0, {:total}, 0, {:maxAttempts}, 0)
	`).Bind(dbx.Params{
		"id": core.GenerateDefaultRandomId(), "owner": actorID, "workspaceId": workspaceID,
		"requestId": requestID, "requestHash": automationDigest(requestID), "kind": rowPageDatabaseCopyJobKind,
		"pageId": targetPageID, "now": now, "payload": string(payload), "total": len(databases) + len(subPages),
		"maxAttempts": defaultDurableJobMaxAttempts,
	}).Execute()
	return err
}

// removeRowPageForeignDatabaseBlock takes away a block that named another
// page's database, once a copy of that database stands after it.
func removeRowPageForeignDatabaseBlock(app core.App, actorID, pageID, blockID string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		page, err := txApp.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		batches := map[string]*structuralMutationBatch{}
		batch, err := loadStructuralMutationBatch(txApp, pageID, batches)
		if err != nil {
			return err
		}
		if err := applyNotionGraphOperations(txApp, batch, page.GetString("owner"), page.GetString("workspace"), pageID, actorID,
			[]graphOperation{{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: blockID}}},
		); err != nil {
			return err
		}
		return saveStructuralMutationBatches(txApp, actorID, batches)
	})
}
