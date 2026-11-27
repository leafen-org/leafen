package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A copy of a page, of a page's body or of blocks takes a synced block
// original as a new original, with its content, and a synced copy of an
// original it copied shows the new one; a copy of an original it did not
// copy keeps showing that original. Notion's page duplicate and its API's
// template copy do so alike (seen 2026-11-14), which the user's decision
// follows since that day. A database page's copy does the same, whether a
// duplicate of it, of its page or database, or a template the Public API or
// MCP applies; so does Notion (seen 2026-11-14), the originals on the
// database pages, templates and pages one copy took all pointed at anew.
// Only the app's own template use, applying a template or duplicating it,
// takes each original as a copy of it, the original's own blocks left out
// (rowPageBodySyncedAsCopies), as Notion's app does (seen 2026-11-14).

// syncedBlockIsOriginal reports whether a block is a synced block original,
// one no copy names a source for.
func syncedBlockIsOriginal(blockType, content string) bool {
	if blockType != graphBlockTypeSyncedBlock {
		return false
	}
	named := struct {
		SourceID string `json:"syncedBlockId"`
	}{}
	_ = json.Unmarshal([]byte(content), &named)
	return strings.TrimSpace(named.SourceID) == ""
}

// syncedBlockCopyOf is the content of a copy of an original on a page.
func syncedBlockCopyOf(originalID, pageID string) string {
	encoded, _ := json.Marshal(map[string]string{"syncedBlockId": originalID, "syncedPageId": pageID})
	return string(encoded)
}

// copiedSyncedBlock is the copy of a block a copy made, on the page it
// went to.
type copiedSyncedBlock struct {
	SourceID     string `db:"source_id"`
	TargetID     string `db:"target_id"`
	TargetPageID string `db:"target_page_id"`
}

// copiedSyncedOriginals finds the copies of the originals the synced copies
// among contents show, which the job made, or the block copy that started it
// (block_copy_page_links.go): of a page's blocks, or on a database page's
// copy.
func copiedSyncedOriginals(app core.App, jobID string, contents []map[string]any) (map[string]copiedSyncedBlock, error) {
	found := map[string]copiedSyncedBlock{}
	ids := syncedCopyIDs(contents)
	if len(ids) == 0 {
		return found, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	parent, err := pageDuplicationParentBlockCopyJob(app, jobID)
	if err != nil {
		return nil, err
	}
	for _, job := range []string{jobID, parent} {
		if job == "" {
			continue
		}
		copied := []copiedSyncedBlock{}
		if err := app.DB().NewQuery(`
			SELECT [[source_id]], [[target_id]], [[target_page_id]] FROM {{leafen_page_duplication_identity_map}}
			WHERE [[job_id]] = {:jobId} AND [[kind]] = 'block' AND [[target_id]] != ''
				AND [[source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		`).Bind(dbx.Params{"jobId": job, "ids": string(encoded)}).All(&copied); err != nil {
			return nil, err
		}
		for _, block := range copied {
			if _, known := found[block.SourceID]; !known {
				found[block.SourceID] = block
			}
		}
		// An original on a database page or template the job copied is on its
		// copy.
		rest := []string{}
		for _, id := range ids {
			if _, known := found[id]; !known {
				rest = append(rest, id)
			}
		}
		onRows, err := syncedOriginalsOnCopiedRows(app, rest, func(pageIDs []string) (map[string]string, error) {
			return pageDuplicationDatabasePageCopies(app, job, pageIDs)
		})
		if err != nil {
			return nil, err
		}
		for id, block := range onRows {
			if _, known := found[id]; !known {
				found[id] = block
			}
		}
		// A copy of a page's body shows the page's copy (synced_block_page_original.go).
		if err := copiedSyncedPageOriginals(app, job, contents, found); err != nil {
			return nil, err
		}
	}
	return found, nil
}

// pageDuplicationDatabasePageCopies names the copies a job made, or makes,
// of the database pages and templates among pageIDs: a row's in its identity
// map, a template's by databaseTemplateCopyID once its data source is copied.
func pageDuplicationDatabasePageCopies(app core.App, jobID string, pageIDs []string) (map[string]string, error) {
	copies, err := pageDuplicationReferenceMappings(app, jobID, "row", pageIDs)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	templates := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[template]].[[id]] FROM {{leafen_database_templates}} [[template]]
		JOIN {{leafen_page_duplication_identity_map}} [[source]]
			ON [[source]].[[job_id]] = {:jobId} AND [[source]].[[kind]] = 'data_source'
			AND [[source]].[[source_id]] = [[template]].[[data_source_id]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND COALESCE([[state]].[[deleted_at]], '') = ''
	`).Bind(dbx.Params{"jobId": jobID, "ids": string(encoded)}).Column(&templates); err != nil {
		return nil, err
	}
	for _, template := range templates {
		copies[template] = databaseTemplateCopyID(jobID, template)
	}
	return copies, nil
}

// rowPageBodyCopyBlockID is the ID a copy of a database page's body gives
// one of its blocks (duplicateDatabaseRowPageBodyRewritten).
func rowPageBodyCopyBlockID(targetRowID, sourceBlockID string) string {
	return targetRowID + ":" + sourceBlockID
}

// syncedOriginalsOnCopiedRows finds, among the originals ids names, those on
// database pages copies names a copy of (source to copy), and names their
// copies on the copy.
func syncedOriginalsOnCopiedRows(
	app core.App, ids []string, copies func(rowIDs []string) (map[string]string, error),
) (map[string]copiedSyncedBlock, error) {
	found := map[string]copiedSyncedBlock{}
	if len(ids) == 0 {
		return found, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	blocks := []struct {
		ID     string `db:"id"`
		PageID string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[page_id]] FROM {{leafen_blocks}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&blocks); err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return found, nil
	}
	pages := make([]string, 0, len(blocks))
	for _, block := range blocks {
		pages = append(pages, block.PageID)
	}
	targets, err := copies(pages)
	if err != nil {
		return nil, err
	}
	for _, block := range blocks {
		if target, copied := targets[block.PageID]; copied && target != "" {
			found[block.ID] = copiedSyncedBlock{
				SourceID: block.ID, TargetID: rowPageBodyCopyBlockID(target, block.ID), TargetPageID: target,
			}
		}
	}
	return found, nil
}

// syncedCopyIDs are the originals the synced copies among contents show.
func syncedCopyIDs(contents []map[string]any) []string {
	ids := []string{}
	for _, content := range contents {
		if id, ok := pageDuplicationString(content["syncedBlockId"]); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// pointRowPageBodySyncedCopies is a database page body copy's rewrite: its
// synced copies of originals the same copy copied elsewhere, as find names
// them, show those originals' copies.
func pointRowPageBodySyncedCopies(
	find func(contents []map[string]any) (map[string]copiedSyncedBlock, error),
) func([]newRowPageBlock) error {
	return func(body []newRowPageBlock) error {
		contents := make([]map[string]any, len(body))
		for index := range body {
			contents[index] = body[index].Content
		}
		originals, err := find(contents)
		if err != nil {
			return err
		}
		for _, content := range contents {
			pointSyncedCopyAtCopiedOriginal(content, originals)
		}
		return nil
	}
}

// pointSyncedCopyAtCopiedOriginal points a synced copy whose original was
// copied at the original's copy.
func pointSyncedCopyAtCopiedOriginal(content map[string]any, originals map[string]copiedSyncedBlock) {
	id, ok := pageDuplicationString(content["syncedBlockId"])
	if !ok {
		return
	}
	if original, copied := originals[id]; copied {
		content["syncedBlockId"], content["syncedPageId"] = original.TargetID, original.TargetPageID
	}
}
