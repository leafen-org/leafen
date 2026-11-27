package app

import (
	"encoding/json"
	"time"
)

// The settings reader is long — about 27,000 characters with its comments —
// and an agent had to write it again, whole and without a slip, into its
// browser tool to run it in the person's Notion tab (2026-11-23: an agent
// stripped the comments by hand and still wrote about 15,000). Notion's page
// takes no script from Leafen's address, by a tag or by fetch (measured
// 2026-11-23 on a synthetic page: its script-src and connect-src name
// Notion's and its partners' hosts alone, and both were refused), but it runs
// code it is handed ('unsafe-eval'), and the Notion tab Leafen's reading page
// opens can hear from that page (notion_migration_formats_reading_page.go).
// So the agent writes a start line of a few hundred characters with the key:
// it asks the page that opened its tab for the reader, and runs it with the
// key; the page holds the reader without it. `reader` stays whole, comments
// and all, for a person to read, and for a Notion tab opened otherwise.

// notionMigrationFormatsStartKey is the name the page's reader takes its key
// by, which the start line binds.
const notionMigrationFormatsStartKey = "leafenKey"

// notionMigrationFormatsReaderStart is the start line for a reader's key: in
// the Notion tab the reading page opened, it asks that page for the reader,
// runs it with the key, and answers what the reader answers once it has
// handed in; without a page that opened the tab, or one that doesn't answer,
// it says so.
func notionMigrationFormatsReaderStart(key string) string {
	encoded, _ := json.Marshal(key)
	return `new Promise((done, failed) => { if (!opener) return failed(Error("Open this Notion tab with reader_page's button.")); ` +
		`addEventListener("message", function take(event) { if (event.source !== opener || typeof event.data?.leafenReader !== "string") return; ` +
		`removeEventListener("message", take); done(Function("` + notionMigrationFormatsStartKey + `", event.data.leafenReader)(` + string(encoded) + `)); }); ` +
		`opener.postMessage({ leafenReaderPlease: true }, "*"); setTimeout(() => failed(Error("Leafen's reader_page didn't answer.")), 10000); })`
}

// notionMigrationFormatsPageReader is the reader the reading page hands the
// start line: its key by the name the line binds, where it hands in left for
// the page to say by its own address, the roots by their ids alone, and it
// answers what the reader answers.
func notionMigrationFormatsPageReader(roots []notionImportRoot, expires time.Time) string {
	ids := make([]notionImportRoot, 0, len(roots))
	for _, root := range roots {
		ids = append(ids, notionImportRoot{ID: root.ID, Kind: root.Kind})
	}
	return "return " + notionMigrationFormatsReaderCode("__ENDPOINT__", notionMigrationFormatsStartKey, ids, expires)
}
