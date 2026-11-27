package app

// People's data source is drawn with Notion's People icon wherever its icon
// is read — the built-in person in a ring, in gray (Notion's
// "/icons/user-circle_gray.svg", seen 2026-11-19 in Chrome at 1440, its
// Business-trial owner: a linked view's "데이터 소스 연결" list, searched and
// not, a relation's "관계형 대상" list and the new relation's "관계형 대상"
// row, 19.19px in their 20px box, #D3D3D3 on dark and #55534E on light), as
// Notion's People carries it as its own icon. The server made it with none
// (people_database.go), so a read gives it this one while it has none, and
// one its owner gives it is its own: a source's lists (the picker,
// database_container_picker_icons.go), a page's read of its databases
// (content_graph_window.go), the sidebar's (sidebar_inline_databases.go),
// a link's target (page_link_targets.go) and the search's
// (workspace_search_databases.go) all read it by these.

const peopleDataSourceIcon = "user-circle:gray"

// peopleSourceWithoutIconSQL holds for the data source the alias names when
// it is a workspace's People and has no icon of its own.
func peopleSourceWithoutIconSQL(source string) string {
	return `(COALESCE(` + source + `.[[icon_type]], '') IN ('', '` + graphDataSourceIconNone + `')
		AND ` + source + `.[[id]] IN (SELECT [[data_source_id]] FROM {{leafen_workspace_people_databases}}))`
}

// dataSourceIconSQL is the icon a read shows for the data source the alias
// names: its own, People's where it is People and has none.
func dataSourceIconSQL(source string) string {
	return `(CASE WHEN ` + peopleSourceWithoutIconSQL(source) + ` THEN '` + peopleDataSourceIcon + `' ELSE ` + source + `.[[icon]] END)`
}

// dataSourceIconTypeSQL is the kind of that icon (dataSourceIconSQL).
func dataSourceIconTypeSQL(source string) string {
	return `(CASE WHEN ` + peopleSourceWithoutIconSQL(source) + ` THEN '` + graphDataSourceIconBuiltIn + `' ELSE ` + source + `.[[icon_type]] END)`
}
