package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A synced copy's children are its original's, as Notion's API gives them:
// a read of the copy lists them (serveNotionBlockChildrenFollowing), its
// has_children is theirs (countNotionSyncedCopyChildren), and an append under
// it puts blocks under the original (notionAppendUnderOriginal).

// notionSyncedCopySourceID is the original a synced block copy shows.
func notionSyncedCopySourceID(block storedNotionBlock) string {
	if block.Type != graphBlockTypeSyncedBlock {
		return ""
	}
	content := struct {
		SourceID string `json:"syncedBlockId"`
	}{}
	if json.Unmarshal([]byte(block.Content), &content) != nil {
		return ""
	}
	return strings.TrimSpace(content.SourceID)
}

// notionSyncedCopyOriginal is the public ID of the original whose children a
// synced copy on copyPage reads and takes, found false for one gone or in
// another workspace, which Notion's API doesn't find (seen 2026-10-14 for a
// read; 2026-11-21 for a read and an append, naming the original).
func notionSyncedCopyOriginal(app core.App, copyPage pageAuthorization, sourceID string) (string, bool, error) {
	source, found, err := loadStoredNotionBlock(app, sourceID)
	if err != nil {
		return "", false, err
	}
	if found && (source.Owner != copyPage.Workspace.OwnerID || source.WorkspaceID != copyPage.Workspace.WorkspaceID) {
		return "", false, nil
	}
	publicID, err := notionBlockPublicID(app, copyPage.Workspace.WorkspaceID, sourceID)
	return publicID, found && err == nil, err
}

// notionSyncedCopyOriginalMissing answers a read or an append under a copy
// whose original sourceID isn't among the page blocks, as Notion's API does
// (seen 2026-11-21, Notion-Version 2026-03-11, synthetic pages under
// 한지테스트페이지): one gone, or in the trash itself, is not found, named;
// one in the trash inside a block there lists no children for a read and
// refuses an append, an empty one too, as under an archived ancestor, as a
// change under it is refused (notion_trash_refusal.go).
func notionSyncedCopyOriginalMissing(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, sourceID, publicID string, err error, appending bool,
) error {
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	if sourceID == syncedBlockSourceless {
		return notionSourcelessCopyChildren(event, appending)
	}
	if publicID == "" {
		return notionBlockNotFound(event)
	}
	read, trashed, err := loadNotionTrashedBlockRead(event.App, identity, sourceID)
	if err != nil {
		return notionBlockProjectionError(event, err)
	}
	if trashed && !read.itself() {
		if appending {
			if refused, answer := refuseNotionTrashedBlockEdit(event, identity, sourceID); refused {
				return answer
			}
		} else {
			return notionNoChildren(event)
		}
	}
	return notionNamedObjectNotFound(event, "block", publicID)
}

// notionSyncedCopyOriginalSQL is the original a block named copy shows.
const notionSyncedCopyOriginalSQL = `(CASE WHEN json_valid([[copy]].[[content]]) THEN json_extract([[copy]].[[content]], '$.syncedBlockId') END)`

// countNotionSyncedCopyChildren sets the counts of the synced copies among
// blockIDs (placeholders binding them in params) to their originals'
// children, as Notion's API gives a copy's has_children (seen 2026-11-21:
// true for a copy of an original holding blocks, on its page or another).
// An original in the trash, itself or inside a block or page there, still
// lends its children (countNotionSyncedCopyTrashedChildren); one gone lends
// none.
func countNotionSyncedCopyChildren(app core.App, params dbx.Params, placeholders string, counts map[string]int) error {
	rows := []struct {
		CopyID string `db:"copy_id"`
		Count  int    `db:"count"`
		Found  int    `db:"found"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[copy]].[[id]] AS [[copy_id]], COUNT([[child]].[[id]]) AS [[count]],
			COUNT([[original]].[[id]]) AS [[found]]
		FROM {{leafen_blocks}} AS [[copy]]
		LEFT JOIN {{leafen_blocks}} AS [[original]]
			ON [[original]].[[id]] = TRIM(` + notionSyncedCopyOriginalSQL + `)
			AND [[original]].[[owner]] = [[copy]].[[owner]] AND [[original]].[[workspace_id]] = [[copy]].[[workspace_id]]
		LEFT JOIN {{leafen_blocks}} AS [[child]] INDEXED BY [[idx_leafen_blocks_page_order]]
			ON [[child]].[[owner]] = [[original]].[[owner]] AND [[child]].[[page_id]] = [[original]].[[page_id]]
			AND [[child]].[[parent_id]] = [[original]].[[id]] AND ` + blockStandsSQL("child") + `
		WHERE [[copy]].[[id]] IN (` + placeholders + `) AND [[copy]].[[type]] = 'synced_block'
			AND TRIM(COALESCE(` + notionSyncedCopyOriginalSQL + `, '')) <> ''
		GROUP BY [[copy]].[[id]]
	`).Bind(params).All(&rows)
	if err != nil {
		return err
	}
	missing := []string{}
	for _, row := range rows {
		counts[row.CopyID] = row.Count
		if row.Found == 0 {
			missing = append(missing, row.CopyID)
		}
	}
	return countNotionSyncedCopyTrashedChildren(app, missing, counts)
}

// countNotionSyncedCopyTrashedChildren sets the counts of copies whose
// original isn't among the page blocks to its children in the block trash,
// as Notion's API reads such a copy (seen 2026-11-21, Notion-Version
// 2026-03-11, synthetic pages under 한지테스트페이지: has_children true for a
// copy of an original holding a block, trashed itself or inside a toggle
// trashed, and false for one holding none; true too with the original's
// page in the trash, whose blocks Leafen keeps among the page blocks).
// The tombstones holding the original are read, by the blocks each holds.
func countNotionSyncedCopyTrashedChildren(app core.App, copyIDs []string, counts map[string]int) error {
	if len(copyIDs) == 0 {
		return nil
	}
	encoded, err := json.Marshal(copyIDs)
	if err != nil {
		return err
	}
	rows := []struct {
		CopyID string `db:"copy_id"`
		Count  int    `db:"count"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[copy]].[[id]] AS [[copy_id]], COUNT(*) AS [[count]]
		FROM {{leafen_blocks}} AS [[copy]]
		CROSS JOIN {{leafen_block_trash_tombstone_blocks}} AS [[held]]
			ON [[held]].[[block_id]] = TRIM(` + notionSyncedCopyOriginalSQL + `)
		CROSS JOIN {{leafen_block_trash_tombstones}} AS [[tombstone]]
			ON [[tombstone]].[[root_block_id]] = [[held]].[[root_block_id]]
			AND [[tombstone]].[[owner_id]] = [[copy]].[[owner]] AND [[tombstone]].[[workspace_id]] = [[copy]].[[workspace_id]]
			AND [[tombstone]].[[expires_at]] > {:now}
		CROSS JOIN json_each(CASE WHEN json_valid([[tombstone]].[[snapshot_json]])
			THEN [[tombstone]].[[snapshot_json]] ELSE '{}' END, '$.blocks') AS [[entry]]
		WHERE [[copy]].[[id]] IN (SELECT [[value]] FROM json_each({:copies}))
			AND json_extract([[entry]].[[value]], '$.parentId') = [[held]].[[block_id]]
		GROUP BY [[copy]].[[id]]
	`).Bind(dbx.Params{"copies": string(encoded), "now": time.Now().UTC().UnixMilli()}).All(&rows)
	for _, row := range rows {
		counts[row.CopyID] = row.Count
	}
	return err
}
