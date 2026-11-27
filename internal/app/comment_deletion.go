package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedCommentDeletion struct {
	ID            string `db:"id"`
	ActorID       string `db:"actor_id"`
	WorkspaceID   string `db:"workspace_id"`
	PageID        string `db:"page_id"`
	RootCommentID string `db:"root_comment_id"`
	Status        string `db:"status"`
	DeletedCount  int    `db:"deleted_count"`
	Revision      int    `db:"revision"`
	CreatedAt     string `db:"created_at"`
	UpdatedAt     string `db:"updated_at"`
	CompletedAt   string `db:"completed_at"`
}

type storedCommentDeletionFrontier struct {
	CommentID          string `db:"comment_id"`
	ChildCursorCreated string `db:"child_cursor_created"`
	ChildCursorID      string `db:"child_cursor_id"`
	Discovered         bool   `db:"discovered"`
	Deleted            bool   `db:"deleted"`
	Ordinal            int    `db:"ordinal"`
}

type commentDeletionChild struct {
	ID        string `db:"id"`
	CreatedAt string `db:"created_at"`
}

func commentDeletionID(actorID, requestID string) string {
	digest := sha256.Sum256([]byte("comment-delete\x00" + actorID + "\x00" + requestID))
	return hex.EncodeToString(digest[:16])
}

func renderCommentDeletion(row storedCommentDeletion) commentDeletionRecord {
	return commentDeletionRecord{
		ID: row.ID, PageID: row.PageID, RootCommentID: row.RootCommentID,
		Status: row.Status, DeletedCount: row.DeletedCount, Revision: row.Revision,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, CompletedAt: row.CompletedAt,
	}
}

// errCommentDeletionMissing is a deletion job no page has: a comment's
// refusal to the screen, a job not found to its route.
var errCommentDeletionMissing = fmt.Errorf("%w: no such comment deletion", errCommentNotFound)

func loadCommentDeletion(app core.App, pageID, jobID string) (storedCommentDeletion, error) {
	var row storedCommentDeletion
	err := app.DB().NewQuery(`
		SELECT [[id]],[[actor_id]],[[workspace_id]],[[page_id]],[[root_comment_id]],
			[[status]],[[deleted_count]],[[revision]],[[created_at]],[[updated_at]],[[completed_at]]
		FROM {{leafen_comment_deletion_jobs}} WHERE [[page_id]]={:pageId} AND [[id]]={:jobId}
	`).Bind(dbx.Params{"pageId": pageID, "jobId": jobID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedCommentDeletion{}, errCommentDeletionMissing
	}
	return row, err
}

func nextCommentDeletionFrontier(
	app core.App, jobID string,
) (storedCommentDeletionFrontier, bool, error) {
	var row storedCommentDeletionFrontier
	err := app.DB().NewQuery(`
		SELECT [[comment_id]],[[child_cursor_created]],[[child_cursor_id]],
			[[discovered]],[[deleted]],[[ordinal]]
		FROM {{leafen_comment_deletion_frontier}} INDEXED BY [[idx_leafen_comment_deletion_frontier_undeleted]]
		WHERE [[job_id]]={:jobId} AND [[deleted]]=0
		ORDER BY [[ordinal]],[[comment_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&row)
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storedCommentDeletionFrontier{}, false, err
	}
	err = app.DB().NewQuery(`
		SELECT [[comment_id]],[[child_cursor_created]],[[child_cursor_id]],
			[[discovered]],[[deleted]],[[ordinal]]
		FROM {{leafen_comment_deletion_frontier}} INDEXED BY [[idx_leafen_comment_deletion_frontier_undiscovered]]
		WHERE [[job_id]]={:jobId} AND [[discovered]]=0
		ORDER BY [[ordinal]],[[comment_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedCommentDeletionFrontier{}, false, nil
	}
	return row, err == nil, err
}

func discoverCommentDeletionChildren(
	app core.App,
	job storedCommentDeletion,
	item storedCommentDeletionFrontier,
	nextOrdinal *int,
) (bool, error) {
	children := []commentDeletionChild{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[created_at]] FROM {{leafen_comments}} INDEXED BY [[idx_leafen_comments_page_thread]]
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[parent_id]]={:parentId}
			AND ({:afterCreated}='' OR [[created_at]]>{:afterCreated}
				OR ([[created_at]]={:afterCreated} AND [[id]]>{:afterId}))
		ORDER BY [[created_at]],[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": job.WorkspaceID, "pageId": job.PageID, "parentId": item.CommentID,
		"afterCreated": item.ChildCursorCreated, "afterId": item.ChildCursorID,
		"limit": commentDeleteTurnSize,
	}).All(&children)
	if err != nil {
		return false, err
	}
	for _, child := range children {
		_, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_comment_deletion_frontier}}
				([[job_id]],[[comment_id]],[[ordinal]])
			VALUES ({:jobId},{:commentId},{:ordinal})
		`).Bind(dbx.Params{
			"jobId": job.ID, "commentId": child.ID, "ordinal": *nextOrdinal,
		}).Execute()
		if err != nil {
			return false, err
		}
		*nextOrdinal++
	}
	discovered := len(children) < commentDeleteTurnSize
	afterCreated, afterID := item.ChildCursorCreated, item.ChildCursorID
	if len(children) > 0 {
		afterCreated, afterID = children[len(children)-1].CreatedAt, children[len(children)-1].ID
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_comment_deletion_frontier}}
		SET [[child_cursor_created]]={:afterCreated},[[child_cursor_id]]={:afterId},[[discovered]]={:discovered}
		WHERE [[job_id]]={:jobId} AND [[comment_id]]={:commentId}
	`).Bind(dbx.Params{
		"afterCreated": afterCreated, "afterId": afterID, "discovered": discovered,
		"jobId": job.ID, "commentId": item.CommentID,
	}).Execute()
	return discovered, err
}

// processCommentDeletionTurn goes on with a deletion begun before a
// comment's replies stayed in its discussion (comment_thread_handover.go),
// which took a comment's replies with it, a turn of them at a time; one
// begun since is complete when it begins.
func processCommentDeletionTurn(app core.App, job *storedCommentDeletion) error {
	if job.Status == commentDeletionStatusComplete {
		return nil
	}
	var nextOrdinal int
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[ordinal]]),-1)+1 FROM {{leafen_comment_deletion_frontier}}
		INDEXED BY [[idx_leafen_comment_deletion_frontier_next_ordinal]]
		WHERE [[job_id]]={:jobId}
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&nextOrdinal); err != nil {
		return err
	}
	deletedThisTurn := 0
	for deletedThisTurn < commentDeleteTurnSize {
		item, exists, err := nextCommentDeletionFrontier(app, job.ID)
		if err != nil {
			return err
		}
		if !exists {
			break
		}
		if !item.Discovered {
			if _, err := discoverCommentDeletionChildren(app, *job, item, &nextOrdinal); err != nil {
				return err
			}
		}
		if item.Deleted {
			continue
		}
		if err := deleteProductFileCommentReferences(
			app, job.WorkspaceID, job.PageID, item.CommentID,
		); err != nil {
			return err
		}
		// What stays of a deleted comment (comment_tombstones.go).
		if err := keepCommentTombstone(
			app, job.WorkspaceID, job.PageID, item.CommentID, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
		result, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_comments}} WHERE [[page_id]]={:pageId} AND [[id]]={:commentId}
		`).Bind(dbx.Params{"pageId": job.PageID, "commentId": item.CommentID}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err == nil && changed > 0 {
			err = keepDeletedCommentPublicID(app, job.WorkspaceID, item.CommentID)
		}
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_comment_deletion_frontier}} SET [[deleted]]=1
			WHERE [[job_id]]={:jobId} AND [[comment_id]]={:commentId}
		`).Bind(dbx.Params{"jobId": job.ID, "commentId": item.CommentID}).Execute(); err != nil {
			return err
		}
		if changed > 0 {
			deletedThisTurn++
		}
	}
	var pending int
	if err := app.DB().NewQuery(`
		SELECT
			EXISTS(SELECT 1 FROM {{leafen_comment_deletion_frontier}}
				WHERE [[job_id]]={:jobId} AND [[deleted]]=0 LIMIT 1)
			+ EXISTS(SELECT 1 FROM {{leafen_comment_deletion_frontier}}
				WHERE [[job_id]]={:jobId} AND [[discovered]]=0 LIMIT 1)
	`).Bind(dbx.Params{"jobId": job.ID}).Row(&pending); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	job.DeletedCount += deletedThisTurn
	job.Revision++
	job.UpdatedAt = now
	if pending == 0 {
		job.Status, job.CompletedAt = commentDeletionStatusComplete, now
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_comment_deletion_jobs}}
		SET [[status]]={:status},[[deleted_count]]={:deletedCount},[[revision]]={:revision},
			[[updated_at]]={:updatedAt},[[completed_at]]={:completedAt}
		WHERE [[id]]={:jobId}
	`).Bind(dbx.Params{
		"status": job.Status, "deletedCount": job.DeletedCount, "revision": job.Revision,
		"updatedAt": job.UpdatedAt, "completedAt": job.CompletedAt, "jobId": job.ID,
	}).Execute()
	if err == nil && deletedThisTurn > 0 {
		err = advanceCommentPageRevision(app, job.PageID)
	}
	return err
}

func deleteCommentRecord(
	app core.App, actorID, pageID, commentID string, input commentDeleteInput,
) (commentDeletionResponse, error) {
	return deleteCommentRecordForTarget(
		app, actorID, nativePageCommentTarget(pageID), pageID, commentID, input,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeCommentPage(txApp, actorID, pageID, permissionWorkspaceWrite, "")
		},
	)
}

// deleteCommentRecordForTarget deletes a comment, its replies staying in
// their discussion (deleteCommentKeepingReplies). Its deletion record, done
// at once, belongs to the target's storage page, whose deletion routes read
// it and continue deletions begun before replies stayed; requestKey keys the
// request's idempotency.
func deleteCommentRecordForTarget(
	app core.App,
	actorID string,
	target nativeCommentTarget,
	requestKey, commentID string,
	input commentDeleteInput,
	authorize func(core.App) (commentAuthorization, error),
) (commentDeletionResponse, error) {
	input.RequestID, commentID = strings.TrimSpace(input.RequestID), strings.TrimSpace(commentID)
	if !validCommentIdentifier(commentID) {
		return commentDeletionResponse{}, errCommentInvalid
	}
	if problem := firstNonempty(commentIdentifierProblem("requestId", input.RequestID), commentRevisionProblem(input.ExpectedRevision)); problem != "" {
		return commentDeletionResponse{}, commentInvalid("%s", problem)
	}
	pageID := target.StoragePageID
	hash, err := commentRequestHash("delete", requestKey, commentID, input)
	if err != nil {
		return commentDeletionResponse{}, err
	}
	response := commentDeletionResponse{}
	var handover commentHandover
	var handedOver bool
	err = app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err == nil && authorization.Target != target {
			return permissionDenied()
		}
		if errors.Is(err, errCommentDeleteBusy) {
			var receiptResponse commentDeletionResponse
			if replay, receiptErr := readCommentReceipt(txApp, actorID, input.RequestID, hash, &receiptResponse); receiptErr != nil || replay {
				if receiptErr != nil {
					return receiptErr
				}
				job, loadErr := loadCommentDeletion(txApp, pageID, receiptResponse.Deletion.ID)
				if loadErr != nil {
					return loadErr
				}
				response = commentDeletionResponse{Deletion: renderCommentDeletion(job), Duplicate: true}
				return nil
			}
		}
		if err != nil {
			return err
		}
		if replay, err := readCommentReceipt(txApp, actorID, input.RequestID, hash, &response); err != nil || replay {
			if err != nil {
				return err
			}
			job, err := loadCommentDeletion(txApp, pageID, response.Deletion.ID)
			if err != nil {
				return err
			}
			response = commentDeletionResponse{Deletion: renderCommentDeletion(job), Duplicate: true}
			return nil
		}
		root, err := loadCommentForTarget(txApp, target, commentID)
		if err != nil {
			return err
		}
		if err := authorizeCommentMutation(authorization, actorID, root.AuthorID); err != nil {
			return err
		}
		// An open suggestion's thread goes when the suggestion is settled.
		if body, suggestion := pageSuggestionThread(root); suggestion && body.Suggestion.State == "" {
			return errCommentInvalid
		}
		if root.Revision != input.ExpectedRevision {
			return errCommentRevisionConflict
		}
		// The comment alone goes, its replies staying in their discussion
		// (comment_thread_handover.go): the deletion is complete at once. Its
		// place in the frontier keeps its ID from being given again.
		now := time.Now().UTC().Format(time.RFC3339Nano)
		job := storedCommentDeletion{
			ID: commentDeletionID(actorID, input.RequestID), ActorID: actorID,
			WorkspaceID: root.WorkspaceID, PageID: pageID, RootCommentID: commentID,
			Status: commentDeletionStatusComplete, DeletedCount: 1, Revision: 1,
			CreatedAt: now, UpdatedAt: now, CompletedAt: now,
		}
		_, err = txApp.DB().Insert(commentDeletionJobsTable, dbx.Params{
			"id": job.ID, "actor_id": actorID, "workspace_id": job.WorkspaceID,
			"page_id": pageID, "root_comment_id": commentID, "request_id": input.RequestID,
			"request_hash": hash, "status": job.Status, "deleted_count": job.DeletedCount, "revision": job.Revision,
			"created_at": now, "updated_at": now, "completed_at": now,
		}).Execute()
		if err != nil {
			return err
		}
		if _, err := txApp.DB().Insert(commentDeletionFrontierTable, dbx.Params{
			"job_id": job.ID, "comment_id": commentID, "ordinal": 0, "discovered": true, "deleted": true,
		}).Execute(); err != nil {
			return err
		}
		if handover, handedOver, err = deleteCommentKeepingReplies(txApp, root, now); err != nil {
			return err
		}
		recordCommentWebhookEvent(txApp, eventWebhookCommentDeleted, root, actorID)
		response.Deletion = renderCommentDeletion(job)
		return insertCommentReceipt(
			txApp, actorID, job.WorkspaceID, pageID, input.RequestID, hash, "delete", response,
		)
	})
	if err == nil && handedOver {
		moveCommentThreadMarks(app, actorID, handover, authorize)
	}
	return response, err
}

func readCommentDeletion(
	app core.App, actorID, pageID, jobID string,
) (commentDeletionResponse, error) {
	jobID = strings.TrimSpace(jobID)
	if !validCommentIdentifier(jobID) {
		return commentDeletionResponse{}, errCommentInvalid
	}
	job, err := loadCommentDeletion(app, pageID, jobID)
	if err != nil {
		return commentDeletionResponse{}, err
	}
	authorization, err := authorizeCommentPage(app, actorID, pageID, permissionWorkspaceRead, jobID)
	if err != nil {
		return commentDeletionResponse{}, err
	}
	if actorID != job.ActorID && actorID != authorization.Page.Workspace.OwnerID {
		return commentDeletionResponse{}, permissionDenied()
	}
	return commentDeletionResponse{Deletion: renderCommentDeletion(job)}, nil
}

func continueCommentDeletion(
	app core.App, actorID, pageID, jobID string,
) (commentDeletionResponse, error) {
	jobID = strings.TrimSpace(jobID)
	if !validCommentIdentifier(jobID) {
		return commentDeletionResponse{}, errCommentInvalid
	}
	response := commentDeletionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		job, err := loadCommentDeletion(txApp, pageID, jobID)
		if err != nil {
			return err
		}
		authorization, err := authorizeCommentPage(txApp, actorID, pageID, permissionWorkspaceWrite, jobID)
		if err != nil {
			return err
		}
		if actorID != job.ActorID && actorID != authorization.Page.Workspace.OwnerID {
			return permissionDenied()
		}
		if err := processCommentDeletionTurn(txApp, &job); err != nil {
			return err
		}
		response.Deletion = renderCommentDeletion(job)
		return nil
	})
	return response, err
}
