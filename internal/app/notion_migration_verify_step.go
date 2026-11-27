package app

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The check against Notion is a step of the run of its own, after
// everything else the run makes and before its report, which says what it
// found; a run is done only once it has passed (notion_migration_verify.go).

const notionMigrationStepVerify = "verify"

// verifyNotionMigrationRun checks a run's import and keeps what it found in
// the run's report.
func verifyNotionMigrationRun(app core.App, jobID, migrationID string) (notionMigrationVerify, error) {
	planJSON := ""
	if err := app.DB().NewQuery(`SELECT [[plan_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": migrationID}).Row(&planJSON); err != nil {
		return notionMigrationVerify{}, err
	}
	plan := notionMigrationPlan{}
	if planJSON != "" {
		if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
			return notionMigrationVerify{}, err
		}
	}
	verify, err := checkNotionMigration(app, jobID, migrationID, plan)
	if err != nil {
		return notionMigrationVerify{}, err
	}
	return verify, updateNotionMigrationRunReport(app, migrationID, func(report *notionMigrationRunReport) {
		report.Verify = &verify
	})
}

// notionMigrationReportCheck tells one check of a run's report from another,
// so a report written again after a check goes through even when its words
// are those of a version before (a view changed and changed back).
func notionMigrationReportCheck(report notionMigrationRunReport) string {
	if report.Verify == nil {
		return ""
	}
	return strconv.FormatInt(report.Verify.CheckedAt, 10) + "\x00"
}

// loadNotionMigrationVerify is what a migration's last check found, in its
// owner's language, for its report page.
func loadNotionMigrationVerify(app core.App, migrationID string) (*notionMigrationVerify, string, error) {
	stored := struct {
		Owner  string `db:"owner"`
		Report string `db:"run_report_json"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[owner]], [[run_report_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": migrationID}).One(&stored); err != nil {
		return nil, "", err
	}
	report := emptyNotionMigrationRunReport()
	if json.Unmarshal([]byte(stored.Report), &report) != nil || report.Verify == nil {
		return nil, "", nil
	}
	language, err := serverTextLanguage(app, stored.Owner, nil)
	return report.Verify, language, err
}

// notionMigrationVerifyText is the report's words for the check, in the
// person's language.
var notionMigrationVerifyText = map[string]map[string]string{
	accountLanguageKo: {
		"title": "원본과 대조", "checked": "Notion에서 읽은 것과 견줘 본 것: 행 %d개, 템플릿 %d개, 데이터베이스 보기 %d개, 페이지·행 페이지 안의 연결된 보기 %d개.",
		"same": "다른 점이 없습니다.", "differ": "다른 점 %d개:", "more": "그 밖에 %d개가 더 있습니다.",
		"line":  "%s · %s · %s: Notion %s · 리펜 %s — %s",
		"group": "%s · %s — %s: %d개", "item": "%s: Notion %s · 리펜 %s",
		"unchecked": "Notion이 연결에 주지 않아 견주지 못한 것(리펜 기본값으로 둠): %s.",
		"kind:rows": "행", "kind:templates": "템플릿", "kind:view": "보기", "kind:linked_view": "연결된 보기", "kind:relation": "관계",
		"kind:cell": "칸", "kind:column": "나눈 대상의 열", "setting:empty_in_notion": "Notion에서는 빈 칸",
		"setting:one_way": "한쪽만 이어짐", "setting:computed": "계산된 값(수식·롤업)", "setting:count": "개수", "setting:missing": "만들어지지 않음", "setting:columns": "보이는 열", "setting:widths": "열 너비",
		"setting:sorts": "정렬", "setting:filter": "필터 조건", "setting:group": "그룹",
		"reason:changed_meanwhile":    "리펜에서 휴지통으로 옮겨지거나 지워졌습니다",
		"reason:not_made":             "만들어지지 않았습니다",
		"reason:property_not_brought": "옮기지 않은 속성을 가리킵니다(계획에서 보고서로 보냈거나 리펜에 없는 유형)",
		"reason:unexplained":          "까닭을 아직 모릅니다",
		"reason:plan_changed":         "계획이 바꾼 것입니다 — 계획이 바꾼 속성(유형, 옵션 이름, 글로 남김, 보고서로 보냄, 다른 관계로 모음, 다른 관계를 모아 받는 관계, 합친 대상과 같이 쓰는 열)을 읽거나, 계획이 다른 속성의 행을 함께 담게 한 양방향 관계의 반대편입니다",
		"reason:leafen_only":          "리펜에만 값이 있습니다(리펜에서 넣은 값이거나, Notion에서 비운 칸을 따라 비우지 않던 이전 버전의 실행이 남긴 값)",
		"left:page":                   "페이지 너비·글꼴 %d", "left:rows": "행 페이지 너비·글꼴 %d", "left:database": "행 페이지 레이아웃 %d",
		"left:board": "보드 열 %d", "left:form": "폼 질문·응답 받기 %d", "left:row_order": "손으로 정한 행 순서 %d",
		"left:template": "템플릿 너비·글꼴 %d", "left:groups": "보기 그룹 %d", "left:wrap": "표 줄바꿈 %d",
		"left:view_template": "보기에만 정한 기본 템플릿 %d",
		"untitled":           "제목 없음",
	},
	accountLanguageEn: {
		"title": "Checked against Notion", "checked": "Compared with what was read from Notion: %d rows, %d templates, %d database views, %d linked views in pages and row pages.",
		"same": "Nothing differs.", "differ": "%d differences:", "more": "And %d more.",
		"line":  "%s · %s · %s: Notion %s · Leafen %s — %s",
		"group": "%s · %s — %s: %d", "item": "%s: Notion %s · Leafen %s",
		"unchecked": "Not compared, as Notion doesn’t give it to a connection (left at Leafen’s defaults): %s.",
		"kind:rows": "rows", "kind:templates": "templates", "kind:view": "view", "kind:linked_view": "linked view", "kind:relation": "relation",
		"kind:cell": "cell", "kind:column": "column in a split’s target", "setting:empty_in_notion": "empty in Notion",
		"setting:one_way": "one way only", "setting:computed": "computed value (formula or rollup)", "setting:count": "count", "setting:missing": "not made", "setting:columns": "shown columns", "setting:widths": "column widths",
		"setting:sorts": "sorts", "setting:filter": "filter conditions", "setting:group": "group",
		"reason:changed_meanwhile":    "moved to the trash or deleted in Leafen",
		"reason:not_made":             "not made",
		"reason:property_not_brought": "names a property that didn’t come (sent to the report by the plan, or of a type Leafen has none for)",
		"reason:unexplained":          "no known reason yet",
		"reason:plan_changed":         "the plan’s doing — it reads a property the plan changes (its type, option names, kept as text, sent to the report, a relation gathered into another or one others are gathered into, or a column shared with the merge target), or it is the other side of a two-way relation the plan has also hold another property’s rows",
		"reason:leafen_only":          "only Leafen holds a value (put there in Leafen, or left by a run of an earlier version that didn’t empty what Notion emptied)",
		"left:page":                   "pages’ width and font %d", "left:rows": "row pages’ width and font %d", "left:database": "row page layouts %d",
		"left:board": "board columns %d", "left:form": "form questions and who may answer %d", "left:row_order": "rows’ hand order %d",
		"left:template": "templates’ width and font %d", "left:groups": "view groups %d", "left:wrap": "tables’ wrap %d",
		"left:view_template": "views’ own default templates %d",
		"untitled":           "Untitled",
	},
}

// notionMigrationVerifyGroups are the check's differences by what differs and
// why, each with the database, view or linked view it is of and the two
// values: as many as each group has in all, as the check counted them, and
// the ones the check kept listed under it — a group none of whose
// differences it kept is its line alone. A check from before it counted its
// groups counts what it kept.
func notionMigrationVerifyGroups(verify *notionMigrationVerify, language string, reasons map[string]string) []notionMigrationReportGroup {
	if verify == nil {
		return nil
	}
	text := notionMigrationVerifyText[language]
	if text == nil {
		text = notionMigrationVerifyText[accountLanguageEn]
	}
	why := func(reason string) string {
		said := []string{}
		for _, code := range strings.Split(reason, ",") {
			said = append(said, cmpOr(text["reason:"+code], notionMigrationReasonLabel(reasons, code)))
		}
		return strings.Join(said, "; ")
	}
	grouping, lines := newNotionMigrationReportGrouping(), map[string]string{}
	for _, difference := range verify.Remaining {
		title := cmpOr(strings.TrimSpace(difference.Title), text["untitled"])
		key := notionMigrationVerifyGroupKey(difference.Kind, difference.Setting, difference.Reason)
		grouping.add(key, fmt.Sprintf(text["item"], title, cmpOr(difference.Notion, "-"), cmpOr(difference.Leafen, "-")), 1)
		lines[key] = fmt.Sprintf(text["line"], title, text["kind:"+difference.Kind], text["setting:"+difference.Setting],
			cmpOr(difference.Notion, "-"), cmpOr(difference.Leafen, "-"), why(difference.Reason))
	}
	keys := []string{}
	for _, counted := range verify.RemainingGroups {
		key := notionMigrationVerifyGroupKey(counted.Kind, counted.Setting, counted.Reason)
		grouping.tally(key, counted.Count, counted.Count)
		keys = append(keys, key)
	}
	return grouping.list(keys, func(group *notionMigrationReportGroup) {
		parts := strings.SplitN(group.Key, "\x00", 3)
		group.Say = fmt.Sprintf(text["group"], text["kind:"+parts[0]], text["setting:"+parts[1]], why(parts[2]), group.Count)
		switch {
		case len(group.Items) == 0:
			group.One = group.Say
		case group.Total == 1:
			group.One = lines[group.Key]
		}
	})
}

// renderNotionMigrationVerify writes the check's section of the report.
func renderNotionMigrationVerify(verify *notionMigrationVerify, language string, reasons map[string]string, line func(string, ...any)) {
	if verify == nil {
		return
	}
	text := notionMigrationVerifyText[language]
	if text == nil {
		text = notionMigrationVerifyText[accountLanguageEn]
	}
	line("")
	line("## %s", text["title"])
	line("- "+text["checked"], verify.Rows, verify.Templates, verify.Views, verify.LinkedViews)
	if verify.RemainingTotal == 0 {
		line("- %s", text["same"])
	} else {
		line("- "+text["differ"], verify.RemainingTotal)
		writeNotionMigrationReportGroups(line, language, 1, notionMigrationVerifyGroups(verify, language, reasons))
		// Each group says how many more it has than it lists; a check from
		// before it counted its groups says it once.
		if more := verify.RemainingTotal - len(verify.Remaining); more > 0 && len(verify.RemainingGroups) == 0 {
			line("\t- "+text["more"], more)
		}
	}
	left := []string{}
	for _, kind := range verify.Unchecked {
		if format, known := text["left:"+kind.Kind]; known {
			left = append(left, strings.Replace(format, "%d", strconv.Itoa(kind.Count), 1))
		}
	}
	if len(left) > 0 {
		line("- "+text["unchecked"], strings.Join(left, ", "))
	}
}
