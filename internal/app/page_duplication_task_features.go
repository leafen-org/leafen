package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A duplicated or copied database keeps its sub-item, dependency and task
// settings and what it was made as (data_source_starter_identity.go), as
// Notion's duplicate keeps a database's settings. The copy takes
// them once all its rows are in place, so that no dependent task's dates move
// for the rows the copy wrote; they name the copy's own properties.

// copyPageDuplicationTaskFeatures gives the databases the published pages
// hold the settings of the databases they copy.
func copyPageDuplicationTaskFeatures(
	app core.App, job storedPageDuplicationJob, items []storedPageDuplicationItem,
) error {
	for _, item := range items {
		sourceIDs := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_data_sources}} WHERE [[page_id]] = {:sourcePageId} ORDER BY [[id]]
		`).Bind(dbx.Params{"sourcePageId": item.SourcePageID}).Column(&sourceIDs); err != nil {
			return err
		}
		if err := copyDatabaseTaskFeatures(app, job.ID, job.OwnerID, job.WorkspaceID, sourceIDs); err != nil {
			return err
		}
		if err := copyDataSourceStarterIdentity(app, job.ID, sourceIDs); err != nil {
			return err
		}
		if err := copyDatabasePageLayouts(app, job.ID, job.ActorID, job.WorkspaceID, sourceIDs); err != nil {
			return err
		}
		if err := copyDatabasePropertyAccess(app, job.ID, job.ActorID, job.WorkspaceID, sourceIDs); err != nil {
			return err
		}
	}
	return nil
}

// copyBlockCopyTaskFeatures gives the databases a block copy made the
// settings of the databases they copy.
func copyBlockCopyTaskFeatures(app core.App, job storedBlockMoveJob) error {
	sourceIDs := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_block_move_database_scopes}} WHERE [[job_id]] = {:jobId}
		ORDER BY [[data_source_id]]
	`).Bind(dbx.Params{"jobId": job.ID}).Column(&sourceIDs); err != nil {
		return err
	}
	if err := copyDatabaseTaskFeatures(app, job.ID, job.OwnerID, job.WorkspaceID, sourceIDs); err != nil {
		return err
	}
	if err := copyDataSourceStarterIdentity(app, job.ID, sourceIDs); err != nil {
		return err
	}
	if err := copyDatabasePageLayouts(app, job.ID, job.ActorID, job.WorkspaceID, sourceIDs); err != nil {
		return err
	}
	return copyDatabasePropertyAccess(app, job.ID, job.ActorID, job.WorkspaceID, sourceIDs)
}

// copyDatabaseTaskFeatures writes, for each copied data source, the settings
// of the source it copies, its property IDs mapped to the copy's, on the page
// holding the copy.
func copyDatabaseTaskFeatures(app core.App, jobID, ownerID, workspaceID string, sourceIDs []string) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	copies, err := pageDuplicationReferenceMappings(app, jobID, "data_source", sourceIDs)
	if err != nil {
		return err
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(sourceIDs))
	for index, sourceID := range sourceIDs {
		key := fmt.Sprintf("featureSource%d", index)
		params[key] = sourceID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	features := []struct {
		SourceID     string `db:"data_source_id"`
		Subitems     string `db:"subitems_json"`
		Dependencies string `db:"dependencies_json"`
		Tasks        string `db:"tasks_json"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[subitems_json]], [[dependencies_json]], [[tasks_json]]
		FROM {{leafen_database_task_features}} WHERE [[data_source_id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[data_source_id]]
	`).Bind(params).All(&features); err != nil {
		return err
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	for _, feature := range features {
		target := copies[feature.SourceID]
		if target == "" {
			continue
		}
		remapped, err := remapPageDuplicationDatabaseJSON(app, jobID, []json.RawMessage{
			json.RawMessage(feature.Subitems), json.RawMessage(feature.Dependencies), json.RawMessage(feature.Tasks),
		})
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_task_features}}
				([[data_source_id]], [[owner]], [[workspace_id]], [[page_id]], [[authority_revision]],
				 [[subitems_json]], [[dependencies_json]], [[tasks_json]], [[created_at]], [[updated_at]])
			SELECT [[source]].[[id]], {:owner}, {:workspaceId}, [[source]].[[page_id]], 1,
				{:subitems}, {:dependencies}, {:tasks}, {:now}, {:now}
			FROM {{leafen_data_sources}} AS [[source]] WHERE [[source]].[[id]] = {:source}
			ON CONFLICT ([[data_source_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"source": target, "owner": ownerID, "workspaceId": workspaceID,
			"subitems": string(remapped[0]), "dependencies": string(remapped[1]), "tasks": string(remapped[2]),
			"now": stamp,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
