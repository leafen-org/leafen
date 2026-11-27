//go:build (darwin && !ios) || windows

package main

// A window's tabs as kept for the app's next start (tabs.json), as
// Notion's app keeps them: each tab's page, name, icon, pin and group, and
// the window's groups (window_tabs_groups.go).

// A tab keeps the sign-in its page shows — the server and the account on
// it, as its page tells it (web/src/lib/tabSignIn.ts) — as each tab of
// Notion's app keeps its account (7.42.3, read 2026-11-14: its
// appStoreState.currentUserId): the tab opens with it again at the next
// start (notionRestoreUserId), and when it is opened again after it was
// closed or its page was put away. A new tab opens in the sign-in of the tab
// it is opened from, else of the tab in front (Notion's new tab with
// focusedActiveTabCurrentUserId). The page asks for it as it first starts
// (WindowTabs.SignIn); after that its own tab keeps it through the page
// starting over.

// A sign-in as a page tells it is at most this long.
const maxSignIn = 1024

// savedTabOf is a tab as kept, at its place, shown or not.
func savedTabOf(tab *appTab, index int, shown bool) savedTab {
	icon := tab.icon
	if len(icon) > maxKeptIcon {
		icon = ""
	}
	kept := savedTab{Path: tab.path, Title: tab.title, Icon: icon, Pinned: tab.pinned, SignIn: tab.signIn,
		Active: shown, Index: index}
	if tab.group != nil {
		kept.Group = tab.group.id
	}
	return kept
}

// tabFromSaved is a tab as kept, brought back: in the sign-in it had, or a
// new tab's when it kept none.
func (t *windowTabs) tabFromSaved(saved savedTab) *appTab {
	tab := t.newTab(saved.Path)
	tab.title, tab.icon, tab.pinned = saved.Title, saved.Icon, saved.Pinned
	if saved.SignIn != "" && len(saved.SignIn) <= maxSignIn {
		tab.signIn = saved.SignIn
	}
	return tab
}

// savedTabsOf are tabs as kept, the shown one marked.
func savedTabsOf(tabs []*appTab, shown *appTab) []savedTab {
	var kept []savedTab
	for index, tab := range tabs {
		kept = append(kept, savedTabOf(tab, index, tab == shown))
	}
	return kept
}

// savedGroupsOf are the groups the tabs are in, as kept.
func savedGroupsOf(tabs []*appTab) []savedGroup {
	var kept []savedGroup
	seen := map[*tabGroup]bool{}
	for _, tab := range tabs {
		if tab.group != nil && !seen[tab.group] {
			seen[tab.group] = true
			kept = append(kept, savedGroupOf(tab.group))
		}
	}
	return kept
}
