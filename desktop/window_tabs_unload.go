//go:build (darwin && !ios) || windows

package main

import (
	"sort"
	"time"
)

// A tab long unused has its page put away, as Notion's app unloads tabs (its
// TabLifecycleManager, 7.42.3, whose threshold its server sets — ten hours on
// this Mac's Notion, 2026-11-10): every ten hours the app is awake, each tab
// not shown in any window, that has been unused for ten hours or more and is
// not one of the three last used, loses its page and keeps its place, name
// and icon in the strip; choosing it again opens its page anew. Leaving a
// group of tabs puts away its pages so at once, but for the three last used
// (window_tabs_spaces.go).

const (
	// tabUnloadAfter is how long a tab goes unused before its page is put
	// away, and how often the app looks.
	tabUnloadAfter = 10 * time.Hour
	// recentTabsKept is how many tabs last used keep their pages anyway.
	recentTabsKept = 3
)

// startUnloading looks for tabs long unused while the app runs; the timer
// stands still while the Mac sleeps, as Notion's awake-only interval.
func (t *windowTabs) startUnloading() {
	go func() {
		ticker := time.NewTicker(tabUnloadAfter)
		for range ticker.C {
			onMain(func() { t.unloadTabs(nil, tabUnloadAfter) })
		}
	}()
}

// unloadTabs puts away pages of tabs shown in no window: with among, those
// of the given tabs but the three used last of all the app's tabs; without,
// those unused so long, but the three used last of them.
func (t *windowTabs) unloadTabs(among []*appTab, unused time.Duration) {
	shown := map[*appTab]bool{}
	for _, frame := range t.frames {
		for _, tab := range frame.shownTabs() {
			shown[tab] = true
		}
	}
	tabs := t.allTabs()
	sort.SliceStable(tabs, func(i, j int) bool { return tabs[i].unusedSince.After(tabs[j].unusedSince) })
	var going []*appTab
	if among != nil {
		for index, tab := range tabs {
			if index >= recentTabsKept && !shown[tab] && tab.window != nil && containsTab(among, tab) {
				going = append(going, tab)
			}
		}
	} else {
		now := time.Now().Round(0)
		kept := 0
		for _, tab := range tabs {
			if shown[tab] || tab.window == nil || now.Sub(tab.unusedSince.Round(0)) <= unused {
				continue
			}
			if kept < recentTabsKept {
				kept++
				continue
			}
			going = append(going, tab)
		}
	}
	for _, tab := range going {
		t.dropTabWindow(tab)
	}
}
