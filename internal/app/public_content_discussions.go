package app

import (
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// maxPublicDiscussionComments is how many comments a visitor's "토론"
// reads at a time, oldest first; nextCursor reads on to the newest.
const maxPublicDiscussionComments = 200

// readPublicContentDiscussions answers a visitor's "토론", as Notion's public
// page shows its discussions to anyone the link reaches (seen 2026-11-06):
// the page's comments with their authors' names and pictures, read only —
// no attachments, reactions or anything kept for one reader.
func readPublicContentDiscussions(app core.App, input publicContentDiscussionsRequest, viewSecret string) (publicContentDiscussionsResponse, error) {
	// Only a web link's page shows them: Notion's site header has no "토론".
	if action := strings.TrimSpace(input.Action); action != publicContentActionDiscussions {
		return publicContentDiscussionsResponse{}, refuse(errPublicContentInvalid, "action is %s, not %q", publicContentActionDiscussions, action)
	}
	if kind := strings.TrimSpace(input.SourceKind); kind != publicContentSourceShare {
		return publicContentDiscussionsResponse{}, refuse(errPublicContentInvalid,
			"sourceKind is %s, not %q: only a web link's page shows its discussions", publicContentSourceShare, kind)
	}
	snapshot := publicContentSnapshotRequest{
		Action: publicContentActionSnapshot, SourceKind: input.SourceKind,
		Token: input.Token, SiteSlug: input.SiteSlug, PageID: input.PageID,
	}
	authorization, _, page, err := authorizePublicContentPage(app, &snapshot, viewSecret, time.Now().UTC())
	if err != nil {
		return publicContentDiscussionsResponse{}, err
	}
	after, err := decodeTextIDCursor(input.Cursor)
	if err != nil {
		return publicContentDiscussionsResponse{}, refuse(errPublicContentInvalid, "cursor isn't the nextCursor of the last answer: start again without it")
	}
	rows := []storedComment{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],
			[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
			[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}} INDEXED BY [[idx_leafen_comments_page_order]]
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
			AND [[target_kind]]='page' AND [[target_id]]={:pageId}
			AND ({:afterId} = '' OR [[created_at]] > {:afterAt} OR ([[created_at]] = {:afterAt} AND [[id]] > {:afterId}))
		ORDER BY [[created_at]],[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": authorization.Workspace.Id, "pageId": page.Id, "limit": maxPublicDiscussionComments + 1,
		"afterAt": after.At, "afterId": after.ID,
	}).All(&rows); err != nil {
		return publicContentDiscussionsResponse{}, err
	}
	response := publicContentDiscussionsResponse{
		Items: []publicDiscussionComment{}, People: []commentActorProfile{}, SourceRevision: authorization.SourceRevision,
	}
	if len(rows) > maxPublicDiscussionComments {
		rows = rows[:maxPublicDiscussionComments]
		response.NextCursor = encodeTextIDCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
	}
	authors := make([]string, 0, len(rows))
	for _, row := range rows {
		record, err := renderComment(row)
		if err != nil {
			return publicContentDiscussionsResponse{}, err
		}
		response.Items = append(response.Items, publicDiscussionComment{
			ID: record.ID, BlockID: record.BlockID, ParentID: record.ParentID, AuthorID: record.AuthorID,
			Body: record.Body, Resolved: record.Resolved, CreatedAt: record.CreatedAt, EditedAt: record.EditedAt,
		})
		authors = append(authors, record.AuthorID)
	}
	if response.People, err = readCanonicalActorProfiles(app, authors); err != nil {
		return publicContentDiscussionsResponse{}, err
	}
	return response, nil
}
