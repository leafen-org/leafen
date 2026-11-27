package app

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Notion's API reads what is in the trash and what is under it (seen
// 2026-11-11, Notion-Version 2026-03-11). A page, database page, data source
// or block found there reads as before with in_trash true: one in the trash
// itself, and one under it — under a page, a block or a database in the
// trash. What is in the trash itself lists no children and gives no
// property item (not found); what is under it lists no children (an empty
// list) and gives its property items. A data source in the trash or under
// it is not found for a query. A database under a page in the trash reads
// in_trash false, though its block reads true. Changes are refused by
// notion_trash_refusal.go, which asks notionPageTrashPlace too.

// notionTrashPlace is where something a connection reads stands toward the
// trash.
type notionTrashPlace uint8

const (
	notionOutOfTrash notionTrashPlace = iota
	notionInTrashItself
	notionUnderTrash
)

func (place notionTrashPlace) inTrash() bool { return place != notionOutOfTrash }

// notionPageTrashPlace is where a page stands: an ordinary page, a full-page
// database's page or a database page. A database page is in the trash
// itself only by its own trash; while its database or the page holding it
// is in the trash, it is under it.
func notionPageTrashPlace(app core.App, pageID string) (notionTrashPlace, error) {
	effect, trashed, err := loadPageTrashEffect(app, pageID)
	if err != nil || !trashed {
		return notionOutOfTrash, err
	}
	if effect.RootID != pageID {
		return notionUnderTrash, nil
	}
	page, err := findRecordRemembered(app, pagesCollection, pageID)
	if err != nil || !isDatabaseRowPageRecord(page) {
		return notionInTrashItself, err
	}
	own := false
	err = app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} WHERE [[row_id]]={:rowId})
	`).Bind(dbx.Params{"rowId": pageID}).Row(&own)
	if err != nil || own {
		return notionInTrashItself, err
	}
	return notionUnderTrash, nil
}

// notionDataSourceTrashPlace is where a data source stands: in the trash
// itself with its database, or under a page in it.
func notionDataSourceTrashPlace(app core.App, containerPageID string, sourceTrashed bool) (notionTrashPlace, error) {
	if sourceTrashed {
		return notionInTrashItself, nil
	}
	return notionPageTrashPlace(app, containerPageID)
}

// notionNoChildren answers a list of children for what is under the trash:
// Notion lists none.
func notionNoChildren(event *core.RequestEvent) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "results": []any{}, "next_cursor": nil,
		"has_more": false, "type": "block", "block": map[string]any{},
	})
}

// notionTrashedBlockRead is a block in the recoverable block trash that a
// connection reads: the block trashed itself (its tombstone's root) or one
// that went to the trash inside it.
type notionTrashedBlockRead struct {
	Authorization notionBlockAuthorization
	Tombstone     storedRecoverableBlockTrash
	Snapshot      recoverableBlockTrashSnapshot
	Index         int
	// Deleted is a deleted database's block, which the trash of deleted
	// databases keeps instead of a tombstone (notion_deleted_database_reads.go).
	Deleted *notionDeletedDatabase
}

func (read notionTrashedBlockRead) itself() bool { return read.Index == 0 }

// loadNotionTrashedBlockRead finds blockID in the workspace's block trash,
// or a deleted database's block, and whether the connection reads its page;
// a block found nowhere, or on a page out of the connection's reach, isn't
// found.
func loadNotionTrashedBlockRead(
	app core.App, identity notionOAuthBearerIdentity, blockID string,
) (notionTrashedBlockRead, bool, error) {
	now := time.Now().UTC().UnixMilli()
	tombstones := []storedRecoverableBlockTrash{}
	// Its own tombstone, or one it went to the trash inside, found by the
	// blocks each tombstone holds (schema_baseline.json):
	// the tombstones holding the block are read, not every snapshot of the
	// workspace (2026-11-18).
	err := app.DB().NewQuery(`
		SELECT [[tombstone]].[[root_block_id]],[[tombstone]].[[owner_id]],[[tombstone]].[[workspace_id]],
			[[tombstone]].[[page_id]],[[tombstone]].[[parent_id]],[[tombstone]].[[predecessor_id]],
			[[tombstone]].[[successor_id]],[[tombstone]].[[snapshot_json]],[[tombstone]].[[block_count]],
			[[tombstone]].[[byte_size]],[[tombstone]].[[deleted_by]],[[tombstone]].[[deleted_at]],
			[[tombstone]].[[expires_at]]
		FROM {{leafen_block_trash_tombstone_blocks}} AS [[held]]
		CROSS JOIN {{leafen_block_trash_tombstones}} AS [[tombstone]]
			ON [[tombstone]].[[root_block_id]]=[[held]].[[root_block_id]]
		WHERE [[held]].[[block_id]]={:id} AND [[tombstone]].[[workspace_id]]={:workspaceId}
			AND [[tombstone]].[[expires_at]]>{:now}
		ORDER BY [[tombstone]].[[root_block_id]]!={:id}
		LIMIT 1
	`).Bind(dbx.Params{
		"id": blockID, "workspaceId": identity.WorkspaceID, "now": now,
	}).All(&tombstones)
	if err != nil {
		return notionTrashedBlockRead{}, false, err
	}
	if len(tombstones) == 0 {
		return loadNotionDeletedDatabaseBlockRead(app, identity, blockID)
	}
	tombstone := tombstones[0]
	snapshot, err := parseRecoverableBlockTrashSnapshot(tombstone)
	if err != nil {
		return notionTrashedBlockRead{}, false, err
	}
	index := -1
	for position, block := range snapshot.Blocks {
		if block.ID == blockID {
			index = position
			break
		}
	}
	if index < 0 {
		return notionTrashedBlockRead{}, false, nil
	}
	page, err := authorizePageBody(app, identity.PrincipalID, tombstone.PageID, permissionWorkspaceRead)
	if isPermissionDenied(err) || err == nil && (page.Workspace.WorkspaceID != identity.WorkspaceID ||
		page.Workspace.OwnerID != tombstone.OwnerID || !notionOAuthIdentityAllowsPage(app, identity, page)) {
		return notionTrashedBlockRead{}, false, nil
	}
	if err != nil {
		return notionTrashedBlockRead{}, false, err
	}
	return notionTrashedBlockRead{
		Authorization: notionBlockAuthorization{Identity: identity, Page: page},
		Tombstone:     tombstone, Snapshot: snapshot, Index: index,
	}, true, nil
}

// notionTrashedBlockObject is a block in the trash as Notion reads it: as
// it was, in_trash, last edited when it went to the trash.
func notionTrashedBlockObject(
	app core.App, read notionTrashedBlockRead, fileIssuer *notionBlockFileCapabilityIssuer,
) (map[string]any, error) {
	entry := read.Snapshot.Blocks[read.Index]
	block := storedNotionBlock{
		ID: entry.ID, Owner: read.Tombstone.OwnerID, WorkspaceID: read.Tombstone.WorkspaceID,
		PageID: read.Tombstone.PageID, ParentID: entry.ParentID, Position: entry.Position,
		Type: entry.Type, Content: string(entry.Content),
		CreatedBy: entry.CreatedBy, Created: entry.Created,
		LastEditedBy: read.Tombstone.DeletedBy, Updated: read.Tombstone.DeletedAt,
	}
	if block.Created == 0 {
		// A tombstone from before blocks kept when they were made.
		block.CreatedBy, block.Created = read.Tombstone.DeletedBy, read.Tombstone.DeletedAt
	}
	// A deleted database's block reads as it was last edited, as Notion's
	// (seen 2026-11-13: its times unchanged by the write that deleted it).
	if read.Deleted != nil {
		block.Updated = read.Deleted.Updated
	}
	document := crdt.New()
	if len(entry.Text) > 0 {
		document.Transact(func(transaction *crdt.Transaction) {
			transaction.GetText(ygoBlockTextRootPrefix+entry.ID).ApplyDelta(transaction, entry.Text)
		})
	}
	hasChildren := false
	for _, other := range read.Snapshot.Blocks {
		hasChildren = hasChildren || other.ParentID == entry.ID
	}
	authorization := read.Authorization
	authorization.Block = block
	response, err := notionBlockObject(app, authorization, block, hasChildren, document, fileIssuer)
	if err != nil {
		return nil, err
	}
	// A database's block in the trash is its database, named by its ID.
	if block.Type == graphBlockTypeInlineDatabase && response["type"] != "child_database" {
		title, found, err := notionInlineDatabaseTitleWith(app, authorization, block, true)
		if err == nil && !found && read.Deleted != nil {
			title, found, err = databaseContainerTitle(app, block.ID)
		}
		if err != nil {
			return nil, err
		}
		if found {
			if kind, ok := response["type"].(string); ok {
				delete(response, kind)
			}
			response["type"], response["child_database"] = "child_database", map[string]any{"title": title}
			if response["id"], err = ensureNotionObjectIdentity(
				app, notionObjectIdentityDatabase, authorization.Identity.WorkspaceID, block.ID,
			); err != nil {
				return nil, err
			}
		}
	}
	response["in_trash"] = true
	return response, nil
}

// serveNotionTrashedBlockRetrieve answers a read of a block in the trash.
func serveNotionTrashedBlockRetrieve(event *core.RequestEvent, read notionTrashedBlockRead) error {
	response, err := notionTrashedBlockObject(
		event.App, read, newNotionBlockFileCapabilityIssuer(event.Request, read.Authorization.Identity),
	)
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	recordAPIReads(event.App, read.Authorization.Identity, []contentRead{contentReadOfPage(read.Authorization.Page.Page)})
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// serveNotionTrashedBlockChildren answers a list of a trashed block's
// children: the block trashed itself isn't found, one inside it lists none.
func serveNotionTrashedBlockChildren(event *core.RequestEvent, read notionTrashedBlockRead) error {
	if read.itself() {
		return notionBlockNotFound(event)
	}
	return notionNoChildren(event)
}

// notionTrashedBlockMessage is why a change to a block in the trash is
// refused: the block trashed itself takes only being taken out, one inside
// it nothing.
func notionTrashedBlockMessage(read notionTrashedBlockRead) string {
	if read.itself() {
		return notionArchivedCommentMessage
	}
	return notionArchivedAncestorMessage
}

// authorizeDatabaseRowPageAnyTrash is authorizeDatabaseRowPageInTrash for a
// row whose data source may be in the trash too.
func authorizeDatabaseRowPageAnyTrash(
	app core.App, principalID, rowID string, requirement permissionRequirement,
) (databaseRowPageAuthority, error) {
	authority, err := readDatabaseRowPageAuthorityWhere(app, rowID, "TRUE")
	if err != nil {
		return databaseRowPageAuthority{}, err
	}
	authorized, err := authorizeDatabaseRowPageAuthority(app, principalID, authority, requirement)
	if !errors.Is(err, errDatabaseRowPageNotFound) {
		return authorized, err
	}
	row, rowErr := findRecordRemembered(app, pagesCollection, authority.RowID)
	if rowErr != nil {
		return databaseRowPageAuthority{}, err
	}
	if _, found, sourceErr := authorizeRowPageInTrashedSource(app, principalID, row, requirement); sourceErr != nil && !isPermissionDenied(sourceErr) {
		return databaseRowPageAuthority{}, sourceErr
	} else if !found || sourceErr != nil {
		return databaseRowPageAuthority{}, err
	}
	return authority, nil
}

// loadNotionDataSourceParentAuthorityAnyTrash is a data source's database,
// in the trash or not: one trashed out of a database with others is named
// by the database it came from, as its own read names it.
func loadNotionDataSourceParentAuthorityAnyTrash(
	app core.App, workspaceID, dataSourceID string,
) (notionDataSourceParentAuthority, error) {
	state, err := loadNotionDataSourceTrashState(app, workspaceID, dataSourceID)
	if err != nil {
		return notionDataSourceParentAuthority{}, err
	}
	origin, err := notionDataSourceTrashOrigin(app, state)
	if err != nil {
		return notionDataSourceParentAuthority{}, err
	}
	container := notionDataSourceParentAuthority{
		ContainerID: state.ContainerID, OwnerID: state.OwnerID, WorkspaceID: state.WorkspaceID,
		PageID: state.PageID, DataSource: state.DataSource, Title: state.Title,
		CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt,
	}
	if origin != "" {
		container.ContainerID = origin
	}
	return container, nil
}

// authorizeRowPageInTrashedSource authorizes a database page whose data
// source is in the trash with its database, a deleted one too, through the
// page holding the database: the app reaches no such page, its database gone from the page,
// while the Public API reads it. found is false for a page of a data source
// out of the trash.
func authorizeRowPageInTrashedSource(
	app core.App, principalID string, row *core.Record, requirement permissionRequirement,
) (notionTrashedSourceRow, bool, error) {
	if !isDatabaseRowPageRecord(row) {
		return notionTrashedSourceRow{}, false, nil
	}
	holder := struct {
		PageID  string `db:"page_id"`
		Trashed bool   `db:"trashed"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]], NOT ` + databaseSourceNotTrashedSQL("{:sourceId}") + ` OR ` +
		databaseSourceDeletedSQL("{:sourceId}") + ` AS [[trashed]]
		FROM {{leafen_data_sources}}
		WHERE [[id]]={:sourceId} AND [[owner]]={:owner} AND [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{
		"sourceId": row.GetString("data_source_id"), "owner": row.GetString("owner"),
		"workspaceId": row.GetString("workspace"),
	}).One(&holder)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !holder.Trashed {
		return notionTrashedSourceRow{}, false, nil
	}
	if err != nil {
		return notionTrashedSourceRow{}, false, err
	}
	page, err := authorizePageBody(app, principalID, holder.PageID, requirement)
	if err != nil {
		return notionTrashedSourceRow{}, true, err
	}
	if page.Workspace.OwnerID != row.GetString("owner") || page.Workspace.WorkspaceID != row.GetString("workspace") {
		return notionTrashedSourceRow{}, true, permissionDenied()
	}
	return notionTrashedSourceRow{
		Row:    pageAuthorization{Workspace: page.Workspace, Page: row, AccessRole: page.AccessRole},
		Holder: page,
	}, true, nil
}

// notionTrashedSourceRow is a database page of a data source in the trash
// and the page holding its database, which a connection reaches it through.
type notionTrashedSourceRow struct {
	Row    pageAuthorization
	Holder pageAuthorization
}

// authorizeNotionRowInTrashedSource is authorizeRowPageInTrashedSource for
// a connection, which must reach the page holding the database.
func authorizeNotionRowInTrashedSource(
	app core.App, identity notionOAuthBearerIdentity, rowID string, requirement permissionRequirement,
) (pageAuthorization, bool, error) {
	row, err := findRecordRemembered(app, pagesCollection, rowID)
	if errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, false, nil
	}
	if err != nil {
		return pageAuthorization{}, false, err
	}
	trashed, found, err := authorizeRowPageInTrashedSource(app, identity.PrincipalID, row, requirement)
	if !found || isPermissionDenied(err) {
		return pageAuthorization{}, false, nil
	}
	if err != nil {
		return pageAuthorization{}, false, err
	}
	if trashed.Holder.Workspace.WorkspaceID != identity.WorkspaceID ||
		!notionOAuthIdentityAllowsPage(app, identity, trashed.Holder) {
		return pageAuthorization{}, false, nil
	}
	return trashed.Row, true, nil
}

// notionTrashedBlockCommentTarget is a block in the trash as what its
// comments are listed on: a page's block, or a database page's.
func notionTrashedBlockCommentTarget(
	app core.App, identity notionOAuthBearerIdentity, blockID string,
) (notionCommentTarget, bool, error) {
	read, found, err := loadNotionTrashedBlockRead(app, identity, blockID)
	if err != nil || !found {
		return notionCommentTarget{}, false, err
	}
	page := read.Authorization.Page
	target := notionCommentTarget{
		Identity: identity, Page: page, PageID: page.Page.Id, StoragePageID: page.Page.Id,
		TargetKind: commentTargetPage, BlockID: blockID,
	}
	if isDatabaseRowPageRecord(page.Page) {
		authority, err := readDatabaseRowPageAuthorityWhere(app, page.Page.Id, "TRUE")
		if err != nil {
			return notionCommentTarget{}, false, err
		}
		target.StoragePageID, target.TargetKind = authority.ContainerPageID, commentTargetDatabaseRow
	}
	return target, true, nil
}

// notionDataSourceInTrash reports whether a data source is in the trash with
// its database, or under a page in it.
func notionDataSourceInTrash(app core.App, container notionDataSourceParentAuthority) (bool, error) {
	state, err := loadNotionDataSourceTrashState(app, container.WorkspaceID, container.DataSource)
	if err != nil {
		return false, err
	}
	place, err := notionDataSourceTrashPlace(app, state.PageID, state.trashed())
	return place.inTrash(), err
}

// notionTrashedSourceRowCommentTarget is a database page whose database is
// in the trash, as what its comments are listed on.
func notionTrashedSourceRowCommentTarget(
	app core.App, identity notionOAuthBearerIdentity, rowID string,
) (notionCommentTarget, bool, error) {
	page, found, err := authorizeNotionRowInTrashedSource(app, identity, rowID, permissionWorkspaceRead)
	if err != nil || !found {
		return notionCommentTarget{}, false, err
	}
	authority, err := readDatabaseRowPageAuthorityWhere(app, rowID, "TRUE")
	if err != nil {
		return notionCommentTarget{}, false, err
	}
	return notionCommentTarget{
		Identity: identity, Page: page, PageID: rowID,
		StoragePageID: authority.ContainerPageID, TargetKind: commentTargetDatabaseRow,
	}, true, nil
}

// notionMentionTargetInTrash is whether a page or database mention's target
// is in the trash as Notion's "v2" Markdown and its MCP tell it, naming it by
// its address alone, with no type or words (seen 2026-11-13: a page's and a
// database's, written before it went there or after), its API and "v1" by
// kind and name as any. A page is so in the trash itself alone: one under a
// page in the trash, a database page whose database or whose database's page
// is there among them, reads by its name. A database is so in the trash
// itself or under a page there, through the one rule of what the trash holds
// (pageInTrashPath) — seen 2026-11-25 twice through Notion's API: a mention
// of an inline database whose page went to the trash read by its address
// alone, of a row of it by its name.
func notionMentionTargetInTrash(app core.App, mention, targetID string) (bool, error) {
	switch mention {
	case "page":
		place, err := notionPageTrashPlace(app, targetID)
		return place == notionInTrashItself, err
	case "database":
		database, found, err := loadNotionMentionedDatabase(app, targetID)
		if err != nil || !found || database.Trashed {
			return found && database.Trashed, err
		}
		return pageInTrashPath(app, database.PageID)
	}
	return false, nil
}
