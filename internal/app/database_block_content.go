package app

import (
	"database/sql"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database held in a page's body keeps what is its own apart from its
// sources — its own name, the sources sent to the trash with their views —
// in its block. While that block alone is in the trash, its page out of it,
// the block is off its page and the trash keeps it (loadGraphTrashedDatabase),
// and the database goes on being named by it there, as Notion's does (seen
// 2026-11-24 in Chrome at 1440: an inline database of two sources with a name
// of its own, sent there alone, named by that name in a page's path, as
// before; unnamed, by its sources' names). So does what else its block keeps
// of its own: Notion's API reads such a database by its name, icon and lock
// as before (seen 2026-11-25 on one of two sources with all three, under
// a Notion test page: GET /v1/databases after in_trash true, its child_database
// block titled the same), and every reader of its block's content reads it
// here — the API's database object and MCP's fetch with its row pages'
// ancestors.

// databaseBlockContentSQL is the content of the block of the database
// container aliased, given the alias of its block left-joined on its page:
// that block's, else the block as the trash keeps it while it alone is
// there; NULL for neither.
func databaseBlockContentSQL(container, block string) string {
	return `COALESCE(` + block + `.[[content]], (
		SELECT json_extract([[kept_block]].[[value]], '$.content')
		FROM {{leafen_database_container_trash}} [[kept_trash]]
		JOIN {{leafen_block_trash_tombstones}} [[kept_tombstone]]
			ON [[kept_tombstone]].[[root_block_id]] = [[kept_trash]].[[root_block_id]]
			AND [[kept_tombstone]].[[page_id]] = ` + container + `.[[page_id]]
		CROSS JOIN json_each([[kept_tombstone]].[[snapshot_json]], '$.blocks') [[kept_block]]
		WHERE [[kept_trash]].[[container_id]] = ` + container + `.[[id]]
			AND json_extract([[kept_block]].[[value]], '$.id') = ` + container + `.[[id]]
		ORDER BY [[kept_tombstone]].[[deleted_at]] DESC LIMIT 1))`
}

// storedDatabaseBlockContent is the content of a database's block, by its
// ID (its container's) on its page, as databaseBlockContentSQL reads it;
// false for neither.
func storedDatabaseBlockContent(app core.App, ownerID, pageID, blockID string) (string, bool, error) {
	var content sql.NullString
	err := app.DB().NewQuery(`
		SELECT ` + databaseBlockContentSQL("[[container]]", "[[block]]") + `
		FROM (SELECT {:id} AS [[id]], {:page} AS [[page_id]]) AS [[container]]
		LEFT JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[container]].[[id]]
			AND [[block]].[[page_id]] = [[container]].[[page_id]] AND [[block]].[[owner]] = {:owner}
	`).Bind(dbx.Params{"owner": ownerID, "page": pageID, "id": blockID}).Row(&content)
	if err != nil {
		return "", false, err
	}
	return content.String, content.Valid, nil
}
