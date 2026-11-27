package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A deleted comment is kept as what Notion keeps of one (seen 2026-11-21
// three times, comments deleted on Notion's page — a discussion's first
// comment, a reply between two, a block discussion's first): the public API
// still finds it by its ID, with its words gone, and lists leave it out. So
// when a person or a connection deletes a comment, its place — public ID,
// target, discussion, author, display name, when it was made and when
// deleted — stays here without its words, files or reactions; the page's
// lists, discussions and screens read the comments table and don't see it.
// Its public ID stays while it does: the comment's identity, which goes with
// the comment, is made again (keepDeletedCommentPublicID) and goes with this
// row. It goes with its page when the page is deleted for good (the foreign
// key), as the comment would have.

// Migration 840 adds the table.

func ensureNativeCommentTombstonesSchema(app core.App) error {
	for _, statement := range []string{
		`CREATE TABLE {{leafen_comment_tombstones}} (
			[[id]] TEXT PRIMARY KEY NOT NULL,
			[[workspace_id]] TEXT NOT NULL,
			[[page_id]] TEXT NOT NULL,
			[[target_kind]] TEXT NOT NULL CHECK ([[target_kind]] IN ('page','database_row')),
			[[target_id]] TEXT NOT NULL,
			[[block_id]] TEXT NOT NULL DEFAULT '',
			[[author_id]] TEXT NOT NULL,
			[[notion_discussion_id]] TEXT NOT NULL DEFAULT '',
			[[notion_json]] TEXT NOT NULL DEFAULT '{}' CHECK (json_valid([[notion_json]])),
			[[created_at]] TEXT NOT NULL,
			[[deleted_at]] TEXT NOT NULL,
			FOREIGN KEY ([[page_id]]) REFERENCES {{leafen_pages}} ([[id]]) ON DELETE CASCADE
		)`,
		`CREATE INDEX [[idx_leafen_comment_tombstones_workspace]]
			ON {{leafen_comment_tombstones}} ([[workspace_id]], [[page_id]])`,
		`CREATE INDEX [[idx_leafen_comment_tombstones_page]]
			ON {{leafen_comment_tombstones}} ([[page_id]])`,
		`CREATE TRIGGER [[leafen_comment_tombstone_identity_delete]]
			AFTER DELETE ON {{leafen_comment_tombstones}} BEGIN
				DELETE FROM {{leafen_notion_object_identities}}
				WHERE [[object_kind]]='comment' AND [[workspace_id]]=OLD.[[workspace_id]]
					AND [[internal_id]]=OLD.[[id]]
					AND NOT EXISTS (SELECT 1 FROM {{leafen_comments}} WHERE [[id]]=OLD.[[id]]);
			END`,
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	if err := trackInstanceProductExportSourceRevisions(app, "leafen_comment_tombstones"); err != nil {
		return err
	}
	return validateNativeCommentTombstonesSchema(app)
}

func validateNativeCommentTombstonesSchema(app core.App) error {
	var columns, indexes int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_comment_tombstones')
		WHERE [[name]] IN ('id', 'workspace_id', 'page_id', 'target_kind', 'target_id', 'block_id',
			'author_id', 'notion_discussion_id', 'notion_json', 'created_at', 'deleted_at') AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM [[sqlite_master]]
		WHERE ([[type]] = 'index' AND [[name]] IN ('idx_leafen_comment_tombstones_workspace', 'idx_leafen_comment_tombstones_page'))
			OR ([[type]] = 'trigger' AND [[name]] = 'leafen_comment_tombstone_identity_delete')`).Row(&indexes); err != nil {
		return err
	}
	if columns != 11 || indexes != 3 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}

// keepCommentTombstone keeps what stays of a comment about to be deleted,
// read from its row in the same transaction; only its Notion display
// metadata stays of its body (notionStoredCommentMetadata). Once the row is
// deleted, keepDeletedCommentPublicID keeps its public ID.
func keepCommentTombstone(app core.App, workspaceID, pageID, commentID, deletedAt string) error {
	_, err := app.DB().NewQuery(`
		INSERT OR REPLACE INTO {{leafen_comment_tombstones}}
			([[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],[[block_id]],
			[[author_id]],[[notion_discussion_id]],[[notion_json]],[[created_at]],[[deleted_at]])
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],[[block_id]],
			[[author_id]],[[notion_discussion_id]],
			CASE WHEN json_type([[body_json]],'$.notion')='object'
				THEN json_extract([[body_json]],'$.notion') ELSE '{}' END,
			[[created_at]],{:deletedAt}
		FROM {{leafen_comments}}
		WHERE [[page_id]]={:pageId} AND [[id]]={:id} AND [[workspace_id]]={:workspaceId}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": pageID, "id": commentID, "deletedAt": deletedAt,
	}).Execute()
	return err
}

// keepDeletedCommentPublicID makes again the public ID of a comment just
// deleted that keepCommentTombstone kept, which the comment's going took with
// it (leafen_notion_object_identity_comment_delete) — the same ID, made from
// the same workspace and comment.
func keepDeletedCommentPublicID(app core.App, workspaceID, commentID string) error {
	var kept int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_comment_tombstones}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId}`).
		Bind(dbx.Params{"id": commentID, "workspaceId": workspaceID}).Row(&kept); err != nil || kept == 0 {
		return err
	}
	_, err := ensureNotionObjectIdentity(app, notionObjectIdentityComment, workspaceID, commentID)
	return err
}

// loadCommentTombstone reads a deleted comment as a comment row of no words,
// last changed when it was deleted. found is false for an ID no deleted
// comment has, and for one a comment holds again.
func loadCommentTombstone(app core.App, commentID string) (storedComment, bool, error) {
	stored := struct {
		ID                 string `db:"id"`
		WorkspaceID        string `db:"workspace_id"`
		PageID             string `db:"page_id"`
		TargetKind         string `db:"target_kind"`
		TargetID           string `db:"target_id"`
		BlockID            string `db:"block_id"`
		AuthorID           string `db:"author_id"`
		NotionDiscussionID string `db:"notion_discussion_id"`
		NotionJSON         string `db:"notion_json"`
		CreatedAt          string `db:"created_at"`
		DeletedAt          string `db:"deleted_at"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],[[block_id]],
			[[author_id]],[[notion_discussion_id]],[[notion_json]],[[created_at]],[[deleted_at]]
		FROM {{leafen_comment_tombstones}} AS [[gone]]
		WHERE [[id]]={:id}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_comments}} WHERE [[id]]=[[gone]].[[id]])
	`).Bind(dbx.Params{"id": commentID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return storedComment{}, false, nil
	}
	if err != nil {
		return storedComment{}, false, err
	}
	row := storedComment{
		ID: stored.ID, WorkspaceID: stored.WorkspaceID, PageID: stored.PageID,
		TargetKind: stored.TargetKind, TargetID: stored.TargetID, BlockID: stored.BlockID,
		AuthorID: stored.AuthorID, NotionDiscussionID: stored.NotionDiscussionID,
		BodyJSON:  `{"type":"rich_text","rich":[]}`,
		CreatedAt: stored.CreatedAt, UpdatedAt: stored.DeletedAt, Revision: 1,
	}
	if stored.NotionJSON != "{}" {
		row.BodyJSON = `{"type":"rich_text","rich":[],"notion":` + stored.NotionJSON + `}`
	}
	return row, true, nil
}
