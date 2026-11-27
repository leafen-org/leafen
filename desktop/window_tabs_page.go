//go:build (darwin && !ios) || windows

package main

import (
	"strings"
)

// What the shown tab's page draws for the strip, as Notion's app has its
// page draw it (7.42.3 and its web client, measured 2026-11-10): the back
// and forward buttons' history menu, the card of a tab the pointer rests on,
// and whether the tab is pinned, which sends the pages it goes to into new
// tabs. The page draws these under the strip (web/src/lib/desktopTabPage.ts);
// where its web view lists no history of its own (WebView2), the page lists
// what it went through itself.

const (
	// tabHistoryEvent gives the shown tab's page its history one way, to list.
	tabHistoryEvent = "leafen:tab-history"
	// tabPreviewEvent shows the card of the tab the pointer rests on, or none.
	tabPreviewEvent = "leafen:tab-preview"
	// tabPinnedEvent tells a tab's page whether its tab is pinned.
	tabPinnedEvent = "leafen:tab-pinned"
)

// historyEntry is one entry of a tab's page's history as its web view lists
// it (tabHistoryList): its address and the title its page had there.
type historyEntry struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// historyItem is a history entry as the page lists it: how far from the
// shown one, its page's path in the app and the title it had.
type historyItem struct {
	Offset int    `json:"offset"`
	Path   string `json:"path"`
	Title  string `json:"title"`
}

// A tab's history keeps its last 20 entries, as Notion's app's does (its
// web client's electron router store drops the oldest past 20, read
// 2026-11-14): back goes at most 19 entries, however many its web view
// keeps. Its page tells how far back it goes (web/src/lib/desktopTabPage.ts),
// and gates its own back the same way.

// tabCanGo says whether the tab's page can go back and forward: back no
// further than its history goes.
func tabCanGo(tab *appTab) (bool, bool) {
	back, forward := tabHistory(tab.window)
	return back && tab.back != 0, forward
}

// goTab goes back or forward in the tab's page, back no further than its
// history goes.
func goTab(tab *appTab, forward bool) {
	if tab == nil || tab.window == nil || (!forward && tab.back == 0) {
		return
	}
	if !forward && tab.back > 0 {
		// A press again before its page tells goes no further.
		tab.back--
	}
	goTabHistory(tab.window, forward)
}

// pageHistory is the shown tab's page's history one way, as its page lists
// it: back from the newest entry, or forward from the nearest, back no
// further than its history goes. An entry not one of the app's pages is
// left out.
func pageHistory(tab *appTab, forward bool) []historyItem {
	if tab == nil || tab.window == nil {
		return nil
	}
	var items []historyItem
	for index, entry := range tabHistoryList(tab.window, forward) {
		if !forward && tab.back >= 0 && index >= tab.back {
			break
		}
		kind, parsed := newWindowKindOf(entry.URL)
		if kind != newWindowAppPage {
			continue
		}
		offset := -(index + 1)
		if forward {
			offset = index + 1
		}
		items = append(items, historyItem{Offset: offset, Path: appPagePath(parsed), Title: entry.Title})
	}
	return items
}

// showHistoryMenu has the shown tab's page list its history one way, as the
// strip's back or forward button's right or middle press asks (Notion's
// tabHistoryPopup: back entries newest first, forward ones nearest first),
// the page taking the keys for it from the strip pressed: an Escape closes
// it, the button then taking them back (window_tabs_strip_keys.go).
func (t *windowTabs) showHistoryMenu(frame *tabFrame, forward bool) {
	tab := frame.active
	if tab != nil && tab.window != nil {
		focusTabView(frame, tab)
	}
	if pageKeepsHistory {
		// The page lists its own history (web/src/lib/desktopTabPage.ts).
		if tab != nil && tab.window != nil {
			tab.window.EmitEvent(tabHistoryEvent, map[string]any{"window": tab.window.Name(), "forward": forward, "own": true})
		}
		return
	}
	items := pageHistory(tab, forward)
	if len(items) == 0 {
		return
	}
	tab.window.EmitEvent(tabHistoryEvent, map[string]any{"window": tab.window.Name(), "forward": forward, "entries": items})
}

// openHistoryInNewTab opens the page a ⌘-pressed back or forward button
// would go to in a new tab behind the shown one, after the tabs opened from
// it, as Notion's.
func (t *windowTabs) openHistoryInNewTab(frame *tabFrame, forward bool) {
	tab := frame.active
	if pageKeepsHistory {
		// The page knows its history and opens its nearest entry that way.
		if tab != nil && tab.window != nil {
			tab.window.EmitEvent(tabHistoryEvent, map[string]any{"window": tab.window.Name(), "forward": forward, "own": true, "open": true})
		}
		return
	}
	if items := pageHistory(tab, forward); len(items) > 0 {
		t.openTab(frame, items[0].Path, tab, false, false)
	}
}

// previewTab has the shown tab's page show the card of the tab the pointer
// rests on — not the shown one — under it, or none (Notion's
// set-hovered-tab). The tab's place is the strip's, whose left and right are
// the page's.
func (t *windowTabs) previewTab(frame *tabFrame, tab *appTab, left, right int) {
	shown := frame.active
	if shown == nil || shown.window == nil {
		return
	}
	data := map[string]any{"window": shown.window.Name(), "path": ""}
	if tab != nil && tab != shown && strings.HasPrefix(tab.path, "/") {
		data = map[string]any{"window": shown.window.Name(), "path": tab.path, "title": tab.title, "icon": tab.icon,
			"left": left, "right": right}
	}
	shown.window.EmitEvent(tabPreviewEvent, data)
}

// tellPinned tells a tab's page whether its tab is pinned, once it has a page.
func tellPinned(tab *appTab) {
	if tab.window != nil {
		tab.window.EmitEvent(tabPinnedEvent, map[string]any{"window": tab.window.Name(), "pinned": tab.pinned})
	}
}
