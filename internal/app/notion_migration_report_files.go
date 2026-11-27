package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The files a migration still couldn't copy are one count for its report: its
// list of them under what didn't move and its files line under what moved
// both read the migration's standing problems (notion_migration_file_issues.go),
// its earlier runs' too, which stay until a run copies them (2026-11-20: after
// a run again whose source no longer gave two pages, the list named four files
// and the files line, this run's own, said two). Files the plan left in Notion
// are said apart from those that couldn't be copied.

// loadNotionMigrationReportFiles reads them: the first of them to list, and
// how many by why — the migration's standing problems once it has kept them
// (settled), else the run's own outcomes.
func loadNotionMigrationReportFiles(app core.App, jobID, migrationID string, settled bool) ([]storedNotionFileOutcome, map[string]int, error) {
	counts, err := loadNotionMigrationFileCounts(app, jobID, migrationID, settled)
	if err != nil {
		return nil, nil, err
	}
	if settled {
		files, err := loadNotionMigrationFileIssues(app, migrationID, notionMigrationReportIssues)
		return files, counts, err
	}
	listed, cursor := []storedNotionFileOutcome{}, ""
	for {
		files, err := loadNotionImportFileOutcomeRows(app, jobID, cursor, notionMigrationReportIssues)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		for _, file := range files {
			if file.Status == "omitted" {
				listed = append(listed, file)
			}
		}
		if len(files) < notionMigrationReportIssues || len(listed) >= notionMigrationReportIssues {
			return listed, counts, nil
		}
		cursor = files[len(files)-1].SourceID
	}
}

// loadNotionMigrationFileCounts counts them by why: the migration's standing
// problems once it has kept them, else the run's own.
func loadNotionMigrationFileCounts(app core.App, jobID, migrationID string, settled bool) (map[string]int, error) {
	query := `
		SELECT COALESCE([[task]].[[error_code]], '') AS [[code]], COUNT(*) AS [[files]]
		FROM {{leafen_notion_import_file_tasks}} AS [[task]]
		JOIN {{leafen_notion_import_sources}} AS [[source]]
			ON [[source]].[[job_id]]=[[task]].[[job_id]] AND [[source]].[[source_id]]=[[task]].[[source_id]]
		WHERE [[task]].[[job_id]]={:jobId} AND [[task]].[[status]]='omitted'
		GROUP BY 1`
	if settled {
		query = `
			SELECT COALESCE([[reason]], '') AS [[code]], COUNT(*) AS [[files]] FROM {{leafen_notion_migration_issues}}
			WHERE [[migration_id]]={:migrationId} AND [[kind]]={:file}
			GROUP BY 1`
	}
	rows := []struct {
		Code  string `db:"code"`
		Files int    `db:"files"`
	}{}
	if err := app.DB().NewQuery(query).Bind(dbx.Params{
		"jobId": jobID, "migrationId": migrationID, "file": notionMigrationFileIssueKind,
	}).All(&rows); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[notionMigrationReportFileWhy(row.Code)] += row.Files
	}
	return counts, nil
}

// loadNotionMigrationFilesNotCopied is what a finished run's window and its
// agent say of the files that couldn't be copied (not those the plan left in
// Notion or of a type Leafen doesn't take), from the report's count: this
// run's own, which "Copy files again" tries, and the others the report lists —
// earlier runs' this run didn't try, on pages Notion no longer gives or rows it
// left as they were (2026-11-20: after a run again whose source no longer gave
// two pages, the window said two files and the report four).
func loadNotionMigrationFilesNotCopied(app core.App, jobID, migrationID string) (int, int, error) {
	run, err := loadNotionMigrationFileCounts(app, jobID, migrationID, false)
	if err != nil {
		return 0, 0, err
	}
	rows := []struct {
		Code  string `db:"code"`
		Files int    `db:"files"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT COALESCE([[reason]], '') AS [[code]], COUNT(*) AS [[files]] FROM {{leafen_notion_migration_issues}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:file} AND [[issue_id]] NOT IN (
			SELECT [[source_id]] FROM {{leafen_notion_import_file_tasks}} WHERE [[job_id]]={:jobId} AND [[status]] <> 'pending')
		GROUP BY 1
	`).Bind(dbx.Params{"jobId": jobID, "migrationId": migrationID, "file": notionMigrationFileIssueKind}).All(&rows); err != nil {
		return 0, 0, err
	}
	earlier := 0
	for _, row := range rows {
		if notionMigrationReportFileWhy(row.Code) == "fileNotCopied" {
			earlier += row.Files
		}
	}
	return run["fileNotCopied"], earlier, nil
}

// notionMigrationReportFileWhy is the report's why for a file's outcome:
// left in Notion by the plan, a type Leafen doesn't take, or not copied.
func notionMigrationReportFileWhy(code string) string {
	switch code {
	case errorCodeNotionMigrationFileSkipped:
		return "fileSkipped"
	case errorCodeFileUnsafeType:
		return "fileUnsafe"
	}
	return "fileNotCopied"
}

// notionMigrationReportFilesLeft is how many files the report says stay in
// Notion: all it counted.
func notionMigrationReportFilesLeft(details notionMigrationReportDetails) int {
	left := 0
	for _, count := range details.FileCounts {
		left += count
	}
	return left
}

// notionMigrationReportFileGroups are the files by why, each named with the
// page it was on: how many from the counts, the first of them listed — a why
// whose files the list cut off is said by its count alone.
func notionMigrationReportFileGroups(details notionMigrationReportDetails, text map[string]string) []notionMigrationReportGroup {
	grouping := newNotionMigrationReportGrouping()
	for _, file := range details.Files {
		grouping.add(notionMigrationReportFileWhy(file.ErrorCode), fmt.Sprintf("%s · %s", cmpOr(strings.TrimSpace(file.Name), text["untitledPage"]),
			notionMigrationReportPlace(file.PageID, file.PageTitle, text["untitledPage"])), 1)
	}
	groups := []notionMigrationReportGroup{}
	for _, why := range []string{"fileNotCopied", "fileUnsafe", "fileSkipped"} {
		group := notionMigrationReportGroup{Key: why}
		if listed := grouping.groups[why]; listed != nil {
			group = *listed
		}
		if counted := details.FileCounts[why]; counted > group.Total {
			group.Total, group.Count = counted, counted
		}
		if group.Total == 0 {
			continue
		}
		group.Say = fmt.Sprintf(text[why+"Group"], group.Count)
		if group.Total == 1 && len(group.Items) == 1 {
			group.One = group.Items[0] + ": " + text[why]
		} else if len(group.Items) == 0 {
			group.One = group.Say
		}
		groups = append(groups, group)
	}
	return groups
}
