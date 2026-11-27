package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A copy of blocks that carries sub-pages names their copies wherever it
// named them, as Notion's copy does (seen 2026-11-14, its app's page
// duplicate and its API's template_id copy alike): a link or a mention of a
// copied sub-page, of a page under one, or of a row of a copied database
// names the copy, and a page's whole body copied names the page it went to
// where it named the page it came from. The copies' IDs are chosen once the
// blocks are found, before any is copied (planBlockCopyPages); each
// sub-page's own copy (child_page_block_copies.go) takes those IDs for its
// pages, and reads the rest of the body copy's mappings for its links and
// mentions (pageDuplicationMentionTargetMappings).

// planBlockCopyPages chooses the IDs of the copies of the sub-pages a copy
// carries, and of every page under them, and maps a body's page to the page
// it goes to.
func planBlockCopyPages(app core.App, job storedBlockMoveJob) error {
	placement, err := loadBlockMovePlacement(app, job.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	if placement.BodyCopy != "" {
		if err := mapPageDuplicationIdentity(
			app, job.ID, "page", job.SourcePageID, job.DestinationPageID, job.SourcePageID, job.DestinationPageID, now,
		); err != nil {
			return err
		}
	}
	if copies, err := blockCopyCopiesSubPages(app, job.DestinationPageID); err != nil || !copies {
		return err
	}
	// The pages a sub-page's copy takes: those under it that a copy reads
	// (discoverPageDuplicationWindow).
	pages := []string{}
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[tree]]([[id]]) AS (
			SELECT [[page]].[[id]] FROM {{leafen_block_move_items}} AS [[item]]
			JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]] = [[item]].[[block_id]]
				AND [[block]].[[owner]] = {:owner} AND [[block]].[[page_id]] = {:sourcePageId}
				AND [[block]].[[type]] = 'child_page' AND json_valid([[block]].[[content]])
				AND json_extract([[block]].[[content]], '$.childPageId') = [[block]].[[id]]
			JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[block]].[[id]] AND [[page]].[[in_trash]] = 0
			WHERE [[item]].[[job_id]] = {:jobId} AND [[item]].[[target_block_id]] != ''
			UNION
			-- CROSS JOIN steps from the tree by key (workspace, parent), the
			-- trash mark read after (+): SQLite's own order and index read the
			-- workspace's every page for each page.
			SELECT [[page]].[[id]] FROM [[tree]]
			CROSS JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[parent_id]] = [[tree]].[[id]]
			WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[notion_import_staging]] = 0
				AND [[page]].[[duplication_target_job_id]] = '' AND +[[page]].[[in_trash]] = 0
		)
		SELECT [[id]] FROM [[tree]] ORDER BY [[id]]
	`).Bind(dbx.Params{
		"owner": job.sourceOwner(), "sourcePageId": job.SourcePageID, "jobId": job.ID, "workspaceId": job.sourceWorkspace(),
	}).Column(&pages); err != nil {
		return err
	}
	for _, pageID := range pages {
		targetID := newPageID()
		if err := mapPageDuplicationIdentity(app, job.ID, "page", pageID, targetID, pageID, targetID, now); err != nil {
			return err
		}
	}
	return nil
}

// blockCopyCopiesSubPages reports whether the sub-pages a copy brings to a
// page are copied there: a wiki's page keeps links to them. A database
// template's page takes copies, its body holding sub-pages as a page's does.
func blockCopyCopiesSubPages(app core.App, destinationPageID string) (bool, error) {
	_, inWiki, err := loadWikiPlace(app, destinationPageID)
	return !inWiki, err
}

// remapBlockCopyTextDelta names, in a copied block's text, the copies of
// the pages, rows and databases its mentions and links name
// (page_duplication_links.go).
func remapBlockCopyTextDelta(app core.App, jobID string, delta []crdt.Delta) ([]crdt.Delta, error) {
	pageIDs, databaseIDs := map[string]struct{}{}, map[string]struct{}{}
	for _, operation := range delta {
		collectPageDuplicationTextTargets(operation.Attributes, pageIDs, databaseIDs)
	}
	if len(pageIDs) == 0 && len(databaseIDs) == 0 {
		return delta, nil
	}
	pages, databases, err := pageDuplicationTextMentionMappings(app, jobID, pageIDs, databaseIDs)
	if err != nil {
		return nil, err
	}
	return remapPageDuplicationTextDelta(delta, pages, databases), nil
}

// pageDuplicationParentBlockCopyJob is the block copy a page's copy was
// started by, if any.
func pageDuplicationParentBlockCopyJob(app core.App, jobID string) (string, error) {
	parent := ""
	err := app.DB().NewQuery(`SELECT [[parent_block_copy_job_id]] FROM {{leafen_page_duplication_jobs}} WHERE [[job_id]] = {:jobId}`).
		Bind(dbx.Params{"jobId": jobID}).Row(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return parent, err
}

// pageDuplicationPlannedPages are the IDs the block copy that started a
// page's copy chose for the copies of the given pages.
func pageDuplicationPlannedPages(app core.App, jobID string, pageIDs []string) (map[string]string, error) {
	parent, err := pageDuplicationParentBlockCopyJob(app, jobID)
	if err != nil || parent == "" {
		return map[string]string{}, err
	}
	return pageDuplicationReferenceMappings(app, parent, "page", pageIDs)
}
