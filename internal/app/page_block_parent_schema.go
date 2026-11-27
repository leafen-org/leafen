package app

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pages are block records, as Notion's pages are: one ID space with the
// database rows, which become pages whose parent is a data source. This
// first step widens page IDs to the content IDs rows and blocks already use
// (1–255 characters without spaces; new pages keep their generated IDs)
// and names each page's parent kind: the workspace, a page, or a data
// source. A trigger keeps the kind of workspace and page parents in step
// with parent_id, so every page writer stays as it is.

const (
	pageParentTypeWorkspace  = "workspace"
	pageParentTypePage       = "page"
	pageParentTypeDataSource = "data_source"
)

// validPageRecordID reports whether a value can be a page's ID, as the ID
// field takes it (no white space, as the schema baseline's pattern for it
// says, and at most maxGraphIdentifierLength).
func validPageRecordID(value string) bool {
	return value != "" && utf8.RuneCountInString(value) <= maxGraphIdentifierLength &&
		!strings.ContainsFunc(value, unicode.IsSpace)
}
