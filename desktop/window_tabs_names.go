//go:build (darwin && !ios) || windows

package main

import (
	"net/url"
	"strings"
)

// A page's name and icon on every tab that shows it, as Notion's app has
// them (seen 2026-11-12): a page renamed in one tab is renamed at once on
// every other tab of it — in this window or another, opened behind and never
// shown, or brought back from the last time. Each tab's page tells its own
// name (Report), but a page behind is hidden and reads the workspace's
// changes only once it shows (web/src/components/product/useWorkspaceChangeFollow.ts),
// so the app gives a page's new name to its other tabs itself.

// tabSaid is what a tab's page last told of itself: the page it shows, by
// its id, and that page's name and icon.
type tabSaid struct {
	page  string
	title string
	icon  string
}

// pageOfPath is the page a tab's address shows, by its id: "" for a screen
// that isn't one page alone — home, the library, a page with another open
// over it (?p=).
func pageOfPath(path string) string {
	parsed, err := url.Parse(path)
	if err != nil || parsed.Query().Has("p") {
		return ""
	}
	id, ok := strings.CutPrefix(parsed.Path, "/p/")
	id = strings.TrimSuffix(id, "/")
	if !ok || id == "" || strings.Contains(id, "/") {
		return ""
	}
	return id
}

// takeSaid takes what a tab's page tells of its name and icon: what changed
// in what it says — another screen, or its page's new name or icon — becomes
// the tab's, and every other tab's on that page when the page itself said it
// (page, its id: not the app's own name on a screen with none); it says
// whether any other tab changed. What it says again unchanged leaves the tab
// as it is, which a newer name from another tab may hold. A page starting or
// a screen still opening names nothing: the tab keeps the name and icon it
// has — none on a new tab, which the strip draws as an empty skeleton, or
// the one it was brought back with — as Notion's tab does till its page
// names it (2026-11-13: tabs read "Leafen" while their page loaded).
func (t *windowTabs) takeSaid(tab *appTab, title, icon, page string) bool {
	said := tabSaid{page: pageOfPath(tab.path), title: title, icon: icon}
	if title == "" {
		// What it says once named is taken as new.
		tab.said = tabSaid{}
		return false
	}
	before := tab.said
	tab.said = said
	moved := said.page != before.page || said.page == ""
	named := moved || said.title != before.title
	marked := moved || said.icon != before.icon
	if named {
		tab.title = title
	}
	if marked {
		tab.icon = icon
	}
	if said.page == "" || page != said.page || title == "" || (!named && !marked) {
		return false
	}
	shared := false
	for _, other := range t.allTabs() {
		if other == tab || pageOfPath(other.path) != said.page {
			continue
		}
		if named && other.title != title {
			other.title, shared = title, true
		}
		if marked && other.icon != icon {
			other.icon, shared = icon, true
		}
	}
	return shared
}

// titleWindow names a window after the tab it shows, as Notion's app does:
// the window's title is that tab's name (7.43.0's tab bar sets the window's
// title to its shown tab's), so a page renamed in another window renames
// this one's title with its tabs (2026-11-13: it kept the old name till its
// page read the change). A tab with no name yet leaves the title as it is.
func titleWindow(frame *tabFrame) {
	if tab := frame.active; tab != nil && tab.title != "" && frame.window != nil {
		frame.window.SetTitle(tab.title)
	}
}
