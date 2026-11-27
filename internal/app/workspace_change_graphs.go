package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A screen's sidebar draws the content of pages beside the open one — a
// database view section's rows and heading, a favorite database's views
// unfolded, a database menu's views (web sidebar/sidebarHolderGraph.ts) —
// whose rooms aren't open there, so a change to them made on another screen
// or device, through the API or by an agent wasn't heard: a view section
// showed its rows as first read until the address changed (2026-11-18). The
// screen says over its change socket which pages it draws so,
// {"graphs":[…]}, each one the person may read, and the socket tells it
// {"graphs":[…]} once a change to one of their graphs is committed
// (announceGraphReadersAfterCommit), at the next look (a quarter second), as
// the page list's changes reach the sidebar within about a second.

// workspaceChangeGraphsMax bounds the pages a screen follows so; its
// sidebar holds ten view sections a tab at most.
const workspaceChangeGraphsMax = 64

// workspaceChangeSocketReadLimit bounds what a screen may send its socket:
// the pages it draws, nothing more.
const workspaceChangeSocketReadLimit = 16 * 1024

// takeWatchedGraphsWord follows the pages a screen's word names, those the
// person may read, in place of the ones it named before; a word that names
// none isn't one.
func (watch *workspacePageListWatch) takeWatchedGraphsWord(app core.App, waiter *workspacePageListWaiter, message []byte) {
	word := struct {
		Graphs []string `json:"graphs"`
	}{}
	if err := json.Unmarshal(message, &word); err != nil || word.Graphs == nil {
		return
	}
	pages := map[string]struct{}{}
	for _, id := range word.Graphs {
		id = strings.TrimSpace(id)
		if _, seen := pages[id]; seen || id == "" || len(id) > 64 {
			continue
		}
		if len(pages) == workspaceChangeGraphsMax {
			break
		}
		if _, err := authorizePage(app, waiter.userID, id, permissionWorkspaceRead); err != nil {
			continue
		}
		pages[id] = struct{}{}
	}
	watch.mu.Lock()
	waiter.watched = pages
	watch.mu.Unlock()
}

// noteWatchedGraphChange marks, on every server watch, the waits of the
// screens drawing a page's content in their sidebars, once a change to its
// graph is committed; the next look ends them.
func noteWatchedGraphChange(pageID string) {
	workspacePageListWatches.Range(func(_, watch any) bool {
		w := watch.(*workspacePageListWatch)
		w.mu.Lock()
		for waiter := range w.waiters {
			if _, watched := waiter.watched[pageID]; !watched {
				continue
			}
			if waiter.graphs == nil {
				waiter.graphs = map[string]struct{}{}
			}
			waiter.graphs[pageID] = struct{}{}
		}
		w.mu.Unlock()
		return true
	})
}

// changedGraphs are the pages a waiter heard changed, in order, which it
// then forgets.
func (waiter *workspacePageListWaiter) changedGraphs() []string {
	if len(waiter.graphs) == 0 {
		return nil
	}
	pages := make([]string, 0, len(waiter.graphs))
	for page := range waiter.graphs {
		pages = append(pages, page)
	}
	slices.Sort(pages)
	waiter.graphs = nil
	return pages
}
