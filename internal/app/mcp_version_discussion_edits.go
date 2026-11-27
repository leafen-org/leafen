package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A discussion started on a page edits what it was started on, as
// get-version-changes counts it, as Notion's does (seen 2026-11-22, worker
// w2328 and worker w2334, twice): a block listed for an edit in the range
// was first or last edited when a discussion was started on its words, or on
// it, and its commenter is among its contributors, an agent's as the agent's
// (an integration's edit then an MCP comment: both, in that order, last
// edited at the comment); the page's title, listed for its own change, when
// one was started on the page itself (first edited at a page comment before
// the title's rename). A reply counts for nothing (Notion's last edits stayed
// at the discussion's start), and a discussion alone lists no block or title
// (mcpVersionContentAsCompared): this only names the times and people of the
// records listed for their own changes. Resolving or deleting a discussion
// counts for nothing here (Notion's MCP tools for those need its Business
// plan; not measured).

// mcpVersionDiscussionWindow is how many discussion marks, or first
// comments, a range's count reads at first: it reads on, a window at a time,
// until none is left (eachWindow). It read a page's first 25,000 first
// comments by when they were made and the range's first 2,000 marks, and
// then kept those in the range: a later version of a page with more lost
// the people who commented (2026-11-23).
const mcpVersionDiscussionWindow = 200

// mcpVersionDiscussionEdits counts the discussions started on a page between
// start (not included) and end, in milliseconds, among the edits of the
// records already counted: a run of words a discussion marked, from its
// version (pageHistoryVersionCommentMarks), which names the connection it came
// through; a discussion on a block's or the page's own, from its first comment.
func mcpVersionDiscussionEdits(
	app core.App, authority mcpAuthority, pageID string, start, end int64, edits map[string]*mcpVersionEdits,
) error {
	type mark struct {
		VersionCursor  int    `db:"version_cursor"`
		ActorID        string `db:"actor_id"`
		CreatedAt      int64  `db:"created_at"`
		ConnectionName string `db:"connection_name"`
		ConnectionID   string `db:"connection_id"`
	}
	err := eachWindow(mcpVersionDiscussionWindow, func(size int, last *mark) ([]mark, error) {
		after := 0
		if last != nil {
			after = last.VersionCursor
		}
		marks := []mark{}
		err := app.DB().NewQuery(`
			SELECT [[version_cursor]],[[actor_id]],[[created_at]],[[connection_name]],[[connection_id]]
			FROM {{leafen_page_history_versions}}
			WHERE [[page_id]]={:pageId} AND [[source]]={:source} AND [[created_at]]>{:start} AND [[created_at]]<={:end}
				AND [[version_cursor]]>{:after}
			ORDER BY [[version_cursor]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"pageId": pageID, "source": pageHistoryVersionCommentMarks, "start": start, "end": end,
			"after": after, "limit": size,
		}).All(&marks)
		return marks, err
	}, func(marks []mark) (bool, error) {
		for _, mark := range marks {
			events, err := loadPageHistoryEventsUpTo(app, pageID, mark.VersionCursor-1, mark.VersionCursor, 1)
			if err != nil {
				return false, err
			}
			contributor := mcpVersionContributor(pageHistoryVersionWire{
				ActorID: mark.ActorID, ConnectionName: mark.ConnectionName, ConnectionKind: pageHistoryConnectionKind(mark.ConnectionID),
			})
			for _, event := range events {
				envelope, err := pageHistoryEventPayload(event.Payload)
				if err != nil {
					return false, err
				}
				for _, delta := range envelope.TextDeltas {
					if record := edits[delta.BlockID]; record != nil {
						record.add(contributor, mark.CreatedAt)
					}
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	return mcpVersionOwnDiscussionEdits(app, authority, pageID, start, end, edits)
}

// mcpCommentMillisSQL is a comment's time, as written, in milliseconds,
// rounded as SQLite reads it (to the nearest; Go's UnixMilli truncates, so
// one read by both may be a millisecond later here).
const mcpCommentMillisSQL = `CAST(ROUND((julianday([[created_at]]) - 2440587.5) * 86400000) AS INTEGER)`

// mcpVersionOwnDiscussionEdits counts the discussions started on a block's
// or the page's own, which mark no words, by their first comments: the
// block's edits, or the title's for the page's. Its commenter is named with
// the connection the comment was written through (comment_connections.go).
// Only the first comments made in the range are read, as the read itself
// asks (a millisecond wider at its end for the rounding), and each is then
// placed by its time as Go reads it.
func mcpVersionOwnDiscussionEdits(
	app core.App, authority mcpAuthority, pageID string, start, end int64, edits map[string]*mcpVersionEdits,
) error {
	target, err := authorizeMCPCommentTarget(app, authority, pageID, permissionWorkspaceRead)
	if err != nil {
		return nil
	}
	type root struct {
		ID             string `db:"id"`
		BlockID        string `db:"block_id"`
		AuthorID       string `db:"author_id"`
		Body           string `db:"body_json"`
		CreatedAt      string `db:"created_at"`
		ConnectionID   string `db:"connection_id"`
		ConnectionName string `db:"connection_name"`
	}
	return eachWindow(mcpVersionDiscussionWindow, func(size int, last *root) ([]root, error) {
		afterCreated, afterID := "", ""
		if last != nil {
			afterCreated, afterID = last.CreatedAt, last.ID
		}
		roots := []root{}
		err := app.DB().NewQuery(`
			SELECT [[id]],[[block_id]],[[author_id]],[[body_json]],[[created_at]],[[connection_id]],[[connection_name]]
			FROM {{leafen_comments}} INDEXED BY [[idx_leafen_comments_target_thread]]
			WHERE [[workspace_id]]={:workspaceId} AND [[target_kind]]={:targetKind} AND [[target_id]]={:targetId}
				AND [[parent_id]]=''
				AND ({:afterId}='' OR [[created_at]]>{:afterCreated} OR ([[created_at]]={:afterCreated} AND [[id]]>{:afterId}))
				AND ` + mcpCommentMillisSQL + `>{:start} AND ` + mcpCommentMillisSQL + `<={:end}+1
			ORDER BY [[created_at]],[[id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": target.Authorization.Page.Workspace.WorkspaceID, "targetKind": target.Authorization.Target.Kind,
			"targetId": target.Authorization.Target.ID, "start": start, "end": end,
			"afterCreated": afterCreated, "afterId": afterID, "limit": size,
		}).All(&roots)
		return roots, err
	}, func(roots []root) (bool, error) {
		for _, root := range roots {
			at, err := time.Parse(time.RFC3339Nano, root.CreatedAt)
			if err != nil || at.UnixMilli() <= start || at.UnixMilli() > end {
				continue
			}
			key := "title"
			if root.BlockID != "" {
				// Words it was left on are counted by their mark.
				anchor := commentQuoteAnchor{}
				if json.Unmarshal([]byte(root.Body), &anchor) != nil || anchor.onWords() {
					continue
				}
				key = root.BlockID
			}
			if record := edits[key]; record != nil {
				record.add(mcpVersionContributor(pageHistoryVersionWire{
					ActorID: root.AuthorID, ConnectionName: root.ConnectionName, ConnectionKind: pageHistoryConnectionKind(root.ConnectionID),
				}), at.UnixMilli())
			}
		}
		return false, nil
	})
}
