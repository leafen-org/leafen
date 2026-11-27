package app

import "github.com/pocketbase/pocketbase/core"

// A database's automations live outside its page's graph, whose read only
// counts them (markAutomatedDatabases). Once one is made, changed, turned on
// or off, paused or deleted — on another device, by the API or an import —
// the page's open room is told the automations' revision when the change is
// committed, and the screens showing its databases read their automations
// again (web lib/automationRevisionEvents.ts): the toolbar's ⚡, the view
// settings' "자동화 N" and an open list follow at once, as Notion's open
// phone settings showed "자동화 1" and its open list "중단됨" within seconds
// of the change made on a desktop (seen 2026-11-18 on a Galaxy A90 at 411).
// Before, a phone that had the page open said none until it was reloaded,
// and its "자동화" opened a new one.
//
// Changes to automations are written one transaction at a time, so the
// revision taken as each is made grows in the order they are committed.
func announceDatabaseAutomationsAfterCommit(app core.App, pageID string) {
	if pageID == "" {
		return
	}
	announceRoomStateAfterCommit(app, pageID, "leafenAutomations", map[string]any{"revision": nextGraphReaderRevision()})
}
