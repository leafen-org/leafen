package app

import (
	"strings"

	"github.com/google/uuid"
)

// A page the server makes — a copy of a page, of a sub-page or of a
// database's row, a template's copy, a row a template or a request makes, or
// a page a connection, Markdown or a new workspace makes — takes the shape of
// ID the screen gives its own (the web's newId): a UUID, its address and its
// API ID alike, as Notion's every page has (seen 2026-11-14: a page's copy
// opened at …-4181fd67973880968276ef71441f3384, by any path). So do the
// rows and pages an import, a migration, a form's answer and a sent item's
// task make. Pages made before kept their 15-character IDs;
// they still open and answer the public API with the page codec's UUID
// (pageOwnPublicID). What finds again by its parts a row it made — a CSV
// merge taking its rows back, a ZIP batch's log, a migration's objects, an
// answer sent again — still finds one made in the older form (each says
// how), and a migration connection that began before keeps the 15 digits
// its bundles gave its pages, by which the migration finds them in its next
// bundle (workspaceImportArchiveIDSalt).

// newPageID is a new page's ID.
func newPageID() string {
	return uuid.NewString()
}

// derivedPageID is that shape for a page ID derived from its parts, the same
// for the same parts, for a request that may be sent again. The web derives
// a row copy's ID the same way (databaseRowPageApi.ts duplicateRowCopyId).
func derivedPageID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("leafen:page:v1\x00"+strings.Join(parts, "\x00"))).String()
}
