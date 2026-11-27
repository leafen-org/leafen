package app

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The settings reader tells Leafen how far it is while it reads, about a
// second apart, for Leafen's import window to show live
// (notion_migration_live.go); the reading itself still comes whole at the
// end. It goes as an image's address: Notion's pages may load an image from
// any https address, while their scripts reach no other server (seen
// 2026-11-23 in a Notion tab: img-src took an https address, connect-src
// refused fetch and sendBeacon). The address names what was read by its
// Notion IDs alone, which Leafen names from its scan — no page's name or
// text goes in an address, which logs keep — and the reader's key by its
// hash, as the hand-in keeps it. Its answer is empty: a stale or unknown key
// is told nothing.

const notionMigrationFormatsProgressPath = notionMigrationFormatsPath + "/progress"

func notionMigrationFormatsProgressRoute(event *core.RequestEvent) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	query := event.Request.URL.Query()
	if hash := strings.ToLower(strings.TrimSpace(query.Get("h"))); len(hash) == 64 {
		var migrationID string
		if err := event.App.DB().NewQuery(`SELECT [[migration_id]] FROM {{leafen_notion_migration_formats}}
			WHERE [[kind]]='reader' AND [[notion_id]]='' AND json_extract([[value]], '$.keyHash')={:hash}`).
			Bind(dbx.Params{"hash": hash}).Row(&migrationID); err == nil {
			noteNotionMigrationFormatsProgress(event.App, migrationID, query.Get)
		}
	}
	return event.NoContent(http.StatusNoContent)
}

// noteNotionMigrationFormatsProgress notes a reader's counts — pages,
// databases and bodies read, left, and how long Notion asked it to wait —
// and what it read last: p<page ID>, d<database ID>, or r<row ID>.<its
// database's ID>, a row named by its database, which the scan names.
func noteNotionMigrationFormatsProgress(app core.App, migrationID string, value func(string) string) {
	count := func(name string) int {
		number, _ := strconv.Atoi(value(name))
		return min(max(number, 0), notionMigrationFormatsMostItems)
	}
	stage := "formats"
	noteNotionMigrationLiveCounts(migrationID, stage, count("p")+count("d")+count("b"), count("l"), count("w"))
	item := strings.TrimSpace(value("i"))
	if len(item) < 2 || !notionMigrationLiveDue(migrationID) {
		return
	}
	kind, table, id := "page", notionMigrationItemPage, item[1:]
	switch item[0] {
	case 'd':
		kind, table = "database", notionMigrationItemDatabase
	case 'r':
		_, database, _ := strings.Cut(id, ".")
		kind, table, id = "row", notionMigrationItemDatabase, database
	case 'p':
	default:
		return
	}
	var title string
	if err := app.DB().NewQuery(`SELECT COALESCE(json_extract([[value]], '$.title'), '') FROM {{leafen_notion_migration_scan_items}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]={:kind} AND [[item_key]]={:key}`).
		Bind(dbx.Params{"migrationId": migrationID, "kind": table, "key": notionMigrationSourceKey(id)}).Row(&title); err == nil {
		noteNotionMigrationLive(migrationID, stage, kind, title)
	}
}
