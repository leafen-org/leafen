package app

import (
	"fmt"
	"strings"
)

// What the report says of a computed property it reported: its values are
// computed, so none were kept to count.
var notionMigrationReportComputedText = map[string]map[string]string{
	accountLanguageKo: {
		"keptDeleted":  "계산 속성이라 값은 따로 남기지 않았습니다. 정의는 데이터베이스의 \"삭제된 속성\"에 남아 있어, 복원하면 다시 계산됩니다.",
		"keptProperty": "보기가 이 속성으로 묶거나 날짜를 정해 그대로 두었습니다(계산 속성이라 값은 계속 계산됩니다).",
	},
	accountLanguageEn: {
		"keptDeleted":  "A computed property: its values aren’t kept. Its definition stays in the database’s Deleted properties and computes again on restore.",
		"keptProperty": "A view groups or dates by it, so it stays where it was (a computed property, its values go on being computed).",
	},
}

// renderNotionMigrationReportReported writes the report's properties the
// plan reported: for each, how many values there are and where they all
// stay, and the first of them.
func renderNotionMigrationReportReported(
	line func(string, ...any), language string, text map[string]string, report notionMigrationRunReport,
	databases map[string]notionMigrationDatabase, name func(string) string,
) {
	computedText := notionMigrationReportComputedText[language]
	if computedText == nil {
		computedText = notionMigrationReportComputedText[accountLanguageEn]
	}
	if len(report.Reported) > 0 {
		line("")
		line("## %s", text["reported"])
		for _, reported := range report.Reported {
			line("")
			line("### %s · %s", name(databases[notionMigrationSourceKey(reported.DatabaseID)].Title), reported.Name)
			// How many values there are and where they all stay; the first
			// of them follow.
			total := max(reported.Total, len(reported.Values))
			// A rollup's values, as a formula's, are computed: none are
			// kept to count (2026-11-16: a reported rollup read "All 0
			// values stay in Deleted properties").
			computed := reported.Type == graphValueTypeRollup || reported.Type == graphValueTypeFormula
			switch {
			case reported.Kept == "property" && computed:
				line("%s", computedText["keptProperty"])
			case reported.Kept == "property":
				line("%s", fmt.Sprintf(text["keptProperty"], total))
			case reported.Kept == "formula":
				line("%s", text["keptFormula"])
			case computed:
				line("%s", computedText["keptDeleted"])
			default:
				line("%s", fmt.Sprintf(text["keptDeleted"], total))
			}
			shown := 0
			for _, value := range reported.Values {
				if shown == notionMigrationReportValues {
					break
				}
				if value.RowID == "" && strings.Trim(strings.TrimSpace(value.Value), `"`) == "" {
					line("- %s", text["computedEmpty"])
				} else if value.RowID == "" {
					line("- "+text["computed"], value.Value)
				} else {
					line("- %s: %s", cmpOr(strings.TrimSpace(value.RowTitle), text["untitledPage"]), value.Value)
				}
				shown++
			}
			if rest := total - shown; rest > 0 && reported.Kept != "formula" && !computed {
				line("- "+text["more"], rest)
			}
		}
	}
}
