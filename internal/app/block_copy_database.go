package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A block copy whose blocks include an inline database that owns its data
// copies that database as well: its data sources, properties, views, rows and
// dependencies get new identities on the destination page and the copied
// container shows them. Views that belong to a copied linked database follow
// their block. The copy reuses page duplication's identity map and database
// copy windows, scoped to the data sources of the copied containers; files
// keep their records and gain references from the copies, as copied file
// blocks do, and come as copies of them into another workspace
// (block_copy_workspaces.go).

var blockCopyDatabaseKinds = []string{"data_source", "property", "view", "row", "dependency_edge"}

type storedBlockCopyDatabaseState struct {
	JobID      string `db:"job_id"`
	Kind       string `db:"kind"`
	MapCursor  string `db:"map_cursor"`
	Mapped     bool   `db:"mapped"`
	CopyCursor string `db:"copy_cursor"`
	Copied     bool   `db:"copied"`
}

func blockCopyDuplicationJob(job storedBlockMoveJob) storedPageDuplicationJob {
	return storedPageDuplicationJob{
		ID: job.ID, ActorID: job.ActorID, OwnerID: job.OwnerID, WorkspaceID: job.WorkspaceID,
		SourceOwnerID: job.SourceOwnerID, SourceWorkspaceID: job.SourceWorkspaceID,
	}
}

func blockCopyDuplicationState(job storedBlockMoveJob, state storedBlockCopyDatabaseState) storedPageDuplicationEntityState {
	return storedPageDuplicationEntityState{
		JobID: job.ID, SourcePageID: job.SourcePageID, TargetPageID: job.DestinationPageID,
		Kind: state.Kind, MapCursor: state.MapCursor, Mapped: state.Mapped,
		CopyCursor: state.CopyCursor, Copied: state.Copied,
	}
}

// initializeBlockCopyDatabaseLane records every copied block's identity and a
// state per database entity kind once discovery has found all blocks.
func initializeBlockCopyDatabaseLane(app core.App, job storedBlockMoveJob) error {
	var initialized int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_block_copy_database_states}} WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&initialized); err != nil || initialized > 0 {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		INSERT OR IGNORE INTO {{leafen_page_duplication_identity_map}}
			([[job_id]],[[kind]],[[source_id]],[[target_id]],[[source_page_id]],[[target_page_id]],[[created_at]])
		SELECT [[job_id]],'block',[[block_id]],[[target_block_id]],{:sourcePageId},{:targetPageId},{:now}
		FROM {{leafen_block_move_items}} WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": job.SourcePageID, "targetPageId": job.DestinationPageID, "now": now,
	}).Execute(); err != nil {
		return err
	}
	withoutContent, err := durableJobWithoutContent(app, job.ID)
	if err != nil {
		return err
	}
	for _, kind := range blockCopyDatabaseKinds {
		// A kind a copy without content leaves behind starts done.
		done := !databaseCopyKindCopied(kind, withoutContent)
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_block_copy_database_states}}
				([[job_id]],[[kind]],[[mapped]],[[copied]],[[created_at]],[[updated_at]])
			VALUES ({:jobId},{:kind},{:done},{:done},{:now},{:now})
		`).Bind(dbx.Params{"jobId": job.ID, "kind": kind, "done": done, "now": now}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// nextBlockCopyDatabaseState returns the next kind to map, or with copying
// set the next kind to copy, in dependency order.
func nextBlockCopyDatabaseState(app core.App, jobID string, copying bool) (storedBlockCopyDatabaseState, bool, error) {
	condition := "[[mapped]]=0"
	if copying {
		condition = "[[mapped]]=1 AND [[copied]]=0"
	}
	state := storedBlockCopyDatabaseState{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[kind]],[[map_cursor]],[[mapped]],[[copy_cursor]],[[copied]]
		FROM {{leafen_block_copy_database_states}}
		WHERE [[job_id]]={:jobId} AND ` + condition + `
		ORDER BY CASE [[kind]]
			WHEN 'data_source' THEN 0 WHEN 'property' THEN 1
			WHEN 'view' THEN 2 WHEN 'row' THEN 3 ELSE 4 END
		LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return state, false, nil
	}
	return state, err == nil, err
}

// mapBlockCopyDatabaseWindow gives the next window of one kind's entities new
// identities.
func mapBlockCopyDatabaseWindow(app core.App, job storedBlockMoveJob, state storedBlockCopyDatabaseState) error {
	table, _, err := pageDuplicationDatabaseSourceTable(state.Kind)
	if err != nil {
		return err
	}
	scoped := "[[item]].[[data_source_id]] IN (SELECT [[data_source_id]] FROM {{leafen_block_move_database_scopes}} WHERE [[job_id]]={:jobId})"
	switch state.Kind {
	case "data_source":
		scoped = "[[item]].[[id]] IN (SELECT [[data_source_id]] FROM {{leafen_block_move_database_scopes}} WHERE [[job_id]]={:jobId})"
	case "view":
		// An owned database's own views, and the views of any copied block.
		scoped = "(([[item]].[[container_id]]='' AND " + scoped + ") OR [[item]].[[container_id]] IN " +
			"(SELECT [[block_id]] FROM {{leafen_block_move_items}} WHERE [[job_id]]={:jobId}))"
	}
	rows := []pageDuplicationDatabaseSourceID{}
	if err := app.DB().NewQuery(fmt.Sprintf(`
		SELECT [[item]].[[id]] FROM {{%s}} AS [[item]]
		WHERE [[item]].[[workspace_id]]={:workspaceId} AND [[item]].[[owner]]={:ownerId}
			AND [[item]].[[page_id]]={:pageId} AND [[item]].[[id]]>{:cursor} AND %s
		ORDER BY [[item]].[[id]] LIMIT {:limit}
	`, table, scoped)).Bind(dbx.Params{
		"jobId": job.ID, "workspaceId": job.sourceWorkspace(), "ownerId": job.sourceOwner(),
		"pageId": job.SourcePageID, "cursor": state.MapCursor, "limit": maxBlockMoveItemsPerTurn + 1,
	}).All(&rows); err != nil {
		return err
	}
	hasMore := len(rows) > maxBlockMoveItemsPerTurn
	if hasMore {
		rows = rows[:maxBlockMoveItemsPerTurn]
	}
	now := time.Now().UTC().UnixMilli()
	// A copy into another workspace leaves out its relations to databases
	// it doesn't take, as a page's does; any copy leaves out the items its
	// maker can't open (page_duplication_guest.go).
	leftOut := map[string]struct{}{}
	if state.Kind == "property" {
		leftOut, err = blockCopyLeftOutProperties(app, job)
	} else if state.Kind == "row" {
		leftOut, err = pageDuplicationUnreadableRows(app, blockCopyDuplicationJob(job), rows)
	}
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, left := leftOut[row.ID]; left {
			continue
		}
		ids = append(ids, row.ID)
		if err := mapPageDuplicationDatabaseIdentity(
			app, job.ID, state.Kind, row.ID, pageDuplicationDatabaseTargetID(state.Kind),
			job.SourcePageID, job.DestinationPageID, now,
		); err != nil {
			return err
		}
	}
	if err := mapBlockCopyDatabaseFiles(app, job, state.Kind, ids, now); err != nil {
		return err
	}
	nextCursor := state.MapCursor
	if len(rows) > 0 {
		nextCursor = rows[len(rows)-1].ID
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_block_copy_database_states}}
		SET [[map_cursor]]={:cursor},[[mapped]]={:done},[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{
		"jobId": job.ID, "kind": state.Kind, "cursor": nextCursor, "done": !hasMore, "now": now,
	}).Execute()
	return err
}

// mapBlockCopyDatabaseFiles lets a copied data source's icon, cover and
// templates' bodies, and copied rows' file cells, icons, covers and bodies,
// keep the files they name (mapBlockCopyFiles).
func mapBlockCopyDatabaseFiles(app core.App, job storedBlockMoveJob, kind string, ids []string, now int64) error {
	if len(ids) == 0 || kind != "data_source" && kind != "row" {
		return nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	presented := []struct {
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
		Cover    string `db:"cover"`
	}{}
	table := "leafen_data_sources"
	if kind == "row" {
		table = "leafen_pages"
	}
	if err := app.DB().NewQuery(`
		SELECT [[icon]],[[icon_type]],[[cover]] FROM {{` + table + `}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&presented); err != nil {
		return err
	}
	fileIDs := []string{}
	for _, item := range presented {
		if id, managed := productFilePresentationID(item.Icon); managed && item.IconType == graphDataSourceIconImage {
			fileIDs = append(fileIDs, id)
		}
		if id, managed := productFilePresentationID(item.Cover); managed {
			fileIDs = append(fileIDs, id)
		}
	}
	// Their own pages' — the rows' bodies and covers, the sources'
	// templates' bodies (page_duplication_database_page_files.go).
	owned, err := databaseOwnPageFileIDs(app, job.sourceWorkspace(), job.sourceOwner(), kind, ids)
	if err != nil {
		return err
	}
	fileIDs = append(fileIDs, owned...)
	if kind == "row" {
		values := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[value]] FROM {{leafen_database_row_values}}
			WHERE [[row_id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[type]]={:files}
		`).Bind(dbx.Params{"ids": string(encoded), "files": graphValueTypeFiles}).Column(&values); err != nil {
			return err
		}
		for _, value := range values {
			items, parseErr := parseGraphFileValueItems(json.RawMessage(value))
			if parseErr != nil {
				return parseErr
			}
			for _, item := range items {
				if item.FileID != "" {
					fileIDs = append(fileIDs, item.FileID)
				}
			}
		}
	}
	return mapBlockCopyFiles(app, job, fileIDs, now)
}

// copyBlockCopyDatabaseWindow copies the next window of one mapped kind onto
// the destination page.
func copyBlockCopyDatabaseWindow(app core.App, job storedBlockMoveJob, state storedBlockCopyDatabaseState) error {
	operations, rowReferences, nextCursor, done, err := loadPageDuplicationDatabaseOperations(
		app, blockCopyDuplicationJob(job), blockCopyDuplicationState(job, state),
	)
	if err != nil {
		return err
	}
	if len(operations) > 0 {
		if state.Kind == "data_source" {
			if err := nameBlockCopyDatabase(app, job, operations); err != nil {
				return err
			}
		}
		batches := map[string]*structuralMutationBatch{}
		batch, err := loadStructuralMutationBatch(app, job.DestinationPageID, batches)
		if err != nil {
			return err
		}
		if err := applyDatabaseGraphMutationWithRowReferences(
			app, batch.doc, job.OwnerID, job.WorkspaceID, job.DestinationPageID,
			job.ActorID, operations, rowReferences,
		); err != nil {
			return err
		}
		if err := saveBlockCopyDatabaseBatch(app, job, batches); err != nil {
			return err
		}
		if err := duplicatePageDuplicationRowPages(app, blockCopyDuplicationJob(job), operations); err != nil {
			return err
		}
		if err := recordWorkspaceDatabaseCopyActivities(app, blockCopyDuplicationJob(job), job.DestinationPageID, operations); err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_block_copy_database_states}}
		SET [[copy_cursor]]={:cursor},[[copied]]={:done},[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{
		"jobId": job.ID, "kind": state.Kind, "cursor": nextCursor, "done": done,
		"now": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

// nameBlockCopyDatabase names a database copied by itself as a copy, as Notion's
// ⌘D and paste do (seen 2026-11-04); one carried by a copy of other blocks
// keeps its name, as a page's does — a page's whole body copied too, whose
// root is the page (admitPageBodyCopy).
func nameBlockCopyDatabase(app core.App, job storedBlockMoveJob, operations []graphOperation) error {
	root := struct {
		Type    string `db:"type"`
		Content string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId} AND [[id]]={:blockId}
	`).Bind(dbx.Params{
		"ownerId": job.sourceOwner(), "pageId": job.SourcePageID, "blockId": job.RootBlockID,
	}).One(&root); errors.Is(err, sql.ErrNoRows) && job.RootBlockID == job.SourcePageID {
		return nil
	} else if err != nil {
		return err
	}
	if root.Type != graphBlockTypeInlineDatabase {
		return nil
	}
	if placement, err := loadBlockMovePlacement(app, job.ID); err != nil || placement.KeepName {
		return err
	}
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(root.Content))
	if err != nil {
		return err
	}
	sources := content.dataSourceIDs()
	mappings, err := pageDuplicationReferenceMappings(app, job.ID, "data_source", sources)
	if err != nil {
		return err
	}
	for _, source := range sources {
		for index := range operations {
			if named := operations[index].DataSource; named != nil && named.ID == mappings[source] {
				named.Title, named.TitleSpans = duplicateCopyName(named.Title, named.TitleSpans)
			}
		}
	}
	return nil
}

// rollbackBlockCopyDatabaseLane removes what the copy made on the destination
// page, one window at a time, and reports whether nothing is left.
func rollbackBlockCopyDatabaseLane(app core.App, job storedBlockMoveJob) (bool, error) {
	if err := removeDatabaseTemplateCopies(app, job.ID, job.OwnerID, job.DestinationPageID); err != nil {
		return false, err
	}
	for _, kind := range []string{"view", "property", "data_source"} {
		table, _, err := pageDuplicationDatabaseSourceTable(kind)
		if err != nil {
			return false, err
		}
		// A copied source takes its own views and properties with it; only a
		// linked database's views and the relations that would hold a copied
		// source are removed first.
		condition := ""
		switch kind {
		case "view":
			condition = ` AND [[item]].[[data_source_id]] NOT IN (
				SELECT [[target_id]] FROM {{leafen_page_duplication_identity_map}}
				WHERE [[job_id]]={:jobId} AND [[kind]]='data_source')`
		case "property":
			condition = ` AND [[item]].[[type]]='relation'`
		}
		targets := []string{}
		if err := app.DB().NewQuery(fmt.Sprintf(`
			SELECT [[mapping]].[[target_id]] FROM {{leafen_page_duplication_identity_map}} [[mapping]]
			JOIN {{%s}} [[item]] ON [[item]].[[id]]=[[mapping]].[[target_id]]
				AND [[item]].[[owner]]={:ownerId} AND [[item]].[[page_id]]={:pageId}%s
			WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]={:kind}
			ORDER BY [[mapping]].[[target_id]] LIMIT {:limit}
		`, table, condition)).Bind(dbx.Params{
			"jobId": job.ID, "kind": kind, "ownerId": job.OwnerID,
			"pageId": job.DestinationPageID, "limit": maxBlockMoveItemsPerTurn,
		}).Column(&targets); err != nil {
			return false, err
		}
		if len(targets) == 0 {
			continue
		}
		operations := make([]graphOperation, len(targets))
		for index, target := range targets {
			deletion := &graphDatabaseDelete{ID: target}
			switch kind {
			case "view":
				operations[index] = graphOperation{Kind: graphOperationKindDeleteView, ViewDeletion: deletion}
			case "property":
				operations[index] = graphOperation{Kind: graphOperationKindDeleteProperty, PropertyDeletion: deletion}
			default:
				operations[index] = graphOperation{Kind: graphOperationKindDeleteDataSource, DataSourceDeletion: deletion}
			}
		}
		batches := map[string]*structuralMutationBatch{}
		batch, err := loadStructuralMutationBatch(app, job.DestinationPageID, batches)
		if err != nil {
			return false, err
		}
		if err := applyDatabaseGraphMutationWithRowReferences(
			app, batch.doc, job.OwnerID, job.WorkspaceID, job.DestinationPageID, job.ActorID, operations, nil,
		); err != nil {
			return false, err
		}
		return false, saveBlockCopyDatabaseBatch(app, job, batches)
	}
	return true, nil
}

// saveBlockCopyDatabaseBatch keeps a database window's change to the
// destination page, in its history too once that has begun
// (content_graph_structural_batch_history.go).
func saveBlockCopyDatabaseBatch(
	app core.App, job storedBlockMoveJob, batches map[string]*structuralMutationBatch,
) error {
	return saveStructuralMutationBatches(app, job.ActorID, batches)
}
