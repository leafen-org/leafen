package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// authorizeMCPReplyTarget is where a reply goes: the discussion of the
// comment or discussion it names, on whatever page that is, as Notion's MCP
// finds it by that ID alone (seen 2026-11-11: page_id, and a discussion://
// address's page and block, aren't looked at). One the connection can't
// reach is answered as one that isn't there.
func authorizeMCPReplyTarget(
	app core.App, authority mcpAuthority, id, kind string,
) (mcpCommentTargetAuthorization, error) {
	if !validCommentIdentifier(id) {
		return mcpCommentTargetAuthorization{}, mcpReplyTargetMissing(id, kind)
	}
	// A comment is named by the ID the MCP or the public API gives it
	// (mcpCommentInternalID); a refusal names it as given.
	commentID := mcpCommentInternalID(app, id)
	row := storedComment{}
	err := app.DB().NewQuery(`
		SELECT [[page_id]],[[target_kind]],[[target_id]] FROM {{leafen_comments}}
		WHERE [[id]]={:id} LIMIT 1
	`).Bind(dbx.Params{"id": commentID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return mcpCommentTargetAuthorization{}, mcpReplyTargetMissing(id, kind)
	}
	if err != nil {
		return mcpCommentTargetAuthorization{}, failedBecause(errMCPToolFailed, err)
	}
	target := nativePageCommentTarget(row.PageID)
	if row.TargetKind == commentTargetDatabaseRow {
		target = nativeCommentTarget{Kind: commentTargetDatabaseRow, ID: row.TargetID, StoragePageID: row.PageID}
	} else if row.TargetID != "" {
		target = nativePageCommentTarget(row.TargetID)
	}
	if _, err := authorizeMCPCommentTargetCurrent(app, authority, target, permissionWorkspaceRead); err != nil {
		if errors.Is(err, errMCPScopeDenied) {
			return mcpCommentTargetAuthorization{}, mcpReplyTargetMissing(id, kind)
		}
		return mcpCommentTargetAuthorization{}, err
	}
	// Reading it, the account may still not comment there, or it is in Trash.
	authorization, err := authorizeMCPCommentTargetCurrent(app, authority, target, permissionWorkspaceWrite)
	if err != nil {
		return mcpCommentTargetAuthorization{}, err
	}
	return mcpCommentTargetAuthorization{Authorization: authorization, PageID: target.ID, CommentID: commentID}, nil
}

// mcpReplyTargetMissing is Notion's refusal of a reply to a comment or a
// discussion it can't find (seen 2026-11-11).
func mcpReplyTargetMissing(id, kind string) error {
	return mcpDenied("There's no %s with ID: %s that you can reach here; check that you can open it and that this is its workspace: notion-get-comments lists a page's discussions.", kind, id)
}
