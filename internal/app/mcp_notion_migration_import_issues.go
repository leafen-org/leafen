package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// leafenMCPNotionImportIssues are a run's items of what hasn't come whole for
// the agent's status (import_issues): each in its not_whole line, with the
// first pages it was on as its finish screen names them
// (notion_import_issue_places.go) — a template as the template — each linked
// as the person opens it and said by what was there. withPages is false while the run goes and the call
// asks no detail — the status then gives counts alone — and more says there
// are more items than the status names (2026-11-23: a move's import_issues
// said a line for each row page, each "Untitled", 672 in all).
func leafenMCPNotionImportIssues(
	app core.App, authority mcpAuthority, migration notionMigration, job notionImportJob, withPages bool,
) ([]leafenMCPNotionImportIssue, bool) {
	tally := append([]notionImportNotWhole(nil), job.Report.NotWhole...)
	more := len(tally) > leafenMCPNotionMigrationStatusIssues
	if len(tally) == 0 || !withPages {
		return nil, len(tally) > 0
	}
	// An ended run's items came with their pages; a run still going reads them.
	if job.Status == durableJobStatusQueued || job.Status == notionImportStatusRunning {
		if err := notionImportNotWholeWithPages(app, job.ID, migration.ID, tally); err != nil {
			return nil, true
		}
	}
	language, _ := serverTextLanguage(app, authority.owner(), nil)
	text := notionImportNotWholeText(language, migration.Source)
	issues := make([]leafenMCPNotionImportIssue, 0, min(len(tally), leafenMCPNotionMigrationStatusIssues))
	for _, item := range tally[:min(len(tally), leafenMCPNotionMigrationStatusIssues)] {
		issue := leafenMCPNotionImportIssue{
			Kind: item.Kind, Reason: item.Reason, Source: item.Source, Count: item.Count,
			Say: notionImportNotWholeSay(language, text, item), PagesTotal: item.PagesTotal,
		}
		for _, page := range item.Pages {
			link := ""
			if page.PageID != "" {
				link = personLinkAt(app, authority, workspacePath(app, migration.WorkspaceID, "/p/"+page.PageID))
			}
			issue.Pages = append(issue.Pages, leafenMCPNotionImportIssuePage{
				PageID: page.PageID, PageTitle: notionImportIssuePlaceTitle(text, page.PageTitle, page.Template), Link: link,
				What: notionImportIssueWhat(language, text, item, page.Subject), Count: page.Count,
			})
		}
		issues = append(issues, issue)
	}
	return issues, more
}
