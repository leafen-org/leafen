package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
)

// A copied database's rows are copied one database at a time in the order
// they were made, oldest first, so each copy takes its created time in that
// order (databaseRowCreatedAt): a view that shows rows it has not placed
// newest first shows the copies as its source showed its rows, where an
// order by their IDs, which are random, put them in no order at all (seen
// 2026-11-14: a database's ⌘D showed its two rows the other way round).
// The window walks idx_leafen_database_rows_source_created; its cursor is
// the last row's database, created time and ID as a JSON array. A copy
// begun before keeps the plain ID it walked by and finishes by IDs.

type pageDuplicationRowWindowCursor struct {
	legacy       bool
	dataSourceID string
	created      int64
	id           string
}

func parsePageDuplicationRowCursor(cursor string) pageDuplicationRowWindowCursor {
	if cursor == "" {
		return pageDuplicationRowWindowCursor{}
	}
	if !strings.HasPrefix(cursor, "[") {
		return pageDuplicationRowWindowCursor{legacy: true, id: cursor}
	}
	var parts []json.RawMessage
	parsed := pageDuplicationRowWindowCursor{}
	if json.Unmarshal([]byte(cursor), &parts) != nil || len(parts) != 3 ||
		json.Unmarshal(parts[0], &parsed.dataSourceID) != nil ||
		json.Unmarshal(parts[1], &parsed.created) != nil ||
		json.Unmarshal(parts[2], &parsed.id) != nil {
		// A cursor this never writes reads as the start.
		return pageDuplicationRowWindowCursor{}
	}
	return parsed
}

// window is the row window's condition after the cursor and its order, with
// the cursor's parameters added to params.
func (cursor pageDuplicationRowWindowCursor) window(params dbx.Params) (string, string) {
	if cursor.legacy {
		params["rowCursorId"] = cursor.id
		return "[[source]].[[id]]>{:rowCursorId}", "[[source]].[[id]]"
	}
	order := "[[source]].[[data_source_id]],[[source]].[[created]],[[source]].[[id]]"
	if cursor.id == "" {
		return "1=1", order
	}
	params["rowCursorSource"], params["rowCursorCreated"], params["rowCursorId"] =
		cursor.dataSourceID, cursor.created, cursor.id
	return "([[source]].[[data_source_id]],[[source]].[[created]],[[source]].[[id]])>" +
		"({:rowCursorSource},{:rowCursorCreated},{:rowCursorId})", order
}

func pageDuplicationRowCursor(current string, rows []pageDuplicationSourceDatabaseRow) string {
	if len(rows) == 0 {
		return current
	}
	last := rows[len(rows)-1]
	if parsePageDuplicationRowCursor(current).legacy {
		return last.ID
	}
	encoded, err := json.Marshal([]any{last.DataSourceID, last.Created, last.ID})
	if err != nil {
		return last.ID
	}
	return string(encoded)
}
