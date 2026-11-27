package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceImportCommentOwner struct {
	ID     string `db:"id"`
	PageID string `db:"page_id"`
}

type workspaceImportCommentParentOwner struct {
	ID         string `db:"id"`
	TargetKind string `db:"target_kind"`
	TargetID   string `db:"target_id"`
}

func (r *durableJobRunner) applyWorkspaceArchiveComments(
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
			return workspaceImportStateErrorAt("archive-comment-frontier")
		}
		authorization, err := authorizeWorkspace(
			txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		comments, attachments, err := validateWorkspaceImportCommentChunk(
			txApp, current, authorization, chunk,
		)
		if err != nil {
			return err
		}
		allFileIDs := []string{}
		for _, ids := range attachments {
			allFileIDs = append(allFileIDs, ids...)
		}
		if len(allFileIDs) > 0 {
			if _, err := loadGraphFileAuthorityRecords(
				txApp, authorization.WorkspaceID, uniqueWorkspaceImportCommentIDs(allFileIDs),
			); err != nil {
				return workspaceImportFailureAt(err, "archive-comment-file-authority")
			}
		}
		now := r.clock.now().UTC().Format(time.RFC3339Nano)
		for index, comment := range comments {
			createdAt := comment.CreatedAt
			if createdAt == "" {
				createdAt = now
			}
			discussionID, err := notionImportedCommentDiscussionID(
				txApp, authorization.WorkspaceID, comment,
			)
			if err != nil {
				return workspaceImportFailureAt(err, "archive-comment-discussion-identity")
			}
			if _, err := txApp.DB().Insert(commentsTable, dbx.Params{
				"id": comment.ID, "workspace_id": authorization.WorkspaceID,
				"page_id": comment.PageID, "block_id": comment.BlockID,
				"target_kind": comment.TargetKind, "target_id": comment.TargetID,
				"parent_id": comment.ParentID, "author_id": firstNonEmpty(comment.AuthorID, current.Owner),
				"body_json": string(comment.Body), "resolved": comment.Resolved,
				"notion_discussion_id":    discussionID,
				"workspace_import_job_id": current.ID, "created_at": createdAt,
				"updated_at": now, "edited_at": comment.EditedAt,
			}).Execute(); err != nil {
				return workspaceImportFailureAt(err, "archive-comment-insert")
			}
			if _, err := ensureNotionObjectIdentity(
				txApp, notionObjectIdentityComment, authorization.WorkspaceID, comment.ID,
			); err != nil {
				return workspaceImportFailureAt(err, "archive-comment-public-identity")
			}
			if err := ensureNativeCommentTargetNotionIdentities(txApp, storedComment{
				WorkspaceID: authorization.WorkspaceID, TargetKind: comment.TargetKind,
				TargetID: comment.TargetID, BlockID: comment.BlockID,
			}); err != nil {
				return workspaceImportFailureAt(err, "archive-comment-target-identity")
			}
			if len(attachments[index]) > 0 {
				items := make(graphFilesValue, len(attachments[index]))
				for itemIndex, fileID := range attachments[index] {
					items[itemIndex] = graphFileValueItem{FileID: fileID}
				}
				if err := insertProductFileReferences(
					txApp, authorization.WorkspaceID, comment.PageID,
					productFileReferenceOwnerComment, comment.ID, "", items,
				); err != nil {
					return workspaceImportFailureAt(err, "archive-comment-file-reference")
				}
			}
			if err := insertWorkspaceImportCommentReactions(txApp, current.Owner, comment); err != nil {
				return workspaceImportFailureAt(err, "archive-comment-reaction")
			}
		}
		commentPageIDs := make([]string, 0, len(comments))
		for _, comment := range comments {
			commentPageIDs = append(commentPageIDs, comment.PageID)
		}
		for _, pageID := range uniqueWorkspaceImportCommentIDs(commentPageIDs) {
			if err := advanceCommentPageRevision(txApp, pageID); err != nil {
				return workspaceImportFailureAt(err, "archive-comment-page-revision")
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

func validateWorkspaceImportCommentChunk(
	app core.App,
	job storedJob,
	authorization workspaceAuthorization,
	chunk workspaceImportStagedChunk,
) ([]workspaceImportStagedComment, [][]string, error) {
	comments := make([]workspaceImportStagedComment, len(chunk.Items))
	pageIDs := []string{}
	blockIDs := []string{}
	rowBlockIDs := []string{}
	rowIDs := []string{}
	parentIDs := []string{}
	attachments := make([][]string, len(chunk.Items))
	seen := map[string]struct{}{}
	for index, item := range chunk.Items {
		if item.Comment == nil || item.Page != nil || item.Operation != nil ||
			item.PageID == "" || item.PageID != item.Comment.PageID {
			return nil, nil, workspaceImportStateErrorAt("archive-comment-item")
		}
		comment := *item.Comment
		if comment.TargetKind == "" {
			comment.TargetKind, comment.TargetID = commentTargetPage, comment.PageID
		}
		if _, duplicate := seen[comment.ID]; duplicate || !validWorkspaceImportStagedComment(comment) {
			return nil, nil, workspaceImportStateErrorAt("archive-comment-shape")
		}
		seen[comment.ID] = struct{}{}
		comments[index] = comment
		pageIDs = append(pageIDs, comment.PageID)
		if comment.BlockID != "" {
			if comment.TargetKind == commentTargetDatabaseRow {
				rowBlockIDs = append(rowBlockIDs, comment.BlockID)
			} else {
				blockIDs = append(blockIDs, comment.BlockID)
			}
		}
		if comment.TargetKind == commentTargetDatabaseRow {
			rowIDs = append(rowIDs, comment.TargetID)
		}
		if comment.ParentID != "" {
			parentIDs = append(parentIDs, comment.ParentID)
		}
		ids, err := workspaceImportCommentAttachmentIDs(comment.Body)
		if err != nil {
			return nil, nil, err
		}
		attachments[index] = ids
	}
	if err := validateWorkspaceImportCommentPages(
		app, job, authorization, uniqueWorkspaceImportCommentIDs(pageIDs),
	); err != nil {
		return nil, nil, err
	}
	blocks, err := loadWorkspaceImportCommentOwners(
		app, "leafen_blocks", "[[owner]]={:owner} AND [[workspace_id]]={:workspaceId}",
		uniqueWorkspaceImportCommentIDs(blockIDs),
		dbx.Params{"owner": authorization.OwnerID, "workspaceId": authorization.WorkspaceID},
	)
	if err != nil || len(blocks) != len(uniqueWorkspaceImportCommentIDs(blockIDs)) {
		return nil, nil, workspaceImportStateErrorAt("archive-comment-block-owner")
	}
	// A database page's blocks are its row page's blocks.
	rowBlocks, err := loadWorkspaceImportCommentTargetOwners(
		app, "leafen_blocks", "page_id",
		"[[workspace_id]]={:workspaceId}",
		uniqueWorkspaceImportCommentIDs(rowBlockIDs),
		dbx.Params{"workspaceId": authorization.WorkspaceID},
	)
	if err != nil || len(rowBlocks) != len(uniqueWorkspaceImportCommentIDs(rowBlockIDs)) {
		return nil, nil, workspaceImportStateErrorAt("archive-comment-row-block-owner")
	}
	rows, err := loadWorkspaceImportCommentTargetOwners(
		app, "leafen_database_rows", "page_id",
		"[[owner]]={:owner} AND [[workspace_id]]={:workspaceId}",
		uniqueWorkspaceImportCommentIDs(rowIDs),
		dbx.Params{"owner": authorization.OwnerID, "workspaceId": authorization.WorkspaceID},
	)
	if err != nil || len(rows) != len(uniqueWorkspaceImportCommentIDs(rowIDs)) {
		return nil, nil, workspaceImportStateErrorAt("archive-comment-row-owner")
	}
	parents, err := loadWorkspaceImportCommentParents(
		app, job.ID, uniqueWorkspaceImportCommentIDs(parentIDs),
	)
	if err != nil {
		return nil, nil, err
	}
	ready := parents
	for _, comment := range comments {
		if comment.TargetKind == commentTargetPage && comment.TargetID != comment.PageID {
			return nil, nil, workspaceImportStateErrorAt("archive-comment-page-target")
		}
		if comment.TargetKind == commentTargetDatabaseRow && rows[comment.TargetID].PageID != comment.PageID {
			return nil, nil, workspaceImportStateErrorAt("archive-comment-row-target")
		}
		if comment.TargetKind == commentTargetPage && comment.BlockID != "" &&
			blocks[comment.BlockID].PageID != comment.TargetID {
			return nil, nil, workspaceImportStateErrorAt("archive-comment-block-page")
		}
		if comment.TargetKind == commentTargetDatabaseRow && comment.BlockID != "" &&
			rowBlocks[comment.BlockID].PageID != comment.TargetID {
			return nil, nil, workspaceImportStateErrorAt("archive-comment-row-block")
		}
		if comment.ParentID != "" && (ready[comment.ParentID].TargetKind != comment.TargetKind ||
			ready[comment.ParentID].TargetID != comment.TargetID) {
			return nil, nil, workspaceImportStateErrorAt("archive-comment-parent")
		}
		ready[comment.ID] = workspaceImportCommentParentOwner{
			ID: comment.ID, TargetKind: comment.TargetKind, TargetID: comment.TargetID,
		}
	}
	return comments, attachments, nil
}

// insertWorkspaceImportCommentReactions puts back an imported comment's
// reactions, in the order they were used, and makes the exporting person's
// state of its thread the importing person's.
func insertWorkspaceImportCommentReactions(app core.App, ownerID string, comment workspaceImportStagedComment) error {
	base := time.Now().UTC()
	for index, reaction := range comment.Reactions {
		if _, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_comment_reactions}} ([[comment_id]],[[user_id]],[[emoji]],[[created_at]])
			VALUES ({:commentId},{:userId},{:emoji},{:at})
		`).Bind(dbx.Params{
			"commentId": comment.ID, "userId": reaction.UserID, "emoji": reaction.Emoji,
			"at": base.Add(time.Duration(index) * time.Microsecond).Format("2006-01-02T15:04:05.000000000Z07:00"),
		}).Execute(); err != nil {
			return err
		}
	}
	state := comment.ThreadState
	if state == nil || comment.ParentID != "" || (!state.RepliesMuted && !state.Unread) {
		return nil
	}
	_, err := app.DB().Insert(commentThreadStatesTable, dbx.Params{
		"comment_id": comment.ID, "user_id": ownerID, "replies_muted": state.RepliesMuted,
		"marked_unread": state.Unread, "updated_at": base.Format(time.RFC3339Nano),
	}).Execute()
	return err
}

// validWorkspaceImportCommentReactions accepts at most a comment's reactions
// of every person: one emoji each, at most as many per person as a person
// may use.
func validWorkspaceImportCommentReactions(reactions []workspaceImportArchiveReaction) bool {
	seen := map[workspaceImportArchiveReaction]struct{}{}
	for _, reaction := range reactions {
		if _, duplicate := seen[reaction]; duplicate || !validCommentIdentifier(reaction.UserID) ||
			!validCommentReactionEmoji(reaction.Emoji) {
			return false
		}
		seen[reaction] = struct{}{}
	}
	return true
}

// validWorkspaceImportStagedComment takes the IDs a comment is written
// with: a moved workspace keeps them as they were made (a UUID, a row page's
// row-…), not a record's 15 characters (2026-11-03 audit: a page comment
// turned a workspace's move away).
func validWorkspaceImportStagedComment(comment workspaceImportStagedComment) bool {
	return validWorkspaceImportCommentReactions(comment.Reactions) &&
		validCommentIdentifier(comment.ID) &&
		validCommentIdentifier(comment.PageID) &&
		(comment.TargetKind == commentTargetPage || comment.TargetKind == commentTargetDatabaseRow) &&
		validCommentIdentifier(comment.TargetID) &&
		(comment.BlockID == "" || validCommentIdentifier(comment.BlockID)) &&
		(comment.ParentID == "" || validCommentIdentifier(comment.ParentID)) &&
		len(comment.Body) > 0 && len(comment.Body) <= workspaceImportArchiveEntityJSONBytes &&
		json.Valid(comment.Body)
}

func loadWorkspaceImportCommentTargetOwners(
	app core.App,
	table string,
	pageColumn string,
	condition string,
	ids []string,
	params dbx.Params,
) (map[string]workspaceImportCommentOwner, error) {
	result := map[string]workspaceImportCommentOwner{}
	if len(ids) == 0 {
		return result, nil
	}
	list := workspaceImportCommentQueryList(params, "targetOwner", ids)
	rows := []workspaceImportCommentOwner{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[` + pageColumn + `]] AS [[page_id]] FROM {{` + table + `}}
		WHERE ` + condition + ` AND [[id]] IN (` + list + `)
	`).Bind(params).All(&rows)
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, err
}

func loadWorkspaceImportCommentParents(
	app core.App, jobID string, ids []string,
) (map[string]workspaceImportCommentParentOwner, error) {
	result := map[string]workspaceImportCommentParentOwner{}
	if len(ids) == 0 {
		return result, nil
	}
	params := dbx.Params{"jobId": jobID}
	list := workspaceImportCommentQueryList(params, "parent", ids)
	rows := []workspaceImportCommentParentOwner{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[target_kind]],[[target_id]] FROM {{leafen_comments}}
		WHERE [[workspace_import_job_id]]={:jobId} AND [[id]] IN (` + list + `)
	`).Bind(params).All(&rows)
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, err
}

func validateWorkspaceImportCommentPages(
	app core.App,
	job storedJob,
	authorization workspaceAuthorization,
	pageIDs []string,
) error {
	params := dbx.Params{
		"jobId": job.ID, "owner": authorization.OwnerID, "workspaceId": authorization.WorkspaceID,
	}
	list := workspaceImportCommentQueryList(params, "page", pageIDs)
	var count int
	if len(pageIDs) == 0 || app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_workspace_archive_import_pages}} AS [[owned]]
			ON [[owned]].[[page_id]]=[[page]].[[id]]
		WHERE [[owned]].[[job_id]]={:jobId} AND [[page]].[[id]] IN (`+list+`)
			AND [[page]].[[owner]]={:owner} AND [[page]].[[workspace]]={:workspaceId}
			AND [[page]].[[notion_import_staging]]=TRUE
	`).Bind(params).Row(&count) != nil || count != len(pageIDs) {
		return workspaceImportStateErrorAt("archive-comment-page-owner")
	}
	return nil
}

func loadWorkspaceImportCommentOwners(
	app core.App,
	table string,
	condition string,
	ids []string,
	params dbx.Params,
) (map[string]workspaceImportCommentOwner, error) {
	result := map[string]workspaceImportCommentOwner{}
	if len(ids) == 0 {
		return result, nil
	}
	list := workspaceImportCommentQueryList(params, "owner", ids)
	rows := []workspaceImportCommentOwner{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]] FROM {{` + table + `}}
		WHERE ` + condition + ` AND [[id]] IN (` + list + `)
	`).Bind(params).All(&rows)
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, err
}

func workspaceImportCommentQueryList(params dbx.Params, prefix string, values []string) string {
	bindings := make([]string, len(values))
	for index, value := range values {
		key := fmt.Sprintf("%s%d", prefix, index)
		params[key] = value
		bindings[index] = "{:" + key + "}"
	}
	return strings.Join(bindings, ",")
}

func uniqueWorkspaceImportCommentIDs(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func workspaceImportCommentAttachmentIDs(body json.RawMessage) ([]string, error) {
	value := any(nil)
	if json.Unmarshal(body, &value) != nil {
		return nil, workspaceImportStateErrorAt("archive-comment-body")
	}
	ids := []string{}
	var walk func(any, string) error
	walk = func(current any, field string) error {
		if field == "attachmentIds" {
			items, ok := current.([]any)
			if !ok {
				return workspaceImportStateErrorAt("archive-comment-attachments")
			}
			for _, item := range items {
				id, ok := item.(string)
				if !ok || !workspaceExportFileRecordIDPattern.MatchString(id) {
					return workspaceImportStateErrorAt("archive-comment-attachment")
				}
				ids = append(ids, id)
			}
			return nil
		}
		switch typed := current.(type) {
		case []any:
			for _, child := range typed {
				if err := walk(child, field); err != nil {
					return err
				}
			}
		case map[string]any:
			for key, child := range typed {
				if err := walk(child, key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value, ""); err != nil {
		return nil, err
	}
	return uniqueWorkspaceImportCommentIDs(ids), nil
}
