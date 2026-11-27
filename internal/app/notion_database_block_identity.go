package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion database has one ID, which its block answers to as well: a
// child_database block is named by its database's ID, and the block routes
// and the database routes each take either, as in Notion. A database is a
// native database container: an inline database's container is its block,
// and a full-page database's or a wiki's block in its parent's body is its
// page.

// notionDatabaseParent is a database's parent: the page or block its block
// sits in, or, for a full-page database or a wiki, its page's parent.
func notionDatabaseParent(app core.App, container notionDatabaseContainer, page *core.Record) (map[string]any, error) {
	if container.fullPage() {
		if strings.TrimSpace(page.GetString("parent_id")) == "" {
			return map[string]any{"type": "workspace", "workspace": true}, nil
		}
		// A full-page database whose page block sits in another block has
		// that block as parent.
		return notionChildPageParent(app, page)
	}
	if container.ParentBlockID != "" {
		blockID, err := notionBlockPublicID(app, container.WorkspaceID, container.ParentBlockID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "block_id", "block_id": blockID}, nil
	}
	return notionParentInPage(app, container.PageID)
}

// notionBlockIDForDatabase turns a database's ID, asked for as a block, into
// the ID the block routes read: the inline database's block, or the page of
// a full-page database or a wiki. Any other ID stays as it is.
func notionBlockIDForDatabase(app core.App, requested string) string {
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, requested)
	if err != nil {
		return requested
	}
	container, err := loadNotionDatabaseContainerRecord(app, identity.Workspace, identity.InternalID)
	if err != nil {
		return requested
	}
	if container.fullPage() {
		pageID, err := notionPagePublicID(app, container.PageID)
		if err != nil {
			return requested
		}
		return pageID
	}
	blockID, err := notionBlockPublicID(app, container.WorkspaceID, container.BlockID)
	if err != nil {
		return requested
	}
	return blockID
}

// notionDatabaseIDForBlock turns the ID of a database's block, of a
// full-page database's page, or of the page a full-page database turned
// inline left as a signpost, asked for as a database, into the database's
// ID. Any other ID stays as it is.
func notionDatabaseIDForBlock(app core.App, requested string) string {
	if _, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, requested); err == nil {
		return requested
	}
	internalID := ""
	if pageID, err := resolveNotionPageRecordID(app, requested); err == nil {
		internalID = pageID
	} else if blockID, resolved := resolveNotionBlockID(app, requested); resolved {
		internalID = blockID
	}
	if internalID == "" {
		return requested
	}
	container := struct {
		ID          string `db:"id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	// A block names the database whose container it is; a page names the
	// database it is the page of, or leads to as a signpost.
	if err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[workspace_id]] FROM {{leafen_database_containers}} AS [[container]]
		WHERE [[container]].[[id]] = {:id} AND [[container]].[[block_id]] = {:id}
		UNION ALL
		SELECT [[container]].[[id]], [[container]].[[workspace_id]] FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]] = [[container]].[[block_id]]
			AND [[block]].[[page_id]] = [[container]].[[page_id]] AND [[block]].[[parent_id]] = ''
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[container]].[[page_id]]
		WHERE [[container]].[[page_id]] = {:id} AND [[page]].[[kind]] = {:databaseKind}
			AND [[container]].[[parent_block_id]] = ''
		UNION ALL
		SELECT [[container]].[[id]], [[container]].[[workspace_id]] FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_database_page_signposts}} AS [[marker]] ON [[marker]].[[page_id]] = [[page]].[[id]]
		JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[marker]].[[container_id]]
		WHERE [[page]].[[id]] = {:id} AND ` + databasePageSignpostSQL + `
		LIMIT 1
	`).Bind(dbx.Params{"id": internalID, "databaseKind": pageKindDatabase}).One(&container); err != nil {
		return requested
	}
	databaseID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, container.WorkspaceID, container.ID)
	if err != nil {
		return requested
	}
	return databaseID
}
