package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A block on its page is not in the trash. A page's version brought back —
// restored whole or its changes reverted — puts a block the public API sent
// to the trash back under its own ID without going through the trash; its
// trash entry stayed, and with it its linked databases' trash, so the API
// read the block back as unsupported and its view as not found (2026-11-17).
// settleReturnedBlockTrash takes such entries out of the trash where the
// block came back, as the trash's own restore does
// (prepareRecoverableBlockTrashRestore, deleteRecoverableBlockTrash): the
// entry goes, with its databases' trash and their pending deletion (the
// entry's triggers), and the views its linked databases took with them come
// back unless the page holds them already, named as the entry kept them. Blocks of the entry the version
// didn't hold stay gone, as the version had them; an entry one of whose
// databases stays off the page stays, as that database is still in the
// trash with it.
func settleReturnedBlockTrash(
	app core.App, authorization pageAuthorization, actorID string, structural *crdt.Doc,
) error {
	pageID := authorization.Page.Id
	metadata, err := structuralMetadata(structural)
	if err != nil {
		return err
	}
	rows, err := loadReturnedBlockTrash(app, authorization, metadata)
	if err != nil {
		return err
	}
	views := []graphOperation{}
	for _, row := range rows {
		// An entry that can't be read stays, as its restore would refuse it.
		snapshot, err := parseRecoverableBlockTrashSnapshot(row)
		if err != nil || !returnedBlockTrashDatabasesBack(snapshot, metadata) {
			continue
		}
		for _, operation := range restoreRecoverableLinkedDatabaseViews(snapshot) {
			var held int
			if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_views}} WHERE [[id]]={:id}`).
				Bind(dbx.Params{"id": operation.View.ID}).Row(&held); err != nil {
				return err
			}
			if _, live := metadata[operation.View.ContainerID]; live && held == 0 {
				views = append(views, operation)
			}
		}
		if err := deleteRecoverableBlockTrash(app, row.RootBlockID, pageID); err != nil {
			return err
		}
	}
	if len(views) == 0 {
		return nil
	}
	return applyDatabaseGraphMutation(
		app, structural, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID,
		pageID, actorID, views,
	)
}

// loadReturnedBlockTrash reads the page's trash entries, not yet expired,
// whose root block is on the page again, in the order they were made. Of
// the page's whole trash only the entries' root IDs are read, from the
// page's trash index alone (idx_leafen_block_trash_page holds the workspace,
// the page and the root, so the table and its snapshots stay unread); the
// returned roots' rows alone are read whole, by their own key (the page
// is not an index term there, or the page's index would walk every entry
// again). The read follows what the version brought back, not the page's
// 30 days of deletions, inside the restore's transaction (2026-11-18: 401
// entries of 8.9 MB, one back — 2,208 pages read before, 14 after).
func loadReturnedBlockTrash(
	app core.App, authorization pageAuthorization, metadata map[string]structuralBlockMetadata,
) ([]storedRecoverableBlockTrash, error) {
	pageID := authorization.Page.Id
	roots := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[root_block_id]] FROM {{leafen_block_trash_tombstones}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{
		"workspaceId": authorization.Workspace.WorkspaceID, "pageId": pageID,
	}).Column(&roots); err != nil {
		return nil, err
	}
	returned := []string{}
	for _, root := range roots {
		if _, back := metadata[root]; back {
			returned = append(returned, root)
		}
	}
	rows := []storedRecoverableBlockTrash{}
	if len(returned) == 0 {
		return rows, nil
	}
	encoded, err := json.Marshal(returned)
	if err != nil {
		return nil, err
	}
	err = app.DB().NewQuery(`
		SELECT [[root_block_id]],[[owner_id]],[[workspace_id]],[[page_id]],[[parent_id]],
			[[predecessor_id]],[[successor_id]],[[snapshot_json]],[[block_count]],[[byte_size]],
			[[deleted_by]],[[deleted_at]],[[expires_at]]
		FROM {{leafen_block_trash_tombstones}}
		WHERE [[root_block_id]] IN (SELECT [[value]] FROM json_each({:roots}))
			AND +[[page_id]]={:pageId} AND [[expires_at]]>{:now}
		ORDER BY [[deleted_at]],[[root_block_id]]
	`).Bind(dbx.Params{
		"roots": string(encoded), "pageId": pageID, "now": time.Now().UTC().UnixMilli(),
	}).All(&rows)
	return rows, err
}

// returnedBlockTrashDatabasesBack is whether every database of a trash entry
// is on the page again.
func returnedBlockTrashDatabasesBack(
	snapshot recoverableBlockTrashSnapshot, metadata map[string]structuralBlockMetadata,
) bool {
	for _, block := range snapshot.Blocks {
		if _, _, database := recoverableDatabaseContainer(block); !database {
			continue
		}
		if _, back := metadata[block.ID]; !back {
			return false
		}
	}
	return true
}

// settlingReturnedBlockTrash runs a content change's last step after taking
// out of the trash what the change brought back (settleReturnedBlockTrash).
func settlingReturnedBlockTrash(
	actorID string, next func(core.App, pageAuthorization) error,
) func(core.App, pageAuthorization, *crdt.Doc) error {
	return func(app core.App, authorization pageAuthorization, structural *crdt.Doc) error {
		if err := settleReturnedBlockTrash(app, authorization, actorID, structural); err != nil {
			return err
		}
		return next(app, authorization)
	}
}
