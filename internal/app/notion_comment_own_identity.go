package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A comment has one ID for the public API and the MCP's comment tools, as
// Notion's has one (2026-11-26: the same comment read 70f4a239-… in
// notion-get-comments and 2ebc523d-… in GET /v1/comments, and
// notion-update-comment refused the API's). The API answers with the
// comment's own ID where that is a UUID other than the identities made for
// the API (version 8), as a view and a database do
// (notion_object_own_identity.go) — the ID a comment made in the app or by
// the MCP has; one made through the API, whose own ID is no UUID
// ("notion-comment-<UUID>"), answers with the identity made for it. The MCP
// gives the same (mcpCommentID) and finds a comment by either
// (mcpCommentInternalID), and an identity a comment answered with before
// still names it (resolveNotionObjectIdentity).

// commentOwnPublicID is the public ID a comment answers with when its own
// ID is one (objectOwnPublicID).
func commentOwnPublicID(commentID string) (string, bool) {
	commentID = strings.TrimSpace(commentID)
	return commentID, ownPagePublicID(commentID)
}

// notionCommentIdentityByOwnID finds the comment its own ID names
// (commentOwnPublicID): one that stands, or a deleted one its tombstone
// keeps, which the API reads by that ID (comment_tombstones.go).
func notionCommentIdentityByOwnID(app core.App, publicID string) (notionObjectIdentity, error) {
	if !ownPagePublicID(publicID) {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	workspaceID := ""
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]] FROM {{leafen_comments}} WHERE [[id]]={:id}
		UNION ALL
		SELECT [[workspace_id]] FROM {{leafen_comment_tombstones}} WHERE [[id]]={:id}
		LIMIT 1
	`).Bind(dbx.Params{"id": publicID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspaceID == "" {
		return notionObjectIdentity{}, errInvalidNotionObjectIdentity
	}
	if err != nil {
		return notionObjectIdentity{}, err
	}
	return notionObjectIdentity{
		PublicID: publicID, ObjectKind: notionObjectIdentityComment, Workspace: workspaceID, InternalID: publicID,
	}, nil
}

// mcpCommentID is the ID the MCP's comment tools give a comment: the one the
// public API answers with; its own where none can be made.
func mcpCommentID(app core.App, workspaceID, commentID string) string {
	if publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityComment, workspaceID, commentID); err == nil {
		return publicID
	}
	return commentID
}

// mcpCommentInternalID is the comment an ID an agent gives names: the one
// the public API's ID or the MCP's names — a discussion's identity, which
// the discussion tools read, is left as given — or the ID as given.
func mcpCommentInternalID(app core.App, given string) string {
	given = strings.TrimSpace(given)
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityComment, given)
	if err != nil || identity.InternalID == "" || strings.HasPrefix(identity.InternalID, "discussion:") {
		return given
	}
	return identity.InternalID
}
