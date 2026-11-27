package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// A public connection's change to a page's body edits the page as its bot,
// as Notion's does (seen 2026-11-22, worker w2345, twice: after the
// integration's update_content, block update or appended blocks the page's
// last_edited_by was its bot, after an agent's MCP edit the person; its
// appended blocks or update_content in a database page's body made that
// row's last_edited_by its bot, the page holding the database as it was; its
// page comment changed nothing). The page's edit is noted once the change is
// applied (noteGraphPageEdited), after the write's own marks were compared
// (withConnectionGraphAuthorship), so the connection is named at the marks
// that note left.

// recordConnectionPageEdit notes that the public connection a write came
// through edited its page, and the row whose page it is, at the edit marks
// the page's edit just left; nothing for a write of the person's own.
func recordConnectionPageEdit(ctx context.Context, app core.App, pageID string) error {
	identity, ok := connectionAuthorFrom(ctx)
	if !ok || notionConnectionGrant(identity) == "" {
		return nil
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	if err := recordConnectionAuthorship(app, identity, connectionAuthoredPage, pageID, false, page.GetString("updated")); err != nil {
		return err
	}
	mark, err := connectionStoredMark(app, connectionAuthoredRow, pageID)
	if err != nil || mark == "" {
		return err
	}
	return recordConnectionAuthorship(app, identity, connectionAuthoredRow, pageID, false, mark)
}
