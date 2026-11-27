package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A block copy may take a database of a page's own from a page of another
// workspace: a database page's body copied into another workspace — a page
// moved there takes its databases' rows with their bodies — takes the
// body's own databases with it (database_template_body_databases.go), as
// Notion's copy there takes what the body shows. The job then reads the
// source page in its workspace and writes the copy in the destination's,
// as a page's copy into another workspace does (page_duplication_
// workspaces.go), and hands over what doesn't cross by itself as that copy
// does: the files the database names come as the destination's own copies
// of them, copied before the copy goes on; the workspace emoji it shows come
// as carried ones (page_duplication_custom_emoji.go); a relation to a
// database the copy doesn't take is left out, with the rollups through it;
// people values stay as they are. Copied as a block alone before, the copy
// named the source workspace's database: "데이터베이스를 사용할 수 없습니다".

const (
	blockCopyFilesTable        = "leafen_block_copy_files"
	blockCopyCustomEmojisTable = "leafen_block_copy_custom_emojis"
)

// sourceOwner is the owner of the page a copy reads.
func (job storedBlockMoveJob) sourceOwner() string {
	if job.SourceOwnerID != "" {
		return job.SourceOwnerID
	}
	return job.OwnerID
}

// sourceWorkspace is the workspace of the page a copy reads.
func (job storedBlockMoveJob) sourceWorkspace() string {
	if job.SourceWorkspaceID != "" {
		return job.SourceWorkspaceID
	}
	return job.WorkspaceID
}

// crossWorkspace reports a copy from another workspace.
func (job storedBlockMoveJob) crossWorkspace() bool {
	return job.sourceWorkspace() != job.WorkspaceID
}

// blockCopyCrossesWorkspaces says whether a block may be copied into another
// workspace: a database of its page's own, with no blocks under it.
func blockCopyCrossesWorkspaces(app core.App, source workspaceAuthorization, pageID, blockID string) (bool, error) {
	block := struct {
		Type     string `db:"type"`
		Content  string `db:"content"`
		Children int    `db:"children"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[block]].[[type]], [[block]].[[content]],
			(SELECT COUNT(*) FROM {{leafen_blocks}} AS [[child]]
				WHERE [[child]].[[page_id]] = [[block]].[[page_id]] AND [[child]].[[parent_id]] = [[block]].[[id]]) AS [[children]]
		FROM {{leafen_blocks}} AS [[block]]
		WHERE [[block]].[[owner]] = {:ownerId} AND [[block]].[[workspace_id]] = {:workspaceId}
			AND [[block]].[[page_id]] = {:pageId} AND [[block]].[[id]] = {:blockId}
	`).Bind(dbx.Params{
		"ownerId": source.OwnerID, "workspaceId": source.WorkspaceID, "pageId": pageID, "blockId": blockID,
	}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errBlockMoveNotFound
	}
	if err != nil || block.Type != graphBlockTypeInlineDatabase || block.Children != 0 {
		return false, err
	}
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(block.Content))
	if err != nil {
		return false, nil
	}
	return !content.LinkedDatabaseSource && content.DataSourceID != "", nil
}

// mapBlockCopyFiles names the files the copy's database names as the copy
// names them: the same files within their workspace, which gain references
// from the copy, and copies of them into another workspace, each made before
// the copy goes on (advanceBlockCopyFiles).
func mapBlockCopyFiles(app core.App, job storedBlockMoveJob, fileIDs []string, now int64) error {
	seen := map[string]struct{}{}
	for _, fileID := range fileIDs {
		if _, done := seen[fileID]; done || !workspaceExportFileRecordIDPattern.MatchString(fileID) {
			continue
		}
		seen[fileID] = struct{}{}
		targetID := fileID
		if job.crossWorkspace() {
			if _, err := app.DB().NewQuery(`
				INSERT INTO {{leafen_block_copy_files}}
					([[job_id]],[[source_file_id]],[[target_file_id]],[[status]],[[created_at]],[[updated_at]])
				VALUES ({:jobId},{:fileId},{:targetId},'pending',{:now},{:now})
				ON CONFLICT ([[job_id]],[[source_file_id]]) DO NOTHING
			`).Bind(dbx.Params{
				"jobId": job.ID, "fileId": fileID, "targetId": core.GenerateDefaultRandomId(), "now": now,
			}).Execute(); err != nil {
				return err
			}
			if err := app.DB().NewQuery(`
				SELECT [[target_file_id]] FROM {{leafen_block_copy_files}}
				WHERE [[job_id]]={:jobId} AND [[source_file_id]]={:fileId}
			`).Bind(dbx.Params{"jobId": job.ID, "fileId": fileID}).Row(&targetID); err != nil {
				return err
			}
		}
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_page_duplication_identity_map}}
				([[job_id]],[[kind]],[[source_id]],[[target_id]],[[source_page_id]],[[target_page_id]],[[created_at]])
			VALUES ({:jobId},'file',{:fileId},{:targetId},{:sourcePageId},{:targetPageId},{:now})
		`).Bind(dbx.Params{
			"jobId": job.ID, "fileId": fileID, "targetId": targetID, "sourcePageId": job.SourcePageID,
			"targetPageId": job.DestinationPageID, "now": now,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// advanceBlockCopyFiles copies, for a copy from another workspace under
// way, the next file it names that isn't copied yet, and says whether it
// did. A file the person can no longer read ends the copy, as a page's copy
// ends then.
func advanceBlockCopyFiles(ctx context.Context, app core.App, jobID string) (bool, error) {
	job, err := loadBlockMoveJobByID(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (job.Status != durableJobStatusQueued ||
		job.Phase != blockMovePhaseMoving || !job.crossWorkspace())) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	task := struct {
		SourceID string `db:"source_file_id"`
		TargetID string `db:"target_file_id"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[source_file_id]],[[target_file_id]] FROM {{leafen_block_copy_files}}
		WHERE [[job_id]]={:jobId} AND [[status]]='pending' ORDER BY [[source_file_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID}).One(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	release := acquireProductFileOperationLock(blockMoveJobKind, task.TargetID)
	defer release()
	target, _, err := copyFileIntoWorkspace(
		app, personAccessSubject(job.ActorID), job.ActorID, job.sourceWorkspace(), job.WorkspaceID,
		"block-copy-file-"+job.ID+"-"+task.TargetID, task.SourceID, task.TargetID,
	)
	if errors.Is(err, errPageDuplicationConflict) {
		return true, app.RunInTransaction(func(txApp core.App) error {
			current, err := loadBlockMoveJobByID(txApp, jobID, "")
			if err != nil || current.Status != durableJobStatusQueued || current.Phase != blockMovePhaseMoving {
				return err
			}
			return beginBlockMoveRollback(txApp, current, errorCodeBlockMoveConflict)
		})
	}
	if err != nil {
		return false, err
	}
	return true, app.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_block_copy_files}} SET [[status]]='copied',[[updated_at]]={:now}
			WHERE [[job_id]]={:jobId} AND [[source_file_id]]={:sourceId} AND [[status]]='pending'
		`).Bind(dbx.Params{"jobId": job.ID, "sourceId": task.SourceID, "now": time.Now().UTC().UnixMilli()}).Execute(); err != nil {
			return err
		}
		if err := cancelProductFileReferenceCleanup(ctx, txApp, target.Id); err != nil {
			return err
		}
		// A brought emoji whose picture this is comes now.
		if err := settleCarriedCustomEmojis(
			txApp, blockCopyDuplicationJob(job), blockCopyCustomEmojisTable, blockCopyFilesTable,
		); err != nil {
			return err
		}
		return rescheduleBlockMove(txApp, job)
	})
}

// endBlockCopyFiles lets the files a copy from another workspace made go
// their usual way once it ends: kept while the copy shows them, and the
// rest, a copy that failed among them, taken away after the usual grace.
func endBlockCopyFiles(app core.App, job storedBlockMoveJob) error {
	if !job.crossWorkspace() {
		return nil
	}
	copied := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[target_file_id]] FROM {{leafen_block_copy_files}}
		WHERE [[job_id]]={:jobId} AND [[status]]='copied' ORDER BY [[target_file_id]]
	`).Bind(dbx.Params{"jobId": job.ID}).Column(&copied); err != nil {
		return err
	}
	return reconcileProductFileReferenceLifecycle(app, job.WorkspaceID, copied)
}

// blockCopyCustomEmojiMappings maps, for a block copy from another
// workspace, the workspace emoji of its source that ids name to the carried
// ones it brings; their pictures come as its other files do.
func blockCopyCustomEmojiMappings(app core.App, jobID string, ids map[string]struct{}) (map[string]string, error) {
	job, err := loadBlockMoveJobByID(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !job.crossWorkspace()) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	duplication := blockCopyDuplicationJob(job)
	return carriedCustomEmojiMappings(app, duplication, blockCopyCustomEmojisTable, ids,
		func(emojis []storedCustomEmoji, now int64) error {
			pictures := make([]string, 0, len(emojis))
			for _, emoji := range emojis {
				pictures = append(pictures, emoji.FileID)
			}
			if err := mapBlockCopyFiles(app, job, pictures, now); err != nil {
				return err
			}
			return settleCarriedCustomEmojis(app, duplication, blockCopyCustomEmojisTable, blockCopyFilesTable)
		})
}

// blockCopyLeftOutProperties are the properties of the copied databases a
// copy from another workspace leaves out: relations to data sources it
// doesn't copy, which stay in their workspace, and the rollups through them.
func blockCopyLeftOutProperties(app core.App, job storedBlockMoveJob) (map[string]struct{}, error) {
	if !job.crossWorkspace() {
		return map[string]struct{}{}, nil
	}
	return crossWorkspaceLeftOutProperties(app, job.sourceOwner(), job.sourceWorkspace(), job.SourcePageID,
		func(dataSourceID string) (bool, error) {
			var copied int
			err := app.DB().NewQuery(`
				SELECT EXISTS(SELECT 1 FROM {{leafen_block_move_database_scopes}}
					WHERE [[job_id]]={:jobId} AND [[data_source_id]]={:dataSourceId})
			`).Bind(dbx.Params{"jobId": job.ID, "dataSourceId": dataSourceID}).Row(&copied)
			return copied == 1, err
		})
}
