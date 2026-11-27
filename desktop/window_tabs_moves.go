//go:build (darwin && !ios) || windows

package main

import "encoding/json"

// What the person does to tabs beyond opening, showing and closing them
// (window_tabs.go): open again what was closed, reorder, pin, duplicate,
// and take a tab to another window or a new one.

// remember keeps what was closed to open again (⌘⇧T), the newest last.
func (t *windowTabs) remember(gone closedTabs) {
	const kept = 50
	t.closed = append(t.closed, gone)
	if len(t.closed) > kept {
		t.closed = t.closed[len(t.closed)-kept:]
	}
}

// reopen opens what was closed last: a window with its tabs as it was, or
// tabs at their places in their window — the window in front when theirs has
// closed since. Tabs closed behind the shown one come back behind it, the
// shown tab and the keys staying; the tab shown as they closed shows again
// (Notion 7.44.1's restore of closed tabs, its activateTabId: measured in its
// Windows app 2026-11-24 by its tab menu and Ctrl+Shift+T, twice each).
func (t *windowTabs) reopen() {
	if len(t.closed) == 0 {
		return
	}
	last := t.closed[len(t.closed)-1]
	t.closed = t.closed[:len(t.closed)-1]
	if last.window != nil {
		t.newFrame(last.window, true)
		return
	}
	frame := last.frame
	if !containsFrame(t.frames, frame) {
		frame, _ = t.front()
	}
	if frame == nil {
		t.newFrame(&savedFrame{Tabs: last.tabs}, true)
		return
	}
	if shown := t.putBack(frame, last); shown != nil {
		t.selectTab(frame, shown)
	} else {
		t.changed(frame)
	}
	// A window behind comes to the front; one in front already is left as
	// it is, for its keys to stay where they are (on Windows, focusing it
	// again would hand them to its strip).
	if !frame.window.IsFocused() {
		frame.window.Show()
		frame.window.Focus()
	}
}

// putBack puts tabs closed together back in a frame at their places, into
// their groups — made again when they went with them (Notion's restore of
// closed tabs) — and says which of them to show: the one shown as they
// closed, or none, the frame's shown tab staying.
func (t *windowTabs) putBack(frame *tabFrame, last closedTabs) *appTab {
	var shown *appTab
	groups := map[string]*tabGroup{}
	for _, saved := range last.tabs {
		tab := t.tabFromSaved(saved)
		frame.insertAt(tab, saved.Index)
		if group := t.closedGroup(frame, groups, last.groups, saved.Group); group != nil && !tab.pinned {
			t.setTabsGroup(frame, []*appTab{tab}, group)
		}
		if saved.Active {
			shown = tab
		}
	}
	return shown
}

// closedGroup is the group a tab closed in goes back to: the window's group
// still there under that id, else one made again as it was.
func (t *windowTabs) closedGroup(frame *tabFrame, made map[string]*tabGroup, kept []savedGroup, id string) *tabGroup {
	if id == "" {
		return nil
	}
	if group := made[id]; group != nil {
		return group
	}
	if group := frame.groupByID(id); group != nil {
		return group
	}
	for _, saved := range kept {
		if saved.ID == id {
			group := t.groupFromSaved(saved)
			frame.groups = append(frame.groups, group)
			made[id] = group
			return group
		}
	}
	return nil
}

// dropTab moves a tab dropped in its strip to the place it was dropped at,
// among the tabs without it, into the group the place is in (Notion's
// moveTab): a tab dropped into a group is no longer pinned; a pinned tab
// stays among the pinned and an unpinned one past them.
func (t *windowTabs) dropTab(frame *tabFrame, tab *appTab, at int, group *tabGroup) {
	if frame.index(tab) < 0 {
		return
	}
	frame.remove(tab)
	if group != nil && tab.pinned {
		tab.pinned = false
		tellPinned(tab)
	}
	tab.group = group
	frame.insertAt(tab, at)
	t.dropEmptyGroups(frame)
	t.changed(frame)
}

// pin pins a tab or lets it go: pinned tabs stand first, narrow, by their
// icon, and in no group.
func (t *windowTabs) pin(frame *tabFrame, tab *appTab, pinned bool) {
	frame.remove(tab)
	tab.pinned = pinned
	if pinned {
		tab.group = nil
	}
	tellPinned(tab)
	at := 0
	for at < len(frame.section.tabs) && frame.section.tabs[at].pinned {
		at++
	}
	frame.insertAt(tab, at)
	t.dropEmptyGroups(frame)
	t.changed(frame)
}

// duplicate opens the tab's page again right after it, in its group.
func (t *windowTabs) duplicate(frame *tabFrame, tab *appTab) {
	copied := t.newTab(tab.path)
	copied.title, copied.icon, copied.pinned, copied.signIn = tab.title, tab.icon, tab.pinned, tab.signIn
	frame.insertAt(copied, frame.index(tab)+1)
	if tab.group != nil && !copied.pinned {
		t.setTabsGroup(frame, []*appTab{copied}, tab.group)
	}
	t.selectTab(frame, copied)
}

// moveTab takes a tab, page and all, to another frame at a place, or to a
// new window where the person let it go.
func (t *windowTabs) moveTab(from *tabFrame, tab *appTab, to *tabFrame, at int, point *[2]int) {
	if from == to || from.index(tab) < 0 {
		return
	}
	if len(from.section.tabs) == 1 && to == nil {
		return
	}
	wasActive := from.active == tab
	from.remove(tab)
	tab.opener, tab.group = nil, nil
	t.dropEmptyGroups(from)
	if len(from.section.tabs) == 0 {
		t.leaveFrame(from)
	} else if wasActive {
		t.selectTab(from, from.section.tabs[min(max(0, at), len(from.section.tabs)-1)])
	} else {
		t.changed(from)
	}
	if tab.window != nil {
		releaseTabView(tab.window)
		tab.host = nil
	}
	if to == nil {
		t.openFrame([]*appTab{tab}, tab, &savedFrame{At: point, From: from.window}, true)
		return
	}
	to.insertAt(tab, at)
	t.selectTab(to, tab)
	to.window.Show()
	to.window.Focus()
}

// dropOutside takes a tab let go out of its window's strip: into another
// window's strip under the pointer, at the place that strip finds there
// (tab_strip.js's take, which says "insert"), or else into a new window of
// its own there — a window's only tab only into another's strip.
func (t *windowTabs) dropOutside(from *tabFrame, tab *appTab, x, y int) {
	for _, to := range t.frames {
		if to == from || to.window == nil || !to.adopted {
			continue
		}
		if at, ok := stripPointIn(to.window, x, y); ok {
			carried, _ := json.Marshal(map[string]any{"id": tab.id, "from": from.window.Name(), "x": at})
			to.window.ExecJS("window.leafenStrip&&leafenStrip.take(" + string(carried) + ")")
			return
		}
	}
	t.moveTab(from, tab, nil, from.index(tab), &[2]int{x, y})
}

// nextShown is the tab a window shows once its shown tab closes with others:
// of those left, the one chosen last, as Notion 7.44.1's app does (measured
// in its Windows app: ⌃W on a tab went back to the tab shown before it, not
// to its neighbour); with none chosen since the app started, the next one
// along, or else the last.
func nextShown(frame *tabFrame, shut *appTab, closing []*appTab) *appTab {
	var next *appTab
	for _, tab := range frame.section.tabs {
		if !containsTab(closing, tab) && tab.shown > 0 && (next == nil || tab.shown > next.shown) {
			next = tab
		}
	}
	at := frame.index(shut)
	for i := at + 1; i < len(frame.section.tabs) && next == nil; i++ {
		if !containsTab(closing, frame.section.tabs[i]) {
			next = frame.section.tabs[i]
		}
	}
	for i := len(frame.section.tabs) - 1; i >= 0 && next == nil; i-- {
		if !containsTab(closing, frame.section.tabs[i]) {
			next = frame.section.tabs[i]
		}
	}
	return next
}

// moveGroupTo takes a group's tabs, pages and all, to a place in another
// frame's strip, or to a new window where the person let it go, as a group
// there (Notion's adopting of a group dragged out of its tab bar). A group
// holding all its window's tabs goes only into another window's strip.
func (t *windowTabs) moveGroupTo(from *tabFrame, group *tabGroup, to *tabFrame, at int, point *[2]int) {
	tabs := from.groupTabs(group)
	if from == to || len(tabs) == 0 || to == nil && len(tabs) == len(from.section.tabs) {
		return
	}
	shown := tabs[0]
	wasActive := containsTab(tabs, from.active)
	if wasActive {
		shown = from.active
		if len(tabs) < len(from.section.tabs) {
			t.selectTab(from, nextShown(from, from.active, tabs))
		}
	}
	for _, tab := range tabs {
		from.remove(tab)
		tab.opener = nil
		if tab.window != nil {
			releaseTabView(tab.window)
			tab.host = nil
		}
	}
	t.dropEmptyGroups(from)
	if len(from.section.tabs) == 0 {
		t.leaveFrame(from)
	} else {
		t.changed(from)
	}
	if to == nil {
		frame := t.openFrame(tabs, shown, &savedFrame{At: point, From: from.window}, true)
		frame.groups = append(frame.groups, group)
		t.changed(frame)
		return
	}
	rest := to.section.tabs
	place := pastGroup(rest, max(firstFree(rest), min(at, len(rest))))
	to.section.tabs = append(append(append([]*appTab{}, rest[:place]...), tabs...), rest[place:]...)
	to.groups = append(to.groups, group)
	t.selectTab(to, shown)
	to.window.Show()
	to.window.Focus()
}

// dropGroupOutside takes a group let go out of its window's strip: into
// another window's strip under the pointer, at the place that strip finds
// there (tab_strip.js's takeGroup, which says "insert-group"), or else into
// a new window of its own there.
func (t *windowTabs) dropGroupOutside(from *tabFrame, group *tabGroup, x, y int) {
	for _, to := range t.frames {
		if to == from || to.window == nil || !to.adopted {
			continue
		}
		if at, ok := stripPointIn(to.window, x, y); ok {
			carried, _ := json.Marshal(map[string]any{"id": group.id, "from": from.window.Name(), "x": at})
			to.window.ExecJS("window.leafenStrip&&leafenStrip.takeGroup(" + string(carried) + ")")
			return
		}
	}
	t.moveGroupTo(from, group, nil, 0, &[2]int{x, y})
}
