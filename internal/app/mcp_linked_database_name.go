package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A linked database without a name of its own goes by its source's name
// with "보기" (view) after it, as Notion names it in its path and window and
// its MCP fetch names it (seen 2026-11-21: "The title of this Database is:
// 〈source〉 보기", its block holding no title; its block heads it "↗
// 〈source〉"), as the app's window and path do (linkedDatabaseName,
// databaseTitles.ts). Its reader's language says it, as a database without
// a name reads as a new one (mcpDatabaseShownTitle).

// mcpLinkedDatabaseShownTitle is an unnamed linked database's name in fetch,
// its source's name read as given.
func mcpLinkedDatabaseShownTitle(app core.App, principalID, sourceTitle string) string {
	source := mcpDatabaseShownTitle(app, principalID, strings.TrimSpace(sourceTitle))
	if mcpReaderReadsKorean(app, principalID) {
		return source + " 보기"
	}
	return source + " view"
}
