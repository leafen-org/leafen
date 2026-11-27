package app

import (
	"encoding/base64"
	"encoding/json"

	"github.com/pocketbase/dbx"
)

// Where a window of the pages page-children lists (page_children_routes.go)
// goes on from: past the last page it read by the order it reads in — the
// person's own pages by their place among their siblings and ID, then those
// they open only through shares the same way — so a page added or moved
// meanwhile neither shifts the rest nor repeats a window, as an offset
// would.

// pageChildrenCursor is the last page a window read: whether it was among
// those opened only through shares, its place and its ID. The zero value
// reads from the first.
type pageChildrenCursor struct {
	Shared   bool    `json:"s,omitempty"`
	Position float64 `json:"p"`
	ID       string  `json:"i"`
}

// pageChildrenCursorAfter is the nextCursor of a window whose last page
// read is last.
func pageChildrenCursorAfter(last pageChildWire) string {
	encoded, _ := json.Marshal(pageChildrenCursor{Shared: last.Shared, Position: last.Position, ID: last.ID})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// readPageChildrenCursor reads pageChildrenCursorAfter back; false for one
// it didn't make.
func readPageChildrenCursor(raw string) (pageChildrenCursor, bool) {
	cursor := pageChildrenCursor{}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.ID == "" {
		return pageChildrenCursor{}, false
	}
	return cursor, true
}

// pageChildrenAfterSQL keeps of the pages a list reads ([[page]]) those past
// after, binding its place in params; nothing when after is the start. The
// index of a parent's pages by place (pagePositionIndex) starts there.
func pageChildrenAfterSQL(after pageChildrenCursor, params dbx.Params) string {
	if after.ID == "" {
		return ""
	}
	params["afterPosition"], params["afterId"] = after.Position, after.ID
	return `
			AND ([[page]].[[position]], [[page]].[[id]]) > ({:afterPosition}, {:afterId})`
}
