package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A copy's top page takes its body before the rest of the tree is read, so the
// copy that opens at once shows its text soon after, as Notion's does (seen
// 2026-11-14: a big tree's copy open at 2.2 s, its text at 3 s). Done in turn,
// it waited for every page of the tree to be read, its files gathered and its
// blocks mapped (the same day: 28 s for a tree of 2,431 pages on a NAS).
//
// The top goes first only when nothing in its body waits on the rest of the
// tree: each page, block and database its blocks name has its copy already
// (its sub-pages, read with it), is on the top itself, or is outside the
// tree. A page further down the tree its body names takes its copy's ID as the
// top goes first, which the tree's reading then gives it
// (planPageDuplicationTopMentions); a link to it shows the page coming until
// it is made, as a sub-page's does (pageDuplicationTargetToCome). Its own files — a picture, a file, an uploaded icon or cover — are
// copied first, one between each two turns while the tree goes on being read
// (advancePageDuplication), and its blocks then name the copies, its icon and
// cover as each comes (page_duplication_copied_file.go) (2026-11-15: a top
// with a picture waited for the whole tree, about 50 s for 2,431 pages on a
// NAS). A database of its own takes its IDs with the top, so its blocks
// name them, and its content in its turn, once the whole tree is mapped, as
// any page's: what its rows and properties name may be anywhere in the tree
// (pageDuplicationTopHandsOn); the copy's top says its body is written
// meanwhile, and a screen open on it reads it again then (pageCopyTopWritten).
// It is held for its windows as any page is (page_duplication_hold.go), and
// the copy then goes on as before without it; any other top waits its turn.
// What is made is the same either way.

// copyPageDuplicationTopFirstWindow does one window of the top's own work
// while the tree is read, and says whether it did one.
func copyPageDuplicationTopFirstWindow(app core.App, job storedPageDuplicationJob) (bool, error) {
	if job.DestinationParentID == job.SourceRootPageID {
		return false, nil
	}
	item, err := loadPageDuplicationTopItem(app, job)
	if err != nil || !item.Discovered || item.Copied {
		return false, err
	}
	// Its text written, its databases wait their turn.
	if item.BlocksCopied {
		return pageDuplicationTopHandsOn(app, job, item)
	}
	if item.BlockMapCursor == "" && !item.BlocksMapped {
		var held string
		if err := app.DB().NewQuery(`
			SELECT [[held_page_id]] FROM {{leafen_page_duplication_jobs}} WHERE [[job_id]]={:jobId}
		`).Bind(dbx.Params{"jobId": job.ID}).Row(&held); err != nil {
			return false, err
		}
		if held != item.SourcePageID {
			alone, err := pageDuplicationTopGoesFirst(app, job)
			if err != nil || !alone {
				return false, err
			}
			if _, err := app.DB().NewQuery(`
				UPDATE {{leafen_page_duplication_jobs}}
				SET [[held_page_id]]={:pageId}, [[updated_at]]={:updatedAt}
				WHERE [[job_id]]={:jobId} AND [[phase]]='discovering'
			`).Bind(dbx.Params{
				"jobId": job.ID, "pageId": item.SourcePageID, "updatedAt": time.Now().UTC().UnixMilli(),
			}).Execute(); err != nil {
				return false, err
			}
			if err := inventoryPageDuplicationTopFiles(app, job, item); err != nil {
				return false, err
			}
		}
		// Its files are copied between turns; the tree is read meanwhile.
		if _, err := nextPageDuplicationTopFile(app, job); err == nil {
			return false, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}
	if !item.BlocksMapped {
		return true, mapPageDuplicationBlockWindow(app, job, item)
	}
	var databaseMapped bool
	if err := app.DB().NewQuery(`
		SELECT [[database_mapped]] FROM {{leafen_page_duplication_items}}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{"jobId": job.ID, "sourcePageId": item.SourcePageID}).Row(&databaseMapped); err != nil {
		return false, err
	}
	if !databaseMapped {
		state, err := nextPageDuplicationDatabaseMappingState(app, job.ID)
		if err != nil || state.SourcePageID != item.SourcePageID {
			return false, errors.Join(errPageDuplicationConflict, err)
		}
		return true, mapPageDuplicationDatabaseWindow(app, job, state)
	}
	if !item.BlocksCopied {
		return true, copyPageDuplicationBlockWindow(app, job, item)
	}
	return pageDuplicationTopHandsOn(app, job, item)
}

// pageDuplicationTopHandsOn ends the top's own work once its blocks are
// copied: one without a database of its own is copied there, its templates
// with it; one with a database hands it to the copy's turns
// (copyPageDuplicationWindow), which hold the top again for it once the tree
// is mapped. It says whether it did any work.
func pageDuplicationTopHandsOn(app core.App, job storedPageDuplicationJob, item storedPageDuplicationItem) (bool, error) {
	var held string
	if err := app.DB().NewQuery(`
		SELECT [[held_page_id]] FROM {{leafen_page_duplication_jobs}} WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&held); err != nil || held != item.SourcePageID {
		return false, err
	}
	if own, err := pageDuplicationTopOwnsDatabase(app, job); err != nil {
		return false, err
	} else if own {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_page_duplication_jobs}}
			SET [[held_page_id]]='', [[updated_at]]={:updatedAt}
			WHERE [[job_id]]={:jobId} AND [[phase]]='discovering'
		`).Bind(dbx.Params{"jobId": job.ID, "updatedAt": time.Now().UTC().UnixMilli()}).Execute()
		return true, err
	}
	if state, err := nextPageDuplicationDatabaseCopyState(app, job.ID, item.SourcePageID); err == nil {
		return true, copyPageDuplicationDatabaseWindow(app, job, state)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err := copyDatabaseTemplates(
		app, job.ID, job.sourceOwner(), job.sourceWorkspace(), job.OwnerID, job.WorkspaceID, job.ActorID,
		item.SourcePageID, item.TargetPageID,
	); err != nil {
		return false, err
	}
	now := time.Now().UTC().UnixMilli()
	var marked int
	if err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}}
		SET [[copied]]=1, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
			AND [[copied]]=0 AND [[page_created]]=1
			AND [[blocks_mapped]]=1 AND [[database_mapped]]=1 AND [[blocks_copied]]=1
		RETURNING 1
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": item.SourcePageID, "updatedAt": now,
	}).Row(&marked); err != nil {
		return false, err
	}
	// The top keeps its mark until the end, which says the copy goes on; it
	// counts once the tree is read (countPageDuplicationTopFirst).
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[held_page_id]]='', [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='discovering'
	`).Bind(dbx.Params{"jobId": job.ID, "updatedAt": now}).Execute()
	return true, err
}

// countPageDuplicationTopFirst counts a top that went first among the pages
// copied as the tree is read whole: counted at once, the copy's count would
// say it was all but done while its pages were still being read.
func countPageDuplicationTopFirst(app core.App, job storedPageDuplicationJob) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[copied_count]]=[[copied_count]]+(
			SELECT COUNT(*) FROM {{leafen_page_duplication_items}}
			WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId} AND [[copied]]=1
		)
		WHERE [[job_id]]={:jobId} AND [[phase]]='discovering'
	`).Bind(dbx.Params{"jobId": job.ID, "sourcePageId": job.SourceRootPageID}).Execute()
	return err
}

func loadPageDuplicationTopItem(app core.App, job storedPageDuplicationJob) (storedPageDuplicationItem, error) {
	item := storedPageDuplicationItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_page_id]],[[target_page_id]],[[source_parent_id]],
			[[target_parent_id]],[[depth]],[[discovery_cursor]],[[discovered]],
			[[page_created]],[[block_map_cursor]],[[blocks_mapped]],
			[[block_copy_cursor]],[[blocks_copied]],[[copied]],[[published]]
		FROM {{leafen_page_duplication_items}}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{"jobId": job.ID, "sourcePageId": job.SourceRootPageID}).One(&item)
	return item, err
}

// pageDuplicationTopGoesFirst says whether the top may take its body now: it
// is asked once, when the top is read and the pages under it are not yet.
func pageDuplicationTopGoesFirst(app core.App, job storedPageDuplicationJob) (bool, error) {
	// A copy into another workspace or from a site, a wiki page's to private
	// pages and one a block copy started each read the tree their own way.
	if job.crossWorkspace() || job.SourcePublicLinkID != "" {
		return false, nil
	}
	progress := struct {
		Discovered  int    `db:"discovered_count"`
		Unsupported int    `db:"unsupported_count"`
		Held        string `db:"held_page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[discovered_count]],[[unsupported_count]],[[held_page_id]]
		FROM {{leafen_page_duplication_jobs}} WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).One(&progress); err != nil ||
		progress.Discovered != 1 || progress.Unsupported != 0 || progress.Held != "" {
		return false, err
	}
	// Past the moment the tree under the top began to be read, it waits.
	var reading int
	if err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_page_duplication_items}}
			WHERE [[job_id]]={:jobId} AND [[source_page_id]]!={:sourcePageId}
				AND [[discovered]]=0 AND [[discovery_cursor]]!='' LIMIT 1)
	`).Bind(dbx.Params{"jobId": job.ID, "sourcePageId": job.SourceRootPageID}).Row(&reading); err != nil || reading != 0 {
		return false, err
	}
	if parent, err := pageDuplicationParentBlockCopyJob(app, job.ID); err != nil || parent != "" {
		return false, err
	}
	if toPrivate, err := pageDuplicationToPrivatePages(app, job.ID); err != nil || toPrivate {
		return false, err
	}
	references, alone, err := collectPageDuplicationTopReferences(app, job)
	if err != nil || !alone {
		return false, err
	}
	if err := planPageDuplicationTopMentions(app, job, references); err != nil {
		return false, err
	}
	return pageDuplicationReferencesSettled(app, job, references)
}

// planPageDuplicationTopMentions chooses the copies' IDs of the pages further
// down the tree the top's body names, which the tree's reading gives them
// when it comes to them (discoverPageDuplicationWindow), as a block copy's
// plan does (planBlockCopyPages): those it reaches, each page from the one
// named up to the top a page of the workspace the copy reads, out of the
// trash, neither a copy nor an import being made, and none but the one named
// the page the copy goes into, which is taken without what is under it.
func planPageDuplicationTopMentions(app core.App, job storedPageDuplicationJob, references pageDuplicationReferenceSet) error {
	if len(references.pageIDs) == 0 {
		return nil
	}
	mapped, err := pageDuplicationMentionTargetMappings(app, job.ID, referenceIDs(references.pageIDs))
	if err != nil {
		return err
	}
	unmapped := map[string]struct{}{}
	for id := range references.pageIDs {
		if _, ok := mapped[id]; !ok && id != job.SourceRootPageID {
			unmapped[id] = struct{}{}
		}
	}
	if len(unmapped) == 0 {
		return nil
	}
	raw, _ := json.Marshal(referenceIDs(unmapped))
	reached := []string{}
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[up]]([[id]],[[parent_id]],[[named]]) AS (
			SELECT [[id]],[[parent_id]],[[id]] FROM {{leafen_pages}}
			WHERE [[id]] IN (SELECT [[value]] FROM json_each({:pageIds}))
				AND [[workspace]]={:workspaceId} AND [[data_source_id]]='' AND [[notion_import_staging]]=0
				AND [[duplication_target_job_id]]='' AND [[in_trash]]=0
			UNION
			-- CROSS JOIN climbs from the page by key (id): SQLite's own
			-- order read the workspace's every page for each ancestor.
			SELECT [[page]].[[id]],[[page]].[[parent_id]],[[up]].[[named]] FROM [[up]]
			CROSS JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[up]].[[parent_id]]
			WHERE [[page]].[[id]]!={:rootId} AND [[page]].[[id]]!={:destinationId}
				AND [[page]].[[workspace]]={:workspaceId} AND [[page]].[[data_source_id]]=''
				AND [[page]].[[notion_import_staging]]=0 AND [[page]].[[duplication_target_job_id]]=''
				AND [[page]].[[in_trash]]=0
		)
		SELECT DISTINCT [[named]] FROM [[up]] WHERE [[parent_id]]={:rootId} ORDER BY [[named]]
	`).Bind(dbx.Params{
		"pageIds": string(raw), "workspaceId": job.sourceWorkspace(), "rootId": job.SourceRootPageID,
		"destinationId": job.DestinationParentID,
	}).Column(&reached); err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	for _, pageID := range reached {
		targetID := newPageID()
		if err := mapPageDuplicationIdentity(app, job.ID, "page", pageID, targetID, pageID, targetID, now); err != nil {
			return err
		}
	}
	return nil
}

// collectPageDuplicationTopReferences gathers what the top's blocks and their
// text name, as the copy's windows remap them (remapPageDuplicationBlockContents,
// remapPageDuplicationTextDeltas); its files come first on their own
// (inventoryPageDuplicationTopFiles).
func collectPageDuplicationTopReferences(
	app core.App,
	job storedPageDuplicationJob,
) (pageDuplicationReferenceSet, bool, error) {
	references := pageDuplicationReferenceSet{
		pageIDs: map[string]struct{}{}, blockIDs: map[string]struct{}{},
		dataSourceIDs: map[string]struct{}{}, viewIDs: map[string]struct{}{},
	}
	blocks := []pageDuplicationSourceBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{"owner": job.sourceOwner(), "pageId": job.SourceRootPageID}).All(&blocks); err != nil {
		return references, false, err
	}
	text, hasText, err := loadYgoDoc(app, job.SourceRootPageID)
	if err != nil {
		return references, false, err
	}
	for _, block := range blocks {
		content := map[string]any{}
		if err := json.Unmarshal([]byte(block.Content), &content); err != nil {
			return references, false, nil
		}
		collectPageDuplicationReferences(content, false, &references)
		if !hasText {
			continue
		}
		for _, operation := range text.GetText(ygoBlockTextRootPrefix + block.ID).ToDelta() {
			collectPageDuplicationTextTargets(operation.Attributes, references.pageIDs, references.blockIDs)
		}
	}
	return references, true, nil
}

// pageDuplicationReferencesSettled says whether each of references resolves
// now as it will once the whole tree is mapped: its copy is known, it is on
// the top (mapped with it), or nothing under the top holds it.
func pageDuplicationReferencesSettled(
	app core.App,
	job storedPageDuplicationJob,
	references pageDuplicationReferenceSet,
) (bool, error) {
	pages, err := pageDuplicationMentionTargetMappings(app, job.ID, referenceIDs(references.pageIDs))
	if err != nil {
		return false, err
	}
	owners := map[string]struct{}{}
	for id := range references.pageIDs {
		if _, mapped := pages[id]; !mapped {
			owners[id] = struct{}{}
		}
	}
	held := []struct {
		Kind   string `db:"kind"`
		Owner  string `db:"owner"`
		IsPage bool   `db:"is_page"`
	}{}
	encoded := func(values map[string]struct{}) string {
		raw, _ := json.Marshal(referenceIDs(values))
		return string(raw)
	}
	// The blocks are found by id; the owner is read after (+), or SQLite,
	// which keeps no statistics, reads the owner's every block.
	if err := app.DB().NewQuery(`
		WITH [[found]]([[kind]],[[owner]]) AS (
			SELECT 'block',[[page_id]] FROM {{leafen_blocks}}
			WHERE +[[owner]]={:ownerId} AND [[id]] IN (SELECT [[value]] FROM json_each({:blockIds}))
				AND [[page_id]]!={:rootId}
			UNION ALL
			SELECT 'data_source',[[page_id]] FROM {{leafen_data_sources}}
			WHERE [[id]] IN (SELECT [[value]] FROM json_each({:dataSourceIds}))
			UNION ALL
			SELECT 'view',[[page_id]] FROM {{leafen_database_views}}
			WHERE [[id]] IN (SELECT [[value]] FROM json_each({:viewIds}))
		)
		SELECT [[found]].[[kind]],[[found]].[[owner]],
			EXISTS (SELECT 1 FROM {{leafen_pages}} WHERE [[id]]=[[found]].[[owner]]) AS [[is_page]]
		FROM [[found]]
	`).Bind(dbx.Params{
		"ownerId": job.sourceOwner(), "rootId": job.SourceRootPageID,
		"blockIds": encoded(references.blockIDs), "dataSourceIds": encoded(references.dataSourceIDs),
		"viewIds": encoded(references.viewIDs),
	}).All(&held); err != nil {
		return false, err
	}
	for _, found := range held {
		// What is held somewhere other than a page (a template's body) may
		// still be the tree's: the top waits its turn.
		if !found.IsPage {
			return false, nil
		}
		// The top's own databases take their IDs with it.
		if found.Owner != job.SourceRootPageID {
			owners[found.Owner] = struct{}{}
		}
	}
	if len(owners) == 0 {
		return true, nil
	}
	var inside int
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[up]] ([[id]],[[parent_id]]) AS (
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `
			FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:pageIds}))
			UNION
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `
			FROM {{leafen_pages}} [[page]]
			JOIN [[up]] [[below]] ON [[page]].[[id]]=[[below]].[[parent_id]]
			WHERE [[below]].[[parent_id]]!=''
		)
		SELECT EXISTS (SELECT 1 FROM [[up]] WHERE [[id]]={:rootId})
	`).Bind(dbx.Params{"pageIds": encoded(owners), "rootId": job.SourceRootPageID}).Row(&inside)
	return inside == 0, err
}

// inventoryPageDuplicationTopFiles takes in the files of the copy's top — its
// blocks', its icon's and cover's — which are copied before its blocks; the
// copy's own file inventory later finds them taken (storePageDuplicationFileMappings).
func inventoryPageDuplicationTopFiles(app core.App, job storedPageDuplicationJob, item storedPageDuplicationItem) error {
	references := []pageDuplicationFileReference{}
	if err := app.DB().NewQuery(`
		SELECT {:sourcePageId} AS [[source_page_id]],{:targetPageId} AS [[target_page_id]],
			[[owner_kind]],[[owner_id]],[[property_id]],[[file_id]] AS [[source_file_id]]
		FROM {{leafen_file_references}} INDEXED BY [[idx_leafen_file_references_owner]]
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:sourcePageId}
		ORDER BY [[owner_kind]],[[owner_id]],[[property_id]],[[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": job.sourceWorkspace(), "sourcePageId": item.SourcePageID, "targetPageId": item.TargetPageID,
	}).All(&references); err != nil {
		return err
	}
	return storePageDuplicationReadableFiles(app, job, references)
}

// nextPageDuplicationTopFile is the next file of the copy's top still to copy
// while the tree is read.
func nextPageDuplicationTopFile(app core.App, job storedPageDuplicationJob) (storedPageDuplicationFile, error) {
	task := storedPageDuplicationFile{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_file_id]],[[target_file_id]],[[source_page_id]],[[target_page_id]],
			[[status]],[[bytes]],[[target_storage_key]]
		FROM {{leafen_page_duplication_files}}
		WHERE [[job_id]]={:jobId} AND [[status]]='pending' AND [[source_page_id]]={:sourcePageId}
		ORDER BY [[source_file_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID, "sourcePageId": job.SourceRootPageID}).One(&task)
	return task, err
}

// pageDuplicationTopOwnsDatabase says whether the copy's top holds a database
// of its own: a data source or a view on it.
func pageDuplicationTopOwnsDatabase(app core.App, job storedPageDuplicationJob) (bool, error) {
	var own int
	err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_data_sources}}
				WHERE [[workspace_id]]={:workspaceId} AND [[owner]]={:ownerId} AND [[page_id]]={:pageId} LIMIT 1)
			OR EXISTS (SELECT 1 FROM {{leafen_database_views}}
				WHERE [[workspace_id]]={:workspaceId} AND [[owner]]={:ownerId} AND [[page_id]]={:pageId} LIMIT 1)
	`).Bind(dbx.Params{
		"workspaceId": job.sourceWorkspace(), "ownerId": job.sourceOwner(), "pageId": job.SourceRootPageID,
	}).Row(&own)
	return own != 0, err
}

// pageCopyTopWritten says whether the copy this page is the top of has written
// its body while the copy still holds it — its own databases to come in their
// turn (pageDuplicationTopHandsOn): a screen open on it reads it again once,
// and shows its text before the copy is whole.
func pageCopyTopWritten(app core.App, page *core.Record) (bool, error) {
	jobID := page.GetString("duplication_target_job_id")
	if jobID == "" {
		return false, nil
	}
	var written int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_duplication_jobs}} [[job]]
		JOIN {{leafen_page_duplication_items}} [[item]]
			ON [[item]].[[job_id]]=[[job]].[[job_id]] AND [[item]].[[source_page_id]]=[[job]].[[source_root_page_id]]
		WHERE [[job]].[[job_id]]={:jobId} AND [[job]].[[target_root_page_id]]={:pageId}
			AND [[job]].[[phase]] IN ('discovering','copying','publishing')
			AND [[item]].[[blocks_copied]]=1 AND [[item]].[[copied]]=0
	`).Bind(dbx.Params{"jobId": jobID, "pageId": page.Id}).Row(&written)
	return written > 0, err
}
