package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedComment struct {
	ID                 string `db:"id"`
	WorkspaceID        string `db:"workspace_id"`
	PageID             string `db:"page_id"`
	TargetKind         string `db:"target_kind"`
	TargetID           string `db:"target_id"`
	BlockID            string `db:"block_id"`
	ParentID           string `db:"parent_id"`
	AuthorID           string `db:"author_id"`
	BodyJSON           string `db:"body_json"`
	NotionDiscussionID string `db:"notion_discussion_id"`
	Resolved           bool   `db:"resolved"`
	Revision           int    `db:"revision"`
	CreatedAt          string `db:"created_at"`
	UpdatedAt          string `db:"updated_at"`
	EditedAt           string `db:"edited_at"`
}

type storedCommentReceipt struct {
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

type commentListCursor struct {
	SourceRevision int    `json:"sourceRevision"`
	AfterCreated   string `json:"afterCreated"`
	AfterID        string `json:"afterId"`
}

func commentRequestHash(operation, pageID, commentID string, input any) (string, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(strings.Join(
		[]string{operation, pageID, commentID, string(payload)}, "\x00",
	)))
	return hex.EncodeToString(digest[:]), nil
}

func validCommentBody(body json.RawMessage) bool {
	if len(body) == 0 || len(body) > commentBodyBytes || !json.Valid(body) {
		return false
	}
	var value any
	return json.Unmarshal(body, &value) == nil && value != nil
}

func commentAttachmentIDs(body json.RawMessage) ([]string, error) {
	ids, err := workspaceImportCommentAttachmentIDs(body)
	if err != nil {
		return nil, errCommentInvalid
	}
	return ids, nil
}

func readCommentReceipt(
	app core.App, actorID, requestID, requestHash string, response any,
) (bool, error) {
	var receipt storedCommentReceipt
	err := app.DB().NewQuery(`
		SELECT [[request_hash]],[[response_json]] FROM {{leafen_comment_receipts}}
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId}
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if receipt.RequestHash != requestHash {
		return false, errCommentRequestConflict
	}
	if len(receipt.ResponseJSON) > commentReceiptBytes || json.Unmarshal([]byte(receipt.ResponseJSON), response) != nil {
		return false, errCommentFailed
	}
	return true, nil
}

func insertCommentReceipt(
	app core.App, actorID, workspaceID, pageID, requestID, requestHash, operation string, response any,
) error {
	payload, err := json.Marshal(response)
	if err != nil || len(payload) > commentReceiptBytes {
		return failedBecause(errCommentFailed, err)
	}
	_, err = app.DB().Insert(commentReceiptsTable, dbx.Params{
		"actor_id": actorID, "workspace_id": workspaceID, "page_id": pageID,
		"request_id": requestID, "request_hash": requestHash,
		"operation": operation, "response_json": string(payload),
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
	}).Execute()
	return err
}

func renderComment(row storedComment) (commentRecord, error) {
	if !json.Valid([]byte(row.BodyJSON)) {
		return commentRecord{}, errCommentFailed
	}
	logicalPageID := row.TargetID
	if logicalPageID == "" {
		logicalPageID = row.PageID
	}
	return commentRecord{
		ID: row.ID, WorkspaceID: row.WorkspaceID, PageID: logicalPageID,
		BlockID: row.BlockID, ParentID: row.ParentID, AuthorID: row.AuthorID,
		Body: json.RawMessage(row.BodyJSON), Resolved: row.Resolved, Revision: row.Revision,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, EditedAt: row.EditedAt,
	}, nil
}

func loadComment(app core.App, pageID, commentID string) (storedComment, error) {
	return loadCommentForTarget(app, nativePageCommentTarget(pageID), commentID)
}

func loadCommentForTarget(
	app core.App, target nativeCommentTarget, commentID string,
) (storedComment, error) {
	var row storedComment
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],
			[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
			[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}} WHERE [[target_kind]]={:targetKind}
			AND [[target_id]]={:targetId} AND [[id]]={:commentId}
	`).Bind(dbx.Params{
		"targetKind": target.Kind, "targetId": target.ID, "commentId": commentID,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedComment{}, errCommentNotFound
	}
	return row, err
}

func readCommentPageRevision(app core.App, pageID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`
		SELECT COALESCE((
			SELECT [[revision]] FROM {{leafen_comment_page_state}} WHERE [[page_id]]={:pageId}
		), 0)
	`).Bind(dbx.Params{"pageId": pageID}).Row(&revision)
	return revision, err
}

// advanceCommentPageRevision moves a page's comments to their next revision
// and, once that is committed, tells the page's open room.
func advanceCommentPageRevision(app core.App, pageID string) error {
	var revision int
	err := app.DB().NewQuery(`
		INSERT INTO {{leafen_comment_page_state}} ([[page_id]],[[revision]]) VALUES ({:pageId},1)
		ON CONFLICT ([[page_id]]) DO UPDATE SET [[revision]]=[[revision]]+1
		RETURNING [[revision]]
	`).Bind(dbx.Params{"pageId": pageID}).Row(&revision)
	if err == nil {
		announceCommentRevisionAfterCommit(app, pageID, revision)
	}
	return err
}

func encodeCommentCursor(cursor commentListCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(payload)
	if len(value) > commentCursorBytes {
		return "", errCommentInvalid
	}
	return value, nil
}

func decodeCommentCursor(value string) (commentListCursor, error) {
	if value == "" {
		return commentListCursor{}, nil
	}
	if len(value) > commentCursorBytes {
		return commentListCursor{}, errCommentInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return commentListCursor{}, errCommentInvalid
	}
	var cursor commentListCursor
	if json.Unmarshal(payload, &cursor) != nil || cursor.SourceRevision < 0 ||
		cursor.AfterCreated == "" || !validCommentIdentifier(cursor.AfterID) {
		return commentListCursor{}, errCommentInvalid
	}
	return cursor, nil
}

func listCommentRecords(
	app core.App, actorID, pageID, rawLimit, rawCursor string,
) (commentListResponse, error) {
	limit := commentListPageSize
	if rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > commentListPageSize {
			return commentListResponse{}, errCommentInvalid
		}
		limit = parsed
	}
	cursor, err := decodeCommentCursor(rawCursor)
	if err != nil {
		return commentListResponse{}, err
	}
	response := commentListResponse{
		Items: []commentRecord{}, ActorProfiles: []commentActorProfile{},
	}
	err = runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authorization, err := authorizeCommentPage(txApp, actorID, pageID, permissionWorkspaceRead, "")
		if err != nil {
			return err
		}
		revision, err := readCommentPageRevision(txApp, pageID)
		if err != nil {
			return err
		}
		if rawCursor != "" && cursor.SourceRevision != revision {
			return errCommentSnapshotChanged
		}
		params := dbx.Params{
			"workspaceId": authorization.Page.Workspace.WorkspaceID, "pageId": pageID,
			"afterCreated": cursor.AfterCreated, "afterId": cursor.AfterID, "limit": limit + 1,
		}
		rows := []storedComment{}
		err = txApp.DB().NewQuery(`
			SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
				[[block_id]],[[parent_id]],[[author_id]],
				[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
				[[created_at]],[[updated_at]],[[edited_at]]
			FROM {{leafen_comments}} INDEXED BY [[idx_leafen_comments_page_order]]
			WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
				AND [[target_kind]]='page' AND [[target_id]]={:pageId}
				AND ({:afterCreated}='' OR [[created_at]]>{:afterCreated}
					OR ([[created_at]]={:afterCreated} AND [[id]]>{:afterId}))
			ORDER BY [[created_at]],[[id]] LIMIT {:limit}
		`).Bind(params).All(&rows)
		if err != nil {
			return err
		}
		response.SourceRevision = revision
		response.HasMore = len(rows) > limit
		if rawCursor == "" {
			if response.PageReactions, err = loadPageReactions(txApp, nativePageCommentTarget(pageID)); err != nil {
				return err
			}
		}
		if response.HasMore {
			rows = rows[:limit]
		}
		actorIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			record, err := renderComment(row)
			if err != nil {
				return err
			}
			response.Items = append(response.Items, record)
			actorIDs = append(actorIDs, record.AuthorID)
		}
		if err := attachCommentListDetails(txApp, actorID, rows, response.Items); err != nil {
			return err
		}
		response.ActorProfiles, err = readCanonicalActorProfiles(txApp, actorIDs)
		if err != nil {
			return err
		}
		if response.HasMore {
			last := rows[len(rows)-1]
			next, err := encodeCommentCursor(commentListCursor{
				SourceRevision: revision, AfterCreated: last.CreatedAt, AfterID: last.ID,
			})
			if err != nil {
				return err
			}
			response.NextCursor = &next
		}
		return nil
	})
	return response, err
}

// insertCommentRow writes a new comment in a transaction: its anchor and
// files checked, its Notion identities kept, its page's comment revision
// moved on and its notification queued.
func insertCommentRow(
	txApp core.App,
	authorization commentAuthorization,
	target nativeCommentTarget,
	actorID string,
	input commentCreateInput,
	nowTime time.Time,
) (storedComment, error) {
	if err := validateCommentAnchor(txApp, authorization, input.BlockID, input.ParentID); err != nil {
		return storedComment{}, err
	}
	if err := validateCommentPropertyAnchor(txApp, authorization, input); err != nil {
		return storedComment{}, err
	}
	attachments, err := commentAttachmentIDs(input.Body)
	if err != nil {
		return storedComment{}, err
	}
	if len(attachments) > 0 {
		// A file may be attached more than once (notionCommentAttachmentIDs).
		if _, err := loadGraphFileAuthorityRecords(
			txApp, authorization.Page.Workspace.WorkspaceID, uniqueStrings(attachments),
		); err != nil {
			return storedComment{}, refusedUnlessStorage(errCommentInvalid, err)
		}
		if err := authorizeCommentAttachmentFiles(txApp, actorID, attachments, nil); err != nil {
			return storedComment{}, err
		}
	}
	var existing int
	if err := txApp.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_comments}} WHERE [[id]]={:id} LIMIT 1)
			+ EXISTS(SELECT 1 FROM {{leafen_comment_deletion_frontier}}
				INDEXED BY [[idx_leafen_comment_deletion_frontier_comment]]
				WHERE [[comment_id]]={:id} LIMIT 1)
	`).Bind(dbx.Params{"id": input.ID}).Row(&existing); err != nil {
		return storedComment{}, err
	}
	if existing != 0 {
		return storedComment{}, errCommentIDConflict
	}
	now := nowTime.Format(time.RFC3339Nano)
	row := storedComment{
		ID: input.ID, WorkspaceID: authorization.Page.Workspace.WorkspaceID,
		PageID: target.StoragePageID, TargetKind: target.Kind, TargetID: target.ID,
		BlockID: input.BlockID, ParentID: input.ParentID, AuthorID: actorID,
		BodyJSON: string(input.Body), Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	row.NotionDiscussionID, err = notionCommentDiscussionID(txApp, row)
	if err != nil {
		return storedComment{}, err
	}
	_, err = txApp.DB().Insert(commentsTable, dbx.Params{
		"id": row.ID, "workspace_id": row.WorkspaceID, "page_id": row.PageID,
		"target_kind": row.TargetKind, "target_id": row.TargetID,
		"block_id": row.BlockID, "parent_id": row.ParentID, "author_id": row.AuthorID,
		"body_json": row.BodyJSON, "notion_discussion_id": row.NotionDiscussionID,
		"resolved": false, "workspace_import_job_id": "", "connection_id": commentConnectionOf(txApp).ID, "connection_name": commentConnectionOf(txApp).Name,
		"created_at": now, "updated_at": now, "edited_at": "", "revision": 1,
	}).Execute()
	if err != nil {
		return storedComment{}, err
	}
	if _, err := ensureNotionObjectIdentity(
		txApp, notionObjectIdentityComment, row.WorkspaceID, row.ID,
	); err != nil {
		return storedComment{}, err
	}
	if err := ensureNativeCommentTargetNotionIdentities(txApp, row); err != nil {
		return storedComment{}, err
	}
	if len(attachments) > 0 {
		items := make(graphFilesValue, len(attachments))
		for index, fileID := range attachments {
			items[index] = graphFileValueItem{FileID: fileID}
		}
		if err := insertProductFileReferences(
			txApp, row.WorkspaceID, target.StoragePageID, productFileReferenceOwnerComment,
			row.ID, "", items,
		); err != nil {
			return storedComment{}, err
		}
	}
	if err := advanceCommentPageRevision(txApp, target.StoragePageID); err != nil {
		return storedComment{}, err
	}
	if err := insertCommentNotificationEvent(txApp, authorization, row, nowTime.UnixMilli()); err != nil {
		return storedComment{}, err
	}
	recordCommentWebhookEvent(txApp, eventWebhookCommentCreated, row, actorID)
	return row, nil
}

func updateCommentRecord(
	app core.App, actorID, pageID, commentID string, input commentUpdateInput,
) (commentMutationResponse, error) {
	target := nativePageCommentTarget(pageID)
	return updateCommentRecordForTarget(
		app, actorID, target, commentID, input,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeCommentPage(
				txApp, actorID, pageID, permissionWorkspaceWrite, "",
			)
		},
	)
}

func updateCommentRecordForTarget(
	app core.App,
	actorID string,
	target nativeCommentTarget,
	commentID string,
	input commentUpdateInput,
	authorize func(core.App) (commentAuthorization, error),
) (commentMutationResponse, error) {
	input.RequestID, commentID = strings.TrimSpace(input.RequestID), strings.TrimSpace(commentID)
	if !validCommentIdentifier(commentID) || !validCommentIdentifier(target.ID) || !validCommentIdentifier(target.StoragePageID) {
		return commentMutationResponse{}, errCommentInvalid
	}
	bodyProblem := ""
	switch {
	case len(input.Body) == 0 && input.Resolved == nil:
		bodyProblem = "an edit sends body, resolved or both"
	case len(input.Body) != 0:
		bodyProblem = commentBodyRefusalProblem(input.Body)
	}
	if problem := firstNonempty(commentIdentifierProblem("requestId", input.RequestID),
		commentRevisionProblem(input.ExpectedRevision), bodyProblem); problem != "" {
		return commentMutationResponse{}, commentInvalid("%s", problem)
	}
	if len(input.Body) != 0 {
		if err := commentBodyProblem(input.Body); err != nil {
			return commentMutationResponse{}, err
		}
	}
	hash, err := commentRequestHash("update", target.Kind+":"+target.ID, commentID, input)
	if err != nil {
		return commentMutationResponse{}, err
	}
	response := commentMutationResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		if authorization.Target != target {
			return permissionDenied()
		}
		if replay, err := readCommentReceipt(txApp, actorID, input.RequestID, hash, &response); err != nil || replay {
			response.Duplicate = replay
			return err
		}
		row, err := loadCommentForTarget(txApp, target, commentID)
		if err != nil {
			return err
		}
		if err := authorizeCommentMutation(authorization, actorID, row.AuthorID); err != nil {
			return err
		}
		if len(input.Body) != 0 {
			if err := enforceOrganizationDLPContent(txApp, authorization.Page.Workspace.Organization.OrganizationID, input.Body); err != nil {
				return err
			}
		}
		// A suggestion is settled by accepting or rejecting it.
		if _, suggestion := pageSuggestionThread(row); suggestion {
			return errCommentInvalid
		}
		if row.Revision != input.ExpectedRevision {
			return errCommentRevisionConflict
		}
		if len(input.Body) != 0 && !sameCommentPropertyAnchor(row.BodyJSON, string(input.Body)) {
			return errCommentInvalid
		}
		if len(input.Body) != 0 {
			if input.Body, err = keepConnectionCommentPart(input.Body, row.BodyJSON); err != nil {
				return err
			}
		}
		attachments := []string(nil)
		if len(input.Body) != 0 {
			attachments, err = commentAttachmentIDs(input.Body)
			if err != nil {
				return err
			}
			if len(attachments) > 0 {
				if _, err := loadGraphFileAuthorityRecords(
					txApp, authorization.Page.Workspace.WorkspaceID, uniqueStrings(attachments),
				); err != nil {
					return refusedUnlessStorage(errCommentInvalid, err)
				}
				kept, err := commentAttachmentIDs(json.RawMessage(row.BodyJSON))
				if err != nil {
					return err
				}
				if err := authorizeCommentAttachmentFiles(txApp, actorID, attachments, kept); err != nil {
					return err
				}
			}
		}
		nowTime := time.Now().UTC()
		now := nowTime.Format(time.RFC3339Nano)
		previousBody := row.BodyJSON
		if len(input.Body) != 0 {
			row.BodyJSON, row.EditedAt = string(input.Body), now
		}
		if input.Resolved != nil {
			row.Resolved = *input.Resolved
		}
		row.Revision++
		row.UpdatedAt = now
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_comments}} SET [[body_json]]={:body},[[resolved]]={:resolved},
				[[revision]]={:revision},[[updated_at]]={:updated},[[edited_at]]={:edited}
			WHERE [[target_kind]]={:targetKind} AND [[target_id]]={:targetId}
				AND [[id]]={:commentId} AND [[revision]]={:expected}
		`).Bind(dbx.Params{
			"body": row.BodyJSON, "resolved": row.Resolved, "revision": row.Revision,
			"updated": now, "edited": row.EditedAt,
			"targetKind": target.Kind, "targetId": target.ID,
			"commentId": commentID, "expected": input.ExpectedRevision,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errCommentRevisionConflict, err)
		}
		recordCommentWebhookEvent(txApp, eventWebhookCommentUpdated, row, actorID)
		if len(input.Body) != 0 {
			if err := insertCommentEditMentionEvent(
				txApp, authorization, row, actorID, previousBody, nowTime.UnixMilli(),
			); err != nil {
				return err
			}
			if err := deleteProductFileCommentReferences(
				txApp, row.WorkspaceID, target.StoragePageID, row.ID,
			); err != nil {
				return err
			}
			if len(attachments) > 0 {
				items := make(graphFilesValue, len(attachments))
				for index, fileID := range attachments {
					items[index] = graphFileValueItem{FileID: fileID}
				}
				if err := insertProductFileReferences(
					txApp, row.WorkspaceID, target.StoragePageID,
					productFileReferenceOwnerComment,
					row.ID, "", items,
				); err != nil {
					return err
				}
			}
		}
		if err := advanceCommentPageRevision(txApp, target.StoragePageID); err != nil {
			return err
		}
		record, err := renderComment(row)
		if err != nil {
			return err
		}
		if err := attachCommentDetail(txApp, actorID, &record); err != nil {
			return err
		}
		response.Comment = record
		return insertCommentReceipt(
			txApp, actorID, row.WorkspaceID, target.StoragePageID,
			input.RequestID, hash, "update", response,
		)
	})
	return response, err
}
