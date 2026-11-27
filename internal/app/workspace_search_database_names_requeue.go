package app

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// requeueSearchUntitledDatabaseNames is migration 861's content: the index's
// entries made before a database's name there went through the one rule
// (databaseNameFrom) are made again whole — an untitled full-page database's,
// now found by its sources' names (searchUntitledDatabaseWords), and a
// database's held in a page's body that keeps a source in the trash with its
// views, now named by its first source that names it, with the page holding
// it, whose line names it so.
func requeueSearchUntitledDatabaseNames(app core.App) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
		SELECT [[entry]].[[page_id]], [[entry]].[[workspace_id]], 1, {:now} FROM {{leafen_search_pages}} [[entry]]
		WHERE [[entry]].[[page_id]] IN (
				SELECT [[page]].[[id]] FROM {{leafen_pages}} [[page]]
				WHERE [[page]].[[kind]] = {:database} AND TRIM(COALESCE([[page]].[[title]], '')) = '')
			OR [[entry]].[[page_id]] IN (
				SELECT [[container]].[[id]] FROM {{leafen_database_containers}} [[container]]
				JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[container]].[[block_id]] AND [[block]].[[page_id]] = [[container]].[[page_id]]
				WHERE [[container]].[[kind]] = 'source' AND instr([[block]].[[content]], {:gone}) > 0
				UNION ALL
				SELECT [[container]].[[page_id]] FROM {{leafen_database_containers}} [[container]]
				JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[container]].[[block_id]] AND [[block]].[[page_id]] = [[container]].[[page_id]]
				WHERE [[container]].[[kind]] = 'source' AND instr([[block]].[[content]], {:gone}) > 0)
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).Bind(dbx.Params{
		"now": time.Now().UnixMilli(), "database": pageKindDatabase, "gone": `"` + databaseTrashedWithViewsField + `"`,
	}).Execute()
	return err
}

func validateSearchUntitledDatabaseNamesRequeued(app core.App) error {
	for _, table := range []string{"leafen_search_pages", "leafen_search_dirty", "leafen_pages", "leafen_database_containers", "leafen_blocks"} {
		if !app.HasTable(table) {
			return fmt.Errorf("search untitled database names: the table %s is missing", table)
		}
	}
	return nil
}

// requeueSearchPageDatabaseLines is migration 862's content: a page holding
// a database of two or more sources — its own and those it links — is made
// again whole, its line for that database naming it by the one rule
// (searchInlineDatabaseLines): without a name of its own by its sources'
// names listed, where it named it by its first source's alone.
func requeueSearchPageDatabaseLines(app core.App) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
		SELECT [[entry]].[[page_id]], [[entry]].[[workspace_id]], 1, {:now} FROM {{leafen_search_pages}} [[entry]]
		WHERE [[entry]].[[page_id]] IN (
			SELECT [[container]].[[page_id]] FROM {{leafen_database_containers}} [[container]]
			WHERE [[container]].[[kind]] = 'source'
				AND (SELECT COUNT(*) FROM {{leafen_database_container_sources}} [[place]]
					WHERE [[place]].[[container_id]] = [[container]].[[id]]) > 1)
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).Bind(dbx.Params{"now": time.Now().UnixMilli()}).Execute()
	return err
}

func validateSearchPageDatabaseLinesRequeued(app core.App) error {
	for _, table := range []string{"leafen_search_pages", "leafen_search_dirty", "leafen_database_containers", "leafen_database_container_sources"} {
		if !app.HasTable(table) {
			return fmt.Errorf("search page database lines: the table %s is missing", table)
		}
	}
	return nil
}
