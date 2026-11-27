package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A long copy doesn't keep its whole source from changing, as Notion's doesn't
// (seen 2026-11-05: a page typed into throughout its copy): each page is held
// only while its own windows run. What changed in it since the job mapped the
// tree is taken as it is now: blocks and database entities added since get
// their copies, those gone have none. A page gone from the tree by then, with
// what was under it, is left out, as one in the trash at the start is.

func pageDuplicationHeldPage(app core.App, jobID string) (string, error) {
	var held string
	err := app.DB().NewQuery(`
		SELECT [[held_page_id]] FROM {{leafen_page_duplication_jobs}} WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": jobID}).Row(&held)
	return held, err
}

func holdPageDuplicationItem(app core.App, job storedPageDuplicationJob, item storedPageDuplicationItem) error {
	gone, err := pageDuplicationItemGone(app, job, item)
	if err != nil {
		return err
	}
	if gone {
		return skipPageDuplicationItem(app, job, item)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[held_page_id]]={:pageId}, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='copying'
	`).Bind(dbx.Params{"jobId": job.ID, "pageId": item.SourcePageID, "updatedAt": now}).Execute(); err != nil {
		return err
	}
	// One started before holds were kept goes on as it was. The copy's top
	// that wrote its text first takes the parts its databases came to have
	// since they were mapped then (page_duplication_top_first.go).
	if item.BlockCopyCursor != "" || item.BlocksCopied {
		if item.BlocksCopied && item.SourcePageID == job.SourceRootPageID {
			return remapPageDuplicationDatabaseStates(app, job, item, now)
		}
		return nil
	}
	if err := mapPageDuplicationBlocksSinceMapping(app, job, item, now); err != nil {
		return err
	}
	filesAdded, err := inventoryPageDuplicationFilesSinceMapping(app, job, item, now)
	if err != nil {
		return err
	}
	if err := remapPageDuplicationDatabaseStates(app, job, item, now); err != nil {
		return err
	}
	if item.SourcePageID == job.SourceRootPageID {
		if err := refreshPageDuplicationCopyTop(app, job, item); err != nil {
			return err
		}
	}
	if filesAdded {
		return errPageDuplicationTurnEnds
	}
	return nil
}

// errPageDuplicationTurnEnds has a turn end there, its work done, for what
// runs between turns: files to copy before the page's windows go on.
var errPageDuplicationTurnEnds = errors.New("page duplication turn ends")

// inventoryPageDuplicationFilesSinceMapping takes in the files the page came
// to refer to after the job read them all; they are copied before its windows.
func inventoryPageDuplicationFilesSinceMapping(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
	now int64,
) (bool, error) {
	references := []pageDuplicationFileReference{}
	if err := app.DB().NewQuery(`
		SELECT {:sourcePageId} AS [[source_page_id]],{:targetPageId} AS [[target_page_id]],
			[[reference]].[[owner_kind]],[[reference]].[[owner_id]],[[reference]].[[property_id]],
			[[reference]].[[file_id]] AS [[source_file_id]]
		FROM {{leafen_file_references}} [[reference]]
		WHERE [[reference]].[[workspace_id]]={:workspaceId} AND [[reference]].[[page_id]]={:sourcePageId}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_page_duplication_identity_map}} [[mapped]]
				WHERE [[mapped]].[[job_id]]={:jobId} AND [[mapped]].[[kind]]='file'
					AND [[mapped]].[[source_id]]=[[reference]].[[file_id]]
			)
	`).Bind(dbx.Params{
		"jobId": job.ID, "workspaceId": job.sourceWorkspace(),
		"sourcePageId": item.SourcePageID, "targetPageId": item.TargetPageID,
	}).All(&references); err != nil {
		return false, err
	}
	if len(references) == 0 {
		return false, nil
	}
	if err := storePageDuplicationReadableFiles(app, job, references); err != nil {
		return false, err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_file_state}} SET [[files_complete]]=0,[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": job.ID, "now": now}).Execute()
	return err == nil, err
}

// refreshPageDuplicationCopyTop gives the copy's top page, made when the copy
// was asked for, what it could not have then.
func refreshPageDuplicationCopyTop(app core.App, job storedPageDuplicationJob, item storedPageDuplicationItem) error {
	source, err := app.FindRecordById(pagesCollection, item.SourcePageID)
	if err != nil {
		return err
	}
	target, err := app.FindRecordById(pagesCollection, item.TargetPageID)
	if err != nil {
		return err
	}
	icon, cover, err := remapPageDuplicationPageFiles(app, job.ID, source)
	if err != nil {
		return err
	}
	if icon == target.GetString("icon") && cover == target.GetString("cover") {
		return nil
	}
	target.Set("icon", icon)
	target.Set("icon_type", source.GetString("icon_type"))
	target.Set("cover", cover)
	target.Set("cover_position", source.GetInt("cover_position"))
	if err := app.Save(target); err != nil {
		return err
	}
	return replaceProductFilePageReferences(
		app, job.WorkspaceID, target.Id, icon, source.GetString("icon_type"), cover,
	)
}

func pageDuplicationItemGone(app core.App, job storedPageDuplicationJob, item storedPageDuplicationItem) (bool, error) {
	if item.PageCreated || item.SourcePageID == job.SourceRootPageID {
		return false, nil
	}
	source, err := app.FindRecordById(pagesCollection, item.SourcePageID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if source.GetBool("in_trash") || source.GetString("workspace") != job.sourceWorkspace() ||
		source.GetString("owner") != job.sourceOwner() {
		return true, nil
	}
	if item.TargetParentID == "" {
		return false, nil
	}
	if _, err := app.FindRecordById(pagesCollection, item.TargetParentID); errors.Is(err, sql.ErrNoRows) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	return false, nil
}

func skipPageDuplicationItem(app core.App, job storedPageDuplicationJob, item storedPageDuplicationItem) error {
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_entity_states}}
		SET [[mapped]]=1,[[copied]]=1,[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{"jobId": job.ID, "sourcePageId": item.SourcePageID, "updatedAt": now}).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}}
		SET [[page_created]]=1,[[blocks_mapped]]=1,[[database_mapped]]=1,[[blocks_copied]]=1,
			[[copied]]=1,[[published]]=1,[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{"jobId": job.ID, "sourcePageId": item.SourcePageID, "updatedAt": now}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[copied_count]]=[[copied_count]]+1, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='copying'
	`).Bind(dbx.Params{"jobId": job.ID, "updatedAt": now}).Execute()
	return err
}

func mapPageDuplicationBlocksSinceMapping(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
	now int64,
) error {
	cursor := ""
	for {
		blocks := []pageDuplicationSourceBlock{}
		err := app.DB().NewQuery(`
			SELECT [[block]].[[id]],[[block]].[[type]],[[block]].[[content]] FROM {{leafen_blocks}} [[block]]
			WHERE [[block]].[[owner]]={:owner} AND [[block]].[[page_id]]={:pageId} AND [[block]].[[id]]>{:cursor}
				AND NOT EXISTS (
					SELECT 1 FROM {{leafen_page_duplication_identity_map}} [[mapped]]
					WHERE [[mapped]].[[job_id]]={:jobId} AND [[mapped]].[[kind]]='block'
						AND [[mapped]].[[source_id]]=[[block]].[[id]]
				)
			ORDER BY [[block]].[[id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": job.ID, "owner": job.sourceOwner(), "pageId": item.SourcePageID,
			"cursor": cursor, "limit": maxPageDuplicationItemsPerTurn,
		}).All(&blocks)
		if err != nil {
			return err
		}
		if len(blocks) == 0 {
			return nil
		}
		if err := mapPageDuplicationBlocks(app, job, item, blocks, now); err != nil {
			return err
		}
		cursor = blocks[len(blocks)-1].ID
	}
}

// remapPageDuplicationDatabaseStates has a page's database entities mapped
// again from the start, before they are copied: identities already given
// stay, and a page that has come to hold a database since has its work back.
func remapPageDuplicationDatabaseStates(
	app core.App,
	job storedPageDuplicationJob,
	item storedPageDuplicationItem,
	now int64,
) error {
	var databases int
	if err := app.DB().NewQuery(`
		SELECT MAX(
			EXISTS (SELECT 1 FROM {{leafen_data_sources}} WHERE [[workspace_id]]={:workspaceId}
				AND [[owner]]={:ownerId} AND [[page_id]]={:pageId} LIMIT 1),
			EXISTS (SELECT 1 FROM {{leafen_database_views}} WHERE [[workspace_id]]={:workspaceId}
				AND [[owner]]={:ownerId} AND [[page_id]]={:pageId} LIMIT 1)
		)
	`).Bind(dbx.Params{
		"workspaceId": job.sourceWorkspace(), "ownerId": job.sourceOwner(), "pageId": item.SourcePageID,
	}).Row(&databases); err != nil {
		return err
	}
	if databases == 0 {
		return nil
	}
	if err := initializePageDuplicationDatabaseStates(
		app, job.ID, job.sourceOwner(), job.sourceWorkspace(), item.SourcePageID, item.TargetPageID, now,
	); err != nil {
		return err
	}
	withoutContent, err := durableJobWithoutContent(app, job.ID)
	if err != nil {
		return err
	}
	for _, kind := range pageDuplicationDatabaseKinds {
		if !databaseCopyKindCopied(kind, withoutContent) {
			continue
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_page_duplication_entity_states}}
			SET [[mapped]]=0,[[copied]]=0,[[map_cursor]]='',[[copy_cursor]]='',[[updated_at]]={:updatedAt}
			WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId} AND [[kind]]={:kind}
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourcePageId": item.SourcePageID, "kind": kind, "updatedAt": now,
		}).Execute(); err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}} SET [[database_mapped]]=0,[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId} AND [[copied]]=0
	`).Bind(dbx.Params{"jobId": job.ID, "sourcePageId": item.SourcePageID, "updatedAt": now}).Execute()
	return err
}

// Schema: which page a running copy holds.
