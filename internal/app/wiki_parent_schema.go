package app

// A wiki is a database whose pages are its data source's pages, as in
// Notion, and they keep the place they have as pages: a page of a wiki sits
// under the page above it in the wiki, or under the wiki's own page, for its
// ancestry, its place in that page's body and the sidebar. A data source
// marked as a wiki's is what makes its pages sit so; other database pages sit
// under the page holding their database and stay out of the sidebar.

// pageWikiParentSQL names the page a page of a wiki sits under for the page
// record aliased: the page above it in the wiki, or the wiki's own page. It
// is NULL for a page outside wikis.
func pageWikiParentSQL(alias string) string {
	return `(SELECT CASE WHEN COALESCE([[wiki_row]].[[subitem_parent_id]], '') != '' THEN [[wiki_row]].[[subitem_parent_id]]
			ELSE [[wiki_source]].[[page_id]] END
		FROM {{leafen_data_sources}} AS [[wiki_source]]
		LEFT JOIN {{leafen_database_rows}} AS [[wiki_row]] ON [[wiki_row]].[[id]] = ` + alias + `.[[id]]
		WHERE [[wiki_source]].[[id]] = ` + alias + `.[[data_source_id]] AND [[wiki_source]].[[wiki]] = 1)`
}

// pageTreeParentSQL names the page the page record aliased sits under in the
// page tree: the page above it in its wiki for a page of a wiki, else its
// parent.
func pageTreeParentSQL(alias string) string {
	return `COALESCE(` + pageWikiParentSQL(alias) + `, ` + alias + `.[[parent_id]])`
}

// wikiRowSQL holds when the page record aliased is a page of a wiki.
func wikiRowSQL(alias string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_data_sources}} AS [[wiki_row_source]]
		WHERE [[wiki_row_source]].[[id]] = ` + alias + `.[[data_source_id]] AND [[wiki_row_source]].[[wiki]] = 1)`
}
