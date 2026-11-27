package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An agent that has connected to Leafen asks its person how to start —
// moving their Notion workspace here, or from an empty workspace — only
// where nothing has come into their workspace yet: the first connection
// after installing (user 2026-11-20, a hand test on a NAS: asking at the
// first connection is right, but an agent connected later "was just
// connected", and one connected after the move asked again). The one
// judgement is here; everything that offers the start follows it: the
// connection text's end (the connection code's offerStart,
// web/src/lib/agentConnection.ts — the first run's agents step makes the
// same text), what `leafen connect` and an allowed device connection say
// once connected (agentStartNext), and `leafen setup status`'s next step
// (its fact startOffered). Elsewhere the agent says it is connected and
// moves nothing until its person asks; the MCP instructions name where a
// Notion move starts.

// agentStartOffered reports whether each workspace named is one nothing has
// come into yet: it holds at most one page — the welcome page it started
// with — counted as an archive import counts its target's pages
// (workspaceArchiveTargetPages: in the trash and still staging alike, rows
// too), and no Notion move began in it (a scan that failed with nothing run
// doesn't count).
func agentStartOffered(app core.App, workspaceIDs []string) bool {
	if len(workspaceIDs) == 0 {
		return false
	}
	for _, workspaceID := range workspaceIDs {
		pages, err := workspaceArchiveTargetPages(app, workspaceID, false)
		if err != nil || pages > 1 {
			return false
		}
		moves := 0
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_notion_migrations}}
			WHERE [[workspace_id]] = {:workspaceId} AND ([[status]] != 'failed' OR [[run_state]] != '')
		`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&moves); err != nil || moves > 0 {
			return false
		}
	}
	return true
}

// agentStartAsk is what an agent does once connected where the start is
// offered, as the web's connection text says it (agentPrompt startInChat):
// the question itself tells how a move goes, in the move's first words in
// Leafen's words in the person's language (notion_migration_first_words.go),
// so the person knows before choosing and the agent need not say them after
// (2026-11-21, the real Claude Code: given them for the answer after the
// person chose the move, it said part of them in the question and skipped
// the rest after; given them only in a tool's answer, it skipped them once
// in two).
func agentStartAsk(language string) string {
	return "Left to do then: one question to your person, in their language, how they'd like to start — by moving their Notion " +
		"workspace here, carrying in the same question all that this says of how that goes: “" + notionImportWords(language).FirstWords +
		"” — or from an empty workspace. If they choose the move, read leafen_notion_migration_guide, call " +
		"leafen_notion_migration_sources and go on as it says, the Notion connection made here in the conversation."
}

// agentStartNext is the line that offers the start after connecting to the
// workspaces named, or none where something has come into them already, in
// the language of the owner's Leafen.
func agentStartNext(app core.App, ownerID string, workspaceIDs []string) []string {
	if !agentStartOffered(app, workspaceIDs) {
		return nil
	}
	return []string{agentStartAsk(personLanguageCode(app, ownerID, nil))}
}
