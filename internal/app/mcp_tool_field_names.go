package app

import (
	"sort"
	"strings"
)

// A native operation's refusal a tool passes on names the operation's fields
// (Check targetDataSourceId: …), which the agent never gave: the tool took
// what it asked as its own inputs (new_parent.data_source_id), and the agent
// couldn't tell what to change (2026-11-26: notion-move-pages moving a row
// into a database it was in already). A tool that passes one on says each
// field its inputs carry by the input's name, by its table here; a field the
// tool fills itself (a request ID, a revision it read) stays as it is.
// Where the refusal tells a way by the native API — a path's {pageId}, a
// field of another operation's body after a "." — its words stay.

// mcpToolFieldNames are, by a native field's name, the tool input that
// carries it.
type mcpToolFieldNames map[string]string

// mcpMovePagesFieldNames are notion-move-pages' inputs for the fields of the
// row move (POST /database-rows/{rowId}/page/move) and of the conversions
// between a page and a database's item its refusals name
// (page_row_conversion_refusals.go, destination_refusals.go).
var mcpMovePagesFieldNames = mcpToolFieldNames{
	"targetDataSourceId": "new_parent.data_source_id", "dataSourceId": "new_parent.data_source_id",
	"targetPageId": "new_parent.page_id", "targetTeamspaceId": "new_parent",
	"pageId": "page_or_database_ids", "rowId": "page_or_database_ids",
	"acceptDroppedPropertyIds": "drop_values",
}

// words are a refusal's words with each field it names said as the tool's
// input: a whole word, not within a path's braces or after a "." or a "/".
func (names mcpToolFieldNames) words(message string) string {
	if len(names) == 0 {
		return message
	}
	fields := make([]string, 0, len(names))
	for field := range names {
		fields = append(fields, field)
	}
	// The longest first, so that one holding another is said whole.
	sort.Slice(fields, func(a, b int) bool { return len(fields[a]) > len(fields[b]) })
	for _, field := range fields {
		var said strings.Builder
		rest := message
		for {
			at := strings.Index(rest, field)
			if at < 0 {
				said.WriteString(rest)
				break
			}
			end := at + len(field)
			before, after := byte(' '), byte(' ')
			if at > 0 {
				before = rest[at-1]
			}
			if end < len(rest) {
				after = rest[end]
			}
			said.WriteString(rest[:at])
			if mcpFieldNameCharacter(before) || mcpFieldNameCharacter(after) || strings.IndexByte("{/.", before) >= 0 || after == '}' {
				said.WriteString(field)
			} else {
				said.WriteString(names[field])
			}
			rest = rest[end:]
		}
		message = said.String()
	}
	return message
}

func mcpFieldNameCharacter(character byte) bool {
	return character == '_' || character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}
