package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A run again brings the names Notion changed since: a page's title, a
// database's and a property's name, while nobody changed them in Leafen. What
// a person named stays, and so does what the plan names.

func notionMigrationNameFollows(app core.App, payload notionImportJobPayload, sourceID, kind, current, name string) (bool, error) {
	if payload.MigrationRerunSince <= 0 {
		return false, nil
	}
	known := ""
	if err := app.DB().NewQuery(`
		SELECT [[state]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{"migrationId": payload.MigrationID, "sourceId": sourceID, "kind": kind}).Row(&known); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return notionMigrationGivenNameFollows(current, name, map[string]bool{strings.TrimSpace(known): known != ""}), nil
}

// notionMigrationGivenNameFollows is that rule over the names runs gave:
// what has one of them takes the name a run gives it now; any other name is
// one a person gave in Leafen, and stays. A column a run made for a Notion
// property elsewhere goes by it too (notion_migration_rerun_column_names.go).
func notionMigrationGivenNameFollows(current, name string, given map[string]bool) bool {
	current = strings.TrimSpace(current)
	return current != strings.TrimSpace(name) && given[current]
}

// syncNotionMigrationDatabaseTitle gives a database made before Notion's new
// name, and the page that holds it when it has one of its own.
func syncNotionMigrationDatabaseTitle(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource, dataSourceID string,
	structuralDocuments map[string]*structuralMutationBatch, mappings *notionApplyMappings, dataSourcePages *notionApplyDataSourcePages,
) error {
	stored := struct {
		Title  string `db:"title"`
		PageID string `db:"page_id"`
	}{}
	err := app.DB().NewQuery(`SELECT [[title]],[[page_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": dataSourceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	current, pageID := stored.Title, stored.PageID
	title := strings.TrimSpace(source.Title)
	// A database of several goes by a name of its own, apart from its
	// sources' (nameNotionDatabaseOfSeveral): Notion's new name is that one.
	if block, several, err := notionMigrationDatabaseOfSeveral(app, job.ID, source.SourceID); err != nil || several {
		if err != nil {
			return err
		}
		return syncNotionMigrationOwnName(app, job, payload, source, title, block, structuralDocuments, mappings, dataSourcePages)
	}
	follows, err := notionMigrationNameFollows(app, payload, source.SourceID, "data_source", current, title)
	if err != nil || !follows {
		return err
	}
	if err := dataSourcePages.insert(payload.AuthorityOwnerID, pageID, dataSourceID); err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return err
	}
	// The name comes with its formatting as Notion has it now, or plain.
	named := &graphDataSource{ID: dataSourceID, Title: title, TitleSpans: []graphTextSpan{}}
	presentation := notionImportPresentation{}
	if strings.HasPrefix(source.Text, notionPresentationSourcePrefix) &&
		json.Unmarshal([]byte(strings.TrimPrefix(source.Text, notionPresentationSourcePrefix)), &presentation) == nil {
		if spans := notionImportSpelledSpans(presentation.TitleSpans, title); spans != nil {
			named.TitleSpans = spans
		}
	}
	if err := applyNotionGraphOperationsWithDataSourcePages(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, []graphOperation{
		{Kind: graphOperationKindUpsertDataSource, DataSource: named},
	}, dataSourcePages); err != nil {
		return err
	}
	holder, found := mappings.nativeID(notionDatabaseRootPageSourceID(source.SourceID), "page")
	if !found {
		return nil
	}
	return renameNotionMigrationPage(app, job, holder, current, title, nil)
}

// notionMigrationDatabaseOfSeveral is the block of a database a run made
// that holds several sources now, if it does.
func notionMigrationDatabaseOfSeveral(app core.App, jobID, databaseSourceID string) (storedNotionBlock, bool, error) {
	blockID, err := notionImportMappedID(app, jobID, databaseSourceID+":block", "block")
	if err != nil || blockID == "" {
		return storedNotionBlock{}, false, err
	}
	block, found, err := loadStoredNotionBlock(app, blockID)
	if err != nil || !found || block.Type != graphBlockTypeInlineDatabase {
		return storedNotionBlock{}, false, err
	}
	shown := struct {
		Additional []string `json:"additionalDataSourceIds"`
	}{}
	if json.Unmarshal([]byte(block.Content), &shown) != nil {
		return storedNotionBlock{}, false, nil
	}
	return block, slices.ContainsFunc(shown.Additional, func(id string) bool { return id != "" }), nil
}

// syncNotionMigrationOwnName gives an inline database of several Notion's
// new name for it, while it has the one a run gave it, on its block, which
// keeps it whole. A full-page one's is its page's title, which follows as
// its page's (syncNotionMigrationPageTitle). An inline one Notion names by
// its sources' names listed has none to follow. Its sources' names follow
// on their own (syncNotionMigrationSourceName).
func syncNotionMigrationOwnName(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource, title string,
	block storedNotionBlock, structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings, dataSourcePages *notionApplyDataSourcePages,
) error {
	if _, found := mappings.nativeID(notionDatabaseRootPageSourceID(source.SourceID), "page"); found && block.ParentID == "" {
		return nil
	}
	if staged, err := stagedNotionDatabaseNames(app, job.ID, source.SourceID); err != nil || staged.mark.Unnamed {
		return err
	}
	content := map[string]any{}
	if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
		return err
	}
	current := spelledName(blockPageNameSpans([]byte(block.Content), "pageTitleSpans"))
	follows, err := notionMigrationNameFollows(app, payload, source.SourceID, "data_source", current, title)
	if err != nil || !follows {
		return err
	}
	content["pageTitleSpans"] = []graphTextSpan{{Text: title}}
	if title == "" {
		delete(content, "pageTitleSpans")
	}
	var parentID *string
	if block.ParentID != "" {
		parentID = &block.ParentID
	}
	batch, err := loadStructuralMutationBatch(app, block.PageID, structuralDocuments)
	if err != nil {
		return err
	}
	return applyNotionGraphOperationsWithDataSourcePages(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, block.PageID, job.Owner, []graphOperation{
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: block.ID, ParentID: parentID, Position: block.Position, Type: block.Type, Content: notionImportJSON(content),
		}},
	}, dataSourcePages)
}

// syncNotionMigrationSourceName gives a data source of a database of
// several, made before, the name Notion gave it since, while it has the one
// a run gave it.
func syncNotionMigrationSourceName(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource, dataSourceID string,
	structuralDocuments map[string]*structuralMutationBatch, dataSourcePages *notionApplyDataSourcePages,
) error {
	title := strings.TrimSpace(source.Title)
	if payload.MigrationRerunSince <= 0 || title == "" {
		return nil
	}
	stored := struct {
		Title  string `db:"title"`
		PageID string `db:"page_id"`
	}{}
	err := app.DB().NewQuery(`SELECT [[title]],[[page_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": dataSourceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	follows, err := notionMigrationNameFollows(app, payload, source.SourceID, "data_source", stored.Title, title)
	if err != nil || !follows {
		return err
	}
	if err := dataSourcePages.insert(payload.AuthorityOwnerID, stored.PageID, dataSourceID); err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(app, stored.PageID, structuralDocuments)
	if err != nil {
		return err
	}
	return applyNotionGraphOperationsWithDataSourcePages(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, stored.PageID, job.Owner, []graphOperation{
		{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{ID: dataSourceID, Title: title, TitleSpans: []graphTextSpan{}}},
	}, dataSourcePages)
}

// syncNotionMigrationPropertyName gives a property made before Notion's new
// name, and its database's formulas that read it by name, and Notion's
// options since (notion_migration_rerun_options.go).
func syncNotionMigrationPropertyName(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource, propertyID string,
	structuralDocuments map[string]*structuralMutationBatch, dataSourcePages *notionApplyDataSourcePages, propertyReferences *notionApplyPropertyReferences,
) error {
	type storedProperty struct {
		ID           string `db:"id"`
		PageID       string `db:"page_id"`
		DataSourceID string `db:"data_source_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}
	stored := storedProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[data_source_id]],[[type]],[[name]],[[position]],[[config]]
		FROM {{leafen_database_properties}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": propertyID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	follows, err := notionMigrationNameFollows(app, payload, source.SourceID, "property", stored.Name, source.Title)
	if err != nil {
		return err
	}
	// The ledger takes its options as this run gives them, followed or not.
	config, optionsFollow, err := followedNotionMigrationOptions(app, payload, source, stored.Type, stored.Config)
	if err == nil {
		err = keepNotionMigrationOptions(app, payload.MigrationID, source, stored.ID)
	}
	if err != nil || !follows && !optionsFollow {
		return err
	}
	name := stored.Name
	if follows {
		name = source.Title
	}
	operations, err := notionMigrationRenamesWithFormulas(app, stored.PageID, []graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
		ID: stored.ID, DataSourceID: stored.DataSourceID, Type: stored.Type, Name: name,
		Position: stored.Position, Config: json.RawMessage(config),
	}}})
	if err != nil {
		return err
	}
	batch, err := loadStructuralMutationBatch(app, stored.PageID, structuralDocuments)
	if err != nil {
		return err
	}
	return applyNotionGraphOperationsWithReferences(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, stored.PageID, job.Owner, operations,
		dataSourcePages, propertyReferences, nil)
}

// notionMigrationRenamesWithFormulas gives a move's property changes on a
// page the formula rewrites a person's same changes take
// (expandDatabaseFormulaReferenceRenames): each formula and button of the
// data sources whose properties they rename reads the renamed ones by their
// new names, renames that swap names or one a name another column shares
// too. It is the one way a run renames a column — the plan's names
// (restructureNotionDatabase) and the names Notion gave since that a run
// again follows (syncNotionMigrationPropertyName, renameNotionMigrationColumn)
// — so a formula never reads a name its column no longer has (2026-11-24: a
// plan naming 수 숫자 left 두배 reading prop("수"), 0 for 2).
func notionMigrationRenamesWithFormulas(app core.App, pageID string, operations []graphOperation) ([]graphOperation, error) {
	expanded, _, err := expandDatabaseFormulaReferenceRenames(app, pageID, operations)
	return expanded, err
}
