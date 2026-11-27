package app

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func workspaceImportArchiveCustomEmojiItem(
	db *sql.DB, encoded string,
) (workspaceImportStagedItem, error) {
	value := archiveCustomEmojiValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemCustomEmoji)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	fileID, err := workspaceImportArchiveMarkedFileReference(db, value.FileID)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	name, _, err := normalizeCustomEmojiName(value.Name)
	if err != nil || name != value.Name || value.CreatedAt <= 0 || value.UpdatedAt < value.CreatedAt {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	return workspaceImportStagedItem{CustomEmoji: &workspaceImportStagedCustomEmoji{
		ID: id, Name: name, FileID: fileID,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, Carried: value.Carried,
	}}, nil
}

func appendWorkspaceImportArchiveCustomEmojis(
	w *workspaceImportArchiveChunkWriter, db *sql.DB,
) error {
	rows, err := db.Query(`
		SELECT value_json FROM entities WHERE kind = ? ORDER BY sequence
	`, workspaceExportItemCustomEmoji)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return workspaceImportInputError()
		}
		item, err := workspaceImportArchiveCustomEmojiItem(db, encoded)
		if err != nil {
			return err
		}
		if err := w.append(item); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return workspaceImportStorageError(err)
	}
	return nil
}

func (r *durableJobRunner) applyWorkspaceArchiveCustomEmojis(
	job storedJob,
	payload workspaceImportJobPayload,
	chunk workspaceImportStagedChunk,
	nextOffset int64,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued || current.Cursor != job.Cursor {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ChunkIndex != payload.ChunkIndex || currentPayload.ItemOffset != 0 {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-custom-emoji-frontier")
		}
		authorization, err := authorizeWorkspace(
			txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		emojis, fileIDs, err := validateWorkspaceImportCustomEmojiChunk(chunk)
		if err != nil {
			return err
		}
		files, err := loadGraphFileAuthorityRecords(txApp, authorization.WorkspaceID, fileIDs)
		if err != nil {
			return workspaceImportFailureAt(err, "archive-custom-emoji-file-authority")
		}
		for _, emoji := range emojis {
			if !strings.HasPrefix(strings.ToLower(files[emoji.FileID].ContentType), "image/") {
				return workspaceImportInputError()
			}
			// The emoji a page's archive brings, and those a workspace's
			// carries, come carried (custom_emoji_carried.go).
			if _, err := txApp.DB().Insert(customEmojisTable, dbx.Params{
				"id": emoji.ID, "workspace_id": authorization.WorkspaceID,
				"name": emoji.Name, "name_key": customEmojiNameKeyFor(emoji.ID, emoji.Name, emoji.Carried),
				"file_id": emoji.FileID, "carried": emoji.Carried,
				"created_by": current.Owner, "workspace_import_job_id": current.ID,
				"revision": 1, "created_at": emoji.CreatedAt, "updated_at": emoji.UpdatedAt,
			}).Execute(); err != nil {
				return workspaceImportFailureAt(err, "archive-custom-emoji-insert")
			}
		}
		for _, fileID := range fileIDs {
			if err := reconcileProductFileReferenceLifecycle(
				txApp, authorization.WorkspaceID, []string{fileID},
			); err != nil {
				return workspaceImportFailureAt(err, "archive-custom-emoji-file-lifecycle")
			}
		}
		currentPayload.SourceVerified = true
		currentPayload.ItemOffset = 0
		currentPayload.ChunkIndex++
		currentPayload.ReadyToPublish = chunk.Last
		currentPayload.Phase = workspaceImportPhaseApply
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, strconv.FormatInt(nextOffset, 10),
			current.Processed+len(chunk.Items), true,
		)
	})
}

func validateWorkspaceImportCustomEmojiChunk(
	chunk workspaceImportStagedChunk,
) ([]workspaceImportStagedCustomEmoji, []string, error) {
	emojis := make([]workspaceImportStagedCustomEmoji, len(chunk.Items))
	fileIDs := make([]string, 0, len(chunk.Items))
	seenIDs := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	for index, item := range chunk.Items {
		if item.CustomEmoji == nil || item.Page != nil || item.Comment != nil ||
			item.Template != nil || item.BlockTrash != nil || item.Operation != nil {
			return nil, nil, workspaceImportStateErrorAt("archive-custom-emoji-item")
		}
		emoji := *item.CustomEmoji
		name, _, err := normalizeCustomEmojiName(emoji.Name)
		// A carried emoji's name is no one else's.
		nameKey := customEmojiNameKeyFor(emoji.ID, emoji.Name, emoji.Carried)
		_, duplicateID := seenIDs[emoji.ID]
		_, duplicateName := seenNames[nameKey]
		if err != nil || name != emoji.Name || !validCustomEmojiID(emoji.ID) ||
			!workspaceExportFileRecordIDPattern.MatchString(emoji.FileID) ||
			emoji.CreatedAt <= 0 || emoji.UpdatedAt < emoji.CreatedAt || duplicateID || duplicateName {
			return nil, nil, workspaceImportStateErrorAt("archive-custom-emoji-shape")
		}
		seenIDs[emoji.ID] = struct{}{}
		seenNames[nameKey] = struct{}{}
		emojis[index] = emoji
		fileIDs = append(fileIDs, emoji.FileID)
	}
	return emojis, uniqueWorkspaceImportCommentIDs(fileIDs), nil
}
