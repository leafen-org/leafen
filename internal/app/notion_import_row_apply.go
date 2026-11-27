package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notionRowPlace is how a migration's plan makes a row: in a split's target
// (notion_migration_split.go) — the data source and the column each of its
// properties has there — and without the properties made in the link phase
// (Skip, notion_migration_objects.go), its options as the plan merges them
// (Value, notion_migration_values.go). Empty parts are the row's own.
type notionRowPlace struct {
	Run          *notionMigrationRunPlan
	DataSourceID string
	Column       func(propertySource string) (string, error)
	Skip         func(propertySource string) bool
	Value        func(propertySource string, value notionDatabaseRowValuePlan) (notionDatabaseRowValuePlan, error)
}

func (place *notionRowPlace) run() *notionMigrationRunPlan {
	if place == nil {
		return nil
	}
	return place.Run
}

// applyNotionDatabaseRow makes a staged row in its database; a row made
// before takes the cells the source changed since, and says true when it did.
// The source comes as the migration's plan (run) takes it (keepShownAsText).
func applyNotionDatabaseRow(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	run *notionMigrationRunPlan,
	source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings,
	dataSourcePages *notionApplyDataSourcePages,
	propertyReferences *notionApplyPropertyReferences,
	rowIdentities *notionApplyRowIdentities,
	place *notionRowPlace,
) (bool, error) {
	// A row made before takes the cells the source changed since, and only
	// those (notion_migration_source_change.go).
	if rowID, mapped := mappings.nativeID(source.SourceID, "row"); mapped {
		change, err := notionMigrationRowChanged(app, payload, run, source)
		// It takes its cells of a property made since too (notion_import_late.go).
		change = notionImportRowLateCells(payload, source, change)
		if err != nil || !change.Changed {
			return false, err
		}
		return true, updateNotionMigrationRow(app, job, payload, source, rowID, change, mappings, structuralDocuments, place)
	}
	dataSourceID, found := mappings.nativeID(source.ParentSourceID, "data_source")
	if place != nil && place.DataSourceID != "" {
		dataSourceID, found = place.DataSourceID, true
	}
	if !found {
		return false, errors.New(errorCodeNotionImportCreateFailed)
	}
	pageID, found := dataSourcePages.pageID(dataSourceID)
	if !found {
		return false, errors.New(errorCodeNotionImportCreateFailed)
	}
	values := map[string]notionDatabaseRowValuePlan{}
	if err := json.Unmarshal([]byte(source.Text), &values); err != nil {
		return false, err
	}
	rowID, found := rowIdentities.nativeID(source.SourceID)
	if !found {
		return false, errors.New(errorCodeNotionImportCreateFailed)
	}
	icon, iconType, err := notionImportRowIcon(app, job, source.SourceID, values)
	if err != nil {
		return false, err
	}
	cover, err := notionImportRowCover(app, job, source.SourceID, values)
	if err != nil {
		return false, err
	}
	archived := notionImportRowArchived(values)
	created := notionImportRowCreated(values)
	rowValues := make(map[string]graphRowValue, len(values))
	runtimeWritable := map[string]struct{}{}
	propertySources := make([]string, 0, len(values))
	for propertySource := range values {
		propertySources = append(propertySources, propertySource)
	}
	sort.Strings(propertySources)
	knownProperties := propertyReferences.references()
	sourceOf := make(map[string]string, len(propertySources))
	for _, propertySource := range propertySources {
		value := values[propertySource]
		if notionImportRowSpecialKey(propertySource) || value.Type == graphValueTypeRelation ||
			place != nil && place.Skip != nil && place.Skip(propertySource) {
			// Related rows are linked once every database has been applied.
			continue
		}
		propertyID, found := mappings.nativeID(propertySource, "property")
		if place != nil && place.Column != nil {
			var err error
			if propertyID, err = place.Column(propertySource); err != nil {
				return false, err
			}
			found = propertyID != ""
			knownProperties = propertyReferences.references()
		}
		if !found && slices.Contains(payload.LeftOut, propertySource) {
			// Its property was left out of the run (notion_import_left_out.go).
			continue
		}
		if !found {
			return false, errors.New(errorCodeNotionImportCreateFailed)
		}
		sourceOf[propertyID] = propertySource
		known, exists := knownProperties[propertyID]
		if exists && known.Type != "" && known.Type != value.Type {
			// An option goes into a column of another option type it stands
			// for; else Notion changed the property's type while the import
			// read it.
			converted, fits := notionOptionValueAs(value, known.Type)
			if !fits {
				if err := recordNotionImportPropertyIssue(app, job.ID, propertySource, notionImportIssuePropertyValueOmitted, true); err != nil {
					return false, err
				}
				continue
			}
			value = converted
		}
		// Options the plan merges, and a merged database's option as its
		// target column's of that name.
		if place != nil && place.Value != nil {
			var err error
			if value, err = place.Value(propertySource, value); err != nil {
				return false, err
			}
		}
		if exists {
			var err error
			if value, err = notionMergedOptionValue(app, place.run(), job.ID, propertySource, known.Config, value); err != nil {
				return false, err
			}
		}
		resolved, complete, err := notionImportResolvedRowValue(app, job, value)
		if err != nil {
			return false, err
		}
		if !complete {
			if err := recordNotionImportPropertyIssue(app, job.ID, propertySource, notionImportIssuePropertyValueOmitted, true); err != nil {
				return false, err
			}
		}
		if resolved == nil {
			continue
		}
		encoded, err := json.Marshal(resolved)
		if err != nil {
			return false, err
		}
		if value.Type == graphValueTypeUniqueID {
			// Notion numbered the row; the import keeps its number.
			runtimeWritable[propertyID] = struct{}{}
		}
		rowValues[propertyID] = graphRowValue{Type: value.Type, Value: encoded, Spans: value.Spans}
	}
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return false, err
	}
	operations := []graphOperation{{
		Kind: graphOperationKindUpsertRow,
		Row: &graphDatabaseRow{
			ID: rowID, DataSourceID: dataSourceID,
			Position: fmt.Sprintf("p%010d", source.Position), Values: rowValues,
		},
	}}
	references := notionGraphApplyReferences{
		sources: dataSourcePages.references(), properties: knownProperties,
		rows: rowIdentities.cache(), runtimeWritable: runtimeWritable,
	}
	err = applyNotionGraphOperationsWith(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations, references)
	if refused := (&notionImportRefusedError{}); errors.As(err, &refused) {
		// Without the values it refuses (notion_import_row_refused_values.go).
		if operations[0].Row.Values, err = notionImportRowValuesTaken(
			app, job, payload, pageID, *operations[0].Row, references, sourceOf,
		); err == nil {
			err = applyNotionGraphOperationsWith(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations, references)
		}
	}
	if err != nil {
		return false, err
	}
	if icon != "" {
		if err := updateDatabaseRowPagePresentation(app, rowID, job.WorkspaceID, pageID, job.Owner, icon, iconType); err != nil {
			return false, err
		}
	}
	if cover != "" {
		if err := applyNotionImportRowCover(app, rowID, job.WorkspaceID, job.Owner, cover); err != nil {
			return false, err
		}
	}
	// Notion gives no archive time, so an archived row is archived by the
	// person importing it when it arrives, as a workspace archive does.
	if archived {
		if _, err := applyDatabaseRowPageArchiveState(
			app, rowID, job.WorkspaceID, pageID, job.Owner, true, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return false, err
		}
	}
	// The row keeps the time Notion created it, which its created time
	// property shows and its database's newest-first order follows (the row's
	// own time, database_query_linked.go).
	if !created.IsZero() {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_row_pages}} SET [[created]] = {:created}
			WHERE [[row_id]] = {:rowId} AND [[workspace_id]] = {:workspaceId};
			UPDATE {{leafen_database_rows}} SET [[created]] = {:created}
			WHERE [[id]] = {:rowId} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{
			"created": created.UnixMilli(), "rowId": rowID, "workspaceId": job.WorkspaceID,
		}).Execute(); err != nil {
			return false, err
		}
	}
	page, err := app.FindRecordById(pagesCollection, rowID)
	if err != nil {
		return false, err
	}
	page.Set("notion_import_staging", true)
	if err := app.Save(page); err != nil {
		return false, err
	}
	return false, mappings.insert(source.SourceID, rowID, "row")
}

// notionImportRowSpecialKey reports whether a staged value key carries
// something about the row rather than a property's value.
func notionImportRowSpecialKey(key string) bool {
	return strings.HasPrefix(key, "leafen:")
}

// notionImportRowCreated takes the Notion creation time out of a row's
// staged values.
func notionImportRowCreated(values map[string]notionDatabaseRowValuePlan) time.Time {
	stored, found := values[notionImportRowCreatedKey]
	delete(values, notionImportRowCreatedKey)
	text, _ := stored.Value.(string)
	if !found || text == "" {
		return time.Time{}
	}
	created, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}
	}
	return created
}

// notionImportResolvedRowValue is the value a row stores for a staged value,
// with each Notion-hosted file replaced by its copy. It reports false when a
// hosted file could not be copied and was left out.
func notionImportResolvedRowValue(app core.App, job storedJob, value notionDatabaseRowValuePlan) (any, bool, error) {
	if value.Type != graphValueTypeFiles {
		return value.Value, true, nil
	}
	items, _ := value.Value.([]any)
	resolved := make([]any, 0, len(items))
	complete := true
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		sourceID, hosted := item[notionImportPropertyFileSourceKey].(string)
		if !hosted {
			resolved = append(resolved, item)
			continue
		}
		task, err := loadNotionFileTask(app, job.ID, sourceID)
		if errors.Is(err, sql.ErrNoRows) {
			complete = false
			continue
		}
		if err != nil {
			return nil, false, err
		}
		if task.Status != "copied" || task.FileID == "" {
			complete = false
			continue
		}
		file, err := app.FindRecordById(filesCollection, task.FileID)
		if err != nil || !workspaceFileRecordBelongsTo(file, job.WorkspaceID) {
			return nil, false, errors.New(errorCodeNotionImportFileCopyRequired)
		}
		resolved = append(resolved, graphFileValueItemFromManaged(canonicalGraphFileContent(file)))
	}
	if len(resolved) == 0 {
		return nil, complete, nil
	}
	return resolved, complete, nil
}

// recordNotionImportPropertyIssue reports what an imported property lost
// after it was staged, in its database's place. An issue found while sources
// are still being applied adds to the import's items, since the apply turns
// read it after the source that found it.
func recordNotionImportPropertyIssue(app core.App, jobID, propertySourceID, reason string, applying bool) error {
	result, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_import_sources}}
			([[job_id]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]],
			 [[text]], [[blueprint]], [[discovery_status]], [[discovery_cursor]], [[discovery_position]], [[created]])
		SELECT [[job_id]], [[source_id]] || '#' || {:reason}, 'issue', [[parent_source_id]], {:parentKind},
			[[position]], [[title]], {:reason}, 0, 'complete', '', 0, {:created}
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='property'
		ON CONFLICT ([[job_id]], [[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "sourceId": propertySourceID, "reason": reason,
		"parentKind": notionImportPropertyIssueParentKind, "created": time.Now().UTC().UnixMilli(),
	}).Execute()
	if err != nil || !applying {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted == 0 {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[total_items]] = [[total_items]] + {:inserted} WHERE [[id]] = {:jobId}
	`).Bind(dbx.Params{"jobId": jobID, "inserted": inserted}).Execute()
	return err
}
