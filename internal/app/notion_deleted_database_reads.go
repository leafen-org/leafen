package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A source database whose block left its page through a write — the
// editor, or a page Markdown replace or update — is kept by the trash of
// deleted databases (deleted_databases.go), without its block or container.
// Notion's API reads such a database as one in the trash, with nothing else
// changed (seen 2026-11-13: an inline database a page Markdown replace with
// allow_deleting_content deleted reads in_trash true as a database, a block
// and a data source, times and names as before; its pages in_trash, their
// property items and Markdown as before and their children none; a query
// of its source and its block's children not found; a change to it, its
// block or its pages refused as archived, a page made in its source made in
// the trash; its source's in_trash false changes nothing; in_trash false on
// the database or its block puts it back on its page). The Public API reads
// one from what that trash keeps: its block's place and content and its
// sources. Its times are its sources' (the trash keeps none of its block's),
// up to when it was deleted.

type notionDeletedDatabase struct {
	BlockID       string `db:"block_id"`
	Owner         string `db:"owner"`
	WorkspaceID   string `db:"workspace_id"`
	PageID        string `db:"page_id"`
	ParentBlockID string `db:"parent_block_id"`
	DataSourceIDs string `db:"data_source_ids"`
	BlockContent  string `db:"block_content"`
	DeletedBy     string `db:"deleted_by"`
	DeletedAt     int64  `db:"deleted_at"`
	Created       int64  `db:"created"`
	Updated       int64  `db:"updated"`
	Wiki          bool   `db:"wiki"`
	PageKind      string `db:"page_kind"`
}

const notionDeletedDatabaseSelectSQL = `
	SELECT [[deleted]].[[block_id]], [[deleted]].[[owner]], [[deleted]].[[workspace_id]], [[deleted]].[[page_id]],
		[[deleted]].[[parent_block_id]], [[deleted]].[[data_source_ids]], [[deleted]].[[block_content]],
		[[deleted]].[[deleted_by]], [[deleted]].[[deleted_at]],
		MIN(COALESCE((SELECT MIN([[source]].[[created]]) FROM json_each([[deleted]].[[data_source_ids]]) AS [[member]]
			JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[value]]),
			[[deleted]].[[deleted_at]]), [[deleted]].[[deleted_at]]) AS [[created]],
		MIN(COALESCE((SELECT MAX([[source]].[[updated]]) FROM json_each([[deleted]].[[data_source_ids]]) AS [[member]]
			JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[value]]),
			[[deleted]].[[deleted_at]]), [[deleted]].[[deleted_at]]) AS [[updated]],
		EXISTS (SELECT 1 FROM json_each([[deleted]].[[data_source_ids]]) AS [[member]]
			JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[value]]
			WHERE [[source]].[[wiki]] = 1) AS [[wiki]],
		COALESCE((SELECT [[holder]].[[kind]] FROM {{leafen_pages}} AS [[holder]]
			WHERE [[holder]].[[id]] = [[deleted]].[[page_id]]), '') AS [[page_kind]]
	FROM {{leafen_deleted_databases}} AS [[deleted]]`

// loadNotionDeletedDatabase reads the workspace's deleted database whose
// block, and so container, was containerID; false for none, or one whose
// time in the trash is over.
func loadNotionDeletedDatabase(app core.App, workspaceID, containerID string) (notionDeletedDatabase, bool, error) {
	return queryNotionDeletedDatabase(app, `[[deleted]].[[block_id]] = {:id}`, workspaceID, containerID)
}

// loadNotionDeletedDatabaseOfSource reads the workspace's deleted database
// that holds the data source.
func loadNotionDeletedDatabaseOfSource(app core.App, workspaceID, dataSourceID string) (notionDeletedDatabase, bool, error) {
	return queryNotionDeletedDatabase(app, `EXISTS (SELECT 1 FROM json_each([[deleted]].[[data_source_ids]]) AS [[held]]
		WHERE [[held]].[[value]] = {:id})`, workspaceID, dataSourceID)
}

func queryNotionDeletedDatabase(app core.App, condition, workspaceID, id string) (notionDeletedDatabase, bool, error) {
	if workspaceID == "" || id == "" {
		return notionDeletedDatabase{}, false, nil
	}
	found := []notionDeletedDatabase{}
	err := app.DB().NewQuery(notionDeletedDatabaseSelectSQL + `
		WHERE ` + condition + ` AND [[deleted]].[[workspace_id]] = {:workspaceId}
			AND ([[deleted]].[[expires_at]] = 0 OR [[deleted]].[[expires_at]] > {:now})
		ORDER BY [[deleted]].[[deleted_at]] DESC, [[deleted]].[[block_id]] LIMIT 1
	`).Bind(dbx.Params{"id": id, "workspaceId": workspaceID, "now": time.Now().UTC().UnixMilli()}).All(&found)
	if err != nil || len(found) == 0 {
		return notionDeletedDatabase{}, false, err
	}
	return found[0], true, nil
}

// container is the deleted database as its container stood, in the trash.
func (deleted notionDeletedDatabase) container() notionDatabaseContainer {
	return notionDatabaseContainer{
		ID: deleted.BlockID, Owner: deleted.Owner, WorkspaceID: deleted.WorkspaceID, PageID: deleted.PageID,
		BlockID: deleted.BlockID, ParentBlockID: deleted.ParentBlockID, Created: deleted.Created, Updated: deleted.Updated,
		Trashed: true, TopLevel: deleted.ParentBlockID == "", Wiki: deleted.Wiki, PageKind: deleted.PageKind,
		Deleted: true, DeletedContent: deleted.BlockContent,
	}
}

// loadNotionDeletedDatabaseContainer is loadNotionDatabaseContainerRecord
// for a deleted database; sql.ErrNoRows for none.
func loadNotionDeletedDatabaseContainer(app core.App, workspaceID, containerID string) (notionDatabaseContainer, error) {
	deleted, found, err := loadNotionDeletedDatabase(app, workspaceID, containerID)
	if err != nil {
		return notionDatabaseContainer{}, err
	}
	if !found {
		return notionDatabaseContainer{}, sql.ErrNoRows
	}
	return deleted.container(), nil
}

// notionDatabaseBlockContent is a database's block content: its block's, the
// block as the trash keeps it while it alone is there
// (storedDatabaseBlockContent), or what the trash keeps of a deleted one's;
// sql.ErrNoRows for no block.
func notionDatabaseBlockContent(app core.App, container notionDatabaseContainer) (string, error) {
	if container.Deleted {
		return container.DeletedContent, nil
	}
	content, found, err := storedDatabaseBlockContent(app, container.Owner, container.PageID, container.BlockID)
	if err == nil && !found {
		err = sql.ErrNoRows
	}
	return content, err
}

// notionDeletedDatabaseMembers lists a deleted database's sources in the
// order its block had them.
func notionDeletedDatabaseMembers(app core.App, containerID string, into any) error {
	return app.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[title]], [[source]].[[title_spans]], [[source]].[[icon]], [[source]].[[icon_type]],
			[[source]].[[description]], [[source]].[[description_spans]], [[source]].[[cover]], '' AS [[source_container_id]]
		FROM {{leafen_deleted_databases}} AS [[deleted]], json_each([[deleted]].[[data_source_ids]]) AS [[member]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[value]]
		WHERE [[deleted]].[[block_id]] = {:id}
		ORDER BY [[member]].[[key]]
	`).Bind(dbx.Params{"id": containerID}).All(into)
}

// sourceTrashState is the data source's trash state as its deleted
// database keeps it.
func (deleted notionDeletedDatabase) sourceTrashState(app core.App, dataSourceID string) (notionDataSourceTrashState, error) {
	state := notionDataSourceTrashState{}
	err := app.DB().NewQuery(`
		SELECT [[id]] AS [[data_source_id]], [[title]], [[created]], [[updated]] FROM {{leafen_data_sources}}
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"id": dataSourceID, "workspaceId": deleted.WorkspaceID}).One(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return notionDataSourceTrashState{}, errDatabaseRowPageNotFound
	}
	state.ContainerID, state.OwnerID, state.WorkspaceID, state.PageID = deleted.BlockID, deleted.Owner, deleted.WorkspaceID, deleted.PageID
	state.Deleted = true
	return state, err
}

// loadNotionDeletedDatabaseBlockRead reads a deleted database's block as a
// block in the trash, for a connection that reads its page.
func loadNotionDeletedDatabaseBlockRead(
	app core.App, identity notionOAuthBearerIdentity, blockID string,
) (notionTrashedBlockRead, bool, error) {
	deleted, found, err := loadNotionDeletedDatabase(app, identity.WorkspaceID, blockID)
	if err != nil || !found {
		return notionTrashedBlockRead{}, false, err
	}
	page, err := authorizePageBody(app, identity.PrincipalID, deleted.PageID, permissionWorkspaceRead)
	if isPermissionDenied(err) || err == nil && (page.Workspace.WorkspaceID != identity.WorkspaceID ||
		page.Workspace.OwnerID != deleted.Owner || !notionOAuthIdentityAllowsPage(app, identity, page)) {
		return notionTrashedBlockRead{}, false, nil
	}
	if err != nil {
		return notionTrashedBlockRead{}, false, err
	}
	return notionTrashedBlockRead{
		Authorization: notionBlockAuthorization{Identity: identity, Page: page},
		Tombstone: storedRecoverableBlockTrash{
			RootBlockID: deleted.BlockID, OwnerID: deleted.Owner, WorkspaceID: deleted.WorkspaceID,
			PageID: deleted.PageID, ParentID: deleted.ParentBlockID, DeletedBy: deleted.DeletedBy, DeletedAt: deleted.DeletedAt,
		},
		Snapshot: recoverableBlockTrashSnapshot{Format: 1, RootID: deleted.BlockID, Blocks: []recoverableBlockTrashBlock{{
			ID: deleted.BlockID, ParentID: deleted.ParentBlockID, Type: graphBlockTypeInlineDatabase,
			Content: json.RawMessage(deleted.BlockContent), CreatedBy: page.Page.GetString("created_by"), Created: deleted.Created,
		}}},
		Deleted: &deleted,
	}, true, nil
}

// notionDeletedDatabaseLocked is a deleted inline database's own lock, as
// its block kept it.
func notionDeletedDatabaseLocked(content string) bool {
	fields := struct {
		Locked bool `json:"locked"`
	}{}
	return json.Unmarshal([]byte(content), &fields) == nil && fields.Locked
}

// restoreNotionDeletedDatabase puts a deleted database's block back on its
// page for a connection, as the trash's restore does (deleted_databases.go).
// It answers the block's ID.
func restoreNotionDeletedDatabase(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity, deleted notionDeletedDatabase,
) (string, error) {
	stored := storedDeletedDatabase{
		BlockID: deleted.BlockID, Owner: deleted.Owner, WorkspaceID: deleted.WorkspaceID, PageID: deleted.PageID,
		DataSourceIDs: deleted.DataSourceIDs, BlockContent: deleted.BlockContent,
	}
	return restoreDeletedDatabaseBlock(event.Request.Context(), event.App, ygo, identity.PrincipalID, stored,
		authorizeNotionPageMutation(identity, deleted.PageID))
}

// restoreNotionDeletedDatabaseBlock answers in_trash false on a deleted
// database's block: the block back on its page, read as it stands, as
// Notion's (seen 2026-11-13).
func restoreNotionDeletedDatabaseBlock(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity, read notionTrashedBlockRead,
) error {
	// Not out of a page in the trash (notion_trash_refusal.go).
	if refused, err := refuseNotionTrashedBlockBodyEdit(event, identity, read.Tombstone.PageID); refused {
		return err
	}
	if _, err := authorizePageBody(event.App, identity.PrincipalID, read.Tombstone.PageID, permissionWorkspaceWrite); isPermissionDenied(err) {
		return notionBlockNotFound(event)
	} else if err != nil {
		return notionBlockProjectionError(event, err)
	}
	blockID, err := restoreNotionDeletedDatabase(event, ygo, identity, *read.Deleted)
	if err != nil {
		return notionBlockMutationError(event, err)
	}
	block, found, err := loadStoredNotionBlock(event.App, blockID)
	if err != nil || !found {
		return notionBlockProjectionError(event, errors.Join(errDatabaseRowPageNotFound, err))
	}
	page, err := authorizePageBody(event.App, identity.PrincipalID, block.PageID, permissionWorkspaceRead)
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	authorization := notionBlockAuthorization{Identity: identity, Page: page, Block: block}
	response, err := loadNotionBlockReadSnapshot(event.App, authorization, newNotionBlockFileCapabilityIssuer(event.Request, identity))
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	response["in_trash"] = false
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// keepDeletedDatabasePublicID keeps the public ID of a database whose block
// just left its page, which its container's going took with it, while the
// trash holds it.
func keepDeletedDatabasePublicID(app core.App, blockID string) error {
	var workspaceID string
	err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_deleted_databases}} WHERE [[block_id]] = {:id}`).
		Bind(dbx.Params{"id": blockID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = ensureNotionObjectIdentity(app, notionObjectIdentityDatabase, workspaceID, blockID)
	return err
}

// dropDeletedDatabasePublicID lets the public ID of a database deleted
// forever go, unless a database stands under it again.
func dropDeletedDatabasePublicID(app core.App, workspaceID, blockID string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_object_identities}}
		WHERE [[object_kind]] = 'database' AND [[workspace_id]] = {:workspaceId} AND [[internal_id]] = {:id}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_containers}} WHERE [[id]] = {:id})
	`).Bind(dbx.Params{"workspaceId": workspaceID, "id": blockID}).Execute()
	return err
}

// databaseSourceDeletedSQL holds for a data source the trash of deleted
// databases holds, which stays small: its databases leave it within 30 days.
func databaseSourceDeletedSQL(dataSourceExpression string) string {
	return `EXISTS (
		SELECT 1 FROM {{leafen_deleted_databases}} AS [[deleted_source_holder]],
			json_each([[deleted_source_holder]].[[data_source_ids]]) AS [[deleted_source_member]]
		WHERE [[deleted_source_member]].[[value]] = ` + dataSourceExpression + `
	)`
}
