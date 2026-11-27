package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A view block only shows views, with no database of its own, as Notion's
// linked database, linked view still waiting for its source and dashboard
// (seen 2026-11-09: the API's `data_sources` is empty). It is named by its
// own name, and "제목 없음" while it has none — in the sidebar under its
// page, among favorites, and as the page it becomes, in that page's links,
// path and window — where a database is "새 데이터베이스" (the web's
// databaseViewBlock, nativeDataSourceManagement.ts, says the same of a
// block it has read). Here a dashboard and a waiting link stand on an
// unnamed source of their own, so a database is a view block when it links
// its source, or when it has tabs — its views other than a dashboard's
// widgets — and each of them is a dashboard or waits for its source, while
// no widget of its dashboards stands on a source of its own: a database
// whose one tab became a dashboard keeps its source in its first widget,
// the view it was, and stays a database, as Notion's (seen 2026-11-15).

// databaseViewBlockSQL is true for the database container row named by
// container while it is a view block.
func databaseViewBlockSQL(container string) string {
	tabs := func(also string) string {
		return `SELECT 1 FROM {{leafen_database_container_views}} [[view_block_place]]
				JOIN {{leafen_database_views}} [[view_block_view]] ON [[view_block_view]].[[id]]=[[view_block_place]].[[view_id]]
				WHERE [[view_block_place]].[[container_id]]=` + container + `.[[id]]
					AND COALESCE(json_extract([[view_block_view]].[[config]], '$.dashboardViewId'), '')=''` + also
	}
	// The views of its own sources are those it shows that no container owns.
	ownWidget := `SELECT 1 FROM {{leafen_database_container_views}} [[view_block_widget_place]]
				JOIN {{leafen_database_views}} [[view_block_widget]] ON [[view_block_widget]].[[id]]=[[view_block_widget_place]].[[view_id]]
				WHERE [[view_block_widget_place]].[[container_id]]=` + container + `.[[id]]
					AND [[view_block_widget]].[[container_id]]=''
					AND COALESCE(json_extract([[view_block_widget]].[[config]], '$.dashboardViewId'), '')!=''`
	return `(` + container + `.[[kind]]='` + databaseContainerKindLinked + `' OR (EXISTS (` + tabs("") + `) AND NOT EXISTS (` + tabs(`
					AND [[view_block_view]].[[type]]!='dashboard'
					AND COALESCE(json_extract([[view_block_view]].[[config]], '$.linkPending'), 0)!=1`) + `) AND NOT EXISTS (` + ownWidget + `)))`
}

// viewBlockDatabasePageSQL is true for the page row [[page]] while it is a
// full-page database whose database, the one at the top of its body, is a
// view block.
var viewBlockDatabasePageSQL = `([[page]].[[kind]]='` + pageKindDatabase + `' AND EXISTS (
				SELECT 1 FROM {{leafen_database_containers}} [[page_view_block]]
				WHERE [[page_view_block]].[[page_id]]=[[page]].[[id]] AND [[page_view_block]].[[parent_block_id]]=''
					AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} [[view_block_trash]]
						WHERE [[view_block_trash]].[[container_id]]=[[page_view_block]].[[id]])
					AND ` + databaseViewBlockSQL("[[page_view_block]]") + `))`

// fullPageDatabaseContainerSQL is a full-page database page's own database
// (the source at its top), or ” for any other page: what a database mention
// of it names, as an inline one's names its block's (2026-11-10: the "@"
// menu mentioned a full-page database as a page).
var fullPageDatabaseContainerSQL = `COALESCE((SELECT [[page_container]].[[id]] FROM {{leafen_database_containers}} [[page_container]]
				WHERE [[page]].[[kind]]='` + pageKindDatabase + `' AND [[page_container]].[[page_id]]=[[page]].[[id]]
					AND [[page_container]].[[parent_block_id]]='' AND [[page_container]].[[kind]]='source'
				ORDER BY [[page_container]].[[position]], [[page_container]].[[id]] LIMIT 1), '')`

// fullPageDatabaseBlockSQL is the block of a full-page database page's own
// database (the one at the top of its body, linked or not), or ” for any
// other page: its address, as Notion's one ID — the block keeps its ID as the
// database turns inline and into a page again, where its page doesn't
// (page_links.go).
var fullPageDatabaseBlockSQL = `COALESCE((SELECT [[address_container]].[[block_id]] FROM {{leafen_database_containers}} [[address_container]]
				WHERE [[page]].[[kind]]='` + pageKindDatabase + `' AND [[address_container]].[[page_id]]=[[page]].[[id]]
					AND [[address_container]].[[parent_block_id]]=''
				ORDER BY [[address_container]].[[position]], [[address_container]].[[id]] LIMIT 1), '')`

// isViewBlockDatabasePage reports whether a page is a full-page database
// whose database is a view block.
func isViewBlockDatabasePage(app core.App, pageID string) (bool, error) {
	var viewBlock bool
	err := app.DB().NewQuery(`SELECT ` + viewBlockDatabasePageSQL + ` FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[id]] = {:id}`).
		Bind(dbx.Params{"id": pageID}).Row(&viewBlock)
	return viewBlock, err
}
