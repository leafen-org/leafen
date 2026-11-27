package app

import (
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A notice's comment thread, for an agent (leafen_inbox_item): the
// discussion the comment is in, a window of it — its newest comments,
// reaching back to the one the notice is about when that is older — with how
// many it leaves out before and after, and the discussion's name, which
// notion-get-comments reads whole.

const mcpInboxThreadWindow = 50

type storedMCPInboxThreadComment struct {
	ID      string `db:"id"`
	Author  string `db:"author"`
	Body    string `db:"body_json"`
	Created string `db:"created_at"`
}

// readMCPInboxThread reads a notice's thread as the agent reads it: a
// mention closed to it "Untitled" (mcp_reader_mentions.go).
func readMCPInboxThread(app core.App, closed searchMentionCloser, notice mcpInboxNotice, output *leafenMCPInboxItemOutput) error {
	about := struct {
		ID      string `db:"id"`
		Parent  string `db:"parent_id"`
		Created string `db:"created_at"`
	}{}
	err := app.DB().NewQuery(`SELECT [[id]], [[parent_id]], [[created_at]] FROM {{leafen_comments}} WHERE [[id]] = {:id} AND [[page_id]] = {:pageId}`).
		Bind(dbx.Params{"id": notice.CommentID, "pageId": notice.PageID}).One(&about)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	root := struct {
		ID          string `db:"id"`
		BlockID     string `db:"block_id"`
		TargetID    string `db:"target_id"`
		WorkspaceID string `db:"workspace_id"`
	}{ID: about.ID}
	if about.Parent != "" {
		root.ID = about.Parent
	}
	if err := app.DB().NewQuery(`SELECT [[id]], [[block_id]], [[target_id]], [[workspace_id]] FROM {{leafen_comments}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": root.ID}).One(&root); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	const thread = ` FROM {{leafen_comments}} [[comment]]
		WHERE [[comment]].[[page_id]] = {:pageId} AND ([[comment]].[[id]] = {:root} OR [[comment]].[[parent_id]] = {:root})`
	const comments = `SELECT [[comment]].[[id]], COALESCE(NULLIF(trim([[user]].[[name]]), ''), [[user]].[[email]], '') AS [[author]],
		[[comment]].[[body_json]], [[comment]].[[created_at]]
		FROM {{leafen_comments}} [[comment]] LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[comment]].[[author_id]]
		WHERE [[comment]].[[page_id]] = {:pageId} AND ([[comment]].[[id]] = {:root} OR [[comment]].[[parent_id]] = {:root})`
	params := dbx.Params{
		"pageId": notice.PageID, "root": root.ID, "at": about.Created, "id": about.ID, "limit": mcpInboxThreadWindow,
	}
	newer := 0
	if err := app.DB().NewQuery(`SELECT COUNT(*)` + thread + `
		AND ([[comment]].[[created_at]] > {:at} OR ([[comment]].[[created_at]] = {:at} AND [[comment]].[[id]] > {:id}))`).
		Bind(params).Row(&newer); err != nil {
		return err
	}
	rows := []storedMCPInboxThreadComment{}
	if newer < mcpInboxThreadWindow {
		// The newest comments hold the one the notice is about.
		if err := app.DB().NewQuery(comments + `
			ORDER BY [[comment]].[[created_at]] DESC, [[comment]].[[id]] DESC LIMIT {:limit}`).Bind(params).All(&rows); err != nil {
			return err
		}
		slices.Reverse(rows)
	} else if err := app.DB().NewQuery(comments + `
		AND ([[comment]].[[created_at]] > {:at} OR ([[comment]].[[created_at]] = {:at} AND [[comment]].[[id]] >= {:id}))
		ORDER BY [[comment]].[[created_at]], [[comment]].[[id]] LIMIT {:limit}`).Bind(params).All(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	total, earlier := 0, 0
	if err := app.DB().NewQuery(`SELECT COUNT(*)` + thread).Bind(params).Row(&total); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*)` + thread + `
		AND ([[comment]].[[created_at]] < {:firstAt} OR ([[comment]].[[created_at]] = {:firstAt} AND [[comment]].[[id]] < {:firstId}))`).
		Bind(dbx.Params{"pageId": notice.PageID, "root": root.ID, "firstAt": rows[0].Created, "firstId": rows[0].ID}).Row(&earlier); err != nil {
		return err
	}
	for _, row := range rows {
		body, err := decodeNotionCommentProjectionBody(row.Body)
		if err != nil {
			continue
		}
		var text strings.Builder
		for _, span := range body.Rich {
			text.WriteString(searchMentionWords(closed, span.Mention, span.PageID, span.DatabaseID, span.Text))
		}
		output.Thread = append(output.Thread, leafenMCPInboxThreadComment{From: row.Author, Text: text.String(), At: row.Created})
	}
	output.ThreadEarlier, output.ThreadLater = earlier, max(total-earlier-len(rows), 0)
	if root.TargetID != "" {
		output.DiscussionID = mcpDiscussionID(app, root.WorkspaceID, root.TargetID, root.BlockID, root.ID)
		output.DiscussionPage = root.TargetID
	}
	return nil
}
