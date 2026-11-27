package app

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A synced block copy may show a whole page's body instead of an original
// synced block's: Notion's public API makes one from a new page's Markdown
// whose empty <synced-block-reference> gives the address of a page, or of a
// database's page (seen 2026-11-27, twice, "v1" and "v2", page and data
// source parents). Its screen shows the page's blocks where the copy stands
// and edits them there, lists that page as the original among the pages the
// block is synced to, and shows a notice while the page is in the trash; the
// page itself shows nothing of it.
//
// Such a copy names the page as its original and as its original's page,
// one ID twice: {"syncedBlockId": P, "syncedPageId": P}. A sub-page's block
// carries its page's ID, so every reader that would look the original up as
// a block asks syncedCopyPage first. A page's body is its blocks whose
// parent is "" (its top level), so a reader walking an original's tree
// walks the page's from there.

// syncedCopyPage is the page whose body a synced copy's content shows; ""
// for a copy of an original block, an original, or any other block.
func syncedCopyPage(content map[string]any) string {
	original, _ := content["syncedBlockId"].(string)
	page, _ := content["syncedPageId"].(string)
	if original = strings.TrimSpace(original); original != "" && original == strings.TrimSpace(page) {
		return original
	}
	return ""
}

// syncedCopyPageRaw is syncedCopyPage of stored content.
func syncedCopyPageRaw(content string) string {
	decoded := map[string]any{}
	if json.Unmarshal([]byte(content), &decoded) != nil {
		return ""
	}
	return syncedCopyPage(decoded)
}

// syncedOriginalRoot is the parent an original's blocks stand under on its
// page: the original block, or "" (the page's top level) for a copy of a
// page's body.
func syncedOriginalRoot(originalID, pageID string) string {
	if originalID != "" && originalID == pageID {
		return ""
	}
	return originalID
}

// nameSyncedPageOriginals gives a copy a Leafen write makes naming a page of
// the page's workspace as its original, with no original's page, that page
// as its original's page too, so it shows the page's body as a pasted one
// does; a copy naming a synced block keeps naming it, and one naming a
// full-page database's own page shows no database
// (notion_page_markdown_synced_database.go).
func nameSyncedPageOriginals(app core.App, owner, pageID string, operations []graphOperation) error {
	for _, operation := range operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.Type != graphBlockTypeSyncedBlock {
			continue
		}
		content := map[string]any{}
		if json.Unmarshal(block.Content, &content) != nil {
			continue
		}
		original, _ := content["syncedBlockId"].(string)
		named, _ := content["syncedPageId"].(string)
		if original = strings.TrimSpace(original); original == "" || original == syncedBlockSourceless || strings.TrimSpace(named) != "" {
			continue
		}
		var page int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_pages}} AS [[page]]
			JOIN {{leafen_pages}} AS [[here]] ON [[here]].[[id]] = {:here} AND [[here]].[[workspace]] = [[page]].[[workspace]]
			WHERE [[page]].[[id]] = {:id} AND COALESCE([[page]].[[kind]], '') <> {:database}
				AND NOT EXISTS (SELECT 1 FROM {{leafen_blocks}} WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[type]] = 'synced_block')
		`).Bind(dbx.Params{"id": original, "here": pageID, "owner": owner, "database": pageKindDatabase}).Row(&page); err != nil {
			return err
		}
		if page == 0 {
			continue
		}
		content["syncedPageId"] = original
		encoded, err := json.Marshal(content)
		if err != nil {
			return err
		}
		block.Content = encoded
	}
	return nil
}

// loadSyncedPageOriginal is where a copy of the page's body reads its
// original: the page, for a reader who may open it, and whether it is in
// the trash, by itself or under a page there.
func loadSyncedPageOriginal(app core.App, principalID, pageID string) (syncedBlockLocation, error) {
	if _, err := authorizePageBody(app, principalID, pageID, permissionWorkspaceRead); err != nil {
		return syncedBlockLocation{}, refusedUnlessStorage(errSyncedBlockUnsyncUnavailable, err)
	}
	_, trashed, err := loadPageTrashEffect(app, pageID)
	return syncedBlockLocation{BlockID: pageID, PageID: pageID, InTrash: trashed}, err
}

// syncedOriginalPlace names where blocks a copy can't hold go: under its
// original block, or on the page whose body it shows.
func syncedOriginalPlace(app core.App, original string) string {
	var page int
	_ = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id}`).Bind(dbx.Params{"id": original}).Row(&page)
	if page > 0 {
		return "on the page whose body it shows, page " + strconv.Quote(original)
	}
	return "under the original, block " + strconv.Quote(original)
}

// countNotionSyncedPageCopyChildren sets the counts of the copies of a
// page's body among blockIDs to the blocks the page holds at its top level,
// as Notion's API gives such a copy has_children (seen 2026-11-27: true for
// a page holding blocks), though a read of its children lists none.
func countNotionSyncedPageCopyChildren(app core.App, params dbx.Params, placeholders string, counts map[string]int) error {
	rows := []struct {
		CopyID string `db:"copy_id"`
		Count  int    `db:"count"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[copy]].[[id]] AS [[copy_id]], COUNT([[child]].[[id]]) AS [[count]]
		FROM {{leafen_blocks}} AS [[copy]]
		LEFT JOIN {{leafen_blocks}} AS [[child]] INDEXED BY [[idx_leafen_blocks_page_order]]
			ON [[child]].[[owner]] = [[copy]].[[owner]] AND [[child]].[[page_id]] = TRIM(` + notionSyncedCopyOriginalSQL + `)
			AND [[child]].[[parent_id]] = '' AND ` + blockStandsSQL("child") + `
		WHERE [[copy]].[[id]] IN (` + placeholders + `) AND [[copy]].[[type]] = 'synced_block' AND ` + notionSyncedCopyOfPageSQL + `
		GROUP BY [[copy]].[[id]]
	`).Bind(params).All(&rows)
	for _, row := range rows {
		counts[row.CopyID] = row.Count
	}
	return err
}

// notionSyncedCopyOfPageSQL holds for a block named copy showing a page's body.
const notionSyncedCopyOfPageSQL = `(CASE WHEN json_valid([[copy]].[[content]]) THEN TRIM(COALESCE(json_extract([[copy]].[[content]], '$.syncedBlockId'), '')) <> '' AND
	TRIM(COALESCE(json_extract([[copy]].[[content]], '$.syncedBlockId'), '')) = TRIM(COALESCE(json_extract([[copy]].[[content]], '$.syncedPageId'), '')) ELSE 0 END)`

// notionAppendToPageOriginal sends an append under a copy of a page's body
// to the end of that page's top level, as Notion's API does (seen
// 2026-11-27: the answer lists the block appended, the page reads it last);
// a place after a block is refused as not the copy's, as under a copy of a
// block (notionAppendUnderOriginal).
func notionAppendToPageOriginal(
	event *core.RequestEvent, target notionBlockAppendTarget, position *notionAppendPosition, afterID *string, pageID string,
) (notionBlockAppendTarget, bool, error) {
	if afterID != nil && position != nil && position.AfterBlock != nil {
		return target, true, notionBodyAnchorError(event, errNotionBodyAnchorOutside, position.AfterBlock.ID)
	}
	publicID, err := notionPagePublicID(event.App, pageID)
	if err != nil {
		return target, true, notionRequestNotCompleted(event, err)
	}
	var page notionPageAuthorization
	authorized := false
	if row, rowErr := isDatabaseRowPage(event.App, pageID); rowErr != nil {
		return target, true, notionRequestNotCompleted(event, rowErr)
	} else if row {
		page, authorized, err = authorizeNotionRowPageBody(event, publicID, "pages:write", permissionWorkspaceWrite)
	} else {
		page, authorized, err = authorizeNotionPage(event, publicID, "pages:write", permissionWorkspaceWrite)
		if err == nil && authorized {
			if refused, refusal := refuseNotionTrashedEdit(event, pageID, true, false, notionTrashedEdit); refused {
				return target, true, refusal
			}
		}
	}
	if err != nil || !authorized {
		return target, true, err
	}
	return notionBlockAppendTarget{
		Identity: page.Identity, Page: page.Page, PageID: page.Page.Page.Id, Owner: page.Page.Workspace.OwnerID,
	}, false, nil
}

// isDatabaseRowPage reports whether a page is a database's page.
func isDatabaseRowPage(app core.App, pageID string) (bool, error) {
	var rows int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]] = {:id} AND COALESCE([[data_source_id]], '') <> ''`).
		Bind(dbx.Params{"id": pageID}).Row(&rows)
	return rows > 0, err
}

// syncedPageOriginalTrashed reports whether a copy's page original is in the
// trash now, for readers that show such a copy a notice instead.
func syncedPageOriginalTrashed(app core.App, pageID string) bool {
	_, trashed, err := loadPageTrashEffect(app, pageID)
	return err == nil && trashed
}
