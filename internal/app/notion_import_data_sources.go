package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A Notion database with several data sources comes over as one database
// holding a data source for each, in Notion's order, as a native database
// keeps its data sources. The first is read and staged as the database
// itself, by the database's name until a second comes, when the database
// keeps that name as its own and the first takes its own; each other one,
// after the first's archived rows, is staged under the database by its key,
// with its own properties, rows and archived rows, and becomes a data source
// of the database the first made. The views then go to the data source each
// one shows, and a relation to one of them points at it.

const notionImportDataSourceKeyMarker = ":source:"

// notionImportDataSourceKey names a database's data source after its first
// as the import stages it.
func notionImportDataSourceKey(databaseID, dataSourceID string) string {
	return databaseID + notionImportDataSourceKeyMarker + dataSourceID
}

// notionImportActiveSourceKey is the staged key the active data source's
// properties and rows sit under: the database for its first data source.
func notionImportActiveSourceKey(payload notionImportJobPayload) string {
	if payload.DatabaseSourceKey != "" {
		return payload.DatabaseSourceKey
	}
	return payload.ActiveDatabaseID
}

// notionImportKeyedIDs are the Notion IDs a staged database or data source
// key names: the database, and the data source after the first.
func notionImportKeyedIDs(key string) (string, string) {
	databaseID, dataSourceID, _ := strings.Cut(key, notionImportDataSourceKeyMarker)
	return databaseID, dataSourceID
}

// nextNotionImportDatabaseSource moves discovery on from a data source's
// archived rows to the database's next data source, or to its views after
// the last.
func nextNotionImportDatabaseSource(payload *notionImportJobPayload) {
	if len(payload.DatabaseOtherSources) == 0 {
		startNotionImportDatabaseViews(payload)
		return
	}
	next := payload.DatabaseOtherSources[0]
	payload.DatabaseOtherSources = payload.DatabaseOtherSources[1:]
	payload.DatabaseSourceIndex++
	payload.DatabaseDataSourceID = next.ID
	payload.DatabaseSourceKey = notionImportDataSourceKey(payload.ActiveDatabaseID, next.ID)
	payload.DatabaseSourceName = next.Name
	payload.DatabasePropertyCursor = ""
	payload.DatabasePropertyFingerprint = ""
	payload.DatabasePropertiesComplete = false
	payload.DatabaseCursor = ""
	payload.DatabaseArchivedPhase = false
	payload.DatabaseArchivedSourcesLoaded = false
	payload.DatabaseArchivedSourceIDs = nil
}

// discoverNotionDataSourceProperties reads a data source after the first
// and stages it under its database with a chunk of its properties; its
// first chunk stages the data source itself. A data source Notion will not
// give is reported and left out.
func (r *durableJobRunner) discoverNotionDataSourceProperties(
	ctx context.Context, job storedJob, payload notionImportJobPayload, client notionProviderClient,
) error {
	databaseID, key := payload.ActiveDatabaseID, payload.DatabaseSourceKey
	source, err := client.dataSource(ctx, payload.DatabaseDataSourceID)
	if notionProviderRefused(err) {
		issue := notionImportDataSourceIssue(databaseID, notionProviderOtherDataSource{
			ID: payload.DatabaseDataSourceID, Name: payload.DatabaseSourceName,
		}, payload.DatabaseSourceIndex)
		payload.DatabaseSourceKey, payload.DatabaseSourceName = "", ""
		nextNotionImportDatabaseSource(&payload)
		return r.commitNotionDiscovery(job, payload, []stagedNotionSource{issue})
	}
	if err != nil {
		return err
	}
	staged := []stagedNotionSource{}
	if payload.DatabasePropertyCursor == "" && payload.DatabasePropertyFingerprint == "" {
		// One never named comes over unnamed, which its database's list calls
		// "New data source" (notion_import_database_unnamed.go).
		title := notionObjectTitle(source)
		staged = append(staged, stagedNotionSource{
			SourceID: key, Kind: "database", ParentSourceID: databaseID, ParentKind: notionImportPropertyIssueParentKind,
			Position: payload.DatabaseSourceIndex, Title: title,
		})
	}
	// Its properties are staged under its key, and its formulas name them by
	// the data source.
	keyed := notionProviderObject{Object: "database", ID: key, Properties: source.Properties}
	properties, cursor, complete, fingerprint, err := notionDatabasePropertyChunk(
		keyed, payload.DatabasePropertyCursor, notionImportProviderPageItems,
	)
	if err != nil {
		return err
	}
	if payload.DatabasePropertyFingerprint != "" && payload.DatabasePropertyFingerprint != fingerprint {
		return errors.New(errorCodeNotionImportProviderFailed)
	}
	payload.DatabasePropertyCursor = cursor
	payload.DatabasePropertyFingerprint = fingerprint
	payload.DatabasePropertiesComplete = complete
	return r.commitNotionDiscovery(job, payload, append(staged, properties...))
}

// applyNotionDataSource adds a staged data source to the database its
// database became, after the data sources it already holds, with a table
// view its first Notion view takes over.
func applyNotionDataSource(
	app core.App,
	job storedJob,
	payload notionImportJobPayload,
	source storedNotionSource,
	structuralDocuments map[string]*structuralMutationBatch,
	mappings *notionApplyMappings,
	dataSourcePages *notionApplyDataSourcePages,
) error {
	if dataSourceID, found := mappings.nativeID(source.SourceID, "data_source"); found {
		// One a run made before takes the name Notion gave it since.
		return syncNotionMigrationSourceName(app, job, payload, source, dataSourceID, structuralDocuments, dataSourcePages)
	}
	first, found := mappings.nativeID(source.ParentSourceID, "data_source")
	if !found {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	if _, marked := decodeNotionImportFirstDataSource(source.Text); marked {
		return mappings.insert(source.SourceID, first, "data_source")
	}
	pageID, found := dataSourcePages.pageID(first)
	if !found {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	block := struct {
		ID       string `db:"id"`
		ParentID string `db:"parent_id"`
		Position string `db:"position"`
		Content  string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[position]], [[content]] FROM {{leafen_blocks}}
		WHERE [[page_id]]={:pageId} AND [[type]]='inline_database'
			AND json_extract([[content]], '$.dataSourceId')={:dataSourceId}
		LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "dataSourceId": first}).One(&block); err != nil {
		return err
	}
	content := map[string]any{}
	if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
		return err
	}
	members, _ := content["additionalDataSourceIds"].([]any)
	dataSourceID := security.RandomString(15)
	viewID := security.RandomString(15)
	content["additionalDataSourceIds"] = append(members, dataSourceID)
	renamed := []graphOperation(nil)
	if len(members) == 0 {
		var err error
		if renamed, err = nameNotionDatabaseOfSeveral(app, job.ID, source.ParentSourceID, pageID, first, block.ParentID == "", content); err != nil {
			return err
		}
	}
	title := source.Title
	if strings.TrimSpace(title) == "" {
		title = ""
	}
	var parentID *string
	if block.ParentID != "" {
		parentID = &block.ParentID
	}
	operations := []graphOperation{
		{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{ID: dataSourceID, Title: title}},
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: block.ID, ParentID: parentID, Position: block.Position,
			Type: graphBlockTypeInlineDatabase, Content: notionImportJSON(content),
		}},
		{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: viewID, DataSourceID: dataSourceID, Type: graphViewTypeTable,
			Name: "", Position: fmt.Sprintf("p%010d", source.Position),
			Config: notionImportJSON(importViewBase(nil)),
		}},
	}
	operations = append(operations, renamed...)
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return err
	}
	if err := applyNotionGraphOperationsWithDataSourcePages(
		app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations, dataSourcePages,
	); err != nil {
		return err
	}
	if err := dataSourcePages.insert(payload.AuthorityOwnerID, pageID, dataSourceID); err != nil {
		return err
	}
	if err := mappings.insert(source.SourceID, dataSourceID, "data_source"); err != nil {
		return err
	}
	return mappings.insert(notionImportDefaultViewSourceID(source.SourceID), viewID, "view")
}

// relationTarget is the data source a relation points at: the one Notion
// names when its database came over with several, or the database's own. A
// relation to a data source the import did not bring over points at nothing.
func (linker *notionImportLinker) relationTarget(link notionImportPropertyLink) (string, error) {
	if link.RelationDatabaseID == "" {
		return "", nil
	}
	if link.RelationDataSourceID != "" {
		target, err := linker.lookup(notionImportDataSourceKey(link.RelationDatabaseID, link.RelationDataSourceID), "data_source")
		if err != nil || target != "" {
			return target, err
		}
		several := 0
		if err := linker.app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_notion_import_mappings}}
			WHERE [[job_id]]={:jobId} AND [[kind]]='data_source'
				AND substr([[source_id]], 1, length({:prefix}))={:prefix}
		`).Bind(dbx.Params{
			"jobId": linker.job.ID, "prefix": link.RelationDatabaseID + notionImportDataSourceKeyMarker,
		}).Row(&several); err != nil || several > 0 {
			return "", err
		}
	}
	return linker.lookup(link.RelationDatabaseID, "data_source")
}

// notionImportFirstDataSourceMark marks the staged first data source of a
// database of several, which names the data source the database became; its
// staged title is its name in Notion. Unnamed is an inline database Notion
// names by its sources' names listed, as one without a name of its own
// (notionListedSourceNames). One staged before its name was kept carries
// the mark alone, and no title.
type notionImportFirstDataSourceMark struct {
	FirstDataSource bool `json:"firstDataSource"`
	Unnamed         bool `json:"unnamed,omitzero"`
}

func decodeNotionImportFirstDataSource(text string) (notionImportFirstDataSourceMark, bool) {
	mark := notionImportFirstDataSourceMark{}
	if !strings.HasPrefix(text, "{") || json.Unmarshal([]byte(text), &mark) != nil {
		return mark, false
	}
	return mark, mark.FirstDataSource
}

// notionImportFirstDataSource stages the first data source of a database of
// several under its key, so a relation that names it finds it, with the name
// it takes once the database holds a second (nameNotionDatabaseOfSeveral).
func notionImportFirstDataSource(
	database notionProviderObject, first notionProviderOtherDataSource, others []notionProviderOtherDataSource,
) stagedNotionSource {
	text, _ := json.Marshal(notionImportFirstDataSourceMark{
		FirstDataSource: true, Unnamed: notionDatabaseOfSeveralUnnamed(database, notionDatabaseOfSeveralSourceNames(first, others)),
	})
	// One never named comes over unnamed (notion_import_database_unnamed.go).
	title := first.Name
	if first.Unnamed {
		title = ""
	}
	return stagedNotionSource{
		SourceID: notionImportDataSourceKey(database.ID, first.ID), Kind: "database",
		ParentSourceID: database.ID, ParentKind: notionImportPropertyIssueParentKind,
		Title: title, Text: string(text),
	}
}

// notionDatabaseOfSeveralUnnamed reports whether an inline database of the
// sources named has no name of its own: Notion's API names it by its
// sources' names listed (seen 2026-11-11; a source added to one of one
// source leaves it unnamed, seen again 2026-11-15).
func notionDatabaseOfSeveralUnnamed(database notionProviderObject, names []string) bool {
	inline := database.IsInline == nil || *database.IsInline
	return inline && len(names) > 1 && notionObjectTitle(database) == notionListedSourceNames(names)
}

// nameNotionDatabaseOfSeveral names a database as it takes its second
// source, as Notion names one of several (seen 2026-11-11): the database
// keeps a name apart from its sources', the one it came with, and its first
// source takes its own name from Notion, while it still has the one the
// import gave it from its database (a database the plan merges into
// another's keeps that one's). A full-page database's name, now its page's,
// keeps its formatting (database_page_name_spans.go); an inline one keeps it
// whole on its block, as Notion's names it, unless Notion names it by its
// sources' names listed (notionInlineDatabaseName). It returns the change
// that renames the first source, which goes after the block's.
func nameNotionDatabaseOfSeveral(
	app core.App, jobID, databaseSourceID, pageID, firstID string, topLevel bool, content map[string]any,
) ([]graphOperation, error) {
	staged, err := stagedNotionDatabaseNames(app, jobID, databaseSourceID)
	if err != nil {
		return nil, err
	}
	if page, err := app.FindRecordById(pagesCollection, pageID); err == nil && content["pageTitleSpans"] == nil {
		if isFullPageDatabase(page) && topLevel {
			if spans := sourceNameSpansForPage(app, firstID, page.GetString("title")); len(spans) > 0 {
				content["pageTitleSpans"] = spans
			}
		} else if named := inlineDatabaseOwnName(app, firstID); len(named) > 0 && !staged.mark.Unnamed {
			content["pageTitleSpans"] = named
		}
	}
	current := ""
	if err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": firstID}).Row(&current); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if staged.first == "" || staged.first == current || current != staged.database {
		return nil, nil
	}
	return []graphOperation{{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{ID: firstID, Title: staged.first}}}, nil
}

// stagedNotionDatabaseNamesOf are the names a job staged for a database of
// several: the database's, its first source's, and the first's mark.
type stagedNotionDatabaseNamesOf struct {
	database, first string
	mark            notionImportFirstDataSourceMark
}

func stagedNotionDatabaseNames(app core.App, jobID, databaseSourceID string) (stagedNotionDatabaseNamesOf, error) {
	staged := []struct {
		SourceID string `db:"source_id"`
		Title    string `db:"title"`
		Text     string `db:"text"`
	}{}
	names := stagedNotionDatabaseNamesOf{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]], [[title]], [[text]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='database' AND ([[source_id]]={:databaseId} OR [[parent_source_id]]={:databaseId})
	`).Bind(dbx.Params{"jobId": jobID, "databaseId": databaseSourceID}).All(&staged); err != nil {
		return names, err
	}
	for _, source := range staged {
		if source.SourceID == databaseSourceID {
			names.database = source.Title
		} else if mark, ok := decodeNotionImportFirstDataSource(source.Text); ok {
			names.first, names.mark = source.Title, mark
		}
	}
	return names, nil
}

// keyedPropertySource is the staged source of a property of the data source
// a relation names, when that data source is one of several the import
// brought over under their keys, or the property as the database stages it.
func (linker *notionImportLinker) keyedPropertySource(databaseID, dataSourceID, propertyID string) (string, error) {
	if dataSourceID != "" {
		keyed := notionPropertySourceID(notionImportDataSourceKey(databaseID, dataSourceID), propertyID)
		plan, err := linker.stagedProperty(keyed)
		if err != nil || plan != nil {
			return keyed, err
		}
	}
	return notionPropertySourceID(databaseID, propertyID), nil
}
