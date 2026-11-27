package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion wiki is a database (database_type "wiki") whose one ID is its
// page's, the ID in its address (seen 2026-11-19, Notion-Version 2025-09-03:
// GET /v1/databases/<wiki> 200 with that ID, GET /v1/pages/<wiki> 400 "is a
// database, not a page", PATCH /v1/pages/<wiki> 404, GET /v1/blocks/<wiki> a
// child_database block). Here a wiki's database is the database on its page
// whose data source is the wiki's (wiki_database.go), and it answers the API
// with its page's public ID, the one in the wiki's address; that ID asked
// for as a database names it (databaseOwnPublicID, notionDatabaseIdentityByOwnID).
// So every road that reads a public ID as a database — the database routes,
// a page route asked for one (serveNotionPageAskedForDatabase),
// parent.database_id, the block routes (notionBlockIDForDatabase) — reads a
// wiki's ID as its database, as Notion's does.

// wikiDatabaseSQL joins a wiki's own data source, the container on its page
// that holds it, and that page, as loadWikiPlace finds a wiki.
const wikiDatabaseSQL = `
	FROM {{leafen_database_containers}} AS [[container]]
	JOIN {{leafen_database_container_sources}} AS [[member]] ON [[member]].[[container_id]] = [[container]].[[id]]
		AND COALESCE([[member]].[[source_container_id]], '') = ''
	JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[data_source_id]]
		AND [[source]].[[wiki]] = 1 AND [[source]].[[page_id]] = [[container]].[[page_id]]`

// wikiDatabaseOfPage is the container of the database a page is the wiki
// of, or "" for a page that is no wiki.
func wikiDatabaseOfPage(app core.App, pageID string) (string, error) {
	containerID := ""
	if pageID == "" {
		return "", nil
	}
	err := app.DB().NewQuery(`SELECT [[container]].[[id]] ` + wikiDatabaseSQL + `
		WHERE [[container]].[[page_id]] = {:page} ORDER BY [[container]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"page": pageID}).Row(&containerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return containerID, err
}

// wikiPageOfDatabase is the wiki page whose database a container is, or ""
// for any other database.
func wikiPageOfDatabase(app core.App, containerID string) (string, error) {
	pageID := ""
	if containerID == "" {
		return "", nil
	}
	err := app.DB().NewQuery(`SELECT [[container]].[[page_id]] ` + wikiDatabaseSQL + `
		WHERE [[container]].[[id]] = {:id} LIMIT 1
	`).Bind(dbx.Params{"id": containerID}).Row(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return pageID, err
}

// notionWikiDatabaseIdentity finds the wiki database a page's public ID
// names, as Notion's wiki answers to its page's ID as a database. found is
// false for an ID naming no wiki's page.
func notionWikiDatabaseIdentity(app core.App, publicID string) (notionObjectIdentity, bool, error) {
	pageID, err := resolveNotionPageRecordID(app, publicID)
	if err != nil {
		return notionObjectIdentity{}, false, nil
	}
	containerID, err := wikiDatabaseOfPage(app, pageID)
	if err != nil || containerID == "" {
		return notionObjectIdentity{}, false, err
	}
	workspaceID := ""
	if err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_database_containers}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": containerID}).Row(&workspaceID); err != nil {
		return notionObjectIdentity{}, false, err
	}
	return notionObjectIdentity{
		PublicID: publicID, ObjectKind: notionObjectIdentityDatabase, Workspace: workspaceID, InternalID: containerID,
	}, true, nil
}

// notionPageDatabase is the database a page is, as its block reads.
type notionPageDatabase struct {
	ContainerID, Title string
	HasChildren        bool
}

// notionPageDatabaseBlock is the database a page's block reads as, a
// child_database block: a full-page database's, with no children, or a
// wiki's, whose block has its pages and its Home's blocks as children, as
// Notion's (seen 2026-11-19: has_children false on a full-page database's
// block, true on a wiki's). found is false for any other page.
func notionPageDatabaseBlock(app core.App, page *core.Record, hasChildren bool) (notionPageDatabase, bool, error) {
	if isFullPageDatabase(page) {
		database, found, err := loadFullPageDatabase(app, page)
		return notionPageDatabase{ContainerID: database.ContainerID, Title: database.Title}, found, err
	}
	containerID, err := wikiDatabaseOfPage(app, page.Id)
	return notionPageDatabase{ContainerID: containerID, Title: page.GetString("title"), HasChildren: hasChildren},
		containerID != "", err
}
