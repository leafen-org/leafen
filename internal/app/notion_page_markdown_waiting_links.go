package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notionMarkdownWaitingLink reads a page's linked database that has nothing
// left in sight from the page's workspace (database_gone_links_live.go) —
// its database in another workspace, after a copy or a move there of its
// page or of that database, or out of everyone's sight — as the screen shows
// it: a linked view still waiting for its source, with its tabs over "데이터
// 소스 연결". Its tag stands on the page for every reader, as the screen
// shows it, with none of the database it showed: no title and no source, so
// nothing of another workspace reaches a reader through it, and fetching it
// says its database is gone or elsewhere (resolveMCPDatabaseContainerSource).
// Before, the page's Markdown read the database through whoever read it:
// one who could open the other workspace saw it as a linked view like any
// other, and one who couldn't saw only the heading above it.
func notionMarkdownWaitingLink(app core.App, owner, pageID, blockID string) (notionMarkdownDatabase, bool, error) {
	if blockID == "" {
		return notionMarkdownDatabase{}, false, nil
	}
	container := struct {
		ID          string `db:"id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[workspace_id]] FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[owner]] = {:owner} AND [[container]].[[page_id]] = {:pageId}
			AND [[container]].[[block_id]] = {:blockId} AND [[container]].[[kind]] = 'linked'
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] IN ([[container]].[[id]], [[container]].[[source_container_id]])
			)
		LIMIT 1
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "blockId": blockID}).One(&container)
	if errors.Is(err, sql.ErrNoRows) {
		return notionMarkdownDatabase{}, false, nil
	}
	if err != nil {
		return notionMarkdownDatabase{}, false, err
	}
	members := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_database_container_sources}} WHERE [[container_id]] = {:id}
	`).Bind(dbx.Params{"id": container.ID}).Column(&members); err != nil || len(members) == 0 {
		return notionMarkdownDatabase{}, false, err
	}
	inSight, err := dataSourcesInSight(app, container.WorkspaceID, members)
	if err != nil {
		return notionMarkdownDatabase{}, false, err
	}
	for _, member := range members {
		if inSight[member] {
			return notionMarkdownDatabase{}, false, nil
		}
	}
	return notionMarkdownDatabase{ContainerID: container.ID, Kind: databaseContainerKindLinked}, true, nil
}
