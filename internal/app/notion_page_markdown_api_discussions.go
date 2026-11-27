package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// The public API's page Markdown names a page's discussions as Notion's does
// (seen 2026-11-22, workers w2334 and w2339, Notion-Version 2026-03-11):
// GET /v1/pages/{id}/markdown writes each run of words a discussion was left
// on in <span discussion-urls="…">, by the discussion://page/block/discussion
// address Notion's MCP fetch names it by, in the API's IDs — "v1" inside the
// run's marks ("**<span …>sierra</span>** tango"), "v2" as the MCP fetch —
// and PATCH reads its update_content against that Markdown and its new
// Markdown's spans back, as Leafen's MCP writes do (mcp_write_discussions.go):
// an old_str across a span's edge finds nothing ("echo foxtrot" with "echo"
// marked, 400), a change beside a span keeps its mark ("xray."→"xray2."), and
// a span naming a discussion of the page marks its words, another block's
// too. A connection that may not read comments reads none.

// notionIdentityPageMarkdownReader is the reader of a page's Markdown for a
// public API connection (notionIdentityMarkdownReader), naming the page's
// discussions and reading them back.
func notionIdentityPageMarkdownReader(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, markdownVersion, pageID, publicPageID string,
) (notionMarkdownReader, error) {
	reader, err := notionIdentityMarkdownReader(event, identity, markdownVersion)
	if err != nil {
		return reader, err
	}
	reader.Discussions, reader.Threads, err = notionAPIMarkdownDiscussions(event.App, identity, pageID, publicPageID)
	return reader, err
}

// notionAPIMarkdownDiscussions are a page's discussions as its public API
// Markdown names them: none for a connection without comments:read, one
// whose person can't read the page's comments, or a page without any.
func notionAPIMarkdownDiscussions(
	app core.App, identity notionOAuthBearerIdentity, pageID, publicPageID string,
) (names func(threads, blockID string) string, threads func(string) string, err error) {
	if !notionOAuthHasScope(identity, "comments:read") {
		return nil, nil, nil
	}
	target := notionCommentTarget{Identity: identity, PageID: pageID, StoragePageID: pageID, TargetKind: commentTargetPage}
	// A database page's comments are its row's, as in the app.
	if page, err := app.FindRecordById(pagesCollection, pageID); err == nil && isDatabaseRowPageRecord(page) {
		_, row, err := authorizeDatabaseRowCommentTarget(app, identity.PrincipalID, pageID, permissionWorkspaceRead)
		if err != nil {
			return nil, nil, notionUnreadComments(err)
		}
		target.PageID, target.StoragePageID, target.TargetKind = row.RowID, row.ContainerPageID, commentTargetDatabaseRow
	}
	authorize := func(txApp core.App) (commentAuthorization, error) {
		return target.authorizeCurrent(txApp, permissionWorkspaceRead)
	}
	if _, err := authorize(app); err != nil {
		return nil, nil, notionUnreadComments(err)
	}
	records, err := listAllCommentRecordsForTarget(app, target.nativeTarget(), authorize)
	if err != nil {
		return nil, nil, notionUnreadComments(err)
	}
	pageAddress := notionPageAnswerID(app, pageID, publicPageID)
	// A block is named by its public ID, as the API's Markdown names one.
	public := func(blockID string) string {
		if blockID == "" {
			return ""
		}
		if named, err := notionBlockPublicID(app, identity.WorkspaceID, blockID); err == nil {
			return named
		}
		return blockID
	}
	names, threads = pageMarkdownDiscussions(records, func(root commentRecord) string {
		return mcpDiscussionAddress(pageAddress, public(root.BlockID), mcpDiscussionKey(app, root.WorkspaceID, root.ID))
	}, public)
	return names, threads, nil
}

// notionUnreadComments is nil for comments the connection may not read,
// which leave its Markdown without discussions, and the error otherwise.
func notionUnreadComments(err error) error {
	if isPermissionDenied(err) || errors.Is(err, errCommentNotFound) || errors.Is(err, errDatabaseRowPageNotFound) ||
		errors.Is(err, errCommentDeleteBusy) {
		return nil
	}
	return err
}
