package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Notion's comment object says whether its discussion lost what it was left
// on, original_content_deleted, and whether a file it names is still being
// uploaded, has_pending_attachments, each false until then (seen 2026-11-11,
// every comment made and listed). A discussion on a block turns
// original_content_deleted true for every one of its comments once the block
// is deleted, and one on words once the words are gone from the block's text
// (typed over); a page's own discussion never does. Notion's API refuses a
// comment whose upload isn't done (its status pending, seen 2026-11-11),
// so only a comment left in the app with a file still uploading
// has pending attachments — a Leafen comment can name such a file, which its
// attachments leave out until it is whole (attachCommentAttachments).

// notionCommentOriginals tells, for one answer, whether comments'
// discussions lost what they were left on, each page's text read once and
// each discussion told once.
type notionCommentOriginals struct {
	documents   map[string]*crdt.Doc
	captions    map[string]*commentCaptionMarks
	discussions map[string]bool
}

// deleted is whether the discussion a comment is in lost what it was left
// on: its block is gone, or no place its words were on is marked now.
func (originals *notionCommentOriginals) deleted(app core.App, row storedComment) (bool, error) {
	if row.BlockID == "" {
		return false, nil
	}
	key := row.WorkspaceID + "/" + row.TargetKind + "/" + row.TargetID + "/" + row.NotionDiscussionID
	if told, known := originals.discussions[key]; known && row.NotionDiscussionID != "" {
		return told, nil
	}
	deleted, err := originals.rootDeleted(app, row)
	if err == nil && row.NotionDiscussionID != "" {
		if originals.discussions == nil {
			originals.discussions = map[string]bool{}
		}
		originals.discussions[key] = deleted
	}
	return deleted, err
}

func (originals *notionCommentOriginals) rootDeleted(app core.App, row storedComment) (bool, error) {
	root := row
	if row.ParentID != "" {
		err := app.DB().NewQuery(`
			SELECT [[id]],[[target_id]],[[block_id]],[[body_json]] FROM {{leafen_comments}}
			WHERE [[workspace_id]]={:workspaceId} AND [[notion_discussion_id]]={:discussionId}
				AND [[target_kind]]={:targetKind} AND [[target_id]]={:targetId} AND [[parent_id]]=''
			LIMIT 1
		`).Bind(dbx.Params{
			"workspaceId": row.WorkspaceID, "discussionId": row.NotionDiscussionID,
			"targetKind": row.TargetKind, "targetId": row.TargetID,
		}).One(&root)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	stands := 0
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[id]]={:id} AND ` + blockStandsSQL("") + `
	`).Bind(dbx.Params{"id": root.BlockID}).Row(&stands); err != nil {
		return false, err
	}
	if stands == 0 {
		return true, nil
	}
	anchor := commentQuoteAnchor{}
	if json.Unmarshal([]byte(root.BodyJSON), &anchor) != nil {
		return false, nil
	}
	places := anchor.textPlaces(root.BlockID)
	for _, place := range places {
		marked, err := originals.marked(app, root.TargetID, place, root.ID)
		if err != nil || marked {
			return false, err
		}
	}
	return len(places) > 0, nil
}

// marked is whether a place of a thread's words, in the text of a page or a
// database page (room), holds the thread's mark.
func (originals *notionCommentOriginals) marked(app core.App, room string, place commentTextPlace, threadID string) (bool, error) {
	holds := func(threads any) bool {
		listed, _ := threads.(string)
		return slices.Contains(strings.Fields(listed), threadID)
	}
	if place.caption || place.cell > 0 {
		if originals.captions == nil {
			originals.captions = map[string]*commentCaptionMarks{}
		}
		marks := originals.captions[room]
		if marks == nil {
			marks = &commentCaptionMarks{pageID: room}
			originals.captions[room] = marks
		}
		runs, _, found, err := marks.fieldRuns(app, place.blockID, place.cell)
		if err != nil || !found {
			return false, err
		}
		return slices.ContainsFunc(runs, func(run map[string]any) bool { return holds(run["commentId"]) }), nil
	}
	if originals.documents == nil {
		originals.documents = map[string]*crdt.Doc{}
	}
	document := originals.documents[room]
	if document == nil {
		var err error
		if document, err = loadPageTextDocument(app, room); err != nil {
			return false, err
		}
		originals.documents[room] = document
	}
	for _, part := range document.GetText(ygoBlockTextRootPrefix + place.blockID).ToDelta() {
		if holds(part.Attributes["commentId"]) {
			return true, nil
		}
	}
	return false, nil
}

// notionCommentAttachmentsNow are the files a comment names that are whole,
// by ID, and whether one is still uploading.
func notionCommentAttachmentsNow(
	app core.App, workspaceID string, fileIDs []string,
) (map[string]graphFileBlockContent, bool, error) {
	if len(fileIDs) == 0 {
		return nil, false, nil
	}
	unique := uniqueStrings(fileIDs)
	uploading, err := incompleteProductFileUploadIDs(app, unique)
	if err != nil {
		return nil, false, err
	}
	whole, err := availableGraphFileRecords(app, workspaceID, unique)
	return whole, len(uploading) > 0, err
}

// Notion's comment list puts a discussion that lost what it was left on
// with the page's own comments, in the order they were written, and no
// longer with its block's (seen 2026-11-11, words typed over and a block
// deleted alike); the list of a block that is gone still gives the block's,
// and a block's list gives those whose marks stand in it now
// (notion_comment_marked_blocks.go).

// placed lists a page's or a block's comments where Notion's list puts
// them: at most pageSize from a place in the list, the one after them that
// a next read starts at (notionListPage), and whether the block listed is
// gone. listed reads at most limit of the comments left where the list's
// are, from a place or after it.
func (originals *notionCommentOriginals) placed(
	app core.App, target notionCommentTarget, afterCreated, afterID string, including bool, pageSize int,
	listed func(afterCreated, afterID string, including bool, limit int) ([]storedComment, error),
) ([]storedComment, *storedComment, bool, error) {
	keep := func(storedComment) (bool, error) { return true, nil }
	lost := []storedComment{}
	blockGone := false
	if target.BlockID == "" {
		var err error
		lost, err = notionCommentsKept(afterCreated, afterID, including, pageSize+1, func(afterCreated, afterID string, including bool, limit int) ([]storedComment, error) {
			return listNotionBlockCommentsAfter(app, target, afterCreated, afterID, including, limit)
		}, func(row storedComment) (bool, error) { return originals.deleted(app, row) })
		if err != nil {
			return nil, nil, false, err
		}
	} else {
		stands := 0
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_blocks}} WHERE [[id]]={:id} AND ` + blockStandsSQL("") + `
		`).Bind(dbx.Params{"id": target.BlockID}).Row(&stands); err != nil {
			return nil, nil, false, err
		}
		blockGone = stands == 0
		if !blockGone {
			keep = func(row storedComment) (bool, error) {
				deleted, err := originals.deleted(app, row)
				return !deleted, err
			}
			// Those whose marks stand in it now are listed there too.
			var err error
			if lost, err = notionCommentsMarkedInto(app, target, afterCreated, afterID, including, pageSize+1); err != nil {
				return nil, nil, false, err
			}
			originals.standing(lost)
		}
	}
	rows, err := notionCommentsKept(afterCreated, afterID, including, pageSize+1, listed, keep)
	if err != nil {
		return nil, nil, false, err
	}
	rows = append(rows, lost...)
	slices.SortStableFunc(rows, func(a, b storedComment) int {
		if a.CreatedAt != b.CreatedAt {
			return strings.Compare(a.CreatedAt, b.CreatedAt)
		}
		return strings.Compare(a.ID, b.ID)
	})
	rows, next := notionListPage(rows, pageSize)
	return rows, next, blockGone, nil
}

// notionCommentsKept reads comments from a place or after it, in turns,
// until want of them are kept or none are left.
func notionCommentsKept(
	afterCreated, afterID string, including bool, want int,
	read func(afterCreated, afterID string, including bool, limit int) ([]storedComment, error),
	keep func(storedComment) (bool, error),
) ([]storedComment, error) {
	kept := []storedComment{}
	turn := max(want, 50)
	for len(kept) < want {
		rows, err := read(afterCreated, afterID, including, turn)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if ok, err := keep(row); err != nil {
				return nil, err
			} else if ok && len(kept) < want {
				kept = append(kept, row)
			}
		}
		if len(rows) < turn {
			break
		}
		afterCreated, afterID, including = rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID, false
	}
	return kept, nil
}

// listNotionBlockCommentsAfter reads at most limit of the open comments left
// on a page's blocks, from a place in its list or after it.
func listNotionBlockCommentsAfter(
	app core.App, target notionCommentTarget, afterCreated, afterID string, including bool, limit int,
) ([]storedComment, error) {
	rows := []storedComment{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],
			[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
			[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}}
		WHERE [[workspace_id]]={:workspaceId} AND [[target_kind]]={:targetKind}
			AND [[target_id]]={:targetId} AND [[block_id]]!='' AND [[resolved]]=0
			AND ({:afterCreated}='' OR [[created_at]]>{:afterCreated}
				OR ([[created_at]]={:afterCreated} AND [[id]]` + notionCursorComparison(including) + `{:afterId}))
			AND ` + notionCommentOpenSQL + `
		ORDER BY [[created_at]],[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": target.Identity.WorkspaceID, "targetKind": target.TargetKind, "targetId": target.PageID,
		"afterCreated": afterCreated, "afterId": afterID, "limit": limit,
	}).All(&rows)
	return rows, err
}
