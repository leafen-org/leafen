package app

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Search keeps a date mention by its day in full in English, with its time
// and the zone it was written with ("March 17, 2026", "March 17, 2026 2:30 PM
// (GMT+9)", "April 2, 2026 → April 5, 2026"), whatever it was made with and
// whoever reads it, as Notion's search finds and shows one (seen 2026-11-22:
// its MCP search finds date pages by "March", by "17" and "2026" of
// "2026-03-17", by "PM GMT", not by "3월"; its app's search line in a Korean
// workspace reads "notion bodytimed November 5, 2026 9:40 AM (GMT+9)" and
// finds it by "November"). A page's title, its lines, a sub-page's line and
// a database page's values and body say it so in the index, and a result's
// line and an agent's highlight show it so (mcpDateMentionSearched).

// searchDateMentionWords is a date mention as search keeps it; "" for one it
// can't read, which keeps its words.
func searchDateMentionWords(span graphTextSpan) string {
	return mcpDateMentionSearched(span)
}

// searchIndexTextReader reads a block's text as the index keeps it
// (searchTextReader), each date mention said as search keeps it.
func searchIndexTextReader(app core.App, workspaceID string) func(*crdt.YText) string {
	return searchTextReaderSaying(app, workspaceID, searchDateMentionWords)
}

// searchDateMentionSpanWords is a span's words as search keeps them: a date
// mention's said as search keeps it, any other's as written.
func searchDateMentionSpanWords(span graphTextSpan, text string) string {
	if span.Mention == "date" {
		if words := searchDateMentionWords(span); words != "" {
			return words
		}
	}
	return text
}

// requeueSearchDateMentionWords is migration 845's content: every entry the
// index holds is made again whole, so what it made before says its date
// mentions as search keeps them now.
func requeueSearchDateMentionWords(app core.App) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
		SELECT [[page_id]], [[workspace_id]], 1, {:now} FROM {{leafen_search_pages}} WHERE TRUE
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).Bind(dbx.Params{"now": time.Now().UnixMilli()}).Execute()
	return err
}

func validateSearchDateMentionWordsRequeued(app core.App) error {
	for _, table := range []string{"leafen_search_pages", "leafen_search_dirty"} {
		if !app.HasTable(table) {
			return fmt.Errorf("search date mention words: the table %s is missing", table)
		}
	}
	return nil
}
