package app

import (
	"sort"
)

// What didn't move is the report's first section, after a stop's callout
// and its lead, and its first lines are what didn't come at all (2026-11-20,
// the person: what matters — values that didn't move, failures — at the
// top): what the tool said it couldn't carry, the files not copied, the
// comments Notion didn't give, a people mapping that didn't finish, what
// changed meanwhile and the problems the scan found. Then what didn't come
// whole, each why with the pages it was on folded under it; who made what;
// what no move brings; what stays at Leafen's defaults; the forms; and how
// the comments and people went. The check against Notion follows it, then
// what moved.

// notionMigrationReportGeneralCodes are the agenda's words on what no move
// brings, the looks not read in the person's Notion and the people no member
// stands for, which the report says after what the scan found.
var notionMigrationReportGeneralCodes = map[string]bool{
	notionMigrationAgendaFormatsNotRead: true, notionMigrationAgendaPersonNotMember: true,
	notionMigrationAgendaSharingNotMoved: true, notionMigrationAgendaHistoryNotMoved: true,
	notionMigrationAgendaAutomationsNotMoved: true,
}

// renderNotionMigrationReportNotMoved writes the section.
func renderNotionMigrationReportNotMoved(
	line func(string, ...any), language string, text map[string]string, plan notionMigrationPlan,
	summary notionMigrationSummary, report notionMigrationRunReport, job notionImportJob, origin notionMigrationOrigin,
	importerAuthored int, details notionMigrationReportDetails,
) {
	line("## %s", text["notMoved"])
	for _, loss := range origin.Losses {
		line("- "+text["toolLoss"], loss.What, loss.Count, cmpOr(loss.Why, "—"))
	}
	writeNotionMigrationReportGroups(line, language, 0, notionMigrationReportFileGroups(details, text))
	missedComments, saidComments := notionMigrationReportCommentLines(language, text, report, origin.Tool, details.Resolved)
	for _, comment := range missedComments {
		line("- %s", comment)
	}
	if report.PeopleError != "" {
		line("- "+text["people"], report.PeopleError)
	}
	if report.ChangedMeanwhile > 0 {
		line("- "+text["changedMeanwhile"], report.ChangedMeanwhile)
	}
	// What the list by page names, or a property the plan kept as text, is
	// said there, once (2026-11-07); unreadable comments the run says itself.
	// The scan's problems lead; what no move brings, the looks not read and
	// the people no member stands for come with the looks and the people.
	codes, answered := map[string]int{}, notionMigrationPlanAnswered(plan)
	for _, issue := range summary.Issues {
		// A formula or rollup kept beside its target's moved: the structure's changes say it.
		if !details.Listed[issue.Code] && !answered(issue) && issue.Code != notionMigrationIssueCommentsUnreadable &&
			issue.Code != notionMigrationAgendaComputedBeside {
			codes[issue.Code]++
		}
	}
	keys := make([]string, 0, len(codes))
	for code := range codes {
		keys = append(keys, code)
	}
	sort.Strings(keys)
	agenda := func(general bool) {
		for _, code := range keys {
			if notionMigrationReportGeneralCodes[code] != general {
				continue
			}
			// The people no member stands for are said once, by the people
			// line that names each with what became of them (2026-11-20).
			if code == notionMigrationAgendaPersonNotMember && len(summary.People) > 0 {
				continue
			}
			if label, ok := text["issue:"+code]; ok {
				line("- %s", notionMigrationIssueLine(label, codes[code]))
			} else {
				line("- %s: %d", code, codes[code])
			}
		}
	}
	agenda(false)
	renderNotionImportNotWhole(line, language, text, job.Report, details.Issues)
	notionMigrationPeopleReport(language, text["importerAuthored"], importerAuthored, plan, summary, line)
	agenda(true)
	// Notion's API has no page layouts (its help "Layouts"; read 2026-10-12),
	// page formats, a simple table block's colors and column widths, or a
	// board's columns as set by hand; the run matches them from the person's
	// Notion and lists, kind by kind, what it couldn't
	// (notion_migration_formats_report.go). A database view's column widths,
	// set on Notion's screen too, its views API gives (seen 2026-11-12), and
	// they come with the view (viewProperties). Nor a form's questions
	// (notion_migration_report_forms.go).
	if origin.Tool == "" {
		renderNotionMigrationFormatsLeft(line, language, details)
		renderNotionMigrationReportForms(line, language, details.Forms)
	}
	if origin.Tool == "" && origin.RunAgain {
		line("- %s", text["viewsStay"])
	}
	for _, comment := range saidComments {
		line("- %s", comment)
	}
	if report.PeopleError == "" && report.PeopleMapped > 0 {
		line("- "+text["peopleDone"], report.PeopleMapped)
	}
	if report.PeopleSkippedPages > 0 {
		line("- "+text["peopleSkipped"], report.PeopleSkippedPages)
	}
}
