package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace archive brings a database's sub-item, dependency and task
// settings and its dependency binding back with it, as Notion keeps a
// database's settings when it is moved or copied. The import maps the
// properties the settings name to the imported ones, leaving out a setting
// that names a property the archive does not carry, keeps them until it
// publishes the imported pages, and then writes them with the database's
// rows in place: the binding's links are drawn from the rows' Blocked by,
// and no dependent task's dates move for the rows the import wrote.
// Sub-items or dependencies on whose pair the archive does not carry — one
// from before they became relations — get it from the job runner, asked as
// the settings are written (database_subitem_pair_upkeep.go).

const (
	// workspaceImportArchiveEdgeWindow is how many rows' Blocked by one read
	// takes when the import draws a binding's links.
	workspaceImportArchiveEdgeWindow = 500
	maxWorkspaceImportArchiveDataKey = 64
)

// workspaceImportArchiveTaskSettingKeys name the properties the settings
// hold.
var workspaceImportArchiveTaskSettingKeys = []string{
	"titlePropertyId", "parentPropertyId", "subitemsPropertyId",
	"datePropertyId", "startDatePropertyId", "endDatePropertyId",
	"statusPropertyId", "assigneePropertyId", "dueDatePropertyId",
}

// workspaceImportStagedTaskSettings are an imported data source's settings
// and dependency binding, by their imported IDs.
type workspaceImportStagedTaskSettings struct {
	Subitems     json.RawMessage           `json:"subitems,omitempty"`
	Dependencies json.RawMessage           `json:"dependencies,omitempty"`
	Tasks        json.RawMessage           `json:"tasks,omitempty"`
	Binding      *archiveDependencyBinding `json:"binding,omitempty"`
}

// workspaceImportArchiveTaskSettings maps an archived data source's settings
// and dependency binding; a source without them has none.
func workspaceImportArchiveTaskSettings(
	db workspaceImportArchiveQueryer, value archiveDataSourceValue,
) (*workspaceImportStagedTaskSettings, error) {
	settings := &workspaceImportStagedTaskSettings{}
	if features := value.TaskFeatures; features != nil {
		settings.Subitems = workspaceImportArchiveTaskSetting(db, features.Subitems)
		settings.Dependencies = workspaceImportArchiveTaskSetting(db, features.Dependencies)
		settings.Tasks = workspaceImportArchiveTaskSetting(db, features.Tasks)
	}
	if binding := value.DependencyBinding; binding != nil && binding.DataKey != "" &&
		len(binding.DataKey) <= maxWorkspaceImportArchiveDataKey && strings.TrimSpace(binding.DataKey) == binding.DataKey {
		predecessor, predecessorErr := workspaceImportArchiveReference(db, binding.PredecessorPropertyID, workspaceExportItemProperty)
		successor, successorErr := workspaceImportArchiveReference(db, binding.SuccessorPropertyID, workspaceExportItemProperty)
		if predecessorErr == nil && successorErr == nil && predecessor != successor {
			settings.Binding = &archiveDependencyBinding{
				PredecessorPropertyID: predecessor, SuccessorPropertyID: successor, DataKey: binding.DataKey,
			}
		}
	}
	if settings.Subitems == nil && settings.Dependencies == nil && settings.Tasks == nil && settings.Binding == nil {
		return nil, nil
	}
	return settings, nil
}

// workspaceImportArchiveTaskSetting maps the properties one setting names;
// a setting that is not an object, or names a property the archive does not
// carry, is left out.
func workspaceImportArchiveTaskSetting(db workspaceImportArchiveQueryer, raw json.RawMessage) json.RawMessage {
	setting := map[string]any{}
	if len(raw) == 0 || json.Unmarshal(raw, &setting) != nil || setting == nil {
		return nil
	}
	for _, key := range workspaceImportArchiveTaskSettingKeys {
		value, present := setting[key]
		if !present || value == nil {
			continue
		}
		id, text := value.(string)
		if !text {
			return nil
		}
		if id == "" {
			continue
		}
		mapped, err := workspaceImportArchiveReference(db, id, workspaceExportItemProperty)
		if err != nil {
			return nil
		}
		setting[key] = mapped
	}
	encoded, err := json.Marshal(setting)
	if err != nil {
		return nil
	}
	return encoded
}

// holdWorkspaceImportArchiveTaskSettings keeps an imported data source's
// settings until the import publishes its pages.
func holdWorkspaceImportArchiveTaskSettings(
	app core.App, jobID, sourceID string, settings workspaceImportStagedTaskSettings,
) error {
	encoded, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_archive_import_task_settings}} ([[job_id]], [[data_source_id]], [[settings_json]])
		VALUES ({:jobId}, {:sourceId}, {:settings})
		ON CONFLICT ([[job_id]], [[data_source_id]]) DO UPDATE SET [[settings_json]] = excluded.[[settings_json]]
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID, "settings": string(encoded)}).Execute()
	return err
}

// publishWorkspaceArchiveTaskSettings writes the settings the import kept for
// its data sources, now that their rows are in place: a setting only while
// every property it names is still the data source's, and the dependency
// binding with the links its rows' Blocked by draw.
func publishWorkspaceArchiveTaskSettings(
	txApp core.App, jobID string, authorization workspaceAuthorization, now time.Time,
) error {
	held := []struct {
		SourceID string `db:"data_source_id"`
		PageID   string `db:"page_id"`
		Settings string `db:"settings_json"`
	}{}
	if err := txApp.DB().NewQuery(`
		SELECT [[held]].[[data_source_id]], [[source]].[[page_id]], [[held]].[[settings_json]]
		FROM {{leafen_workspace_archive_import_task_settings}} AS [[held]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[held]].[[data_source_id]]
			AND [[source]].[[owner]] = {:ownerId}
		JOIN {{leafen_workspace_archive_import_pages}} AS [[owned]]
			ON [[owned]].[[job_id]] = [[held]].[[job_id]] AND [[owned]].[[page_id]] = [[source]].[[page_id]]
		WHERE [[held]].[[job_id]] = {:jobId}
		ORDER BY [[held]].[[data_source_id]]
	`).Bind(dbx.Params{"jobId": jobID, "ownerId": authorization.OwnerID}).All(&held); err != nil {
		return err
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	paired := false
	for _, source := range held {
		settings := workspaceImportStagedTaskSettings{}
		if json.Unmarshal([]byte(source.Settings), &settings) != nil {
			continue
		}
		subitems, err := workspaceArchivePublishedTaskSetting(txApp, source.SourceID, settings.Subitems)
		if err != nil {
			return err
		}
		dependencies, err := workspaceArchivePublishedTaskSetting(txApp, source.SourceID, settings.Dependencies)
		if err != nil {
			return err
		}
		tasks, err := workspaceArchivePublishedTaskSetting(txApp, source.SourceID, settings.Tasks)
		if err != nil {
			return err
		}
		if subitems != "null" || dependencies != "null" || tasks != "null" {
			// A wiki's settings the wiki's publishing wrote give way to the
			// ones the archive carried.
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_database_task_features}}
					([[data_source_id]], [[owner]], [[workspace_id]], [[page_id]], [[authority_revision]],
					 [[subitems_json]], [[dependencies_json]], [[tasks_json]], [[created_at]], [[updated_at]])
				VALUES ({:source}, {:owner}, {:workspaceId}, {:pageId}, 1, {:subitems}, {:dependencies}, {:tasks}, {:now}, {:now})
				ON CONFLICT ([[data_source_id]]) DO UPDATE SET
					[[subitems_json]] = CASE WHEN excluded.[[subitems_json]] != 'null' THEN excluded.[[subitems_json]]
						ELSE {{leafen_database_task_features}}.[[subitems_json]] END,
					[[dependencies_json]] = CASE WHEN excluded.[[dependencies_json]] != 'null' THEN excluded.[[dependencies_json]]
						ELSE {{leafen_database_task_features}}.[[dependencies_json]] END,
					[[tasks_json]] = CASE WHEN excluded.[[tasks_json]] != 'null' THEN excluded.[[tasks_json]]
						ELSE {{leafen_database_task_features}}.[[tasks_json]] END,
					[[authority_revision]] = {{leafen_database_task_features}}.[[authority_revision]] + 1,
					[[updated_at]] = excluded.[[updated_at]]
			`).Bind(dbx.Params{
				"source": source.SourceID, "owner": authorization.OwnerID, "workspaceId": authorization.WorkspaceID,
				"pageId": source.PageID, "subitems": subitems, "dependencies": dependencies, "tasks": tasks, "now": stamp,
			}).Execute(); err != nil {
				return err
			}
			paired = paired || subitems != "null" || dependencies != "null"
		}
		if settings.Binding != nil {
			if err := publishWorkspaceArchiveDependencyBinding(
				txApp, authorization, source.SourceID, source.PageID, *settings.Binding, now,
			); err != nil {
				return err
			}
		}
	}
	// Sub-items or dependencies on without a pair the archive carried get
	// theirs from the job runner once the import is in.
	if paired {
		requestDatabaseTaskPairsAfterCommit(txApp)
	}
	_, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_workspace_archive_import_task_settings}} WHERE [[job_id]] = {:jobId}`).
		Bind(dbx.Params{"jobId": jobID}).Execute()
	return err
}

// workspaceArchivePublishedTaskSetting is a kept setting as it is written,
// or null when a property it names is not the data source's.
func workspaceArchivePublishedTaskSetting(app core.App, sourceID string, raw json.RawMessage) (string, error) {
	setting := map[string]any{}
	if len(raw) == 0 || json.Unmarshal(raw, &setting) != nil || setting == nil {
		return "null", nil
	}
	for _, key := range workspaceImportArchiveTaskSettingKeys {
		id, _ := setting[key].(string)
		if id == "" {
			continue
		}
		owned, err := workspaceArchiveSourceProperty(app, sourceID, id, "")
		if err != nil || !owned {
			return "null", err
		}
	}
	return string(raw), nil
}

// workspaceArchiveSourceProperty reports whether the property is the data
// source's, of the type given when one is.
func workspaceArchiveSourceProperty(app core.App, sourceID, propertyID, kind string) (bool, error) {
	found := 0
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_properties}}
		WHERE [[id]] = {:propertyId} AND [[data_source_id]] = {:sourceId} AND ({:kind} = '' OR [[type]] = {:kind})
	`).Bind(dbx.Params{"propertyId": propertyID, "sourceId": sourceID, "kind": kind}).Row(&found)
	return found > 0, err
}

// publishWorkspaceArchiveDependencyBinding binds a data source's Blocked by
// and Blocking again and draws the links its rows' Blocked by make, as
// writing a row's Blocked by draws them.
func publishWorkspaceArchiveDependencyBinding(
	txApp core.App, authorization workspaceAuthorization, sourceID, pageID string,
	binding archiveDependencyBinding, now time.Time,
) error {
	for _, propertyID := range []string{binding.PredecessorPropertyID, binding.SuccessorPropertyID} {
		owned, err := workspaceArchiveSourceProperty(txApp, sourceID, propertyID, graphValueTypeRelation)
		if err != nil || !owned {
			return err
		}
	}
	stamp := now.UnixMilli()
	result, err := txApp.DB().NewQuery(`
		INSERT INTO {{leafen_database_dependency_bindings}}
			([[data_source_id]], [[owner]], [[workspace_id]], [[page_id]], [[predecessor_property_id]],
			 [[successor_property_id]], [[data_key]], [[revision]], [[created]], [[updated]])
		VALUES ({:source}, {:owner}, {:workspaceId}, {:pageId}, {:predecessor}, {:successor}, {:dataKey}, 1, {:now}, {:now})
		ON CONFLICT ([[data_source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"source": sourceID, "owner": authorization.OwnerID, "workspaceId": authorization.WorkspaceID, "pageId": pageID,
		"predecessor": binding.PredecessorPropertyID, "successor": binding.SuccessorPropertyID,
		"dataKey": binding.DataKey, "now": stamp,
	}).Execute()
	if err != nil {
		return err
	}
	if bound, err := result.RowsAffected(); err != nil || bound == 0 {
		return err
	}
	cursor := ""
	for {
		values := []struct {
			RowID string `db:"row_id"`
			Value string `db:"value"`
		}{}
		if err := txApp.DB().NewQuery(`
			SELECT [[value]].[[row_id]], [[value]].[[value]]
			FROM {{leafen_database_row_values}} AS [[value]]
			JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]] = [[value]].[[row_id]]
			WHERE [[row]].[[data_source_id]] = {:source} AND [[value]].[[property_id]] = {:predecessor}
				AND [[value]].[[row_id]] > {:cursor}
			ORDER BY [[value]].[[row_id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"source": sourceID, "predecessor": binding.PredecessorPropertyID, "cursor": cursor,
			"limit": workspaceImportArchiveEdgeWindow,
		}).All(&values); err != nil {
			return err
		}
		for _, value := range values {
			predecessors := []string{}
			if json.Unmarshal([]byte(value.Value), &predecessors) != nil {
				continue
			}
			for _, predecessor := range predecessors {
				if predecessor == "" || predecessor == value.RowID {
					continue
				}
				if _, err := txApp.DB().NewQuery(`
					INSERT INTO {{leafen_database_dependency_edges}}
						([[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[data_key]],
						 [[predecessor_row_id]], [[successor_row_id]], [[created]], [[updated]])
					SELECT {:id}, {:owner}, {:workspaceId}, {:pageId}, {:source}, {:dataKey}, {:predecessor}, {:successor}, {:now}, {:now}
					WHERE EXISTS (SELECT 1 FROM {{leafen_database_rows}} WHERE [[id]] = {:predecessor} AND [[data_source_id]] = {:source})
					ON CONFLICT DO NOTHING
				`).Bind(dbx.Params{
					"id": databaseDependencyEdgeID(sourceID, predecessor, value.RowID), "owner": authorization.OwnerID,
					"workspaceId": authorization.WorkspaceID, "pageId": pageID, "source": sourceID,
					"dataKey": binding.DataKey, "predecessor": predecessor, "successor": value.RowID, "now": stamp,
				}).Execute(); err != nil {
					return err
				}
			}
		}
		if len(values) < workspaceImportArchiveEdgeWindow {
			return nil
		}
		cursor = values[len(values)-1].RowID
	}
}
