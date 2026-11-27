package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Before 2026-11-21 a Notion move gave a database Notion never named the
// name Notion's API makes up for it, as its own: "New database" for one of
// one source, which its block calls it by, and "New data source" for a
// source never named among several, which its database's list calls it by
// (notion_import_database_unnamed.go). Fetch named its source so and wrote
// it in its row's path, and a Korean screen showed it in English where
// Notion shows "새 데이터베이스". Migration 844 takes that name away from
// each data source a move made, its job's records still here, that nobody
// renamed since the move wrote it — its source written no later than its
// job's last turn — so it reads in its reader's language as one made in
// Leafen does. One whose move's scan read its database's own name as that
// very name was named so by hand in Notion, and keeps it; so does one
// renamed since (a person may have named it so); one whose move's records
// are gone can be renamed by hand.

// notionMovedDefaultSourceNames are the names Notion's API makes up for a
// database or a data source never named.
var notionMovedDefaultSourceNames = []string{"New database", "New data source"}

type movedDefaultNamedSource struct {
	ID          string `db:"id"`
	Owner       string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	Title       string `db:"title"`
	Updated     int64  `db:"updated"`
	NotionID    string `db:"source_id"`
	MigrationID string `db:"migration_id"`
}

// clearMovedDefaultSourceNames is migration 844's content.
func clearMovedDefaultSourceNames(app core.App) error {
	found := []movedDefaultNamedSource{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[source]].[[id]], [[source]].[[owner]], [[source]].[[workspace_id]], [[source]].[[page_id]],
			[[source]].[[title]], [[source]].[[updated]], [[mapping]].[[source_id]],
			COALESCE((SELECT [[migration]].[[id]] FROM {{leafen_notion_migrations}} AS [[migration]]
				WHERE [[migration]].[[import_job_id]] = [[job]].[[id]] LIMIT 1), '') AS [[migration_id]]
		FROM {{leafen_data_sources}} AS [[source]]
		JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[kind]] = 'data_source' AND [[mapping]].[[native_id]] = [[source]].[[id]]
		JOIN {{leafen_jobs}} AS [[job]] ON [[job]].[[id]] = [[mapping]].[[job_id]] AND [[job]].[[workspace_id]] = [[source]].[[workspace_id]]
		WHERE [[source]].[[title]] IN ({:database}, {:source}) AND [[source]].[[title_spans]] = ''
			AND [[source]].[[updated]] <= [[job]].[[updated]]
		ORDER BY [[source]].[[page_id]], [[source]].[[id]]
	`).Bind(dbx.Params{
		"database": notionMovedDefaultSourceNames[0], "source": notionMovedDefaultSourceNames[1],
	}).All(&found); err != nil {
		return err
	}
	sources := make([]movedDefaultNamedSource, 0, len(found))
	for _, source := range found {
		named, err := notionMovedSourceNamedByHand(app, source)
		if err != nil {
			return err
		}
		// A source the move made twice (a database of several, mapped by its
		// database and by its own key) is cleared once.
		if !named && (len(sources) == 0 || sources[len(sources)-1].ID != source.ID) {
			sources = append(sources, source)
		}
	}
	cleared := 0
	for start := 0; start < len(sources); {
		end := start + 1
		for end < len(sources) && sources[end].PageID == sources[start].PageID {
			end++
		}
		if err := clearMovedDefaultSourceNamesOnPage(app, sources[start:end]); err != nil {
			return fmt.Errorf("databases moved unnamed on page %s: %w", sources[start].PageID, err)
		}
		cleared += end - start
		start = end
	}
	if cleared > 0 {
		app.Logger().Info("databases a Notion move named by Notion's made-up name left unnamed", "sources", cleared)
	}
	return nil
}

// notionMovedSourceNamedByHand reports whether the move's scan read the
// database a source came from as named that very name of its own: a
// database of one source a person named "New database" in Notion.
func notionMovedSourceNamedByHand(app core.App, source movedDefaultNamedSource) (bool, error) {
	if source.MigrationID == "" {
		return false, nil
	}
	databaseID, _, _ := strings.Cut(source.NotionID, notionImportDataSourceKeyMarker)
	var value string
	err := app.DB().NewQuery(`
		SELECT [[value]] FROM {{leafen_notion_migration_scan_items}}
		WHERE [[migration_id]] = {:migrationId} AND [[kind]] = {:kind} AND [[item_key]] = {:key}
	`).Bind(dbx.Params{
		"migrationId": source.MigrationID, "kind": notionMigrationItemDatabase, "key": notionMigrationSourceKey(databaseID),
	}).Row(&value)
	if err != nil {
		// No scan of it says otherwise.
		return false, nil
	}
	scanned := struct {
		Title string `json:"title"`
	}{}
	if json.Unmarshal([]byte(value), &scanned) != nil {
		return false, nil
	}
	return scanned.Title == source.Title, nil
}

// clearMovedDefaultSourceNamesOnPage takes the made-up name away from the
// data sources a page holds and gives them back their last edits: they
// stand as the move meant them, unedited.
func clearMovedDefaultSourceNamesOnPage(app core.App, sources []movedDefaultNamedSource) error {
	first := sources[0]
	batch, err := loadStructuralMutationBatch(app, first.PageID, map[string]*structuralMutationBatch{})
	if err != nil {
		return err
	}
	operations := make([]graphOperation, 0, len(sources))
	for _, source := range sources {
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{
			ID: source.ID, PageID: source.PageID, Title: "", TitleSpans: []graphTextSpan{},
		}})
	}
	// A source's records are its authority; the page's document keeps none of it.
	if err := applyDatabaseGraphMutation(
		app, batch.doc, first.Owner, first.WorkspaceID, first.PageID, first.Owner, operations,
	); err != nil {
		return err
	}
	for _, source := range sources {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_data_sources}} SET [[updated]] = {:updated} WHERE [[owner]] = {:owner} AND [[id]] = {:id}
		`).Bind(dbx.Params{"updated": source.Updated, "owner": source.Owner, "id": source.ID}).Execute(); err != nil {
			return err
		}
	}
	// Its history keeps the sources as they now stand.
	return recordPageHistoryRewrite(app, first.PageID, first.Owner, true)
}

// validateMovedDefaultSourceNames is its check: the tables it reads and
// writes are there.
func validateMovedDefaultSourceNames(app core.App) error {
	for _, table := range []string{
		"leafen_data_sources", "leafen_notion_import_mappings", "leafen_jobs", "leafen_notion_migrations",
		"leafen_notion_migration_scan_items",
	} {
		if !app.HasTable(table) {
			return fmt.Errorf("databases moved unnamed: the table %s is missing", table)
		}
	}
	return nil
}
