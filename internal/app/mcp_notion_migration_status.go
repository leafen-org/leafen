package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// A migration as its agent follows it: one stage to look at, the link the
// person can open now, the migration, its run's import with its progress
// and real counts, what hasn't come whole so far with what and why, and the
// next step of its road in words (notion_migration_flow.go) — 2026-11-16: a
// real run showed four status fields at once, no link until its end and its
// problems as counts alone.

type leafenMCPNotionMigrationStatusOutput struct {
	// scanning, planning (scanned, not run), reading, creating, comments,
	// verifying, done or stopped.
	Stage string `json:"stage"`
	// What the person can open now (notionMigrationStatusLinks): Leafen's
	// import screen, where the move shows live, until the run is done, and
	// then the moved content's top page; the top page apart while a run goes
	// with content made already, a run again's from the start; and the run's
	// "Migration report" page once written — each as the person opens it, in
	// the desktop app a leafen: link to its window (person_links.go).
	Link      string `json:"link,omitempty"`
	PagesLink string `json:"pages_link,omitempty"`
	// The line for the person as the stage starts, with link's address in it
	// (notion_migration_stage_lines.go).
	StageLine  string `json:"stage_line,omitempty"`
	ReportLink string `json:"report_link,omitempty"`
	// The migration and its run's import, short unless the call asks for
	// detail (mcp_notion_migration_status_short.go).
	Migration map[string]any `json:"migration"`
	Import    map[string]any `json:"import,omitempty"`
	// What hasn't come whole so far, each item of not_whole in one line with
	// the first pages it was on, linked, and what was there; more items with
	// import_issues_more, every page in the report page
	// (notion_import_issue_places.go).
	ImportIssues     []leafenMCPNotionImportIssue `json:"import_issues,omitempty"`
	ImportIssuesMore bool                         `json:"import_issues_more,omitzero"`
	// What hasn't come whole in all, as Leafen's finish screen and the report
	// page count and say it: the three counts, and under them each item in
	// the person's words (notion_import_not_whole.go).
	NotWhole *leafenMCPNotionNotWhole `json:"not_whole,omitempty"`
	// Once done, how many things Notion's API didn't give stay at Leafen's
	// defaults (notion_migration_formats_left.go), and the same by kind and
	// reason, as the report page says each in one line with the ones it is
	// folded under it: the end report says these lines
	// (notion_migration_report_groups.go). Item by item only with list
	// formats_left (mcp_notion_migration_status_lists.go).
	FormatsLeftTotal  int                               `json:"formats_left_total,omitzero"`
	FormatsLeftGroups []leafenMCPNotionFormatsLeftGroup `json:"formats_left_groups,omitempty"`
	// What the check against Notion still finds different, by what differs
	// and why, as the report page says it.
	CheckGroups []leafenMCPNotionReportGroup `json:"check_groups,omitempty"`
	// The files the migration still couldn't copy, its earlier runs' too, by
	// why, as the report page's first lines say them
	// (notion_migration_report_files.go).
	FileGroups []leafenMCPNotionReportGroup `json:"file_groups,omitempty"`
	// Read without migration_id, the person's other migrations, newest first
	// (mcp_notion_migration_latest.go).
	OtherMigrations []leafenMCPNotionMigrationBrief `json:"other_migrations,omitempty"`
	// The window of the one list the call asked for, and where the next
	// window starts.
	FormatsLeft    []leafenMCPNotionFormatLeft       `json:"formats_left,omitempty"`
	Differences    []notionMigrationVerifyDifference `json:"differences,omitempty"`
	ReportedValues []leafenMCPNotionReportedValue    `json:"reported_values,omitempty"`
	NextCursor     string                            `json:"next_cursor,omitempty"`
	Next           string                            `json:"next"`
}

// leafenMCPNotionImportIssue is an item of what hasn't come whole — its kind,
// why, the database a linked one shows when outside the import, how many
// and its line in the person's words, as not_whole says it — with the first
// pages it was on and how many pages in all.
type leafenMCPNotionImportIssue struct {
	Kind       string                           `json:"kind"`
	Reason     string                           `json:"reason"`
	Source     string                           `json:"source,omitempty"`
	Count      int                              `json:"count"`
	Say        string                           `json:"say"`
	Pages      []leafenMCPNotionImportIssuePage `json:"pages,omitempty"`
	PagesTotal int                              `json:"pages_total,omitzero"`
}

// leafenMCPNotionImportIssuePage is a page an item was on: the page, the link
// the person opens it by, what was there — a block by its kind's name, a
// property or view by its own, one with no name by what it is — and how many.
type leafenMCPNotionImportIssuePage struct {
	PageID    string `json:"page_id,omitempty"`
	PageTitle string `json:"page_title,omitempty"`
	Link      string `json:"link,omitempty"`
	What      string `json:"what"`
	Count     int    `json:"count"`
}

type leafenMCPNotionNotWhole struct {
	Blocks     int                           `json:"blocks"`
	Properties int                           `json:"properties"`
	Views      int                           `json:"views"`
	Items      []leafenMCPNotionNotWholeItem `json:"items"`
}

type leafenMCPNotionNotWholeItem struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	Say    string `json:"say"`
}

// leafenMCPNotionMigrationStatusIssues is how many of a run's issues the
// status names.
const leafenMCPNotionMigrationStatusIssues = 20

// leafenMCPNotionMigrationStatus is a run's status as the run starts: its
// next names the stages still to come, as a status asked again doesn't.
func leafenMCPNotionMigrationStatus(app core.App, authority mcpAuthority, migration notionMigration) leafenMCPNotionMigrationStatusOutput {
	return leafenMCPNotionMigrationStatusOf(app, authority, migration, mcpAnswerListing{}, false, true)
}

// leafenMCPNotionMigrationStatusListed is the status with the window of the
// list the call asked for, the migration and its import whole with detail.
func leafenMCPNotionMigrationStatusListed(
	app core.App, authority mcpAuthority, migration notionMigration, listing mcpAnswerListing, detail bool,
) leafenMCPNotionMigrationStatusOutput {
	return leafenMCPNotionMigrationStatusOf(app, authority, migration, listing, detail, detail)
}

func leafenMCPNotionMigrationStatusOf(
	app core.App, authority mcpAuthority, migration notionMigration, listing mcpAnswerListing, detail, steps bool,
) leafenMCPNotionMigrationStatusOutput {
	ownerID := authority.owner()
	language, _ := serverTextLanguage(app, ownerID, nil)
	output := leafenMCPNotionMigrationStatusOutput{Migration: notionMigrationStatusFieldsIn(notionMigrationAnswered(migration), detail, language)}
	var left []leafenMCPNotionFormatLeft
	var job *notionImportJob
	if migration.ImportJobID != "" {
		if read, err := notionImportResponse(app, migration.ImportJobID, ownerID, migration.WorkspaceID); err == nil {
			job = &read
		}
	}
	output.Import = notionImportStatusFields(job, detail)
	output.Stage = notionMigrationStatusStage(migration, job)
	if !detail && notionMigrationStageGoes(output.Stage) {
		output.Import = notionImportGoingFields(output.Import)
	}
	output.Link, output.PagesLink = notionMigrationStatusLinks(app, authority, migration, output.Stage)
	if report := migration.RunReport.ReportPageID; report != "" {
		output.ReportLink = personLinkAt(app, authority, workspacePath(app, migration.WorkspaceID, "/p/"+report))
	}
	if job != nil {
		if report := job.Report; len(report.NotWhole) > 0 {
			output.NotWhole = &leafenMCPNotionNotWhole{Blocks: report.BlockIssues, Properties: report.PropertyIssues, Views: report.ViewIssues}
			text := notionImportNotWholeText(language, migration.Source)
			for _, item := range report.NotWhole {
				output.NotWhole.Items = append(output.NotWhole.Items, leafenMCPNotionNotWholeItem{
					Kind: item.Kind, Reason: item.Reason, Count: item.Count, Say: notionImportNotWholeSay(language, text, item),
				})
			}
		}
		output.ImportIssues, output.ImportIssuesMore = leafenMCPNotionImportIssues(
			app, authority, migration, *job, detail || !notionMigrationStageGoes(output.Stage))
	}
	output.Next = notionMigrationNamesLink(notionMigrationFlowNext(app, ownerID, migration, job, steps), output.Link)
	minutes := 0
	if job != nil && job.Progress != nil {
		minutes = job.Progress.MinutesLeft
	}
	output.StageLine = notionMigrationStageLineIn(language, migration, job, output.Link, minutes)
	if migration.RunState == notionMigrationRunDone {
		text := notionImportNotWholeText(language, migration.Source)
		output.CheckGroups = notionMigrationReportGroupsSaid(notionMigrationVerifyGroups(migration.RunReport.Verify, language, text))
		if settled, err := notionMigrationIssuesSettled(app, migration.ID); err == nil {
			if files, counts, err := loadNotionMigrationReportFiles(app, migration.ImportJobID, migration.ID, settled); err == nil {
				output.FileGroups = notionMigrationReportGroupsSaid(notionMigrationReportFileGroups(
					notionMigrationReportDetails{Files: files, FileCounts: counts}, text))
			}
		}
		if read, err := notionMigrationFormatsLeftAll(app, migration.ImportJobID); err == nil && len(read) > 0 {
			groups := notionMigrationFormatsLeftGroups(language, read)
			left, output.FormatsLeftTotal = read, notionMigrationReportGroupsCount(groups)
			output.FormatsLeftGroups = notionMigrationFormatsLeftGroupsSaid(groups)
			output.Next += " formats_left_groups says, by kind and reason, what Notion's API doesn't give that the run couldn't match to the person's Notion, formats_left_total in all (reason not_read: " +
				"Leafen has no reading of it — leafen_notion_migration_formats reads it, matching at once now the run is done and writing the report again; not_matched: read, and Leafen couldn't match it; " +
				"not_saved: read, but Leafen couldn't save the view it is of (it changed meanwhile, or Leafen refused it), so nothing read of that view came — a form of it takes no answers either). " +
				"They keep Leafen's defaults; the report says each group in one line by its say and count, folding which ones under it, and list formats_left names them one by one."
		}
		output.Next += notionMigrationFilesOmittedNext(migration)
	}
	fillNotionMigrationStatusList(&output, listing, migration, left)
	// While the run goes, what hasn't come whole so far comes by its counts
	// alone, as the agent asks again and again (mcp_notion_migration_status_short.go).
	if !detail && notionMigrationStageGoes(output.Stage) {
		if output.NotWhole != nil {
			output.NotWhole.Items = nil
		}
		output.ImportIssuesMore = output.ImportIssuesMore || len(output.ImportIssues) > 0
		output.ImportIssues = nil
	}
	return output
}

// notionMigrationStatusStage is a migration's one stage: its scan, then its
// run's, as its import job says it.
// notionMigrationStatusLinks are where the person watches a migration and
// where its content is: Leafen's import screen while it is anywhere but done
// — a first run, one gone on from where it stopped and a run again alike
// (2026-11-23: a run again's link was the top page an earlier run made, which
// shows no progress) — and once done the moved content's top page; the top
// page apart while a run goes that has made it already. Each in the
// migration's workspace, which the link names (person_links.go).
func notionMigrationStatusLinks(app core.App, authority mcpAuthority, migration notionMigration, stage string) (link, pages string) {
	if migration.ImportTopPageID != "" {
		pages = personLinkAt(app, authority, workspacePath(app, migration.WorkspaceID, "/p/"+migration.ImportTopPageID))
	}
	if stage == notionMigrationStageDone && pages != "" {
		return pages, ""
	}
	return notionMigrationLiveLink(app, authority, migration.WorkspaceID), pages
}

func notionMigrationStatusStage(migration notionMigration, job *notionImportJob) string {
	switch {
	case migration.Status == notionMigrationStatusScanning:
		return notionMigrationStageScanning
	case migration.Status == notionMigrationStatusFailed || migration.RunState == notionMigrationRunFailed:
		return notionMigrationStageStopped
	case migration.RunState == notionMigrationRunDone:
		return notionMigrationStageDone
	case migration.RunState == "":
		return "planning"
	case job != nil && job.Stage != "":
		return job.Stage
	}
	return notionMigrationStageReading
}
