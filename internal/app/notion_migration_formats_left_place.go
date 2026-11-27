package app

import "strings"

// What the run couldn't match is named where it is (2026-11-22: a report
// folded 301 table views under one line, 99 of them "새 데이터베이스" —
// views left unnamed — none saying which table): a view by the page that
// holds it, its database when that has a name of its own, then its own
// name or, left unnamed, its layout's — "프로젝트 › 업무 › 표"; a database in
// another page's body by that page — "프로젝트 › 업무"; a database template
// by its database. A full-page database's page is the database itself, said
// once. The report (notion_migration_formats_report.go) and the agent's
// status list (formats_left: place, layout) name them so.

// notionMigrationPlaceStep joins the names of where something is.
const notionMigrationPlaceStep = " › "

// notionMigrationFormatsLeftPlace is where a mapped view or data source
// (kind) is: the page that holds it, of holderKind, and the database a view
// shows.
func notionMigrationFormatsLeftPlace(kind, holder, holderKind, database string) string {
	holder, database = strings.TrimSpace(holder), strings.TrimSpace(database)
	switch {
	case kind == "data_source" && holderKind != pageKindDatabase:
		return holder
	case kind != "view":
		return ""
	case holderKind == pageKindDatabase || database == holder:
		return cmpOr(holder, database)
	case holder == "" || database == "":
		return holder + database
	}
	return holder + notionMigrationPlaceStep + database
}

// notionMigrationViewLayoutNames name a view left unnamed by its layout, as
// its tab shows it.
var notionMigrationViewLayoutNames = map[string]map[string]string{
	accountLanguageKo: {
		graphViewTypeTable: "표", graphViewTypeBoard: "보드", graphViewTypeGallery: "갤러리", graphViewTypeList: "리스트",
		graphViewTypeTimeline: "타임라인", graphViewTypeCalendar: "캘린더", graphViewTypeForm: "폼", graphViewTypeChart: "차트",
		graphViewTypeDashboard: "대시보드", graphViewTypeFeed: "피드", graphViewTypeMap: "지도",
	},
	accountLanguageEn: notionImportViewNames,
}

// notionMigrationFormatsLeftNamed is an item as the report folds it: where
// it is, then its name — a view left unnamed by its layout's, anything else
// untitled by untitled.
func notionMigrationFormatsLeftNamed(language string, item leafenMCPNotionFormatLeft, untitled string) string {
	if item.Layout == "" && strings.TrimSpace(item.Title) == "" {
		item.Title = untitled
	}
	return notionMigrationViewNamed(language, item.Place, item.Title, item.Layout)
}

// notionMigrationViewNamed is something where it is (place), by its name or,
// a view left unnamed, its layout's. The check against Notion names the
// views it lists so too (notion_migration_verify.go).
func notionMigrationViewNamed(language, place, name, layout string) string {
	if name = strings.TrimSpace(name); name == "" && layout != "" {
		name = cmpOr(notionMigrationViewLayoutNames[language][layout], notionImportViewNames[layout])
	}
	if place == "" || name == "" {
		return place + name
	}
	return place + notionMigrationPlaceStep + name
}
