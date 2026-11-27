package app

import "github.com/pocketbase/pocketbase/core"

// Before the server changes a page in a way its history keeps, the typing its
// open room holds is stored first (storeRoomTypingAhead), so the change's
// version comes after what was typed before it.

// storeRoomTypingBeforeLock stores the typing a page's open room holds
// before a change to its lock, as a restore stores it (page_history.go): the
// lock's version comes after what was typed before it, as Notion's version of
// a lock shows the lines typed a moment before. Held in the room past the
// lock, the typing reached no version until the next typing after it, and
// the lock's version showed those lines empty (2026-11-16).
func storeRoomTypingBeforeLock(event *core.RequestEvent, pageID string, lockChange bool) {
	if lockChange {
		storeRoomTypingAhead(event.Request.Context(), event.App, pageID)
	}
}

// storeRoomTypingBeforeRevert stores the typing a page's room took and hasn't
// stored yet before a revert of a connection's changes, as a restore's is
// (page_history.go): replacing the page's text over a room that still held
// someone's typing, the typing merged back after it. Stored typing is a newer
// version, which the revert goes ahead over (revertPageHistoryChanges).
func storeRoomTypingBeforeRevert(event *core.RequestEvent, pageID string) {
	storeRoomTypingAhead(event.Request.Context(), event.App, pageID)
}
