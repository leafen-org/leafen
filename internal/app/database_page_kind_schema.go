package app

// The pages that held only their own database when kinds arrived became
// full-page databases; the rest are pages.

// fullPageDatabaseShapedSQL holds for a page of leafen_pages, in a statement
// on leafen_pages itself, shaped as a full-page database before kinds were
// stored: a page, not a row, template or wiki, whose only top-level block is
// its own database and which has no sub-pages, in the trash or not. Each page
// looks at its own containers and their own data sources (2026-11-08: a list
// of every page so shaped, which an archive import's few pages were checked
// against, read every top-level block of the server).
const fullPageDatabaseShapedSQL = `([[data_source_id]] = '' AND NOT [[` + databaseTemplatePageField + `]]
		AND EXISTS (
		SELECT 1 FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_blocks}} AS [[own]] ON [[own]].[[owner]] = [[container]].[[owner]]
			AND [[own]].[[page_id]] = [[container]].[[page_id]] AND [[own]].[[id]] = [[container]].[[block_id]]
			AND [[own]].[[parent_id]] = ''
		WHERE [[container]].[[page_id]] = {{leafen_pages}}.[[id]] AND [[container]].[[kind]] = 'source'
			AND [[container]].[[parent_block_id]] = ''
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]])
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_sources}} AS [[member]]
				CROSS JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[member]].[[data_source_id]]
				WHERE [[member]].[[container_id]] = [[container]].[[id]] AND [[source]].[[wiki]] = 1)
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_blocks}} AS [[top]]
				WHERE [[top]].[[owner]] = [[container]].[[owner]] AND [[top]].[[page_id]] = [[container]].[[page_id]]
					AND [[top]].[[parent_id]] = '' AND [[top]].[[id]] <> [[container]].[[block_id]])
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_pages}} AS [[sub]]
				WHERE [[sub]].[[parent_id]] = [[container]].[[page_id]] AND [[sub]].[[data_source_id]] = '')
	))`
