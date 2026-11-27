package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// pageHistoryVersionPageLock is a version that only locked or unlocked a page
// for everyone: its event is the page's state, replayed as the metadata change
// it is. Notion's history lists one at the lock's minute (seen 2026-11-15 and
// twice 2026-11-16, an unlock too), its preview outlining the page's title; its
// updates list the lock alone, its own line from the lock records, not an edit
// (pageUpdates.ts). It sets off no activity (maintenance) and no webhook.
const pageHistoryVersionPageLock = "page_lock"

// recordPageHistoryPageLock records a page locked or unlocked for everyone as
// a version of its own (pageHistoryVersionPageLock).
func recordPageHistoryPageLock(app core.App, pageID, actorID string) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	state, err := pageHistoryPageStateFor(app, page)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, _, err = recordPageHistoryEventKind(app, pageID, actorID, pageHistoryVersionPageLock, pageHistoryEventMetadata, payload, true)
	return err
}
