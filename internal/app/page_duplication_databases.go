package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedPageDuplicationEntityState struct {
	JobID        string `db:"job_id"`
	SourcePageID string `db:"source_page_id"`
	TargetPageID string `db:"target_page_id"`
	Kind         string `db:"kind"`
	MapCursor    string `db:"map_cursor"`
	Mapped       bool   `db:"mapped"`
	CopyCursor   string `db:"copy_cursor"`
	Copied       bool   `db:"copied"`
}

type pageDuplicationDatabaseSourceID struct {
	ID string `db:"id"`
}

func remapPageDuplicationDatabaseJSON(
	app core.App,
	jobID string,
	values []json.RawMessage,
) ([]json.RawMessage, error) {
	decoded := make([]any, len(values))
	references := newPageDuplicationDatabaseReferenceSet()
	for index, raw := range values {
		if len(raw) == 0 || len(raw) > maxGraphChangeBytes || json.Unmarshal(raw, &decoded[index]) != nil {
			return nil, errPageDuplicationConflict
		}
		collectPageDuplicationDatabaseReferences("", decoded[index], references)
	}
	mappings, err := loadPageDuplicationDatabaseReferenceSet(app, jobID, references)
	if err != nil {
		return nil, err
	}
	result := make([]json.RawMessage, len(values))
	for index := range decoded {
		rewritePageDuplicationDatabaseReferences("", decoded[index], mappings)
		result[index], err = json.Marshal(decoded[index])
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func initializePageDuplicationDatabaseStates(
	app core.App,
	jobID, ownerID, workspaceID, sourcePageID, targetPageID string,
	createdAt int64,
) error {
	withoutContent, err := durableJobWithoutContent(app, jobID)
	if err != nil {
		return err
	}
	for _, kind := range pageDuplicationDatabaseKinds {
		if err := initializePageDuplicationEntityState(
			app, jobID, sourcePageID, targetPageID, kind, createdAt, !databaseCopyKindCopied(kind, withoutContent),
		); err != nil {
			return err
		}
	}
	var hasDependencies int
	if err := app.DB().NewQuery(`
		SELECT MAX(
			EXISTS (SELECT 1 FROM {{leafen_database_dependency_bindings}}
				WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
					AND [[page_id]]={:sourcePageId} LIMIT 1),
			EXISTS (SELECT 1 FROM {{leafen_database_dependency_edges}}
				WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
					AND [[page_id]]={:sourcePageId} LIMIT 1)
		)
	`).Bind(dbx.Params{
		"ownerId": ownerID, "workspaceId": workspaceID, "sourcePageId": sourcePageID,
	}).Row(&hasDependencies); err != nil {
		return err
	}
	if hasDependencies > 0 {
		return initializePageDuplicationEntityState(
			app, jobID, sourcePageID, targetPageID, "dependency_edge", createdAt, withoutContent,
		)
	}
	return nil
}

// initializePageDuplicationEntityState records one entity kind's work; a
// kind a duplicate without content leaves behind starts done.
func initializePageDuplicationEntityState(
	app core.App,
	jobID, sourcePageID, targetPageID, kind string,
	createdAt int64,
	done bool,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_duplication_entity_states}}
			([[job_id]],[[source_page_id]],[[target_page_id]],[[kind]],[[mapped]],[[copied]],[[created_at]],[[updated_at]])
		VALUES ({:jobId},{:sourcePageId},{:targetPageId},{:kind},{:done},{:done},{:createdAt},{:createdAt})
		ON CONFLICT ([[job_id]],[[source_page_id]],[[kind]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "sourcePageId": sourcePageID, "targetPageId": targetPageID,
		"kind": kind, "createdAt": createdAt, "done": done,
	}).Execute()
	return err
}

func pageDuplicationSourceUnsupported(
	app core.App,
	job storedPageDuplicationJob,
	pageID string,
) (bool, error) {
	var unsupported int
	err := app.DB().NewQuery(`
		SELECT
			EXISTS (SELECT 1 FROM {{leafen_data_sources}}
					INDEXED BY [[idx_leafen_data_sources_export_window]]
					WHERE [[workspace_id]]={:workspaceId} AND [[owner]]={:ownerId}
						AND [[page_id]]={:pageId} LIMIT 1)
				AND (
					(SELECT COUNT(*) FROM {{leafen_page_duplication_entity_states}}
					 WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:pageId}) != 4 + MAX(
						EXISTS (SELECT 1 FROM {{leafen_database_dependency_bindings}}
							WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
								AND [[page_id]]={:pageId} LIMIT 1),
						EXISTS (SELECT 1 FROM {{leafen_database_dependency_edges}}
							WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
								AND [[page_id]]={:pageId} LIMIT 1)
					)
				)
	`).Bind(dbx.Params{
		"jobId": job.ID, "workspaceId": job.sourceWorkspace(), "ownerId": job.sourceOwner(),
		"pageId": pageID,
	}).Row(&unsupported)
	return unsupported > 0, err
}

func nextPageDuplicationDatabaseMappingState(
	app core.App,
	jobID string,
) (storedPageDuplicationEntityState, error) {
	state := storedPageDuplicationEntityState{}
	err := app.DB().NewQuery(`
		WITH [[next_item]] AS MATERIALIZED (
			SELECT [[job_id]],[[source_page_id]]
			FROM {{leafen_page_duplication_items}}
				INDEXED BY [[idx_leafen_page_duplication_item_work]]
			WHERE [[job_id]]={:jobId} AND [[blocks_mapped]]=1 AND [[database_mapped]]=0
			ORDER BY [[depth]],[[source_page_id]] LIMIT 1
		)
		SELECT [[state]].[[job_id]],[[state]].[[source_page_id]],[[state]].[[target_page_id]],
			[[state]].[[kind]],[[state]].[[map_cursor]],[[state]].[[mapped]],
			[[state]].[[copy_cursor]],[[state]].[[copied]]
		FROM [[next_item]] [[item]]
		JOIN {{leafen_page_duplication_entity_states}} [[state]]
			INDEXED BY [[idx_leafen_page_duplication_entity_map_order]]
			ON [[state]].[[job_id]]=[[item]].[[job_id]]
			AND [[state]].[[source_page_id]]=[[item]].[[source_page_id]]
		WHERE [[state]].[[mapped]]=0
		ORDER BY CASE [[state]].[[kind]]
				WHEN 'data_source' THEN 0 WHEN 'property' THEN 1
				WHEN 'view' THEN 2 WHEN 'row' THEN 3 ELSE 4 END,
			[[state]].[[kind]]
		LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&state)
	return state, err
}

// pageDuplicationDatabaseTargetID is the ID a copied database's part takes,
// in the shape the screen gives its own: a row is a page (newPageID), a data
// source and a view are a database's (newDatabaseEntityID).
func pageDuplicationDatabaseTargetID(kind string) string {
	switch kind {
	case "row":
		return newPageID()
	case "data_source", "view":
		return newDatabaseEntityID()
	}
	return core.GenerateDefaultRandomId()
}

func mapPageDuplicationDatabaseWindow(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) error {
	table, index, err := pageDuplicationDatabaseSourceTable(state.Kind)
	if err != nil {
		return err
	}
	rows := []pageDuplicationDatabaseSourceID{}
	visibility := ""
	// A source the page's database keeps in the trash where it stands comes
	// with the database that shows it (database_source_trash_in_place.go).
	if state.Kind == "data_source" {
		visibility = " AND " + databaseSourceShownSQL("[[item]].[[id]]")
	} else if state.Kind == "view" || state.Kind == "property" || state.Kind == "row" {
		visibility = " AND " + databaseSourceShownSQL("[[item]].[[data_source_id]]")
	}
	// A copy leaves out the rows in trash, as Notion does.
	switch state.Kind {
	case "row":
		visibility += " AND " + databaseRowNotTrashedSQL("[[item]].[[id]]")
	case "dependency_edge":
		visibility += " AND " + databaseRowNotTrashedSQL("[[item]].[[predecessor_row_id]]") +
			" AND " + databaseRowNotTrashedSQL("[[item]].[[successor_row_id]]")
	}
	query := fmt.Sprintf(`
		SELECT [[item]].[[id]] FROM {{%s}} AS [[item]] INDEXED BY [[%s]]
		WHERE [[item]].[[workspace_id]]={:workspaceId} AND [[item]].[[owner]]={:ownerId}
			AND [[item]].[[page_id]]={:pageId} AND [[item]].[[id]]>{:cursor}%s
		ORDER BY [[item]].[[id]] LIMIT {:limit}
	`, table, index, visibility)
	if err := app.DB().NewQuery(query).Bind(dbx.Params{
		"workspaceId": job.sourceWorkspace(), "ownerId": job.sourceOwner(),
		"pageId": state.SourcePageID, "cursor": state.MapCursor,
		"limit": maxPageDuplicationItemsPerTurn + 1,
	}).All(&rows); err != nil {
		return err
	}
	hasMore := len(rows) > maxPageDuplicationItemsPerTurn
	if hasMore {
		rows = rows[:maxPageDuplicationItemsPerTurn]
	}
	now := time.Now().UTC().UnixMilli()
	leftOut := map[string]struct{}{}
	if state.Kind == "property" {
		leftOut, err = pageDuplicationLeftOutProperties(app, job, state.SourcePageID)
	} else if state.Kind == "row" {
		// A copy leaves out the rows its maker can't read (page_duplication_guest.go).
		leftOut, err = pageDuplicationUnreadableRows(app, job, rows)
	}
	if err != nil {
		return err
	}
	mapped := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, left := leftOut[row.ID]; left {
			continue
		}
		if err := mapPageDuplicationDatabaseIdentity(
			app, job.ID, state.Kind, row.ID, pageDuplicationDatabaseTargetID(state.Kind),
			state.SourcePageID, state.TargetPageID, now,
		); err != nil {
			return err
		}
		mapped = append(mapped, row.ID)
	}
	// Into another workspace, the rows' and templates' own pages bring their
	// files (page_duplication_database_page_files.go).
	if err := takePageDuplicationDatabasePageFiles(app, job, state, mapped); err != nil {
		return err
	}
	nextCursor := state.MapCursor
	if len(rows) > 0 {
		nextCursor = rows[len(rows)-1].ID
	}
	if state.Kind == "data_source" && len(rows) == 0 && state.MapCursor == "" {
		// A page without its own sources may still hold linked databases,
		// whose views are copied with the page.
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_page_duplication_entity_states}}
			SET [[mapped]]=1,[[copied]]=1,[[updated_at]]={:updatedAt}
			WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
				AND ([[kind]] != 'view' OR NOT EXISTS (
					SELECT 1 FROM {{leafen_database_views}} AS [[owned]]
					WHERE [[owned]].[[owner]]={:ownerId} AND [[owned]].[[workspace_id]]={:workspaceId}
						AND [[owned]].[[page_id]]={:sourcePageId} AND [[owned]].[[container_id]]!=''
				))
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourcePageId": state.SourcePageID, "updatedAt": now,
			"ownerId": job.sourceOwner(), "workspaceId": job.sourceWorkspace(),
		}).Execute()
		if err != nil {
			return err
		}
		return settlePageDuplicationDatabaseMappingItem(
			app, job.ID, state.SourcePageID, now,
		)
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_entity_states}}
		SET [[map_cursor]]={:cursor},[[mapped]]={:done},[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": state.SourcePageID, "kind": state.Kind,
		"cursor": nextCursor, "done": !hasMore, "updatedAt": now,
	}).Execute()
	if err != nil {
		return err
	}
	return settlePageDuplicationDatabaseMappingItem(
		app, job.ID, state.SourcePageID, now,
	)
}

func settlePageDuplicationDatabaseMappingItem(
	app core.App,
	jobID, sourcePageID string,
	updatedAt int64,
) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}} AS [[item]]
		SET [[database_mapped]]=1,[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
			AND [[database_mapped]]=0 AND NOT EXISTS (
				SELECT 1 FROM {{leafen_page_duplication_entity_states}} AS [[state]]
				WHERE [[state]].[[job_id]]=[[item]].[[job_id]]
					AND [[state]].[[source_page_id]]=[[item]].[[source_page_id]]
					AND [[state]].[[mapped]]=0 LIMIT 1
			)
	`).Bind(dbx.Params{
		"jobId": jobID, "sourcePageId": sourcePageID, "updatedAt": updatedAt,
	}).Execute()
	return err
}

func mapPageDuplicationDatabaseIdentity(
	app core.App,
	jobID, kind, sourceID, targetID, sourcePageID, targetPageID string,
	createdAt int64,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_duplication_identity_map}}
			([[job_id]],[[kind]],[[source_id]],[[target_id]],
			 [[source_page_id]],[[target_page_id]],[[created_at]])
		VALUES ({:jobId},{:kind},{:sourceId},{:targetId},
			{:sourcePageId},{:targetPageId},{:createdAt})
		ON CONFLICT ([[job_id]],[[kind]],[[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "kind": kind, "sourceId": sourceID, "targetId": targetID,
		"sourcePageId": sourcePageID, "targetPageId": targetPageID, "createdAt": createdAt,
	}).Execute()
	return err
}

func pageDuplicationDatabaseReferenceMappings(
	app core.App,
	jobID, kind string,
	sourceIDs []string,
) (map[string]string, error) {
	mappings := map[string]string{}
	if len(sourceIDs) == 0 {
		return mappings, nil
	}
	encoded, err := json.Marshal(sourceIDs)
	if err != nil {
		return nil, err
	}
	stored := []pageDuplicationIdentity{}
	err = app.DB().NewQuery(`
		SELECT [[source_id]],[[target_id]]
		FROM {{leafen_page_duplication_identity_map}}
		WHERE [[job_id]]={:jobId} AND [[kind]]={:kind}
			AND [[source_id]] IN (SELECT [[value]] FROM json_each({:sourceIds}))
		ORDER BY [[source_id]]
	`).Bind(dbx.Params{
		"jobId": jobID, "kind": kind, "sourceIds": string(encoded),
	}).All(&stored)
	if err != nil {
		return nil, err
	}
	for _, mapping := range stored {
		mappings[mapping.SourceID] = mapping.TargetID
	}
	return mappings, nil
}

func nextPageDuplicationDatabaseCopyState(
	app core.App,
	jobID, sourcePageID string,
) (storedPageDuplicationEntityState, error) {
	state := storedPageDuplicationEntityState{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_page_id]],[[target_page_id]],[[kind]],
			[[map_cursor]],[[mapped]],[[copy_cursor]],[[copied]]
		FROM {{leafen_page_duplication_entity_states}}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
			AND [[mapped]]=1 AND [[copied]]=0
		ORDER BY CASE [[kind]]
			WHEN 'data_source' THEN 0 WHEN 'property' THEN 1
			WHEN 'view' THEN 2 WHEN 'row' THEN 3 ELSE 4 END
		LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID, "sourcePageId": sourcePageID}).One(&state)
	return state, err
}

func copyPageDuplicationDatabaseWindow(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) error {
	operations, rowReferences, nextCursor, done, err :=
		loadPageDuplicationDatabaseOperations(app, job, state)
	if err != nil {
		return err
	}
	if len(operations) > 0 {
		batches := map[string]*structuralMutationBatch{}
		batch, err := loadStructuralMutationBatch(app, state.TargetPageID, batches)
		if err != nil {
			return err
		}
		if err := applyDatabaseGraphMutationWithRowReferences(
			app, batch.doc, job.OwnerID, job.WorkspaceID, state.TargetPageID,
			job.ActorID, operations, rowReferences,
		); err != nil {
			return err
		}
		if err := saveStructuralMutationBatches(app, job.ActorID, batches); err != nil {
			return err
		}
		if err := duplicatePageDuplicationRowPages(app, job, operations); err != nil {
			return err
		}
		if err := copyDatabasePropertyTrashState(app, job.ID, job.ActorID, operations); err != nil {
			return err
		}
		if err := recordWorkspaceDatabaseCopyActivities(app, job, state.TargetPageID, operations); err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_entity_states}}
		SET [[copy_cursor]]={:cursor},[[copied]]={:done},[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": state.SourcePageID, "kind": state.Kind,
		"cursor": nextCursor, "done": done, "updatedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func pageDuplicationDatabaseSourceTable(kind string) (string, string, error) {
	switch kind {
	case "data_source":
		return "leafen_data_sources", "idx_leafen_data_sources_export_window", nil
	case "view":
		return "leafen_database_views", "idx_leafen_database_views_export_window", nil
	case "property":
		return "leafen_database_properties", "idx_leafen_database_properties_export_window", nil
	case "row":
		return "leafen_database_rows", "idx_leafen_database_rows_export_window", nil
	case "dependency_edge":
		return "leafen_database_dependency_edges", "idx_leafen_database_dependency_edges_duplication", nil
	default:
		return "", "", errPageDuplicationConflict
	}
}
