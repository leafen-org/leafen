package app

import (
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Where an import's problems were is read by the items of what didn't come
// whole (notion_import_not_whole.go): each item — one kind, why and, for a
// linked database showing one outside the import, that one — with the pages
// its problems were on, the most first, the first of them and how many in
// all. Leafen's finish screen and 완료됨 row, the agent's
// leafen_notion_migration_status (import_issues) and the migration's report
// page all read them here, in the item's one line (2026-11-23: a move's
// import_issues said a line for each of 672 row pages, each "Untitled"
// linked_source_unavailable, and nobody could tell which row or what).

// notionImportNotWholePagesShown is how many of an item's pages the finish
// screen, its row and the agent's status name; the report names more and
// all are counted.
const notionImportNotWholePagesShown = 3

// notionImportIssuePlaces are an item's places — each a page its problems
// were on, what they were about there and how many (a list by page's
// storedNotionImportBlockIssue) — the first of them, and how many in all.
type notionImportIssuePlaces struct {
	Items []storedNotionImportBlockIssue
	Total int
}

// notionImportIssuePlaceKey is the item a problem is one of: its kind — a
// block's when none is said, as the tally reads one neither a property's nor
// a view's — and its why, and for a linked database showing one outside the
// import, that one, its subject: the tally's own grouping (notionImportNotWholeOf).
func notionImportIssuePlaceKey(kind, reason, source string) string {
	return cmpOr(kind, notionImportNotWholeBlock) + "\x00" + notionImportNotWholeKey(reason, source)
}

// notionImportIssuePlacesOf gathers places by their item, in the order read.
func notionImportIssuePlacesOf(issues []storedNotionImportBlockIssue) map[string]*notionImportIssuePlaces {
	places := map[string]*notionImportIssuePlaces{}
	for _, issue := range issues {
		key := notionImportIssuePlaceKey(issue.Kind, issue.Reason, issue.BlockType)
		found := places[key]
		if found == nil {
			found = &notionImportIssuePlaces{}
			places[key] = found
		}
		found.Items = append(found.Items, issue)
		found.Total = max(found.Total, issue.Places, len(found.Items))
	}
	return places
}

// loadNotionImportIssuePlaces reads each item's first shown places, from
// where its tally comes: a migration's standing problems once it keeps them
// (notion_migration_issues.go), else the job's own, each gone up to the page
// it was on however deep; each step is taken once (UNION), so a chain that
// comes back on itself ends. A page is named by its title in Leafen, where
// the person finds it: a database row's problems are kept without one, as
// its title is a value of its row (2026-11-23: a move's rows were said only
// by what was on them, "Untitled"). A template a row became is its own
// template page, named as the template: the row page it was made as goes
// once it is one (notion_import_templates.go). One the run hasn't made yet —
// the report is written before — is named by the name its staged row keeps,
// with no page to link: its row page is about to go.
func loadNotionImportIssuePlaces(app core.App, jobID, migrationID string, shown int) ([]storedNotionImportBlockIssue, error) {
	params := dbx.Params{
		"jobId": jobID, "migrationId": migrationID, "shown": shown, "outside": notionImportIssueLinkedSourceOutside,
		"propertyParent": notionImportPropertyIssueParentKind, "viewParent": notionImportViewIssueParentKind,
		"file": notionMigrationFileIssueKind, "template": notionMigrationLedgerTemplate,
		"templateName": `$."` + notionImportRowTemplateKey + `".value.name`,
	}
	settled := false
	if migrationID != "" {
		var err error
		if settled, err = notionMigrationIssuesSettled(app, migrationID); err != nil {
			return nil, err
		}
	}
	placed := `
		WITH RECURSIVE [[issue]] AS (
			SELECT [[sequence]], [[parent_kind]] AS [[issue_kind]], [[parent_source_id]] AS [[ancestor_id]],
				[[parent_kind]] AS [[ancestor_kind]], COALESCE([[title]], '') AS [[subject]], COALESCE([[text]], '') AS [[reason]]
			FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[kind]]='issue'
			UNION
			SELECT [[issue]].[[sequence]], [[issue]].[[issue_kind]], [[parent]].[[parent_source_id]], [[parent]].[[parent_kind]],
				[[issue]].[[subject]], [[issue]].[[reason]]
			FROM [[issue]]
			JOIN {{leafen_notion_import_sources}} AS [[parent]]
				ON [[parent]].[[job_id]]={:jobId} AND [[parent]].[[source_id]]=[[issue]].[[ancestor_id]]
			WHERE [[issue]].[[ancestor_kind]] IN ('block', {:propertyParent}, {:viewParent})
		), [[problem]] AS (
			SELECT [[issue_kind]], [[reason]], [[subject]], [[ancestor_id]] AS [[page_source_id]]
			FROM [[issue]] WHERE [[ancestor_kind]] IN ('page', 'row')
		), [[placed]] AS (
			SELECT [[problem]].*, COALESCE([[mapping]].[[native_id]], '') AS [[page_id]], COALESCE([[page]].[[title]], '') AS [[page_title]]
			FROM [[problem]]
			LEFT JOIN {{leafen_notion_import_mappings}} AS [[mapping]]
				ON [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[source_id]]=[[problem]].[[page_source_id]]
				AND [[mapping]].[[kind]] IN ('page', 'row')
			LEFT JOIN {{leafen_notion_import_sources}} AS [[page]]
				ON [[page]].[[job_id]]={:jobId} AND [[page]].[[source_id]]=[[problem]].[[page_source_id]]
		)`
	if settled {
		placed = `
		WITH [[placed]] AS (
			SELECT [[kind]] AS [[issue_kind]], [[reason]], [[subject]], [[page_source_id]], [[page_id]], [[page_title]]
			FROM {{leafen_notion_migration_issues}} WHERE [[migration_id]]={:migrationId} AND [[kind]] <> {:file}
		)`
	}
	rows := []storedNotionImportBlockIssue{}
	err := app.DB().NewQuery(placed + `, [[counted]] AS (
			SELECT CASE [[issue_kind]] WHEN {:propertyParent} THEN 'property' WHEN {:viewParent} THEN 'view' ELSE 'block' END AS [[item_kind]],
				[[reason]], CASE [[reason]] WHEN {:outside} THEN [[subject]] ELSE '' END AS [[source]],
				[[page_source_id]], MAX([[page_id]]) AS [[page_id]], MAX([[page_title]]) AS [[page_title]], [[subject]],
				COUNT(*) AS [[issue_count]]
			FROM [[placed]]
			GROUP BY 1, 2, 3, [[page_source_id]], [[subject]]
		), [[templated]] AS (
			SELECT [[counted]].*, [[template]].[[id]] AS [[template_id]], [[template]].[[name]] AS [[template_name]],
				CASE WHEN json_valid([[staged]].[[text]]) THEN json_extract([[staged]].[[text]], {:templateName}) END AS [[staged_template]]
			FROM [[counted]]
			LEFT JOIN {{leafen_notion_migration_ledger}} AS [[template_entry]]
				ON [[template_entry]].[[migration_id]]={:migrationId} AND [[template_entry]].[[source_id]]=[[counted]].[[page_source_id]]
				AND [[template_entry]].[[kind]]={:template}
			LEFT JOIN {{leafen_database_templates}} AS [[template]] ON [[template]].[[id]]=[[template_entry]].[[native_id]]
			LEFT JOIN {{leafen_notion_import_sources}} AS [[staged]]
				ON [[staged]].[[job_id]]={:jobId} AND [[staged]].[[source_id]]=[[counted]].[[page_source_id]] AND [[staged]].[[kind]]='row'
		), [[titled]] AS (
			SELECT [[templated]].[[item_kind]], [[templated]].[[reason]], [[templated]].[[source]], [[templated]].[[page_source_id]],
				CASE WHEN [[template_id]] IS NOT NULL THEN [[template_id]] WHEN [[staged_template]] IS NOT NULL THEN ''
					ELSE [[templated]].[[page_id]] END AS [[page_id]],
				CASE WHEN [[template_id]] IS NULL AND [[staged_template]] IS NULL
					THEN COALESCE(NULLIF([[leafen_page]].[[title]], ''), [[templated]].[[page_title]])
					ELSE COALESCE([[template_name]], [[staged_template]], '') END AS [[page_title]],
				[[template_id]] IS NOT NULL OR [[staged_template]] IS NOT NULL AS [[in_template]],
				[[templated]].[[subject]], [[templated]].[[issue_count]]
			FROM [[templated]]
			LEFT JOIN {{leafen_pages}} AS [[leafen_page]] ON [[leafen_page]].[[id]]=[[templated]].[[page_id]]
		), [[ranked]] AS (
			SELECT [[titled]].*,
				ROW_NUMBER() OVER (PARTITION BY [[item_kind]], [[reason]], [[source]]
					ORDER BY [[issue_count]] DESC, [[page_title]], [[page_source_id]], [[subject]]) AS [[place]],
				COUNT(*) OVER (PARTITION BY [[item_kind]], [[reason]], [[source]]) AS [[places]]
			FROM [[titled]]
		)
		SELECT [[item_kind]], [[reason]], [[page_id]], [[page_title]], [[in_template]], [[subject]] AS [[block_type]],
			[[issue_count]], [[places]]
		FROM [[ranked]] WHERE [[place]] <= {:shown}
		ORDER BY [[item_kind]], [[reason]], [[source]], [[place]]
	`).Bind(params).All(&rows)
	return rows, err
}

// notionImportNotWholeWithPages gives each item of a tally its first pages
// and how many there are, as Leafen's finish screen and 완료됨 row name them:
// each page as the app names one with no title, by its kind, and what was on
// it by its subject, or none when it had no name (notionImportIssueSubject).
func notionImportNotWholeWithPages(app core.App, jobID, migrationID string, tally []notionImportNotWhole) error {
	if len(tally) == 0 {
		return nil
	}
	issues, err := loadNotionImportIssuePlaces(app, jobID, migrationID, notionImportNotWholePagesShown)
	if err != nil {
		return err
	}
	places := notionImportIssuePlacesOf(issues)
	pageIDs := []string{}
	for _, found := range places {
		for _, place := range found.Items {
			pageIDs = append(pageIDs, place.PageID)
		}
	}
	kinds, err := loadPageNameKinds(app, pageIDs)
	if err != nil {
		return err
	}
	for index := range tally {
		item := &tally[index]
		found := places[notionImportIssuePlaceKey(item.Kind, item.Reason, item.Source)]
		if found == nil {
			continue
		}
		item.PagesTotal = found.Total
		for _, place := range found.Items {
			kind := kinds[place.PageID]
			item.Pages = append(item.Pages, notionImportNotWholePage{
				PageID: place.PageID, PageTitle: place.PageTitle, PageKind: kind.Kind, ViewBlock: kind.ViewBlock,
				SourceTitles: kind.SourceTitles, Template: place.InTemplate,
				Subject: notionImportIssueSubject(*item, place.BlockType), Count: place.Count,
			})
		}
	}
	return nil
}

// notionImportIssueSubject is what an item's problem on a page was about, as
// the import keeps it — a block's kind, a property's or view's name — or none
// when it has no name of its own: a database or linked database Notion's API
// calls "Untitled" for one never named (notion_import_linked_views.go), and a
// linked database showing one outside the import, whose line names that one.
// The places then say it by what it is (notionImportIssueWhat).
func notionImportIssueSubject(item notionImportNotWhole, subject string) string {
	if notionMigrationReportBlockReasons[item.Reason] {
		return subject
	}
	name := strings.TrimSpace(subject)
	switch notionImportNotWholeUnitOf(item) {
	case "database", "data_source", "linked_database":
		if name == notionUnnamedLinkedDatabaseTitle || item.Reason == notionImportIssueLinkedSourceOutside {
			return ""
		}
	case notionImportNotWholeView:
		if item.Reason == notionImportIssueLinkedSourceOutside {
			return ""
		}
	}
	return name
}

// notionImportIssueWhatWords say a place's problem by what it is when it has
// no name: what the item's line counts (notionImportNotWholeUnitOf).
var notionImportIssueWhatWords = map[string]map[string]string{
	accountLanguageKo: {
		notionImportNotWholeBlock: "블록", notionImportNotWholeProperty: "속성", notionImportNotWholeView: "보기",
		"database": "데이터베이스", "data_source": "데이터 소스", "linked_database": "링크된 데이터베이스",
	},
	accountLanguageEn: {
		notionImportNotWholeBlock: "Block", notionImportNotWholeProperty: "Property", notionImportNotWholeView: "View",
		"database": "Database", "data_source": "Data source", "linked_database": "Linked database",
	},
}

// notionImportIssueWhat says what an item's problem on a page was about, as
// the person knows it: a block by its kind's name in the block menu's words
// (notionMigrationReportSubject), a property or view by its own name, and one
// with no name by what it is — a linked database in a row page, say, as a
// linked database.
func notionImportIssueWhat(language string, text map[string]string, item notionImportNotWhole, subject string) string {
	if notionMigrationReportBlockReasons[item.Reason] {
		return notionMigrationReportSubject(text, storedNotionImportBlockIssue{BlockType: subject, Reason: item.Reason})
	}
	if subject = notionImportIssueSubject(item, subject); subject != "" {
		return subject
	}
	words := notionImportIssueWhatWords[language]
	if words == nil {
		words = notionImportIssueWhatWords[accountLanguageEn]
	}
	return words[notionImportNotWholeUnitOf(item)]
}

// notionImportIssuePlaceTitle is a place's page as the person knows it: its
// title, or for a template the template by its name (notionImportIssuePlaces).
func notionImportIssuePlaceTitle(text map[string]string, title string, template bool) string {
	if !template {
		return title
	}
	if name := strings.TrimSpace(title); name != "" {
		return fmt.Sprintf(text["templatePlace"], name)
	}
	return text["templatePlaceUnnamed"]
}

// The report's words for a place that is a template, beside its words for
// what the import met.
func init() {
	notionMigrationReportText[accountLanguageKo]["templatePlace"] = "템플릿 “%s”"
	notionMigrationReportText[accountLanguageKo]["templatePlaceUnnamed"] = "템플릿"
	notionMigrationReportText[accountLanguageEn]["templatePlace"] = "Template “%s”"
	notionMigrationReportText[accountLanguageEn]["templatePlaceUnnamed"] = "Template"
}
