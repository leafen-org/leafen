package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// What a migration's scan, settings reading or run says while it goes on:
// where the person watches it live — Leafen's import window
// (notion_migration_live.go), with its stage, the names passing and the
// counts — and that the call goes on with it; the stage it starts is the
// moment to tell (2026-11-21, w2130's walk: an agent ended its turn on
// "Notion's structure is being read now" and the person had to say "go on"
// for the one question to come; user decision 2026-11-23 "묶어서 올려": the
// window shows the counts an agent relayed about once a minute before). Said
// as facts, not as orders for the agent's turn (2026-11-23,
// mcp_answer_rule.go).

// notionMigrationLiveWords is what an answer that starts a stage says is
// left to do with its link, named by its address (notionMigrationNamesLink;
// 2026-11-23: told "a line to the person with link", five agents sent the
// line without the address); the answer gives it as its stage's line in
// the person's language where it knows them (notionMigrationStageLineNext).
const notionMigrationLiveWords = "Left to do as this stage starts, once for it: a line to the person with their own view of it, " +
	"live in Leafen's import window with its counts — " + notionMigrationLinkMark

// notionMigrationLinkMark stands for an answer's link in its next, until
// the answer names it by its address (notionMigrationNamesLink).
const notionMigrationLinkMark = "the address in link"

// notionMigrationNamesLink names an answer's link by its address where its
// next speaks of it.
func notionMigrationNamesLink(next, link string) string {
	if link == "" {
		return next
	}
	return strings.ReplaceAll(next, notionMigrationLinkMark, link)
}

// notionMigrationStartedNext is a started migration's next.
const notionMigrationStartedNext = "Leafen reads the Notion structure now. " + notionMigrationLiveWords + ". leafen_notion_migration_scan with this migration_id goes on with it " +
	"while its status is scanning; the one question comes once it is scanned."

// notionMigrationWithStageLine is an answer's next with its stage's line in
// the person's language in place of the general words (notionMigrationLiveWords).
func notionMigrationWithStageLine(next, language, stage string) string {
	if line := notionMigrationStageLineNext(language, stage); line != "" {
		return strings.Replace(next, notionMigrationLiveWords, line, 1)
	}
	return next
}

// notionMigrationScanningNext is a scan's next while it reads.
func notionMigrationScanningNext(pages, databases, rows int) string {
	return fmt.Sprintf("Still scanning: %d pages, %d databases and %d rows found so far, as Leafen's import window (link) shows them live. "+
		"Calling again goes on with it.", pages, databases, rows)
}

// notionMigrationLiveLink is where the person watches a migration live:
// Leafen's import window, in the migration's workspace, as they open it.
func notionMigrationLiveLink(app core.App, authority mcpAuthority, workspaceID string) string {
	return personLinkAt(app, authority, workspacePath(app, workspaceID, "/settings?section=import&source=notion"))
}
