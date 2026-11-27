package app

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's write version is what an agent reads a page at and writes it
// back against (MCP's cursor and base_cursor, mcp_page_version.go): one
// number that moves with everything a write to the page reads and may write
// back. It is a digest of the page record's revision (its title, icon,
// cover, lock, verification, place and trash, each change one more —
// commitPageMetadata), for a database page its row page's revision (its
// values, title, icon, cover, trash and place), and the page's body as the
// write reads it: its Markdown, which shows its blocks and what its body
// shows of other pages a write may change — a sub-page's or database's
// title its tag gives, a synced block's original its reference holds, a
// mention's name, the name of the page or database a link leads to — and,
// for a body too long to read whole, its body's
// revision too. Only the same page, read the same, gives the same version;
// what the Markdown doesn't show (a sub-page's own body or icon, an inline
// database's rows, the rest of a synced original's page) leaves it be.
// A version is a name, not an order: it is only ever compared for being the
// same, so it is the digest's first 52 bits, exact as a JSON number.
// Before (2026-11-27), an ordinary page's cursor was its record's alone, and
// then its record's revision and body's revision added: a write sent with an
// earlier read's cursor wrote over a body changed since, then over a
// sub-page's or a full-page database's new title and a synced original's new
// content, and an inline database's new row refused the page's body write.
// The page record's own cursor stays what it is: the screen's fence for a
// page's title and kind (basePageCursor), which a body change leaves be.

// pageWriteVersionOf is a page's write version, its body as read reads it.
func pageWriteVersionOf(app core.App, page *core.Record, read notionPageMarkdownProjection) (int, error) {
	row := 0
	if isDatabaseRowPageRecord(page) {
		if err := app.DB().NewQuery(`
			SELECT COALESCE(MAX([[authority_revision]]), 0) FROM {{leafen_database_row_pages}} WHERE [[row_id]] = {:rowId}
		`).Bind(dbx.Params{"rowId": page.Id}).Row(&row); err != nil {
			return 0, err
		}
	}
	digest := sha256.New()
	digest.Write([]byte("leafen-page-write-version-1\x00" + page.Id + "\x00" + strconv.Itoa(page.GetInt("revision")) + "\x00" + strconv.Itoa(row) + "\x00"))
	if read.Truncated {
		digest.Write([]byte("truncated\x00" + strconv.Itoa(read.GraphRevision) + "\x00"))
	}
	digest.Write([]byte(read.Markdown))
	return int(binary.BigEndian.Uint64(digest.Sum(nil)[:8]) >> 12), nil
}

// holdPageWriteVersion refuses a write sent against a version the page has
// moved on from, its record and body as the write's transaction reads them;
// none given holds to none.
func holdPageWriteVersion(app core.App, page *core.Record, read notionPageMarkdownProjection, base *int) error {
	if base == nil {
		return nil
	}
	version, err := pageWriteVersionOf(app, page, read)
	if err != nil {
		return err
	}
	if version != *base {
		return graphConflictError(errorCodeGraphPageCursorConflict,
			"The page changed since the version given was read. Read it again, then make the change on what it holds now.")
	}
	return nil
}
