package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// Whether the Notion connection may read comments, asked first in a scan and
// when an agent lists what a connection reaches. Notion makes a new internal
// connection without "Read comments" (a real move, 2026-11-16) and then
// answers every comment request 403, so a plan bringing comments would bring
// none, and nothing said so before the run. Notion's API tells a
// connection's capabilities only by refusing. The scan keeps it as an issue
// of its own, which the plan's agenda holds while the plan brings comments,
// and the steps for making a connection say to tick it. Leafen only reads
// Notion (its searches and queries are POSTs that change nothing), so the
// update and insert capabilities may stay off. The agent ticks it itself on
// the connection's own page in Notion, after one yes, and shows the person
// only that step where it can't (2026-11-22, the user: everything a person
// can do, an agent does to the end — "사람보고 해달라고 징징하면 안되").

// notionMigrationIssueCommentsUnreadable is the scan's issue for a
// connection Notion keeps every comment from.
const notionMigrationIssueCommentsUnreadable = "comments_unreadable"

// notionConnectionCommentsWithheld asks Notion for a page's or database's
// comments and says whether Notion keeps comments from the connection; any
// other refusal (one deleted since) tells nothing of the connection.
func notionConnectionCommentsWithheld(ctx context.Context, client notionProviderClient, id string) (bool, error) {
	_, err := client.comments(ctx, id, "")
	var status *notionProviderStatusError
	if errors.As(err, &status) && status.status == http.StatusForbidden {
		return true, nil
	}
	if err != nil && !notionProviderRefused(err) {
		return false, err
	}
	return false, nil
}

// notionMigrationScanFirst is what a scan reads before its roots: whether
// the connection may read comments, asked of the first root.
func notionMigrationScanFirst(roots []notionImportRoot) []notionMigrationScanItem {
	if len(roots) == 0 {
		return nil
	}
	return []notionMigrationScanItem{{Kind: "comments", ID: roots[0].ID}}
}

// scanNotionMigrationComments asks it of the scan's first root.
func scanNotionMigrationComments(
	ctx context.Context, client notionProviderClient, change *notionMigrationScanChange, item notionMigrationScanItem,
) error {
	withheld, err := notionConnectionCommentsWithheld(ctx, client, item.ID)
	if err != nil {
		return err
	}
	if withheld {
		// Its detail is the connection's page, where the agent ticks it.
		change.issues = append(change.issues, notionMigrationIssue{Code: notionMigrationIssueCommentsUnreadable,
			Detail: notionConnectionPage(ctx, client)})
	}
	return nil
}

// notionConnectionCommentsNext is what an agent listing a connection's
// roots hears when Notion keeps comments from it: it ticks it itself before
// the scan. Asked of the first root listed, with what is left of the
// listing's time; a query's matches are not asked again.
func notionConnectionCommentsNext(
	ctx context.Context, client notionProviderClient, roots []notionImportRoot, input leafenMCPNotionMigrationSourcesInput,
) string {
	if len(roots) == 0 || strings.TrimSpace(input.Query) != "" || strings.TrimSpace(input.Cursor) != "" {
		return ""
	}
	if withheld, err := notionConnectionCommentsWithheld(ctx, client, roots[0].ID); err != nil || !withheld {
		return ""
	}
	return " Notion keeps comments from this connection, so none would come: before the scan " +
		notionConnectionCommentsTick(notionConnectionPage(ctx, client)) + " If they'd rather not, plan comments \"skip\"."
}

// notionConnectionCommentsTick is how the agent turns "Read comments" on
// for the connection: itself, on the connection's page, after one yes.
func notionConnectionCommentsTick(page string) string {
	return "turn on its “Read comments” yourself. Left to do first: one question to the person, in their language, carrying that “Read " +
		"comments” gets ticked for this connection in their Notion so its comments come, and that Leafen only reads, so “Update content”, " +
		"“Insert content” and “Insert comments” stay as they are. On their yes, open the connection’s own page, " + page + ", " + agentInTheirBrowser +
		"; on its “Configuration” tab under “Capabilities” (a Korean screen reads “구성”, “기능”), tick “Read comments” (“댓글 읽기”), " +
		"and a line says when it’s done. In their browser that tick is the one step left to them: shown on that page, marked, and waited for."
}

// notionConnectionPage is the connection's own page in Notion's developer
// tools, where its capabilities are ticked, at the address Notion's list of
// connections opens it at: its bot user's ID, in the workspace its bot
// names (seen 2026-11-22: GET /v1/users/me's id and bot.workspace_id were
// the test connection's /developers/connections/<id>?spaceId=<workspace>).
// Unknown, it is the list of connections.
func notionConnectionPage(ctx context.Context, client notionProviderClient) string {
	var me struct {
		ID  string `json:"id"`
		Bot struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"bot"`
	}
	if err := client.request(ctx, http.MethodGet, "/v1/users/me", nil, &me); err != nil || !notionConnectionPageID(me.ID) {
		return notionConnectionsPage
	}
	page := notionConnectionsPage + "/" + me.ID
	if notionConnectionPageID(me.Bot.WorkspaceID) {
		page += "?spaceId=" + me.Bot.WorkspaceID
	}
	return page
}

// notionConnectionPageID is an ID Notion gives: hex digits and dashes.
func notionConnectionPageID(id string) bool {
	return id != "" && len(id) <= 64 && strings.Trim(id, "0123456789abcdefABCDEF-") == ""
}

func init() {
	notionMigrationReportText[accountLanguageKo]["issue:"+notionMigrationIssueCommentsUnreadable] =
		"스캔 때 Notion 연결이 댓글을 읽지 못했습니다 — Notion에서 연결의 “구성” → “기능”의 “댓글 읽기”를 켜고 다시 스캔하면 댓글이 옵니다"
	notionMigrationReportText[accountLanguageEn]["issue:"+notionMigrationIssueCommentsUnreadable] =
		"At the scan, the Notion connection couldn’t read comments — tick “Read comments” under the connection’s “Configuration” → “Capabilities” in Notion and scan again to bring them"
}
