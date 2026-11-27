package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An inline database of several data sources with no name of its own comes
// from Notion's API named by its sources' names listed in English ("A and
// B", notionListedSourceNames). Notion's screen lists them in its reader's
// language — its sidebar row "소스 가 및 소스 나" (seen 2026-11-15), as
// Leafen's own sidebar does (untitledDatabaseSourcesName: two are "A 및 B",
// more "A, B, C") — and heads its block with the shown view's source. A
// migration's scan, and an import's staged database, name such a database
// as the sidebar does, in the language the person's report is written in
// (serverTextLanguage), so the migration's plan and checks, its report and
// an import's list of what it couldn't bring call it what Notion's screen
// does. The imported database itself stays unnamed and is shown by its
// sources' names (nameNotionDatabaseOfSeveral).

// notionSourceListWords are the words listing a database's sources: between
// two, for an untitled one among them, for one a database shows from another
// (a linked view's name, "X 보기") and for that one untitled, the database it
// is alone in.
var notionSourceListWords = map[string]struct{ and, untitled, view, database string }{
	accountLanguageKo: {and: " 및 ", untitled: "새 데이터 소스", view: "%s 보기", database: "새 데이터베이스"},
	accountLanguageEn: {and: " and ", untitled: "New data source", view: "%s view", database: "New database"},
}

// notionDatabaseOfSeveralSourceNames are a database's sources' names, its
// first's first.
func notionDatabaseOfSeveralSourceNames(first notionProviderOtherDataSource, others []notionProviderOtherDataSource) []string {
	names := []string{first.Name}
	for _, other := range others {
		names = append(names, other.Name)
	}
	return names
}

// notionDatabaseShownName is a database's name: Notion's, or for an inline
// database of several that Notion names by its sources' names alone, those
// names listed in the language of the person named (as Notion's English
// listing when that language can't be read).
func notionDatabaseShownName(
	app core.App, personID string, database notionProviderObject,
	first notionProviderOtherDataSource, others []notionProviderOtherDataSource,
) string {
	names := notionDatabaseOfSeveralSourceNames(first, others)
	if len(others) == 0 || !notionDatabaseOfSeveralUnnamed(database, names) {
		return notionObjectTitle(database)
	}
	language, err := serverTextLanguage(app, personID, nil)
	if err != nil {
		language = accountLanguageEn
	}
	return untitledDatabaseSourcesNameIn(language, ownDatabaseSourceTitles(names))
}

// notionMigrationScanDatabaseTitle is a scanned database's name
// (notionDatabaseShownName), in its migration owner's language.
func notionMigrationScanDatabaseTitle(
	store notionMigrationScanStore, database notionProviderObject,
	first notionProviderOtherDataSource, others []notionProviderOtherDataSource,
) string {
	owner := ""
	if len(others) > 0 {
		if err := store.app.DB().NewQuery(`SELECT [[owner]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
			Bind(dbx.Params{"id": store.migrationID}).Row(&owner); err != nil {
			return notionObjectTitle(database)
		}
	}
	return notionDatabaseShownName(store.app, owner, database, first, others)
}

// nameNotionImportDatabaseOfSeveral gives a staged inline database of
// several that Notion names by its sources' names alone that name in the
// importer's language, which what the import couldn't bring of it is listed
// under.
func nameNotionImportDatabaseOfSeveral(
	app core.App, job storedJob, database notionProviderObject,
	first notionProviderOtherDataSource, others []notionProviderOtherDataSource,
) error {
	if len(others) == 0 || !notionDatabaseOfSeveralUnnamed(database, notionDatabaseOfSeveralSourceNames(first, others)) {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_sources}} SET [[title]] = {:title}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = 'database'
	`).Bind(dbx.Params{
		"title": notionDatabaseShownName(app, job.Owner, database, first, others), "jobId": job.ID, "sourceId": database.ID,
	}).Execute()
	return err
}
