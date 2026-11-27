package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Some IDs Leafen makes end as a slugged address does, in a hyphen and 32
// hexadecimal digits: a database template's database and its example rows,
// "starter-<hex>" (databaseStarterIdentity), a toolbar template's row. Read
// as Title-ID (mcpSlugAddressID), their addresses named the hex alone and
// found nothing (2026-11-22: notion-fetch of a template's database, at the
// address its own fetch gave, and of its rows answered LEAFEN_PAGE_NOT_FOUND,
// while the screen opened them). One that names a page or a database here
// reads as itself; a page's slugged address, whose whole last part names
// nothing, still reads as its ID.

// mcpOwnSlugShapedID is the ID an address or what was written names when it
// is one of those, whole.
func mcpOwnSlugShapedID(app core.App, raw string) (string, bool) {
	part, slugged := mcpReferencePart(raw)
	if !slugged || mcpSlugAddressID(part) == part || len(part) > maxGraphIdentifierLength {
		return "", false
	}
	found := 0
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM {{leafen_pages}} WHERE [[id]]={:id}
			UNION ALL
			SELECT 1 FROM {{leafen_database_containers}} WHERE [[id]]={:id}
			LIMIT 1
		)
	`).Bind(dbx.Params{"id": part}).Row(&found); err != nil || found == 0 {
		return "", false
	}
	return part, true
}
