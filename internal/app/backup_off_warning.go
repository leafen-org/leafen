package app

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Scheduled backups are off until an administrator turns them on, and the
// band that suggests them over every page waits a week
// (scheduled_backup_suggestion.go). Where the server's person looks first or
// a lot comes in, it says so at once, with where they are turned on, and
// says nothing once they are on (2026-11-16: a server holding a company's
// pages and files ran with backups off, and nothing had told its person):
// the first run's setup screen and every start of the server, a Notion
// migration's start and its one confirmation, and the server's settings
// (web: BackupOffLine).

// scheduledBackupsOff says whether scheduled backups are off on this server.
func scheduledBackupsOff(app core.App) bool {
	stored, err := loadScheduledBackup(app)
	return err == nil && !stored.Enabled
}

// serverHoldsCounts is the pages and files the server holds now: its
// people's pages out of the trash, not each workspace's People database and
// its people's pages, which the server makes (workspaceArchiveTargetPageSQL),
// nor an import's pages still being made.
func serverHoldsCounts(app core.App) (pages, files int) {
	_ = app.DB().NewQuery(`SELECT
		(SELECT COUNT(*) FROM {{leafen_pages}} AS [[page]] WHERE [[page]].[[in_trash]] = FALSE
			AND [[page]].[[notion_import_staging]] = FALSE AND `+workspaceArchiveTargetPageSQL("[[page]]")+`),
		(SELECT COUNT(*) FROM {{leafen_files}})`).Row(&pages, &files)
	return pages, files
}

// serverHoldsWords is what the server holds, in words ("3 pages and 1
// file"), "" for nothing.
func serverHoldsWords(pages, files int) string {
	counted := func(count int, one string) string {
		if count == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", count, one)
	}
	switch {
	case pages > 0 && files > 0:
		return counted(pages, "page") + " and " + counted(files, "file")
	case pages > 0:
		return counted(pages, "page")
	case files > 0:
		return counted(files, "file")
	}
	return ""
}

// warnBackupsOff says at the server's start that backups are off, for
// whoever runs it and reads its output (a NAS's container log among them).
// A new server, holding nothing yet, only says where they are turned on
// (2026-11-17: on a first Docker install its "0 pages and 0 files … a
// broken disk loses them" read as something gone wrong).
func warnBackupsOff(app core.App) {
	if !scheduledBackupsOff(app) {
		return
	}
	pages, files := serverHoldsCounts(app)
	where := mcpScreen("", "settings", "server", "serverBackups")
	holds := serverHoldsWords(pages, files)
	if holds == "" {
		message := "Scheduled backups are off; this server holds no pages or files yet. An administrator turns them on in " + where + "."
		app.Logger().Info(message)
		fmt.Println(message)
		return
	}
	are, them := "are", "them"
	if pages+files == 1 {
		are, them = "is", "it"
	}
	message := fmt.Sprintf("Scheduled backups are off: this server's %s %s on it alone, and a broken disk loses %s. "+
		"An administrator turns them on in %s.", holds, are, them, where)
	app.Logger().Warn(message)
	fmt.Println("Warning: " + message)
}

// notionMigrationBackupsOffLine is what a Notion migration's start and its
// one confirmation say while backups are off, "" once they are on: what is
// left to do is a line to the person with the recommendation, in the
// message the step goes in, and whoever administers the server turns them
// on in the screen, which takes the place and its keys. The move doesn't
// wait for it.
func notionMigrationBackupsOffLine(app core.App, personID string) string {
	if !scheduledBackupsOff(app) {
		return ""
	}
	pages, files := serverHoldsCounts(app)
	screen := mcpScreen(mcpScreenLanguage(app, personID), "settings", "server", "serverBackups")
	who := "the person turns them on in " + screen
	if administrator, err := isAdministrator(app, personID); err != nil || !administrator {
		who = "the server's administrator turns them on in " + screen + ", at the person's word"
	}
	alone := "everything this move brings will be on it alone, and a broken disk loses it"
	if holds := serverHoldsWords(pages, files); holds != "" {
		alone = "its " + holds + ", and everything this move brings, are on it alone, and a broken disk loses them"
	}
	// Said as facts and what is left to do, as every answer is (mcp_answer_rule.go).
	return fmt.Sprintf("Scheduled backups are off on this server: %s; %s, choosing the place and entering its keys there — the screen takes them, "+
		"not a conversation. Left to do, in the message this step's line goes in: that, with the recommendation to turn them on first. "+
		"The move runs either way.", alone, who)
}

// notionMigrationStartNoted puts the backups line before a started
// migration's next step.
type notionMigrationStartNoted struct {
	app core.App
	ctx context.Context
}

func (noted notionMigrationStartNoted) with(result *mcp.CallToolResult, output leafenMCPNotionMigrationOutput, err error) (
	*mcp.CallToolResult, leafenMCPNotionMigrationOutput, error) {
	if err != nil || output.Migration.ID == "" {
		return result, output, err
	}
	authority, failed := mcpAuthorityFromContext(noted.ctx)
	if failed != nil {
		return result, output, err
	}
	if line := notionMigrationBackupsOffLine(noted.app, authority.owner()); line != "" {
		output.Next = line + " " + output.Next
	}
	return result, output, err
}
