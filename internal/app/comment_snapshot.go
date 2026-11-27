package app

import (
	"context"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's comments are read whole for what names or lists its discussions
// for an agent or the public API — get-comments, fetch's discussions, a
// version's, a page's Markdown spans, suggested edits — however many there
// are: a person comments on such a page and reads its comments in the app
// whatever it holds, and so does an agent (2026-11-24: on a page with more
// than 25,000 comments every one of those calls, and an agent's comment,
// was refused as invalid). They are read in one snapshot, a window at a time
// from where the last ended (eachWindow), so no one read grows with the page.

// commentSnapshotWindow is how many of a target's comments one read takes.
const commentSnapshotWindow = 5000

// listAllCommentRecordsForTarget is every comment on a target, in the order
// they were made, read as authorize allows in one snapshot.
func listAllCommentRecordsForTarget(
	app core.App,
	target nativeCommentTarget,
	authorize func(core.App) (commentAuthorization, error),
) ([]commentRecord, error) {
	result := []commentRecord{}
	err := readCommentTargetSnapshot(app, target, authorize, func(txApp core.App, workspaceID string) error {
		return eachWindow(commentSnapshotWindow, func(size int, last *commentRecord) ([]commentRecord, error) {
			after := commentRecord{}
			if last != nil {
				after = *last
			}
			return readCommentTargetWindow(txApp, workspaceID, target, `
				AND ({:afterCreated}='' OR [[created_at]]>{:afterCreated}
					OR ([[created_at]]={:afterCreated} AND [[id]]>{:afterId}))
				ORDER BY [[created_at]],[[id]]`, dbx.Params{"afterCreated": after.CreatedAt, "afterId": after.ID}, size)
		}, func(batch []commentRecord) (bool, error) {
			result = append(result, batch...)
			return false, nil
		})
	})
	return result, err
}

// readCommentTargetSnapshot runs read in one snapshot once authorize allows
// the target, with the workspace its comments are in.
func readCommentTargetSnapshot(
	app core.App,
	target nativeCommentTarget,
	authorize func(core.App) (commentAuthorization, error),
	read func(txApp core.App, workspaceID string) error,
) error {
	return runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		if authorization.Target != target {
			return permissionDenied()
		}
		return read(txApp, authorization.Page.Workspace.WorkspaceID)
	})
}

// readCommentTargetWindow reads up to size of a target's comments that the
// SQL after its target's condition keeps, in the order it gives, with params.
func readCommentTargetWindow(
	app core.App, workspaceID string, target nativeCommentTarget, after string, params dbx.Params, size int,
) ([]commentRecord, error) {
	params["workspaceId"], params["targetKind"], params["targetId"], params["limit"] = workspaceID, target.Kind, target.ID, size
	rows := []storedComment{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],[[body_json]],[[notion_discussion_id]],
			[[resolved]],[[revision]],[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}} INDEXED BY [[idx_leafen_comments_target_order]]
		WHERE [[workspace_id]]={:workspaceId} AND [[target_kind]]={:targetKind}
			AND [[target_id]]={:targetId} ` + after + ` LIMIT {:limit}
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	records := make([]commentRecord, 0, len(rows))
	for _, row := range rows {
		record, err := renderComment(row)
		if err != nil || !json.Valid(record.Body) {
			return nil, failedBecause(errCommentFailed, err)
		}
		records = append(records, record)
	}
	return records, nil
}
