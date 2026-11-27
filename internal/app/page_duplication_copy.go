package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageDuplicationSourceBlock struct {
	ID             string `db:"id"`
	ParentID       string `db:"parent_id"`
	TargetID       string `db:"target_id"`
	TargetParentID string `db:"target_parent_id"`
	Position       string `db:"position"`
	Type           string `db:"type"`
	Content        string `db:"content"`
	Created        int64  `db:"created"`
	Updated        int64  `db:"updated"`
}

type pageDuplicationIdentity struct {
	SourceID string `db:"source_id"`
	TargetID string `db:"target_id"`
}

func mapPageDuplicationIdentity(
	app core.App,
	jobID, kind, sourceID, targetID, sourcePageID, targetPageID string,
	createdAt int64,
) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_duplication_identity_map}}
			([[job_id]],[[kind]],[[source_id]],[[target_id]],
			 [[source_page_id]],[[target_page_id]],[[created_at]])
		VALUES ({:jobId},{:kind},{:sourceId},{:targetId},
			{:sourcePageId},{:targetPageId},{:createdAt})
		ON CONFLICT ([[job_id]],[[kind]],[[source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": jobID, "kind": kind, "sourceId": sourceID, "targetId": targetID,
		"sourcePageId": sourcePageID, "targetPageId": targetPageID, "createdAt": createdAt,
	}).Execute()
	return err
}

func copyPageDuplicationWindow(
	app core.App,
	job storedPageDuplicationJob,
) (bool, error) {
	mappingItem, err := nextPageDuplicationBlockMappingItem(app, job.ID)
	if err == nil {
		return false, mapPageDuplicationBlockWindow(app, job, mappingItem)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	databaseMapping, err := nextPageDuplicationDatabaseMappingState(app, job.ID)
	if err == nil {
		return false, mapPageDuplicationDatabaseWindow(app, job, databaseMapping)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	item, err := nextPageDuplicationCopyItem(app, job.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	// The page under the copy's hand holds still for its own windows.
	held, err := pageDuplicationHeldPage(app, job.ID)
	if err != nil {
		return false, err
	}
	if held != item.SourcePageID {
		return false, holdPageDuplicationItem(app, job, item)
	}
	if !item.PageCreated {
		return false, createPageDuplicationTarget(app, job, item)
	}
	if !item.BlocksCopied {
		return false, copyPageDuplicationBlockWindow(app, job, item)
	}
	databaseState, err := nextPageDuplicationDatabaseCopyState(app, job.ID, item.SourcePageID)
	if err == nil {
		return false, copyPageDuplicationDatabaseWindow(app, job, databaseState)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	// The page's databases take their templates once their data is copied.
	if err := copyDatabaseTemplates(
		app, job.ID, job.sourceOwner(), job.sourceWorkspace(), job.OwnerID, job.WorkspaceID, job.ActorID,
		item.SourcePageID, item.TargetPageID,
	); err != nil {
		return false, err
	}
	now := time.Now().UTC().UnixMilli()
	var marked int
	err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}}
		SET [[copied]]=1, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
			AND [[copied]]=0 AND [[page_created]]=1
			AND [[blocks_mapped]]=1 AND [[database_mapped]]=1 AND [[blocks_copied]]=1
		RETURNING 1
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": item.SourcePageID,
		"updatedAt": now,
	}).Row(&marked)
	if err != nil {
		return false, err
	}
	// Done, a page under the copy's top is a page like any: it shows and takes
	// changes while the rest is made, as under Notion's copy (seen 2026-11-05).
	// The top keeps its mark until the end, which says the copy goes on.
	if item.TargetPageID != job.TargetRootPageID {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_pages}} SET [[duplication_target_job_id]]=''
			WHERE [[id]]={:pageId} AND [[duplication_target_job_id]]={:jobId}
		`).Bind(dbx.Params{"pageId": item.TargetPageID, "jobId": job.ID}).Execute(); err != nil {
			return false, err
		}
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[copied_count]]=[[copied_count]]+1, [[held_page_id]]='', [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='copying'
	`).Bind(dbx.Params{"jobId": job.ID, "updatedAt": now}).Execute()
	return false, err
}

func nextPageDuplicationBlockMappingItem(
	app core.App,
	jobID string,
) (storedPageDuplicationItem, error) {
	item := storedPageDuplicationItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_page_id]],[[target_page_id]],[[block_map_cursor]],[[blocks_mapped]]
		FROM {{leafen_page_duplication_items}}
			INDEXED BY [[idx_leafen_page_duplication_item_work]]
		WHERE [[job_id]]={:jobId} AND [[blocks_mapped]]=0 AND [[database_mapped]]=0
		ORDER BY [[depth]],[[source_page_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&item)
	return item, err
}

func nextPageDuplicationCopyItem(
	app core.App,
	jobID string,
) (storedPageDuplicationItem, error) {
	item := storedPageDuplicationItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_page_id]],[[target_page_id]],[[source_parent_id]],
			[[target_parent_id]],[[depth]],[[discovery_cursor]],[[discovered]],
			[[page_created]],[[block_map_cursor]],[[blocks_mapped]],
			[[block_copy_cursor]],[[blocks_copied]],[[copied]],[[published]]
		FROM {{leafen_page_duplication_items}}
		WHERE [[job_id]]={:jobId} AND [[copied]]=0
		ORDER BY [[depth]],[[source_page_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&item)
	return item, err
}

func createPageDuplicationTarget(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
) error {
	source, err := app.FindRecordById(pagesCollection, item.SourcePageID)
	if err != nil || source.GetString("workspace") != job.sourceWorkspace() ||
		source.GetString("owner") != job.sourceOwner() {
		if err != nil {
			return err
		}
		return errPageDuplicationConflict
	}
	pages, err := app.FindCollectionByNameOrId(pagesCollection)
	if err != nil {
		return err
	}
	cursors, err := reservePageCursors(app, job.OwnerID, 1)
	if err != nil {
		return err
	}
	nextCursor, err := cursors.take()
	if err != nil {
		return err
	}
	position := source.GetFloat("position")
	targetTeamspaceID := source.GetString("teamspace_id")
	// A visitor's copy of a published page goes to their private pages too.
	if job.crossWorkspace() || job.SourcePublicLinkID != "" {
		// Another workspace's teamspaces are its own: a copy at its top goes
		// to the teamspace the person chose there, which they must still be
		// able to add to, or is private.
		targetTeamspaceID = ""
		if item.SourcePageID == job.SourceRootPageID && item.TargetParentID == "" {
			if targetTeamspaceID, err = pageDuplicationDestinationTeamspace(app, job.ID); err != nil {
				return err
			}
			if targetTeamspaceID != "" {
				if err := pageDuplicationTeamspaceWritable(app, job.ActorID, job.WorkspaceID, targetTeamspaceID); err != nil {
					return err
				}
			}
		}
	}
	toPrivate := false
	if item.SourcePageID == job.SourceRootPageID && item.TargetParentID == "" && !job.crossWorkspace() {
		if toPrivate, err = pageDuplicationToPrivatePages(app, job.ID); err != nil {
			return err
		}
		if toPrivate {
			targetTeamspaceID = ""
		}
	}
	if item.TargetParentID != "" {
		targetParent, err := app.FindRecordById(pagesCollection, item.TargetParentID)
		if err != nil || targetParent.GetString("workspace") != job.WorkspaceID ||
			targetParent.GetString("owner") != job.OwnerID {
			if err != nil {
				return err
			}
			return errPageDuplicationConflict
		}
		targetTeamspaceID = targetParent.GetString("teamspace_id")
	}
	if item.SourcePageID == job.SourceRootPageID {
		position, err = nextPageSiblingPosition(app, job.WorkspaceID, item.TargetParentID)
		// A copy beside its original stands right after it, as Notion's
		// does (2026-10-25: it stood before it, then at the end).
		if !job.crossWorkspace() && job.SourcePublicLinkID == "" && item.TargetParentID == source.GetString("parent_id") &&
			targetTeamspaceID == source.GetString("teamspace_id") {
			position, err = pageSiblingPositionAfter(app, job.WorkspaceID, item.TargetParentID, source.GetFloat("position"))
		}
		if toPrivate {
			// A copy to private pages heads them, as Notion's does.
			position, err = firstPageSiblingPosition(app, job.WorkspaceID, "")
		}
		if err != nil {
			return err
		}
	}
	target := core.NewRecord(pages)
	target.Id = item.TargetPageID
	target.Set("owner", job.OwnerID)
	target.Set("workspace", job.WorkspaceID)
	target.Set("teamspace_id", targetTeamspaceID)
	title := source.GetString("title")
	if item.SourcePageID == job.SourceRootPageID {
		title = job.Title
	}
	target.Set("title", title)
	titleSpans, err := pageDuplicationTitleSpans(app, job.ID, keptPageTitleSpans(source, title))
	if err != nil {
		return err
	}
	target.Set("title_spans", titleSpans)
	target.Set("content", source.GetString("content"))
	target.Set("revision", 0)
	target.Set("cursor", nextCursor)
	target.Set("parent_id", item.TargetParentID)
	target.Set("position", position)
	target.Set("is_locked", source.GetBool("is_locked"))
	target.Set("locked_by", source.GetString("locked_by"))
	target.Set("kind", source.GetString("kind"))
	applyPageTrashState(target, false, "")
	icon, cover, err := remapPageDuplicationPageFiles(app, job.ID, source)
	if err != nil {
		return err
	}
	target.Set("icon", icon)
	target.Set("icon_type", source.GetString("icon_type"))
	target.Set("cover", cover)
	target.Set("cover_position", source.GetInt("cover_position"))
	target.Set("font", source.GetString("font"))
	target.Set("small_text", source.GetBool("small_text"))
	target.Set("full_width", source.GetBool("full_width"))
	target.Set("backlinks_display", source.GetString("backlinks_display"))
	target.Set("table_of_contents_display", pageTableOfContentsDisplay(source))
	target.Set("inline_comments_display", pageInlineCommentsDisplay(source))
	target.Set("page_comments_display", source.GetString("page_comments_display"))
	target.Set("duplication_target_job_id", job.ID)
	// A sub-page hanging on a line hangs on the line's copy, which mentions
	// the copy as its line does it (mention_anchored_pages.go); one on its
	// parent's title, on the copy's title, and one on a cell, on the copy's
	// cell of its property (mention_anchored_fields.go).
	if anchor := source.GetString(pageAnchorBlockField); anchor != "" && item.SourcePageID != job.SourceRootPageID {
		copied, err := pageDuplicationAnchor(app, job.ID, anchor)
		if err != nil {
			return err
		}
		setCopiedPageAnchor(target, source, copied)
	}
	setNewPageCustodianProvenance(target, job.ActorID)
	if err := app.Save(target); err != nil {
		return err
	}
	quiet, err := pageDuplicationQuietMentions(app, job.ID)
	if err != nil {
		return err
	}
	if !quiet {
		if err := expectPagePersonMentions(app, target.Id, time.Now().UTC().UnixMilli()); err != nil {
			return err
		}
	}
	if err := replaceProductFilePageReferences(
		app, job.WorkspaceID, target.Id, icon, source.GetString("icon_type"), cover,
	); err != nil {
		return err
	}
	if err := recordNotionAdminLegalHoldPageInteraction(app, pageCreatedInteraction(
		job.OrganizationID, job.WorkspaceID, target.Id, job.ActorID,
		time.Now().UTC().UnixMilli(),
	)); err != nil {
		return err
	}
	// A copy at the top says it was made once it is done (placePageDuplicationCopy).
	if !pageDuplicationTopCopy(job, item.SourcePageID, target) {
		if err := recordPageDuplicationMade(app, job, target); err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`
        UPDATE {{leafen_page_duplication_items}}
        SET [[page_created]]=1, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": item.SourcePageID,
		"updatedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func projectPageDuplicationDatabaseContainerBlock(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
	block pageDuplicationSourceBlock,
) error {
	if block.Type != graphBlockTypeInlineDatabase {
		return nil
	}
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(block.Content))
	if err != nil {
		return errPageDuplicationConflict
	}
	return projectDatabaseContainerBlock(
		app, job.OwnerID, job.WorkspaceID, item.TargetPageID,
		block.TargetID, block.TargetParentID, block.Position, content,
	)
}

func mapPageDuplicationBlockWindow(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
) error {
	blocks := []pageDuplicationSourceBlock{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]>{:cursor}
		ORDER BY [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": job.sourceOwner(), "pageId": item.SourcePageID,
		"cursor": item.BlockMapCursor, "limit": maxPageDuplicationItemsPerTurn + 1,
	}).All(&blocks)
	if err != nil {
		return err
	}
	hasMore := len(blocks) > maxPageDuplicationItemsPerTurn
	if hasMore {
		blocks = blocks[:maxPageDuplicationItemsPerTurn]
	}
	now := time.Now().UTC().UnixMilli()
	if err := mapPageDuplicationBlocks(app, job, item, blocks, now); err != nil {
		return err
	}
	nextCursor := item.BlockMapCursor
	if len(blocks) > 0 {
		nextCursor = blocks[len(blocks)-1].ID
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}}
		SET [[block_map_cursor]]={:cursor}, [[blocks_mapped]]={:done}, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": item.SourcePageID,
		"cursor": nextCursor, "done": !hasMore, "updatedAt": now,
	}).Execute()
	return err
}

func mapPageDuplicationBlocks(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
	blocks []pageDuplicationSourceBlock,
	now int64,
) error {
	for _, block := range blocks {
		targetID := uuid.NewString()
		if isStoredChildPageBlock(block.ID, block.Type, block.Content) {
			// A sub-page's block becomes its copy's block, which has the
			// copy's ID; a sub-page not copied leaves no block.
			err := app.DB().NewQuery(`
				SELECT [[target_page_id]] FROM {{leafen_page_duplication_items}}
				WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:pageId}
			`).Bind(dbx.Params{"jobId": job.ID, "pageId": block.ID}).Row(&targetID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
		}
		if err := mapPageDuplicationIdentity(
			app, job.ID, "block", block.ID, targetID,
			item.SourcePageID, item.TargetPageID, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func copyPageDuplicationBlockWindow(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
) error {
	blocks := []pageDuplicationSourceBlock{}
	err := app.DB().NewQuery(`
		SELECT [[block]].[[id]],[[block]].[[parent_id]],
			[[self]].[[target_id]],COALESCE([[parent]].[[target_id]],'') AS [[target_parent_id]],
			[[block]].[[position]],[[block]].[[type]],[[block]].[[content]],
			[[block]].[[created]],[[block]].[[updated]]
		FROM {{leafen_blocks}} [[block]]
		JOIN {{leafen_page_duplication_identity_map}} [[self]]
			ON [[self]].[[job_id]]={:jobId} AND [[self]].[[kind]]='block'
			AND [[self]].[[source_id]]=[[block]].[[id]]
		LEFT JOIN {{leafen_page_duplication_identity_map}} [[parent]]
			ON [[parent]].[[job_id]]={:jobId} AND [[parent]].[[kind]]='block'
			AND [[parent]].[[source_id]]=[[block]].[[parent_id]]
		WHERE [[block]].[[owner]]={:owner} AND [[block]].[[page_id]]={:pageId}
			AND [[block]].[[id]]>{:cursor}
		ORDER BY [[block]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "owner": job.sourceOwner(), "pageId": item.SourcePageID,
		"cursor": item.BlockCopyCursor, "limit": maxPageDuplicationItemsPerTurn + 1,
	}).All(&blocks)
	if err != nil {
		return err
	}
	hasMore := len(blocks) > maxPageDuplicationItemsPerTurn
	if hasMore {
		blocks = blocks[:maxPageDuplicationItemsPerTurn]
	}
	nextCursor := item.BlockCopyCursor
	if len(blocks) > 0 {
		nextCursor = blocks[len(blocks)-1].ID
	}
	// A write that was on its way as the page was taken can leave a block under
	// one with no copy: the copy goes without it rather than stopping.
	blocks = slices.DeleteFunc(blocks, func(block pageDuplicationSourceBlock) bool {
		return block.TargetID == "" || (block.ParentID != "" && block.TargetParentID == "")
	})
	// A linked database of one left in another workspace waits there.
	if err := newPageDuplicationUnlinks(app, job).blocks(blocks); err != nil {
		return err
	}
	if err := remapPageDuplicationBlockContents(app, job.ID, blocks); err != nil {
		return err
	}
	nameDuplicatedDatabaseBlock(app, job, item.SourcePageID, blocks)
	for index := range blocks {
		block := &blocks[index]
		block.Content = pageDuplicationClosedToggle(block.Type, block.Content)
		_, err = app.DB().NewQuery(`
			INSERT INTO {{leafen_blocks}}
				([[id]],[[owner]],[[workspace_id]],[[page_id]],[[parent_id]],[[position]],
				 [[type]],[[content]],[[created_by]],[[last_edited_by]],[[created]],[[updated]])
			VALUES ({:id},{:owner},{:workspaceId},{:pageId},{:parentId},{:position},
				{:type},{:content},{:actorId},{:actorId},{:created},{:updated})
			ON CONFLICT ([[id]]) DO NOTHING
		`).Bind(dbx.Params{
			"id": block.TargetID, "owner": job.OwnerID, "workspaceId": job.WorkspaceID,
			"pageId": item.TargetPageID, "parentId": block.TargetParentID,
			"position": block.Position, "type": block.Type, "content": block.Content,
			"actorId": job.ActorID, "created": block.Created, "updated": block.Updated,
		}).Execute()
		if err != nil {
			return err
		}
		if err := recordWorkspaceCopiedBlock(app, job.ActorID, job.ID, item.TargetPageID, block.TargetID, block.Type, time.Now().UTC().UnixMilli(), false); err != nil {
			return err
		}
		if err := projectPageDuplicationDatabaseContainerBlock(app, job, item, *block); err != nil {
			return err
		}
	}
	if err := recordPageDuplicationBlockFileReferences(
		app, job, item.TargetPageID, blocks,
	); err != nil {
		return err
	}
	if err := copyPageDuplicationDocumentsWindow(app, job, item, blocks); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}}
		SET [[block_copy_cursor]]={:cursor}, [[blocks_copied]]={:done}, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": item.SourcePageID,
		"cursor": nextCursor, "done": !hasMore, "updatedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func publishPageDuplicationTargets(
	app core.App,
	job storedPageDuplicationJob,
) (bool, error) {
	items := []storedPageDuplicationItem{}
	err := app.DB().NewQuery(`
		SELECT [[source_page_id]],[[target_page_id]]
		FROM {{leafen_page_duplication_items}}
		WHERE [[job_id]]={:jobId} AND [[copied]]=1 AND [[published]]=0
		ORDER BY [[depth]],[[source_page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "limit": maxPageDuplicationItemsPerTurn,
	}).All(&items)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return true, nil
	}
	now := time.Now().UTC().UnixMilli()
	targets := make([]searchDirtyTarget, 0, len(items))
	sourceIDs := make([]string, 0, len(items))
	for _, item := range items {
		targets = append(targets, searchDirtyTarget{
			PageID: item.TargetPageID, WorkspaceID: job.WorkspaceID,
		})
		sourceIDs = append(sourceIDs, item.SourcePageID)
	}
	if err := markSearchDirtyTargets(context.Background(), app, targets); err != nil {
		return false, err
	}
	for _, item := range items {
		if item.SourcePageID != job.SourceRootPageID {
			continue
		}
		if err := placePageDuplicationCopy(app, job, item); err != nil {
			return false, err
		}
	}
	if err := copyPageDuplicationTaskFeatures(app, job, items); err != nil {
		return false, err
	}
	params, placeholders := pageDuplicationIDParams("sourcePage", sourceIDs)
	params["jobId"] = job.ID
	params["updatedAt"] = now
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}}
		SET [[published]]=1, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]] IN (` + joinSQL(placeholders) + `)
			AND [[copied]]=1 AND [[published]]=0
	`).Bind(params).Execute()
	if err != nil {
		return false, err
	}
	return false, nil
}

// pageDuplicationClosedToggle starts a copy's toggles closed, as Notion's are
// (seen 2026-10-31): which toggles are open is how a reader left the
// original, not part of what is copied.
func pageDuplicationClosedToggle(blockType, content string) string {
	if !isToggleBlockType(blockType) {
		return content
	}
	fields := map[string]any{}
	if err := json.Unmarshal([]byte(content), &fields); err != nil {
		return content
	}
	if !closeCopiedToggle(blockType, fields) {
		return content
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return content
	}
	return string(encoded)
}

// closeCopiedToggle closes a copied toggle's or toggle heading's content in
// place, as every copy's starts — a page's, a block's duplicate or paste, a
// button's inserted blocks (Notion's, seen 2026-10-31 and 2026-11-13, open
// or closed in what was copied) — and says whether it changed it.
func closeCopiedToggle(blockType string, content map[string]any) bool {
	if !isToggleBlockType(blockType) || content == nil {
		return false
	}
	if collapsed, _ := content["collapsed"].(bool); collapsed {
		return false
	}
	content["collapsed"] = true
	return true
}

func isToggleBlockType(blockType string) bool {
	return blockType == graphBlockTypeToggle || strings.HasPrefix(blockType, "toggle_heading_")
}
