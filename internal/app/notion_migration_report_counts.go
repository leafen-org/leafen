package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The report counts as the person knows their Notion (2026-11-16: a real
// report said 49 pages and 1,014 rows of 37 and 913 — it counted the page
// Leafen makes to hold each full-page database, and rows in Notion's trash —
// and "398 views with their filters and sorts" where the person knew 68:
// the rest were linked views in row pages made from templates, and some
// settings hadn't come). What the run read from Notion is counted as Notion
// has it, rows in Notion's trash apart; the views apart for row pages; and
// the views' line says where to see what differs.

// notionMigrationRunCounts is what a run read from Notion, as Notion counts
// it, and its views and linked databases in row pages.
type notionMigrationRunCounts struct {
	Pages       int `db:"pages"`
	Databases   int `db:"databases"`
	Rows        int `db:"rows"`
	TrashedRows int `db:"trashed_rows"`
	// The databases' templates, apart from their rows: those read, and those
	// an earlier run made that Notion still gives and left as they were, so a
	// run again didn't read them.
	Templates    int `db:"templates"`
	Blocks       int `db:"blocks"`
	ViewsInRows  int `db:"views_in_rows"`
	LinkedInRows int `db:"linked_in_rows"`
}

// loadNotionMigrationRunTally is what a migration's last run counted it made
// new, changed and left as it was; nil when it didn't count.
func loadNotionMigrationRunTally(app core.App, migrationID string) (*notionMigrationRunTally, error) {
	raw := ""
	if err := app.DB().NewQuery(`
		SELECT COALESCE(json_extract([[run_report_json]], '$.tally'), '') FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": migrationID}).Row(&raw); err != nil || raw == "" {
		return nil, err
	}
	tally := notionMigrationRunTally{}
	if err := json.Unmarshal([]byte(raw), &tally); err != nil {
		return nil, err
	}
	return &tally, nil
}

// loadNotionMigrationRunCounts counts what a run read as its progress and a
// run again's tally do (notion_import_counted.go).
func loadNotionMigrationRunCounts(app core.App, jobID, migrationID string) (notionMigrationRunCounts, error) {
	counts := notionMigrationRunCounts{}
	err := app.DB().NewQuery(`
		SELECT
			COALESCE(SUM([[counted]] = 'page'), 0) + ` + notionImportKeptTemplateBodySQL("pages") + ` AS [[pages]],
			COALESCE(SUM([[counted]] = 'database'), 0) + ` + notionImportKeptTemplateBodySQL("databases") + ` AS [[databases]],
			COALESCE(SUM([[counted]] = 'row'), 0) + ` + notionImportKeptTemplateBodySQL("rows") + ` AS [[rows]],
			COALESCE(SUM([[counted]] = 'trashed_row'), 0) AS [[trashed_rows]],
			COALESCE(SUM([[counted]] = 'template'), 0) + ` + notionImportKeptTemplatesSQL + ` AS [[templates]],
			COALESCE(SUM([[counted]] = 'block'), 0) + ` + notionImportKeptTemplateBodySQL("blocks") + ` AS [[blocks]],
			(SELECT COUNT(*) FROM {{leafen_notion_import_mappings}} AS [[mapping]]
				JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]]=[[mapping]].[[native_id]]
				JOIN {{leafen_database_row_pages}} AS [[row]] ON [[row]].[[row_id]]=[[view]].[[page_id]]
				WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='view') AS [[views_in_rows]],
			(SELECT COUNT(*) FROM {{leafen_notion_import_sources}} AS [[linked]]
				JOIN {{leafen_notion_import_mappings}} AS [[mapping]] ON [[mapping]].[[job_id]]=[[linked]].[[job_id]]
					AND [[mapping]].[[source_id]]=[[linked]].[[source_id]] AND [[mapping]].[[kind]]='block'
				JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]]=[[mapping]].[[native_id]]
				JOIN {{leafen_database_row_pages}} AS [[row]] ON [[row]].[[row_id]]=[[block]].[[page_id]]
				WHERE [[linked]].[[job_id]]={:jobId} AND [[linked]].[[kind]]='block'
					AND instr([[linked]].[[text]], '"linkedDatabase":true')>0) AS [[linked_in_rows]]
		FROM (SELECT ` + notionImportCountedKindSQL + ` AS [[counted]] FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:jobId})
	`).Bind(notionImportCountedParams(dbx.Params{"jobId": jobID, "migrationId": migrationID})).One(&counts)
	return counts, err
}

var notionMigrationReportCountText = map[string]map[string]string{
	accountLanguageKo: {
		"readLine":      "Notion에서 읽어 옮긴 것: 페이지 %d · 데이터베이스 %d · 행 %d · 블록 %d",
		"trashedLine":   "Notion 휴지통에 있던 행 %d개는 리펜에서도 휴지통에 있습니다.",
		"templatesLine": "데이터베이스 템플릿 %d개는 행으로 세지 않고 각 데이터베이스의 템플릿으로 옮겼습니다.",
		"madeLine":      "이번 실행에서 새로 만들거나 바꾼 것: 페이지 %d · 데이터베이스 %d · 행 %d · 블록 %d",
		"newLine":       "이번 실행에서 새로 만든 것: 페이지 %d · 데이터베이스 %d · 행 %d · 블록 %d",
		"changedLine":   "Notion에서 바뀌어 이번 실행이 바꾼 것: 페이지 %d · 데이터베이스 %d · 행 %d · 블록 %d",
		"keptLine":      "Notion에서 바뀌지 않아 그대로 둔 것(수정 시각과 편집한 사람도 그대로): 페이지 %d · 데이터베이스 %d · 행 %d · 블록 %d",
		"countsNote":    "스캔과 수가 다른 것은 실행이 행 본문 안의 페이지·데이터베이스까지 읽어 스캔이 찾지 못한 것을 더 찾았거나, 그 사이 Notion이 바뀌었기 때문입니다.",
		"viewsLine":     "Notion 보기 %d개를 옮겼습니다: 데이터베이스와 페이지의 보기 %d개, 행 페이지 안의 보기 %d개. Notion과 다르게 온 설정은 위 대조 결과에 까닭과 함께 있습니다.",
		"linkedLine":    "연결된 데이터베이스 %d개를 원본에 이어 옮겼습니다(행 페이지 안의 %d개 포함).",
	},
	accountLanguageEn: {
		"readLine":            "Read from Notion and moved: pages %d · databases %d · rows %d · blocks %d",
		"trashedLine":         "%d rows in Notion’s trash are in Leafen’s trash too.",
		"templatesLine_one":   "1 database template came as its database’s template, not counted among the rows.",
		"templatesLine_other": "%d database templates came as their databases’ templates, not counted among the rows.",
		"madeLine":            "Made or changed by this run: pages %d · databases %d · rows %d · blocks %d",
		"newLine":             "Made new by this run: pages %d · databases %d · rows %d · blocks %d",
		"changedLine":         "Changed by this run, as Notion changed them: pages %d · databases %d · rows %d · blocks %d",
		"keptLine":            "Left as they were, unchanged in Notion (their times and editors too): pages %d · databases %d · rows %d · blocks %d",
		"countsNote":          "The scan counted otherwise because the run reads into rows’ bodies, where it found pages and databases the scan didn’t, or Notion changed since.",
		"viewsLine":           "%d Notion views came over: %d of databases and pages, %d inside row pages. Settings that came otherwise than in Notion are above with the check, each with why.",
		"linkedLine":          "%d linked databases came over, linked to their sources (%d of them inside row pages).",
	},
}

// notionMigrationReportCountLines are the report's lines of what moved: what
// it read from Notion as Notion counts it, what a run again made, the scan's
// count and why the two differ, the files, the views and linked databases.
func notionMigrationReportCountLines(
	language string, text map[string]string, summary notionMigrationSummary, job notionImportJob,
	details notionMigrationReportDetails, origin notionMigrationOrigin,
) []string {
	counted := notionMigrationReportCountText[language]
	if counted == nil {
		counted = notionMigrationReportCountText[accountLanguageEn]
	}
	if origin.Tool != "" {
		counted = notionMigrationReportForTool(counted, origin.Tool)
	}
	real := details.Counts
	lines := []string{fmt.Sprintf(counted["readLine"], real.Pages, real.Databases, real.Rows, real.Blocks)}
	if real.TrashedRows > 0 {
		lines = append(lines, fmt.Sprintf(counted["trashedLine"], real.TrashedRows))
	}
	if real.Templates > 0 {
		words, plain := counted["templatesLine"]
		if !plain {
			words = counted["templatesLine"+map[bool]string{true: "_one", false: "_other"}[real.Templates == 1]]
		}
		lines = append(lines, strings.Replace(words, "%d", strconv.Itoa(real.Templates), 1))
	}
	// A run again says what it made new, changed and left as it was
	// (notion_migration_run_again.go); one that ran before it counted so,
	// what it made.
	switch tally := details.Tally; {
	case origin.RunAgain && tally != nil:
		lines = append(lines,
			fmt.Sprintf(counted["newLine"], tally.Made.Pages, tally.Made.Databases, tally.Made.Rows, tally.Made.Blocks),
			fmt.Sprintf(counted["changedLine"], tally.Changed.Pages, tally.Changed.Databases, tally.Changed.Rows, tally.Changed.Blocks),
			fmt.Sprintf(counted["keptLine"], tally.Kept.Pages, tally.Kept.Databases, tally.Kept.Rows, tally.Kept.Blocks))
	case origin.RunAgain:
		lines = append(lines, fmt.Sprintf(counted["madeLine"], job.Report.Pages, job.Report.Databases, job.Report.Rows, job.Report.Blocks))
	}
	scanRows := 0
	for _, database := range summary.Databases {
		scanRows += database.Rows
	}
	lines = append(lines, fmt.Sprintf(text["scanLine"], summary.Counts.Pages, len(summary.Databases), scanRows))
	if real.Pages != summary.Counts.Pages || real.Databases != len(summary.Databases) || real.Rows != scanRows {
		lines = append(lines, counted["countsNote"])
	}
	if origin.Tool != "" {
		lines = append(lines, fmt.Sprintf(text["filesLineTool"], job.Report.FilesCopied))
	} else {
		// What stays in Notion is the files the list under what didn't move
		// counts (notion_migration_report_files.go).
		left := job.Report.FilesOmitted
		if details.FileCounts != nil {
			left = notionMigrationReportFilesLeft(details)
		}
		lines = append(lines, fmt.Sprintf(text["filesLine"], job.Report.FilesCopied, left))
	}
	if details.Views > 0 {
		inRows := min(real.ViewsInRows, details.Views)
		lines = append(lines, fmt.Sprintf(counted["viewsLine"], details.Views, details.Views-inRows, inRows))
	}
	if details.LinkedViews > 0 {
		lines = append(lines, fmt.Sprintf(counted["linkedLine"], details.LinkedViews, min(real.LinkedInRows, details.LinkedViews)))
	}
	return lines
}
