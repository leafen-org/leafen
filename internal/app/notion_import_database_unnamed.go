package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's API names a database never named with a name of its own making:
// its child_database block reads "New database", as its database does in the
// data source version and a data source never named does among several
// ("New data source"), while the database's own title in the older version
// and the data source's own title are [] (seen 2026-11-21, one made unnamed
// in Notion's screen and through its API, on a Korean account too). Notion's
// screen heads such a database "새 데이터베이스", in its reader's language,
// and its MCP names it by nothing (its data source's name "", its page's tag
// empty). A move brings it over unnamed, as one made in Leafen is, so every
// screen and fetch names it in its reader's language; one named "New
// database" by hand keeps that name, as its own title says it.

// unnameNotionImportDatabase takes the name its block gave a database of one
// source with no title of its own away from what the import staged of it.
func unnameNotionImportDatabase(
	app core.App, jobID, databaseID string, database notionProviderObject, others []notionProviderOtherDataSource,
) error {
	if len(others) > 0 || notionObjectTitle(database) != "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_sources}} SET [[title]] = ''
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = 'database' AND [[title]] != ''
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": databaseID}).Execute()
	return err
}
