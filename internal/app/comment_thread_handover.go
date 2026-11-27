package app

import (
	"bytes"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Deleting a comment deletes that comment alone, as Notion's "삭제하기" does
// (seen 2026-11-21, synthetic pages under a Notion test page — a page
// discussion, a block discussion and two text discussions, each with
// replies): its replies stay as their discussion, under the same
// discussion_id, and a discussion's first comment going leaves its first
// reply first. The discussion keeps what is its own: what it was left on
// (its block, the words it quotes and the marks on them, a database page's
// property) and whether it is resolved, and each person's state of it; the
// emptied comment stays as comment_tombstones.go keeps it. A person's, an
// app's MCP and a connection's API delete all go through
// deleteCommentKeepingReplies.

// commentThreadBodyKeys are the parts of a first comment's body that are its
// discussion's rather than its own: what the discussion was left on.
var commentThreadBodyKeys = []string{
	"quote", "quoteStart", "quoteEnd", "quoteField", "quoteBlocks", "propertyId", "propertyName",
}

// commentHandover is a discussion that lost its first comment: the comment
// deleted and the reply now first, whose marks are moved after the delete is
// stored (moveCommentThreadMarks).
type commentHandover struct {
	FormerID string
	Thread   storedComment
}

// deleteCommentKeepingReplies deletes one comment in a transaction, keeping
// what stays of it, and gives its replies to its discussion: a first
// comment's first reply becomes the discussion's first comment, the others
// its replies; a reply's own replies, which only the API can make, go to its
// parent. The handover is the discussion's new first comment, if one took
// over; a comment changed since current was read is not deleted.
func deleteCommentKeepingReplies(
	txApp core.App, current storedComment, deletedAt string,
) (commentHandover, bool, error) {
	children := []storedComment{}
	if err := txApp.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],
			[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
			[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}} INDEXED BY [[idx_leafen_comments_target_thread]]
		WHERE [[workspace_id]]={:workspaceId} AND [[target_kind]]={:targetKind}
			AND [[target_id]]={:targetId} AND [[parent_id]]={:parentId}
		ORDER BY [[created_at]],[[id]]
	`).Bind(dbx.Params{
		"workspaceId": current.WorkspaceID, "targetKind": current.TargetKind,
		"targetId": current.TargetID, "parentId": current.ID,
	}).All(&children); err != nil {
		return commentHandover{}, false, err
	}
	handover, handedOver := commentHandover{}, false
	newParentID := current.ParentID
	if current.ParentID == "" && len(children) > 0 {
		thread, err := takeOverCommentThread(txApp, current, children[0])
		if err != nil {
			return commentHandover{}, false, err
		}
		handover, handedOver, newParentID = commentHandover{FormerID: current.ID, Thread: thread}, true, thread.ID
	}
	if _, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_comments}} SET [[parent_id]]={:newParent}
		WHERE [[workspace_id]]={:workspaceId} AND [[target_kind]]={:targetKind} AND [[target_id]]={:targetId}
			AND [[parent_id]]={:oldParent} AND [[id]]!={:newParent}
	`).Bind(dbx.Params{
		"newParent": newParentID, "workspaceId": current.WorkspaceID,
		"targetKind": current.TargetKind, "targetId": current.TargetID, "oldParent": current.ID,
	}).Execute(); err != nil {
		return commentHandover{}, false, err
	}
	if err := deleteProductFileCommentReferences(txApp, current.WorkspaceID, current.PageID, current.ID); err != nil {
		return commentHandover{}, false, err
	}
	if err := keepCommentTombstone(txApp, current.WorkspaceID, current.PageID, current.ID, deletedAt); err != nil {
		return commentHandover{}, false, err
	}
	result, err := txApp.DB().NewQuery(`
		DELETE FROM {{leafen_comments}}
		WHERE [[target_kind]]={:targetKind} AND [[target_id]]={:targetId}
			AND [[id]]={:id} AND [[revision]]={:revision}
	`).Bind(dbx.Params{
		"targetKind": current.TargetKind, "targetId": current.TargetID,
		"id": current.ID, "revision": current.Revision,
	}).Execute()
	if err != nil {
		return commentHandover{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return commentHandover{}, false, refusedUnlessStorage(errCommentRevisionConflict, err)
	}
	if err := keepDeletedCommentPublicID(txApp, current.WorkspaceID, current.ID); err != nil {
		return commentHandover{}, false, err
	}
	return handover, handedOver, advanceCommentPageRevision(txApp, current.PageID)
}

// takeOverCommentThread makes a first comment's first reply the discussion's
// first comment: its place, its quote, whether it is resolved and each
// person's state of it go to the reply, which keeps its own words, author
// and times.
func takeOverCommentThread(txApp core.App, former, reply storedComment) (storedComment, error) {
	body, err := commentBodyWithThreadParts(reply.BodyJSON, former.BodyJSON)
	if err != nil {
		return storedComment{}, err
	}
	reply.ParentID, reply.BlockID, reply.Resolved, reply.BodyJSON = "", former.BlockID, former.Resolved, body
	reply.Revision++
	if _, err := txApp.DB().NewQuery(`
		UPDATE {{leafen_comments}}
		SET [[parent_id]]='',[[block_id]]={:blockId},[[resolved]]={:resolved},
			[[body_json]]={:body},[[revision]]={:revision}
		WHERE [[id]]={:id} AND [[target_kind]]={:targetKind} AND [[target_id]]={:targetId}
	`).Bind(dbx.Params{
		"blockId": reply.BlockID, "resolved": reply.Resolved, "body": reply.BodyJSON,
		"revision": reply.Revision, "id": reply.ID,
		"targetKind": reply.TargetKind, "targetId": reply.TargetID,
	}).Execute(); err != nil {
		return storedComment{}, err
	}
	// A reply reads its thread under the first comment's ID, so it has no
	// state of its own to keep.
	_, err = txApp.DB().NewQuery(`
		UPDATE OR REPLACE {{leafen_comment_thread_states}} SET [[comment_id]]={:thread}
		WHERE [[comment_id]]={:former}
	`).Bind(dbx.Params{"thread": reply.ID, "former": former.ID}).Execute()
	return reply, err
}

// commentBodyWithThreadParts is a reply's body with its discussion's parts
// (commentThreadBodyKeys) as the former first comment's body held them.
func commentBodyWithThreadParts(replyBody, formerBody string) (string, error) {
	reply, former := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	if json.Unmarshal([]byte(replyBody), &reply) != nil || json.Unmarshal([]byte(formerBody), &former) != nil {
		return "", errCommentInvalid
	}
	for _, key := range commentThreadBodyKeys {
		delete(reply, key)
		if value, found := former[key]; found {
			reply[key] = value
		}
	}
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(reply); err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(out.Bytes())), nil
}
