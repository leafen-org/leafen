package app

import "github.com/pocketbase/pocketbase/core"

// seedYgoSnapshotUnlessStored keeps a room's seeded document as the page's
// text unless one was stored since the room found none: a room opens its
// document outside the transaction that keeps the seed, and a write between
// the two — a copy's top writing its text while the copy opened on a screen
// joined its room (2026-11-15: the copy's text came back empty in 2 of 5
// copies, NAS condition) — was replaced by the seed of a page without words.
// The stored one is the room's then (LoadDoc reads it after).
func seedYgoSnapshotUnlessStored(txApp core.App, room string, state []byte) error {
	if _, found, err := loadMaterializedYgoDocument(txApp, room); err != nil || found {
		return err
	}
	if err := replaceYgoSnapshot(txApp, room, state); err != nil {
		return err
	}
	return markSearchDirtyByPageID(txApp, room)
}
