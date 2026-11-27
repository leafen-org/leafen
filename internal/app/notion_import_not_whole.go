package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What an import didn't bring whole is one tally: by kind — blocks, database
// properties, database views — and under each kind by why, how many. The
// import's report counts are its sums; Leafen's finish screen and its
// 완료됨 row (NotionImportBlockIssues.tsx), the agent's
// leafen_notion_migration_status (not_whole) and the migration's report page
// all say it from here, each item in the same words (2026-11-19: the app said
// "blocks 5 / properties 1 / views 21" while the agent's report named other
// things by other counts, and the person couldn't pair them). A migration's
// run counts what the migration still hasn't fully moved, its earlier runs'
// too (notion_migration_issues.go), and a property the person's plan kept as
// the text Notion showed says that, not the import's problem the plan
// answered (settleNotionMigrationPlanAnswers).

const (
	notionImportNotWholeBlock    = "block"
	notionImportNotWholeProperty = "property"
	notionImportNotWholeView     = "view"
	// A property the plan kept as the text Notion showed ("preserve"), in
	// place of the problem the import had with it.
	notionMigrationPropertyPreserved = "property_values_preserved"
)

var notionImportNotWholeKinds = []string{notionImportNotWholeBlock, notionImportNotWholeProperty, notionImportNotWholeView}

// notionImportNotWholeOf is an import's tally: a migration's standing problems
// once it has kept them, else the job's own.
func notionImportNotWholeOf(app core.App, jobID, migrationID string) ([]notionImportNotWhole, error) {
	params := dbx.Params{
		"id": jobID, "propertyParent": notionImportPropertyIssueParentKind, "viewParent": notionImportViewIssueParentKind,
		"file": notionMigrationFileIssueKind,
	}
	issues := `SELECT [[parent_kind]] AS [[issue_kind]], COALESCE([[text]], '') AS [[reason]], COALESCE([[title]], '') AS [[subject]]
		FROM {{leafen_notion_import_sources}} WHERE [[job_id]]={:id} AND [[kind]]='issue'`
	if migrationID != "" {
		settled, err := notionMigrationIssuesSettled(app, migrationID)
		if err != nil {
			return nil, err
		}
		if settled {
			params["id"] = migrationID
			issues = `SELECT [[kind]] AS [[issue_kind]], [[reason]], [[subject]]
				FROM {{leafen_notion_migration_issues}} WHERE [[migration_id]]={:id} AND [[kind]] <> {:file}`
		}
	}
	// A linked database showing one outside the import is counted by that
	// one, which its issue is named by (notion_import_linked_views.go).
	params["outside"] = notionImportIssueLinkedSourceOutside
	rows := []struct {
		Kind   string `db:"kind"`
		Reason string `db:"reason"`
		Source string `db:"source"`
		Count  int    `db:"issue_count"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT CASE [[issue_kind]] WHEN {:propertyParent} THEN 'property' WHEN {:viewParent} THEN 'view' ELSE 'block' END AS [[kind]],
			[[reason]], CASE [[reason]] WHEN {:outside} THEN [[subject]] ELSE '' END AS [[source]], COUNT(*) AS [[issue_count]]
		FROM (` + issues + `) GROUP BY 1, 2, 3
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	tally := make([]notionImportNotWhole, 0, len(rows))
	for _, row := range rows {
		tally = append(tally, notionImportNotWhole{Kind: row.Kind, Reason: row.Reason, Count: row.Count, Source: row.Source})
	}
	// Blocks, properties, views; under each the most first.
	order := map[string]int{notionImportNotWholeBlock: 0, notionImportNotWholeProperty: 1, notionImportNotWholeView: 2}
	sort.SliceStable(tally, func(a, b int) bool {
		if order[tally[a].Kind] != order[tally[b].Kind] {
			return order[tally[a].Kind] < order[tally[b].Kind]
		}
		if tally[a].Count != tally[b].Count {
			return tally[a].Count > tally[b].Count
		}
		if tally[a].Reason != tally[b].Reason {
			return tally[a].Reason < tally[b].Reason
		}
		return tally[a].Source < tally[b].Source
	})
	return tally, nil
}

// notionImportNotWholeCounts are a tally's sums, the report's three counts.
func notionImportNotWholeCounts(tally []notionImportNotWhole) (blocks, properties, views int) {
	for _, item := range tally {
		switch item.Kind {
		case notionImportNotWholeProperty:
			properties += item.Count
		case notionImportNotWholeView:
			views += item.Count
		default:
			blocks += item.Count
		}
	}
	return blocks, properties, views
}

// settleNotionMigrationPlanAnswers names, among a migration's standing
// problems, the properties its plan kept as the text Notion showed: such a
// property's problem — a formula naming a property that didn't come, say —
// is what made the person choose it, and its values came as Notion showed
// them. It counts once, however many problems the import had with it.
func settleNotionMigrationPlanAnswers(app core.App, migrationID string) error {
	stored := struct {
		PlanJSON string `db:"plan_json"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[plan_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": migrationID}).One(&stored); err != nil {
		return err
	}
	plan := notionMigrationPlan{}
	if err := json.Unmarshal([]byte(cmpOr(stored.PlanJSON, "{}")), &plan); err != nil {
		return err
	}
	columns := notionMigrationPlanColumns(plan)
	issues := []struct {
		IssueID string `db:"issue_id"`
		Reason  string `db:"reason"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[issue_id]], [[reason]] FROM {{leafen_notion_migration_issues}}
		WHERE [[migration_id]]={:id} AND [[kind]]={:propertyParent} ORDER BY [[issue_id]]
	`).Bind(dbx.Params{"id": migrationID, "propertyParent": notionImportPropertyIssueParentKind}).All(&issues); err != nil {
		return err
	}
	answered := map[string]bool{}
	for _, issue := range issues {
		// A property's problem is staged as "<database>:property:<property>#<reason>"
		// (notionImportPropertyIssueSource).
		databaseID, rest, found := strings.Cut(issue.IssueID, ":property:")
		propertyID, _, _ := strings.Cut(rest, "#")
		if !found || columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)].Action != "preserve" {
			continue
		}
		property := databaseID + ":property:" + propertyID
		query := ""
		switch {
		case answered[property]:
			query = `DELETE FROM {{leafen_notion_migration_issues}} WHERE [[migration_id]]={:id} AND [[issue_id]]={:issueId}`
		case issue.Reason != notionMigrationPropertyPreserved:
			query = `UPDATE {{leafen_notion_migration_issues}} SET [[reason]]={:reason} WHERE [[migration_id]]={:id} AND [[issue_id]]={:issueId}`
		}
		answered[property] = true
		if query == "" {
			continue
		}
		params := dbx.Params{"id": migrationID, "issueId": issue.IssueID, "reason": notionMigrationPropertyPreserved}
		if _, err := app.DB().NewQuery(query).Bind(params).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// notionMigrationPlanAnswered tells a scan's problem the plan answered by
// keeping its property as the text Notion showed: the report says that
// property under the values it kept and under what didn't come whole
// (property_values_preserved), not again under what didn't move.
func notionMigrationPlanAnswered(plan notionMigrationPlan) func(notionMigrationIssue) bool {
	columns := notionMigrationPlanColumns(plan)
	return func(issue notionMigrationIssue) bool {
		return issue.PropertyID != "" &&
			columns[notionMigrationSourceKey(issue.SourceID)][notionMigrationPropertyKey(issue.PropertyID)].Action == "preserve"
	}
}

// notionImportNotWholeUnits name what an item counts: by its kind, and for a
// problem a database's views have as a whole, or one of its data sources,
// that instead.
var notionImportNotWholeUnits = map[string]map[string][2]string{
	accountLanguageKo: {
		notionImportNotWholeBlock: {"블록 %d개", "블록 %d개"}, notionImportNotWholeProperty: {"속성 %d개", "속성 %d개"},
		notionImportNotWholeView: {"보기 %d개", "보기 %d개"}, "database": {"데이터베이스 %d개", "데이터베이스 %d개"},
		"data_source": {"데이터 소스 %d개", "데이터 소스 %d개"}, "linked_database": {"링크된 데이터베이스 %d개", "링크된 데이터베이스 %d개"},
	},
	accountLanguageEn: {
		notionImportNotWholeBlock: {"%d block", "%d blocks"}, notionImportNotWholeProperty: {"%d property", "%d properties"},
		notionImportNotWholeView: {"%d view", "%d views"}, "database": {"%d database", "%d databases"},
		"data_source": {"%d data source", "%d data sources"}, "linked_database": {"%d linked database", "%d linked databases"},
	},
}

// notionImportNotWholeUnitOf is what an item of a reason counts, as the
// app's finish screen says it too (blockIssues.notWhole).
func notionImportNotWholeUnitOf(item notionImportNotWhole) string {
	switch item.Reason {
	case notionImportIssueViewsUnavailable, notionImportIssueViewRowOrderUnavailable, notionImportIssueDatabaseUnavailable:
		return "database"
	case notionImportIssueDataSourceUnavailable:
		return "data_source"
	case notionImportIssueLinkedSourceOutside, notionImportIssueLinkedSourceUnavailable:
		if item.Kind == notionImportNotWholeBlock {
			return "linked_database"
		}
	}
	return item.Kind
}

// notionImportNotWholeSay is an item in words: how many of what, and why, in
// the report's words for why (notion_migration_report_details.go) — text is
// the report's text in language, another tool's named for it.
func notionImportNotWholeSay(language string, text map[string]string, item notionImportNotWhole) string {
	units := notionImportNotWholeUnits[language]
	if units == nil {
		units = notionImportNotWholeUnits[accountLanguageEn]
	}
	unit := units[notionImportNotWholeUnitOf(item)]
	form := unit[1]
	if item.Count == 1 {
		form = unit[0]
	}
	why := notionMigrationReasonLabel(text, item.Reason)
	if named, ok := text["reason:"+item.Reason+"_named"]; ok && item.Source != "" {
		why = fmt.Sprintf(named, item.Source)
	}
	return fmt.Sprintf(form, item.Count) + " — " + why
}

// notionImportNotWholeKey is what an item of the tally, or an issue it
// counts, is one of: its why, and for a linked database showing one outside
// the import, that one.
func notionImportNotWholeKey(reason, source string) string {
	if reason != notionImportIssueLinkedSourceOutside {
		return reason
	}
	return reason + "\x00" + source
}

// renderNotionImportNotWhole writes the report's line of what didn't come
// whole, its three counts, and under them each item as Leafen's finish
// screen says it, the pages it was on folded under it, each with what was
// there (2026-11-20: the list by page said each why again on every page,
// dozens of times) — issues are each item's first places with how many it
// has in all (notion_import_issue_places.go).
func renderNotionImportNotWhole(
	line func(string, ...any), language string, text map[string]string, report notionImportReport,
	issues []storedNotionImportBlockIssue,
) {
	line("- "+text["importIssues"], report.BlockIssues, report.PropertyIssues, report.ViewIssues)
	writeNotionMigrationReportGroups(line, language, 1, notionImportNotWholeGroups(language, text, report.NotWhole, issues))
}

// notionImportNotWholeGroups are the items, each with the pages it was on and
// what was there — one with no name said by what it is, a linked database in
// a row page as a linked database (2026-11-23: 672 row pages each said
// "Untitled (1)") — and how many pages in all; places of an item the tally
// doesn't hold are a group of their own.
func notionImportNotWholeGroups(
	language string, text map[string]string, tally []notionImportNotWhole, issues []storedNotionImportBlockIssue,
) []notionMigrationReportGroup {
	places := notionImportIssuePlacesOf(issues)
	said := func(item notionImportNotWhole, found *notionImportIssuePlaces, group *notionMigrationReportGroup) {
		for _, place := range found.Items {
			// A template the run hasn't made yet is said by its name; one made is
			// a mention of its page, which the report says as the template
			// (notion_migration_report_template_mentions.go).
			where := notionMigrationReportPlace(place.PageID, place.PageTitle, text["untitledPage"])
			if place.InTemplate && place.PageID == "" {
				where = notionImportIssuePlaceTitle(text, place.PageTitle, true)
			}
			group.Items = append(group.Items, fmt.Sprintf("%s · %s (%d)", where, notionImportIssueWhat(language, text, item, place.BlockType), place.Count))
		}
		group.Total = found.Total
	}
	groups, claimed := []notionMigrationReportGroup{}, map[string]bool{}
	for _, item := range tally {
		key := notionImportIssuePlaceKey(item.Kind, item.Reason, item.Source)
		group := notionMigrationReportGroup{Key: key, Say: notionImportNotWholeSay(language, text, item), Count: item.Count}
		if found := places[key]; found != nil {
			said(item, found, &group)
			claimed[key] = true
		}
		switch group.Total {
		case 0:
			group.One = group.Say
		case 1:
			group.One = group.Say + ": " + group.Items[0]
		}
		groups = append(groups, group)
	}
	for _, issue := range issues {
		key := notionImportIssuePlaceKey(issue.Kind, issue.Reason, issue.BlockType)
		if claimed[key] {
			continue
		}
		claimed[key] = true
		item := notionImportNotWhole{Kind: cmpOr(issue.Kind, notionImportNotWholeBlock), Reason: issue.Reason}
		group := notionMigrationReportGroup{Key: key, Say: notionMigrationReasonLabel(text, issue.Reason)}
		said(item, places[key], &group)
		for _, place := range places[key].Items {
			group.Count += place.Count
		}
		if group.Total == 1 {
			group.One = group.Items[0] + ": " + group.Say
		}
		groups = append(groups, group)
	}
	return groups
}

// notionImportNotWholeText is the report's text a migration's items are said
// in: the person's language, another tool's named for it.
func notionImportNotWholeText(language, tool string) map[string]string {
	text := notionMigrationReportText[language]
	if text == nil {
		text = notionMigrationReportText[accountLanguageEn]
	}
	if tool != "" {
		text = notionMigrationReportForTool(text, tool)
	}
	return text
}

// The report's words for a property the plan kept as text, beside its words
// for what the import met.
func init() {
	notionMigrationReportText[accountLanguageKo]["reason:"+notionMigrationPropertyPreserved] = "계획에서 선택한 대로 Notion에 보이던 값을 텍스트로 남겼습니다"
	notionMigrationReportText[accountLanguageEn]["reason:"+notionMigrationPropertyPreserved] = "kept as text as Notion showed it, as the plan chose"
}
