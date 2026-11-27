package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// workspaceActivityDatabaseOwnBlock says whether a block is a full-page
// database's own, the one its page holds the database in: no edit of its page
// in Notion's updates, where a full-page database is its page — one made
// lists its database's lines alone ("〈DB〉 컬렉션을 생성했습니다", its view's
// and its first property's, and at the top its page's made line; seen
// 2026-11-17 05:0x in the free test workspace's '모든 워크스페이스 업데이트',
// first account, at the top and under a page, in two tabs). The block holds
// nothing of its page's own: what changes in it is its database's, whose
// lines say it.
func workspaceActivityDatabaseOwnBlock(page *core.Record, kind string) bool {
	return kind == graphBlockTypeInlineDatabase && page.GetString("kind") == pageKindDatabase
}

// retractWorkspaceActivityDatabaseStart takes back the person's edit of a
// page that has just become a full-page database: a page becomes one in two
// changes, the first giving it its database while it is still a page — its
// edit then, the database's block and the empty lines it took — and the
// second its kind (fullPageDatabase.ts). Notion lists that start as its
// database's lines alone. The edit, of this window or the one before, goes
// from the feed; the organization's audit log keeps it.
func retractWorkspaceActivityDatabaseStart(app core.App, page *core.Record, actorID string, at int64) error {
	ids := []any{}
	for _, when := range []int64{at, at - workspaceActivityWindow.Milliseconds()} {
		entry, err := workspaceActivityEntryOf(app, page, actorID, "page", page.Id, "edit", workspaceActivityMetadata{}, when, 0, 1)
		if err != nil || entry == nil {
			return err
		}
		ids = append(ids, workspaceActivityGroupID(*entry))
	}
	_, err := app.DB().Delete("leafen_workspace_activity", dbx.In("id", ids...)).Execute()
	return err
}

// workspaceActivityDatabaseBlock is the block an inline database's own line,
// a view's and a property's link to, as Notion's workspace updates do (seen
// 2026-11-16: the database's name opens /p/〈its block〉, a view's /p/〈its
// block〉?v=〈view〉, never the page holding it): the container on the page
// the line is read from that holds the view, or the source, its own
// database before a linked one. A full-page database is its page, so none.
func workspaceActivityDatabaseBlock(app core.App, entityType, entityID, sourceID string, holder *core.Record) (string, error) {
	if holder == nil || holder.GetString("kind") == pageKindDatabase {
		return "", nil
	}
	var blockID string
	var err error
	switch {
	case entityType == "view":
		err = app.DB().NewQuery(`
			SELECT [[container]].[[block_id]] FROM {{leafen_database_container_views}} [[place]]
			JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]]=[[place]].[[container_id]]
			WHERE [[place]].[[view_id]]={:id} AND [[container]].[[page_id]]={:page}
			LIMIT 1
		`).Bind(dbx.Params{"id": entityID, "page": holder.Id}).Row(&blockID)
	case sourceID != "":
		err = app.DB().NewQuery(`
			SELECT [[container]].[[block_id]] FROM {{leafen_database_container_sources}} [[place]]
			JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]]=[[place]].[[container_id]]
			WHERE [[place]].[[data_source_id]]={:source} AND [[place]].[[page_id]]={:page}
			ORDER BY [[container]].[[kind]]='linked', [[container]].[[position]], [[container]].[[id]]
			LIMIT 1
		`).Bind(dbx.Params{"source": sourceID, "page": holder.Id}).Row(&blockID)
	default:
		return "", nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return blockID, err
}
