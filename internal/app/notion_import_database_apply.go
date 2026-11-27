package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

func applyNotionGraphOperations(
	app core.App,
	batch *structuralMutationBatch,
	owner, workspaceID, pageID, actorID string,
	operations []graphOperation,
) error {
	return applyNotionGraphOperationsWithDataSourcePages(
		app, batch, owner, workspaceID, pageID, actorID, operations, nil,
	)
}

func applyNotionGraphOperationsWithDataSourcePages(
	app core.App,
	batch *structuralMutationBatch,
	owner, workspaceID, pageID, actorID string,
	operations []graphOperation,
	dataSourcePages *notionApplyDataSourcePages,
) error {
	return applyNotionGraphOperationsWithReferences(
		app, batch, owner, workspaceID, pageID, actorID, operations, dataSourcePages, nil, nil,
	)
}

func applyNotionGraphOperationsWithReferences(
	app core.App,
	batch *structuralMutationBatch,
	owner, workspaceID, pageID, actorID string,
	operations []graphOperation,
	dataSourcePages *notionApplyDataSourcePages,
	propertyReferences *notionApplyPropertyReferences,
	rowReferences *graphReferenceCache,
) error {
	return applyNotionGraphOperationsWith(app, batch, owner, workspaceID, pageID, actorID, operations, notionGraphApplyReferences{
		sources: dataSourcePages.references(), properties: propertyReferences.references(), rows: rowReferences,
	})
}

// notionGraphApplyReferences are what an import mutation may name besides
// its own page: known data sources, properties and rows, rows on other pages
// a relation value points at, and the unique ID values Notion numbered.
type notionGraphApplyReferences struct {
	sources         map[string]graphEntityReference
	properties      map[string]graphEntityReference
	rows            *graphReferenceCache
	externalRows    map[string]graphEntityReference
	runtimeWritable map[string]struct{}
}

// applyNotionGraphOperationsWith writes an import's change. A row it
// writes that was there before keeps when and by whom it was last edited:
// the import gives each row Notion's history last (notion_import_row_stamps.go)
// — on a run again only to the rows Notion changed since — so a run again's
// writes to a row Notion left as it was, its relations as Notion gives them
// and their other sides (2026-11-24: their last edits took the run's time),
// leave it as the run before or a person in Leafen since left it.
func applyNotionGraphOperationsWith(
	app core.App,
	batch *structuralMutationBatch,
	owner, workspaceID, pageID, actorID string,
	operations []graphOperation,
	references notionGraphApplyReferences,
) error {
	rowReferences := references.rows
	if _, err := validateGraphMutationWithAllReferenceCachesMode(
		app, owner, workspaceID, pageID, operations, references.runtimeWritable,
		references.sources, nil, references.properties, rowReferences, references.externalRows, false, false,
	); err != nil {
		return &notionImportRefusedError{cause: err}
	}
	if _, err := applyStructuralGraphMutation(
		app, batch.doc, owner, workspaceID, pageID, actorID, operations, false, batch.starts,
	); err != nil {
		return err
	}
	if err := applyDatabaseGraphMutationWithOptions(
		app, batch.doc, owner, workspaceID, pageID, actorID, operations, rowReferences,
		databaseRecordMutationOptions{PreserveRowEditStamps: true},
	); err != nil {
		return err
	}
	// The batch's save marks the rows for search (server_save_search_marks.go).
	batch.wroteRows(workspaceID, operations)
	return nil
}

// notionImportRefusedError is an imported block or value the graph's validation
// refused: nothing of it has been written, so a caller can leave it out and go on.
type notionImportRefusedError struct {
	cause error
}

func (err *notionImportRefusedError) Error() string { return err.cause.Error() }
func (err *notionImportRefusedError) Unwrap() error { return err.cause }

func notionAppendAfter(batch *structuralMutationBatch, parentID string) (*string, error) {
	order, err := structuralOrder(batch.doc, parentID)
	if err != nil || len(order) == 0 {
		return nil, err
	}
	afterID := order[len(order)-1]
	return &afterID, nil
}

func applyNotionDatabase(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings,
	dataSourcePages *notionApplyDataSourcePages,
) error {
	if dataSourceID, found := mappings.nativeID(source.SourceID, "data_source"); found {
		return syncNotionMigrationDatabaseTitle(app, job, payload, source, dataSourceID, structuralDocuments, mappings, dataSourcePages)
	}
	// A database inside a toggle or column stays inside that block.
	pageID, parentID, err := notionImportBlockParent(app, source, mappings)
	if err != nil {
		return err
	}
	dataSourceID := security.RandomString(15)
	blockID := uuid.NewString()
	// The database starts with one table view, keeping its own row order
	// (importViewBase); the first of its Notion views that is a tab
	// takes this view over once every property exists.
	tableViewID := security.RandomString(15)
	// An untitled Notion database stays untitled; each reader sees the
	// placeholder in their own language.
	title, err := notionImportFittedTitle(app, job.ID, source, "child_database")
	if err != nil {
		return err
	}
	if strings.TrimSpace(title) == "" {
		title = ""
	}
	dataSource, err := notionImportDataSource(app, job, source, dataSourceID, title)
	if err != nil {
		return err
	}
	content, err := json.Marshal(map[string]string{"dataSourceId": dataSourceID, "viewId": tableViewID})
	if err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return err
	}
	afterID, err := placeNotionImportBlock(app, job, payload, source, batch, parentID, blockID, mappings)
	if err != nil {
		return err
	}
	operations := []graphOperation{
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: blockID, ParentID: optionalNotionImportParentID(parentID), AfterID: afterID,
			Position: fmt.Sprintf("notion:%010d", source.Position),
			Type:     graphBlockTypeInlineDatabase, Content: content,
		}},
		{Kind: graphOperationKindUpsertDataSource, DataSource: dataSource},
		{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: tableViewID, DataSourceID: dataSourceID, Type: graphViewTypeTable,
			Name: "", Position: "p0000000000", Config: notionImportJSON(importViewBase(nil)),
		}},
	}
	if err := applyNotionGraphOperationsWithDataSourcePages(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations, dataSourcePages,
	); err != nil {
		return err
	}
	if err := dataSourcePages.insert(payload.AuthorityOwnerID, pageID, dataSourceID); err != nil {
		return err
	}
	for _, mapping := range []struct{ sourceID, nativeID, kind string }{
		{source.SourceID, dataSourceID, "data_source"},
		{source.SourceID + ":block", blockID, "block"},
		{notionImportDefaultViewSourceID(source.SourceID), tableViewID, "view"},
	} {
		if err := mappings.insert(mapping.sourceID, mapping.nativeID, mapping.kind); err != nil {
			return err
		}
	}
	return nil
}

func applyNotionDatabasePropertyMapping(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings,
	dataSourcePages *notionApplyDataSourcePages,
	propertyReferences *notionApplyPropertyReferences,
) (string, error) {
	if nativeID, found := mappings.nativeID(source.SourceID, "property"); found {
		return nativeID, syncNotionMigrationPropertyName(app, job, payload, source, nativeID, structuralDocuments, dataSourcePages, propertyReferences)
	}
	dataSourceID, found := mappings.nativeID(source.ParentSourceID, "data_source")
	if !found {
		return "", errors.New(errorCodeNotionImportCreateFailed)
	}
	pageID, found := dataSourcePages.pageID(dataSourceID)
	if !found {
		return "", errors.New(errorCodeNotionImportCreateFailed)
	}
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return "", err
	}
	if plan.linked() {
		// Made once every database has been applied.
		return "", nil
	}
	config, err := json.Marshal(plan.Config)
	if err != nil {
		return "", err
	}
	propertyID := security.RandomString(15)
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return "", err
	}
	operations := []graphOperation{{
		Kind: graphOperationKindUpsertProperty,
		Property: &graphProperty{
			ID: propertyID, DataSourceID: dataSourceID, Type: plan.Type, Name: source.Title,
			Position: fmt.Sprintf("p%010d", source.Position), Config: config,
		},
	}}
	if err := applyNotionGraphOperationsWithReferences(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations,
		dataSourcePages, propertyReferences, nil,
	); err != nil {
		return "", err
	}
	if err := propertyReferences.insert(
		payload.AuthorityOwnerID, pageID, dataSourceID, propertyID, plan.Type, string(config),
	); err != nil {
		return "", err
	}
	if err := mappings.insert(source.SourceID, propertyID, "property"); err != nil {
		return "", err
	}
	// Its options as the run gave them, for a run again (notion_migration_rerun_options.go).
	return propertyID, keepNotionMigrationOptions(app, payload.MigrationID, source, propertyID)
}

// notionImportDefaultViewSourceID names the table view an imported database
// starts with.
func notionImportDefaultViewSourceID(databaseID string) string {
	return databaseID + ":table"
}
