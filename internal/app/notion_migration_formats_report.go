package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The migration report names what Notion's API doesn't give that the run
// couldn't match to the person's Notion (notion_migration_formats_left.go),
// with why: Leafen had no reading of it, read it and couldn't match it, or
// read it and couldn't save the view it is of — a line for each kind and
// why with how many, the ones it is folded under it
// (notion_migration_report_groups.go), and the agent's status says the same
// lines (formats_left_groups). What it matched isn't listed. A simple table
// block's colors and column widths, which no reading brings, stay a line of
// their own.

var notionMigrationFormatsReportText = map[string]map[string]string{
	accountLanguageKo: {
		"head":          "Notion이 연결에 주지 않는 꾸밈 중 맞추지 못한 것 %d개:",
		"tables":        "간단한 표 블록의 색·열 너비: Notion이 연결에 주지 않아 기본값으로 두었습니다.",
		"page":          "페이지",
		"rows":          "데이터베이스 행 %d개",
		"template":      "데이터베이스 템플릿",
		"database":      "데이터베이스 페이지 레이아웃",
		"board":         "보드 열의 순서·숨김",
		"groups":        "보기 그룹의 순서·숨김",
		"wrap":          "표 보기의 줄바꿈",
		"form":          "폼 질문·응답 받기",
		"view_template": "이 보기에만 정한 기본 템플릿",
		"row_order":     "보기의 행 순서",
		"not_read":      "Notion에서 읽지 못함",
		"not_matched":   "읽었지만 맞추지 못함",
		"not_saved":     "읽었지만 그 보기를 저장하지 못함 — 그 사이 보기가 바뀌었거나 리펜이 받지 않음",
		"untitled":      "새 페이지",
	},
	accountLanguageEn: {
		"head":          "%d things Notion doesn’t give a connection that the run couldn’t match:",
		"tables":        "A simple table block’s colors and column widths: Notion doesn’t give them to a connection, so they are at their defaults.",
		"page":          "Page",
		"rows":          "%d database rows",
		"template":      "Database template",
		"database":      "Database page layout",
		"board":         "Board columns’ order and hiding",
		"groups":        "View groups’ order and hiding",
		"wrap":          "Table view’s wrap",
		"form":          "Form questions and who may answer",
		"view_template": "Default template set for this view only",
		"row_order":     "View’s row order",
		"not_read":      "not read from Notion",
		"not_matched":   "read, but couldn’t be matched",
		"not_saved":     "read, but its view couldn’t be saved — it changed meanwhile, or Leafen refused it",
		"untitled":      "New page",
	},
}

// notionMigrationFormatsLeftGroupText is how the report says a kind of them
// left for one reason, many at once: what of what was left, why, and how
// many (the reason first, then the count).
var notionMigrationFormatsLeftGroupText = map[string]map[string]string{
	accountLanguageKo: {
		"page":                          "너비·글꼴을 %s 페이지 %d개",
		"rows":                          "너비·글꼴을 %s 행 페이지 %d개",
		"template":                      "너비·글꼴을 %s 데이터베이스 템플릿 %d개",
		"database":                      "행 페이지 레이아웃을 %s 데이터베이스 %d개",
		"board":                         "열 순서·숨김을 %s 보드 보기 %d개",
		"groups":                        "그룹 순서·숨김을 %s 보기 %d개",
		"wrap":                          "줄바꿈을 %s 표 보기 %d개",
		"form":                          "질문·응답 받기를 %s 폼 %d개",
		notionMigrationViewTemplateLeft: "그 보기에만 정한 기본 템플릿을 %s 보기 %d개",
		"row_order":                     "손으로 정한 행 순서를 %s 보기 %d개",
		"not_read":                      "Notion에서 읽지 못한",
		"not_matched":                   "읽었지만 맞추지 못한",
		notionMigrationViewUnsaved:      "읽었지만 그 보기를 저장하지 못한(그 사이 보기가 바뀌었거나 리펜이 받지 않음)",
		"rowsItem":                      "%s (행 %d개)",
	},
	accountLanguageEn: {
		"page":                          "%[2]d pages with width and font %[1]s",
		"rows":                          "%[2]d row pages with width and font %[1]s",
		"template":                      "%[2]d database templates with width and font %[1]s",
		"database":                      "%[2]d databases with row page layout %[1]s",
		"board":                         "%[2]d board views with columns’ order and hiding %[1]s",
		"groups":                        "%[2]d views with groups’ order and hiding %[1]s",
		"wrap":                          "%[2]d table views with wrap %[1]s",
		"form":                          "%[2]d forms with questions and who may answer %[1]s",
		notionMigrationViewTemplateLeft: "%[2]d views with a default template of their own %[1]s",
		"row_order":                     "%[2]d views with rows put in order by hand %[1]s",
		"not_read":                      "not read from Notion",
		"not_matched":                   "read but not matched",
		notionMigrationViewUnsaved:      "read in a view Leafen couldn’t save (it changed meanwhile, or Leafen refused it)",
		"rowsItem":                      "%s (%d rows)",
	},
}

// notionMigrationFormatsLeftKindOrder and notionMigrationFormatsLeftReasonOrder
// order the groups: by kind as the import screen counts them
// (notionMigrationFormatsLeftKinds), and under a kind what was read and still
// didn't come before what wasn't read.
var (
	notionMigrationFormatsLeftKindOrder = []string{"page", "rows", "template", "database", graphViewTypeBoard, "groups", "wrap",
		graphViewTypeForm, notionMigrationViewTemplateLeft, "row_order"}
	notionMigrationFormatsLeftReasonOrder = []string{notionMigrationViewUnsaved, "not_matched", "not_read"}
)

// notionMigrationFormatsLeftGroups are them by kind and reason, as the report
// and the agent's status say them.
func notionMigrationFormatsLeftGroups(language string, left []leafenMCPNotionFormatLeft) []notionMigrationReportGroup {
	text := notionMigrationFormatsReportText[language]
	grouped := notionMigrationFormatsLeftGroupText[language]
	if text == nil || grouped == nil {
		text, grouped = notionMigrationFormatsReportText[accountLanguageEn], notionMigrationFormatsLeftGroupText[accountLanguageEn]
	}
	// An untitled page or template is Notion's "New page", a database its
	// "New database", a view its layout's name.
	untitled := cmpOr(notionMigrationReportText[language]["untitled"], notionMigrationReportText[accountLanguageEn]["untitled"])
	grouping, lines := newNotionMigrationReportGrouping(), map[string]string{}
	for _, item := range left {
		fallback := untitled
		if item.Kind == "page" || item.Kind == "template" {
			fallback = text["untitled"]
		}
		// Named where it is (notion_migration_formats_left_place.go).
		title := notionMigrationFormatsLeftNamed(language, item, fallback)
		kind, named, weight := text[item.Kind], title, 1
		if item.Kind == "rows" {
			kind = strings.Replace(kind, "%d", strconv.Itoa(item.Count), 1)
			named, weight = fmt.Sprintf(grouped["rowsItem"], title, item.Count), item.Count
		}
		key := item.Kind + "\x00" + item.Reason
		grouping.add(key, named, weight)
		lines[key] = fmt.Sprintf("%s: %s (%s)", kind, title, text[item.Reason])
	}
	keys := []string{}
	for _, kind := range notionMigrationFormatsLeftKindOrder {
		for _, reason := range notionMigrationFormatsLeftReasonOrder {
			keys = append(keys, kind+"\x00"+reason)
		}
	}
	return grouping.list(keys, func(group *notionMigrationReportGroup) {
		kind, reason, _ := strings.Cut(group.Key, "\x00")
		group.Say = fmt.Sprintf(grouped[kind], cmpOr(grouped[reason], reason), group.Count)
		if group.Total == 1 {
			group.One = lines[group.Key]
		}
	})
}

// renderNotionMigrationFormatsLeft writes the report's lines on them: how
// many, and under it a line for each kind left for one reason.
func renderNotionMigrationFormatsLeft(line func(string, ...any), language string, details notionMigrationReportDetails) {
	text := notionMigrationFormatsReportText[language]
	if text == nil {
		text = notionMigrationFormatsReportText[accountLanguageEn]
	}
	line("- %s", text["tables"])
	if len(details.FormatsLeft) == 0 {
		return
	}
	// One tally for the line and its groups: a database's rows count as rows.
	groups := notionMigrationFormatsLeftGroups(language, details.FormatsLeft)
	line("- "+text["head"], notionMigrationReportGroupsCount(groups))
	writeNotionMigrationReportGroups(line, language, 1, groups)
}

// rewriteNotionMigrationReportAfterReading writes a done run's report again
// once a reading that came after it was matched, so what it lists as left
// at Leafen's defaults — its own lines and its check's "not compared" — and
// the resolved discussions the reading counted are the reading's; the
// person's changes to the page stay (notion_migration_report_merge.go).
func rewriteNotionMigrationReportAfterReading(app core.App, migrationID string, job storedJob, payload notionImportJobPayload) error {
	left, err := notionMigrationFormatsLeftKinds(app, job.ID)
	if err != nil {
		return err
	}
	if err := updateNotionMigrationRunReport(app, migrationID, func(report *notionMigrationRunReport) {
		if report.Verify != nil {
			report.Verify.Unchecked = append([]notionMigrationFormatsLeftKind{}, left...)
		}
	}); err != nil {
		return err
	}
	runner, ok := app.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner)
	if !ok {
		return nil
	}
	return runner.writeNotionMigrationReport(context.Background(), app, job, payload)
}
