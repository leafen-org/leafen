package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A database block stands on its page with its row among the databases
// (leafen_database_containers), which is what keeps it among favorites, gives
// it its public ID, names it to search and to agents and carries it through a
// backup. Whatever writes a block writes that row in one place,
// projectDatabaseBlock: the content graph's writes — an edit, ⌘Z and ⌘⇧Z,
// the trash's restore, the public API's block restore, a revert of a page's
// history — and a version of the page restored, which writes the page's
// blocks itself (page_history_linked_views_back.go). A restored version that
// brought back a linked database wrote its block alone: it showed, and had no
// row (2026-11-25).

// projectDatabaseBlock writes the database row of a block written to its
// page: a database block takes its row, its sources and views; one its page
// took away unable to show it comes back as a block alone
// (database_unavailable_blocks.go); a block of another type has none.
func projectDatabaseBlock(
	app core.App, owner, workspaceID, pageID, blockID, blockType, parentID, position string, content json.RawMessage,
) error {
	containerContent := databaseContainerBlockContent{}
	if blockType == graphBlockTypeInlineDatabase {
		var err error
		if containerContent, err = parseDatabaseContainerBlockContent(content); err != nil {
			return err
		}
		if returned, err := takeBackUnavailableDatabaseBlock(app, pageID, blockID, containerContent); err != nil || returned {
			return err
		}
	}
	return projectDatabaseContainerBlock(app, owner, workspaceID, pageID, blockID, parentID, position, containerContent)
}
