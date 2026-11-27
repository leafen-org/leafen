package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A data source moving to a database on another page runs as a block move
// without blocks. Its data lanes go to the destination page except the views,
// view references and form links the original database keeps with its linked
// views, and a last turn rewrites both databases. The job payload names the
// source and its new table view.

type dataSourceMovePayload struct {
	DataSourceID string `json:"dataSourceId"`
	ViewID       string `json:"viewId"`
	// ViewName names that view (dataSourceMoveSpec.ViewName); a move queued
	// without it leaves the view never named.
	ViewName    string `json:"viewName,omitempty"`
	NewDatabase bool   `json:"newDatabase,omitzero"`
	MoveViews   bool   `json:"moveViews,omitzero"`
	// Back undoes a move of the source alone (data_source_move_back.go).
	Back *dataSourceMoveBack `json:"back,omitempty"`
}

func (payload dataSourceMovePayload) spec(targetID string) dataSourceMoveSpec {
	return dataSourceMoveSpec{
		TargetID: targetID, NewDatabase: payload.NewDatabase, MoveViews: payload.MoveViews, ViewID: payload.ViewID,
		ViewName: payload.ViewName, Back: payload.Back,
	}
}

// dataSourceMoveKeepsLane reports the lanes that stay on the original page.
func dataSourceMoveKeepsLane(lane int) bool {
	return lane == blockMoveDatabaseLaneView || lane == blockMoveDatabaseLaneViewReference ||
		lane == blockMoveDatabaseLaneForm
}

func loadDataSourceMovePayload(app core.App, jobID string) (dataSourceMovePayload, bool, error) {
	var encoded string
	if err := app.DB().NewQuery(`
		SELECT [[payload_json]] FROM {{leafen_jobs}} WHERE [[id]]={:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).Row(&encoded); err != nil {
		return dataSourceMovePayload{}, false, err
	}
	payload := struct {
		DataSourceMove *dataSourceMovePayload `json:"dataSourceMove"`
	}{}
	if json.Unmarshal([]byte(encoded), &payload) != nil {
		return dataSourceMovePayload{}, false, errBlockMoveConflict
	}
	if payload.DataSourceMove == nil {
		return dataSourceMovePayload{}, false, nil
	}
	return *payload.DataSourceMove, true, nil
}

// admitDataSourceMove queues moving a source from its database to a database
// on another page. Both pages stay fenced until the job finishes.
func admitDataSourceMove(
	app core.App, actorID string, current notionDatabaseRowPageAuthorization,
	target notionDataSourceMoveTarget, spec dataSourceMoveSpec,
) (string, error) {
	origin := current.Container
	// A wiki's data source stays in the wiki's database.
	if _, wiki, err := loadWikiSourceProperties(app, origin.DataSource); err != nil {
		return "", err
	} else if wiki {
		return "", notionDataSourceMoveInvalid("A wiki's data source stays with the wiki.")
	}
	active, err := blockMoveSetFenceActive(app, []string{origin.PageID, target.PageID})
	if err != nil {
		return "", err
	}
	if active {
		return "", errDatabaseRowPageConflict
	}
	if _, err := dataSourceMoveLinkers(app, origin.ContainerID, origin.DataSource, target.ContainerID); err != nil {
		return "", err
	}
	if spec.MoveViews {
		members, err := loadMCPDatabaseContainerMemberships(app, origin.ContainerID)
		if err != nil {
			return "", err
		}
		if len(members) <= 1 {
			return "", notionDataSourceMoveInvalid(
				"This is the database's only data source; move the database itself to take its views along.",
			)
		}
	}
	// A shared form answers through its view, which stays on the original
	// page unless the views move too.
	var forms int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_form_links}}
		WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId} AND [[enabled]]=1
	`).Bind(dbx.Params{"pageId": origin.PageID, "sourceId": origin.DataSource}).Row(&forms); err != nil {
		return "", err
	}
	if forms != 0 && !spec.MoveViews {
		return "", notionDataSourceMoveInvalid("Turn off the data source's shared forms before moving it to another page.")
	}
	payload, err := json.Marshal(map[string]any{"dataSourceMove": dataSourceMovePayload{
		DataSourceID: origin.DataSource, ViewID: spec.ViewID, ViewName: spec.ViewName, NewDatabase: spec.NewDatabase,
		MoveViews: spec.MoveViews, Back: spec.Back,
	}})
	if err != nil {
		return "", err
	}
	jobID := core.GenerateDefaultRandomId()
	digest := sha256.Sum256([]byte(strings.Join([]string{
		blockMoveJobKind, "data-source", origin.DataSource, origin.ContainerID, target.ContainerID,
	}, "\x00")))
	requestHash := hex.EncodeToString(digest[:])
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_jobs}}
			([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],
			 [[status]],[[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],
			 [[payload_json]],[[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],
			 [[chunk_count]],[[captured_bytes]])
		VALUES
			({:id},{:owner},{:workspaceId},{:requestId},{:requestHash},{:kind},{:pageId},
			 'queued','','',{:now},{:now},{:now},{:payload},'',0,1,0,{:maxAttempts},0,0)
	`).Bind(dbx.Params{
		"id": jobID, "owner": actorID, "workspaceId": origin.WorkspaceID,
		"requestId": "notion-data-source-move:" + jobID, "requestHash": requestHash,
		"kind": blockMoveJobKind, "pageId": origin.PageID, "now": now,
		"payload": string(payload), "maxAttempts": blockMoveDefaultRetries,
	}).Execute(); err != nil {
		return "", err
	}
	if _, err := app.DB().Insert(blockMoveJobsTable, dbx.Params{
		"job_id": jobID, "actor_id": actorID, "owner_id": origin.OwnerID,
		"workspace_id": origin.WorkspaceID, "source_page_id": origin.PageID,
		"destination_page_id": target.PageID, "root_block_id": origin.ContainerID,
		"target_root_block_id": target.ContainerID, "operation": blockMoveOperationMove,
		"request_hash": requestHash, "phase": blockMovePhaseMoving,
		"revision": 1, "discovered_count": 0, "moved_count": 0,
		"error_code": "", "created_at": now, "updated_at": now, "completed_at": 0,
	}).Execute(); err != nil {
		return "", err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_block_move_database_scopes}}
			([[job_id]],[[data_source_id]],[[created_at]],[[updated_at]])
		VALUES ({:jobId},{:dataSourceId},{:now},{:now})
	`).Bind(dbx.Params{"jobId": jobID, "dataSourceId": origin.DataSource, "now": now}).Execute()
	return jobID, err
}

// advanceDataSourceMoveForward moves the source's data, then rewrites both
// databases in the same turn as the last lane check.
func advanceDataSourceMoveForward(
	app core.App, job storedBlockMoveJob, payload dataSourceMovePayload,
) (bool, error) {
	keep := dataSourceMoveKeepsLane
	if payload.MoveViews {
		keep = nil
	}
	done, err := advanceBlockMoveDatabaseForwardKeeping(app, job, keep)
	if err != nil || !done {
		return false, err
	}
	// Databases that link the source through the original follow it, read
	// now so links added during the move follow too.
	linkers, err := dataSourceMoveLinkers(app, job.RootBlockID, payload.DataSourceID, job.TargetRootBlockID)
	if err != nil {
		return false, beginBlockMoveRollback(app, job, errorCodeBlockMoveConflict)
	}
	batches := map[string]*structuralMutationBatch{}
	if payload.Back != nil {
		if moved, err := applyDataSourceMoveBackTurn(app, job, payload, batches); err != nil || !moved {
			return false, err
		}
		return finishDataSourceMove(app, job, payload, batches, linkers)
	}
	origin, err := loadStructuralMutationBatch(app, job.SourcePageID, batches)
	if err != nil {
		return false, err
	}
	spec := payload.spec(job.TargetRootBlockID)
	originOperations, _, err := dataSourceMoveOriginOperations(
		app, origin.doc, job.SourcePageID, job.OwnerID, job.RootBlockID, payload.DataSourceID, spec,
	)
	if errors.Is(err, errDatabaseRowPageConflict) {
		return false, beginBlockMoveRollback(app, job, errorCodeBlockMoveConflict)
	}
	if err != nil {
		return false, err
	}
	// The original database lets the source go before the destination takes it.
	noteDataSourceMoving(app, job.WorkspaceID, payload.DataSourceID)
	if err := applyDataSourceMoveStep(app, job, job.SourcePageID, origin, originOperations); err != nil {
		return false, err
	}
	destination, err := loadStructuralMutationBatch(app, job.DestinationPageID, batches)
	if err != nil {
		return false, err
	}
	var destinationOperations []graphOperation
	if spec.NewDatabase {
		destinationOperations, err = dataSourceMoveNewDatabaseOperations(
			app, job.DestinationPageID, job.OwnerID, job.WorkspaceID, payload.DataSourceID, spec, nil,
		)
	} else {
		destinationOperations, err = dataSourceMoveTargetOperations(
			app, destination.doc, job.DestinationPageID, job.OwnerID, payload.DataSourceID, spec, nil,
		)
	}
	if err != nil {
		return false, errors.Join(errBlockMoveConflict, err)
	}
	if err := applyDataSourceMoveStep(app, job, job.DestinationPageID, destination, destinationOperations); err != nil {
		return false, err
	}
	return finishDataSourceMove(app, job, payload, batches, linkers)
}

// finishDataSourceMove points the linking databases at the source's new
// database and records the move in the workspace's activity.
func finishDataSourceMove(
	app core.App, job storedBlockMoveJob, payload dataSourceMovePayload,
	batches map[string]*structuralMutationBatch, linkers []dataSourceMoveLinker,
) (bool, error) {
	if err := followDataSourceMoveLinkers(
		app, batches, job.OwnerID, job.WorkspaceID, job.ActorID, "data-source-move:"+job.ID,
		linkers, payload.DataSourceID, job.TargetRootBlockID,
	); err != nil {
		return false, errors.Join(errBlockMoveConflict, err)
	}
	page, err := app.FindRecordById(pagesCollection, job.DestinationPageID)
	if err != nil {
		return false, err
	}
	var title string
	if err = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).Bind(dbx.Params{"id": payload.DataSourceID}).Row(&title); err != nil {
		return false, err
	}
	if err = recordWorkspaceActivity(app, page, job.ActorID, "data-source-move:"+job.ID, "dataSource", payload.DataSourceID, "move", workspaceActivityMetadata{Name: title, LocationID: page.Id, SourceID: payload.DataSourceID, OperationJobID: job.ID}, time.Now().UTC().UnixMilli()); err != nil {
		return false, err
	}
	return true, nil
}

// followDataSourceMoveLinkers points each page's linking databases at the
// source's new database, one recorded mutation per page.
func followDataSourceMoveLinkers(
	app core.App, batches map[string]*structuralMutationBatch, ownerID, workspaceID, actorID, mutationPrefix string,
	linkers []dataSourceMoveLinker, sourceID, targetID string,
) error {
	pages := []string{}
	for _, linker := range linkers {
		if !slices.Contains(pages, linker.PageID) {
			pages = append(pages, linker.PageID)
		}
	}
	for _, pageID := range pages {
		batch, err := loadStructuralMutationBatch(app, pageID, batches)
		if err != nil {
			return err
		}
		operations, err := dataSourceMoveLinkerOperations(app, batch.doc, pageID, ownerID, linkers, sourceID, targetID)
		if err != nil {
			return err
		}
		if err := applyRecordedDatabaseStep(
			app, ownerID, workspaceID, actorID, pageID, mutationPrefix+":links:"+pageID, batch, operations,
		); err != nil {
			return err
		}
	}
	return nil
}

// followDataSourceMoveLinkersElsewhere points the linking databases on other
// pages at the source's new database after a move within one page.
func followDataSourceMoveLinkersElsewhere(
	app core.App, authorization notionDatabaseRowPageAuthorization, originID, targetID string,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		linkers, err := dataSourceMoveLinkers(txApp, originID, authorization.Container.DataSource, targetID)
		if err != nil || len(linkers) == 0 {
			return err
		}
		return followDataSourceMoveLinkers(
			txApp, map[string]*structuralMutationBatch{}, authorization.Container.OwnerID,
			authorization.Container.WorkspaceID, authorization.Identity.PrincipalID,
			"data-source-move:"+authorization.Container.DataSource+":"+newDiagnosticReference(),
			linkers, authorization.Container.DataSource, targetID,
		)
	})
}

// applyDataSourceMoveStep applies one page's database rewrite as an ordinary
// recorded graph mutation with history.
func applyDataSourceMoveStep(
	app core.App, job storedBlockMoveJob, pageID string, batch *structuralMutationBatch, operations []graphOperation,
	operationJobID ...string,
) error {
	return applyRecordedDatabaseStep(
		app, job.OwnerID, job.WorkspaceID, job.ActorID, pageID, "data-source-move:"+job.ID+":"+pageID, batch, operations, job.ID,
	)
}

// applyRecordedDatabaseStep applies database block and view operations to a
// page outside the mutation owner and records them like a graph mutation.
func applyRecordedDatabaseStep(
	app core.App, ownerID, workspaceID, actorID, pageID, mutationID string,
	batch *structuralMutationBatch, operations []graphOperation,
	operationJobID ...string,
) error {
	if len(operations) == 0 {
		return nil
	}
	if err := ensurePageHistoryBaseline(app, pageID); err != nil {
		return err
	}
	databases, err := beginPageHistoryDatabaseCapture(app, ownerID, pageID)
	if err != nil {
		return err
	}
	rows, err := beginPageHistoryRowCapture(app, pageID, operations)
	if err != nil {
		return err
	}
	activity := workspaceActivityGraphContext{Databases: databases, Rows: rows}
	if len(operationJobID) > 0 {
		activity.OperationJobID = operationJobID[0]
	}
	capture, err := beginPageHistoryGraphCapture(batch.doc, operations)
	if err != nil {
		return err
	}
	mark := crdt.MarkChanges(batch.doc)
	if _, err := applyStructuralGraphMutation(
		app, batch.doc, ownerID, workspaceID, pageID, actorID, operations, false, nil,
	); err != nil {
		return err
	}
	if err := applyDatabaseGraphMutation(
		app, batch.doc, ownerID, workspaceID, pageID, actorID, operations,
	); err != nil {
		return err
	}
	deltas, err := capture.finish(app, pageID)
	if err != nil {
		return err
	}
	update := crdt.EncodeChangesSinceV1(batch.doc, mark)
	if len(update) == 0 || len(update) > maxYgoUpdateBytes {
		return errBlockMoveConflict
	}
	if err := appendYgoStructuralUpdate(app, pageID, update); err != nil {
		return err
	}
	counts := graphMutationCounts{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertBlock {
			counts.Blocks++
		} else {
			counts.Views++
		}
	}
	if err := recordNativeMigrationGraphMutation(
		app, ownerID, pageID, mutationID, actorID, update, "", deltas, counts, activity,
	); err != nil {
		return err
	}
	return markSearchDirtyByPageID(app, pageID)
}

// dataSourceMoveActive reports whether the source is between databases.
func dataSourceMoveActive(app core.App, dataSourceID string) (bool, error) {
	var active int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_block_move_database_scopes}} AS [[scope]]
		JOIN {{leafen_block_move_jobs}} AS [[move]] ON [[move]].[[job_id]] = [[scope]].[[job_id]]
		WHERE [[scope]].[[data_source_id]] = {:dataSourceId}
			AND [[move]].[[phase]] NOT IN ('completed','failed')
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&active)
	return active != 0, err
}
