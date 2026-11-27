package app

// Backlinks come from more than a page's body, as Notion's do (seen
// 2026-11-10): a page or database page mentioned in a database page's title
// or text cell, a database page a one-way relation holds — a two-way one,
// shown on both its sides, makes none — and a page mentioned in an ordinary
// page's title. Each such field feeds the one page reference table beside
// the body's blocks, under a key of its own in the block column: "cell:"
// with the database page and its property for a cell, "title:" with the
// page for a title. A database's own name makes none. Triggers keep those
// rows as the cells, relation properties and titles change, and a relation
// to its own database that pairs with itself is two-way here.

const (
	pageFieldReferenceCellType    = "database_cell"
	pageFieldReferenceTitleType   = "page_title"
	pageFieldReferenceCellPrefix  = "cell:"
	pageFieldReferenceTitlePrefix = "title:"
)

// pageFieldCellKeySQL is a cell's key in the page reference table.
func pageFieldCellKeySQL(cell string) string {
	return `'` + pageFieldReferenceCellPrefix + `' || ` + cell + `.[[row_id]] || ':' || ` + cell + `.[[property_id]]`
}

// pageFieldTitleKeySQL is a page title's key in the page reference table.
func pageFieldTitleKeySQL(page string) string {
	return `'` + pageFieldReferenceTitlePrefix + `' || ` + page + `.[[id]]`
}

// jsonArraySQL is a column's JSON array, or an empty one for anything else.
func jsonArraySQL(column string) string {
	return `(CASE WHEN json_valid(` + column + `) THEN CASE WHEN json_type(` + column + `) = 'array'
			THEN ` + column + ` ELSE '[]' END ELSE '[]' END)`
}

// spanObjectSQL is a span of a spans list as an object: one that is no
// object reads as an empty one.
const spanObjectSQL = `(CASE WHEN [[span]].[[type]] = 'object' THEN [[span]].[[value]] ELSE '{}' END)`

// pageTitleSpellsSQL is true while an ordinary page's title spans spell its
// title; spans that don't are no longer its title's (page_title_spans.go).
func pageTitleSpellsSQL(page string) string {
	return page + `.[[data_source_id]] = '' AND ` + page + `.[[kind]] != '` + pageKindDatabase + `'
		AND ` + page + `.[[title_spans]] != ''
		AND (SELECT group_concat(IFNULL(json_extract(` + spanObjectSQL + `, '$.text'), ''), '' ORDER BY [[span]].[[key]])
			FROM json_each(` + jsonArraySQL(page+`.[[title_spans]]`) + `) [[span]]) IS ` + page + `.[[title]]`
}
