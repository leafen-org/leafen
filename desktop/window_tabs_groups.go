//go:build (darwin && !ios) || windows

package main

import (
	"strconv"
	"strings"
)

// Groups of tabs in a window's strip, as Notion 7.44.1's app's tab groups
// (its web client's virtual tab bar and its tab and group stores, read
// 2026-11-24 from the app's cache and measured in its Windows app): a group
// is a run of the window's tabs side by side behind a colored ground, led by
// its head — an icon, a name, a short line — and closed by a chevron that
// folds it to its head, the shown tab staying out while it is folded. A tab
// goes into a group from its menu ("탭을 다음 위치로 이동": a group, "신규 그룹")
// or by being dropped among the group's tabs; a tab opened from a grouped tab
// joins its group. The head's press folds or unfolds the group, its right
// press opens the group's menu — its name, icon and color, "그룹 해제" and
// "그룹 닫기" — which the shown page draws (window_tabs_strip_menus.go). Pinned
// tabs are never in a group, and a group with no tab left is gone. Groups
// belong to their window and go with their tabs.

// tabGroup is a group of a window's tabs: its name ("" untitled), Notion's
// color for it, its icon (an emoji, or "" for Notion's folder) and whether
// it is folded.
type tabGroup struct {
	id        string
	name      string
	color     string
	icon      string
	collapsed bool
}

// groupColors are Notion's colors for a group, in the order its color menu
// shows them.
var groupColors = []string{"default", "gray", "brown", "yellow", "orange", "green", "blue", "purple", "pink", "red"}

// newGroupColors are the colors a new group takes in turn: the first no
// other group of the window has, else the next round (Notion's createGroup).
var newGroupColors = []string{"default", "blue", "green", "orange", "purple", "pink", "yellow", "red", "brown", "gray"}

// The longest name a group keeps, Notion's input's.
const maxGroupName = 80

func knownGroupColor(color string) bool {
	for _, each := range groupColors {
		if each == color {
			return true
		}
	}
	return false
}

func (frame *tabFrame) groupByID(id string) *tabGroup {
	for _, group := range frame.groups {
		if group.id == id {
			return group
		}
	}
	return nil
}

// groupTabs are the window's tabs in a group, in their order.
func (frame *tabFrame) groupTabs(group *tabGroup) []*appTab {
	var tabs []*appTab
	for _, tab := range frame.section.tabs {
		if tab.group == group {
			tabs = append(tabs, tab)
		}
	}
	return tabs
}

// heldGroups are the groups some tab of the window is in.
func (frame *tabFrame) heldGroups() map[*tabGroup]bool {
	held := map[*tabGroup]bool{}
	for _, tab := range frame.section.tabs {
		if tab.group != nil {
			held[tab.group] = true
		}
	}
	return held
}

// newGroup makes a group of the window's given tabs, in the next color, and
// gives it.
func (t *windowTabs) newGroup(frame *tabFrame, tabs []*appTab) *tabGroup {
	held := frame.heldGroups()
	used := map[string]bool{}
	for group := range held {
		used[group.color] = true
	}
	color := newGroupColors[len(held)%len(newGroupColors)]
	for _, each := range newGroupColors {
		if !used[each] {
			color = each
			break
		}
	}
	t.seq++
	group := &tabGroup{id: "group-" + strconv.Itoa(t.seq), color: color}
	frame.groups = append(frame.groups, group)
	t.setTabsGroup(frame, tabs, group)
	return group
}

// setTabsGroup puts tabs into a group, unpinned, as Notion's setTabsGroup:
// with the group's run they join it — those before it at its start, those
// after it at its end; into a group with no tab yet they go together where
// the first of them stood, past any other group they would split.
func (t *windowTabs) setTabsGroup(frame *tabFrame, tabs []*appTab, group *tabGroup) {
	for _, tab := range tabs {
		if tab.pinned && frame.index(tab) >= 0 {
			tab.pinned = false
			tellPinned(tab)
		}
	}
	all := frame.section.tabs
	moving := func(tab *appTab) bool { return containsTab(tabs, tab) && !tab.pinned && tab.group != group }
	first := -1
	var rest, before, after []*appTab
	seen := false
	for i, tab := range all {
		if tab.group == group {
			seen = true
		}
		if moving(tab) {
			if first < 0 {
				first = i
			}
			if seen {
				after = append(after, tab)
			} else {
				before = append(before, tab)
			}
			continue
		}
		rest = append(rest, tab)
	}
	if first < 0 {
		t.dropEmptyGroups(frame)
		return
	}
	for _, tab := range append(append([]*appTab{}, before...), after...) {
		tab.group = group
	}
	start, end := -1, -1
	for i, tab := range rest {
		if tab.group == group {
			if start < 0 {
				start = i
			}
			end = i
		}
	}
	var order []*appTab
	if start < 0 {
		at := pastGroup(rest, first)
		order = append(order, rest[:at]...)
		order = append(order, before...)
		order = append(order, after...)
		order = append(order, rest[at:]...)
	} else {
		order = append(order, rest[:start]...)
		order = append(order, before...)
		order = append(order, rest[start:end+1]...)
		order = append(order, after...)
		order = append(order, rest[end+1:]...)
	}
	frame.section.tabs = order
	t.dropEmptyGroups(frame)
}

// pastGroup is a place in the tabs moved past the end of the group it would
// fall inside, so that no run of a group is split (Notion's indexAfterGroup).
func pastGroup(tabs []*appTab, at int) int {
	at = max(0, min(at, len(tabs)))
	if at == 0 {
		return 0
	}
	group := tabs[at-1].group
	for group != nil && at < len(tabs) && tabs[at].group == group {
		at++
	}
	return at
}

// removeFromGroups takes tabs out of their groups: each goes just past the
// rest of its group (Notion's removeTabsFromGroups).
func (t *windowTabs) removeFromGroups(frame *tabFrame, tabs []*appTab) {
	for i := len(frame.section.tabs) - 1; i >= 0; i-- {
		tab := frame.section.tabs[i]
		if !containsTab(tabs, tab) || tab.group == nil {
			continue
		}
		group := tab.group
		rest := append(append([]*appTab{}, frame.section.tabs[:i]...), frame.section.tabs[i+1:]...)
		at := i
		for j := len(rest) - 1; j >= 0; j-- {
			if rest[j].group == group {
				at = max(at, j+1)
				break
			}
		}
		tab.group = nil
		frame.section.tabs = append(rest[:at], append([]*appTab{tab}, rest[at:]...)...)
	}
	t.dropEmptyGroups(frame)
}

// ungroup takes every tab out of a group where it stands, and the group is
// gone (Notion's "그룹 해제").
func (t *windowTabs) ungroup(frame *tabFrame, group *tabGroup) {
	for _, tab := range frame.section.tabs {
		if tab.group == group {
			tab.group = nil
		}
	}
	t.dropEmptyGroups(frame)
	t.changed(frame)
}

// closeGroup closes a group's tabs, kept to open again with it — not when
// they are all the window has (Notion's "그룹 닫기" is off then).
func (t *windowTabs) closeGroup(frame *tabFrame, group *tabGroup) {
	tabs := frame.groupTabs(group)
	if len(tabs) == 0 || len(tabs) >= len(frame.section.tabs) {
		return
	}
	t.closeTabs(frame, tabs)
}

// foldGroup folds a group to its head or opens it again; a folded group's
// shown tab stays out (Notion's setGroupCollapsed).
func (t *windowTabs) foldGroup(frame *tabFrame, group *tabGroup, folded bool) {
	group.collapsed = folded
	t.changed(frame)
}

// moveGroup moves a group's whole run to a place among the others, past the
// pinned tabs and outside any other group (Notion's moveGroup).
func (t *windowTabs) moveGroup(frame *tabFrame, group *tabGroup, to int) {
	var run, rest []*appTab
	for _, tab := range frame.section.tabs {
		if tab.group == group {
			run = append(run, tab)
		} else {
			rest = append(rest, tab)
		}
	}
	if len(run) == 0 {
		return
	}
	at := pastGroup(rest, max(firstFree(rest), min(to, len(rest))))
	frame.section.tabs = append(append(append([]*appTab{}, rest[:at]...), run...), rest[at:]...)
	t.changed(frame)
}

// firstFree is the place of the first tab not pinned, or the end.
func firstFree(tabs []*appTab) int {
	for i, tab := range tabs {
		if !tab.pinned {
			return i
		}
	}
	return len(tabs)
}

// takeNeighbours gives a tab just put at a place the group its place says,
// as Notion's insertion: between two tabs of one group it is in that group;
// beside none of its own group's tabs, while others are left, it is in none;
// pinned, never.
func (frame *tabFrame) takeNeighbours(at int) {
	tabs := frame.section.tabs
	if at < 0 || at >= len(tabs) {
		return
	}
	tab := tabs[at]
	if tab.pinned {
		tab.group = nil
		return
	}
	var before, after *tabGroup
	if at > 0 {
		before = tabs[at-1].group
	}
	if at+1 < len(tabs) {
		after = tabs[at+1].group
	}
	if before != nil && before == after {
		tab.group = before
		return
	}
	if own := tab.group; own != nil && before != own && after != own {
		for i, other := range tabs {
			if i != at && other.group == own {
				tab.group = nil
				return
			}
		}
	}
}

// joinOpenersGroup puts a tab opened from a grouped tab into that group; one
// opened behind into a folded group unfolds it (Notion's
// addOpenedTabToOpenerGroup).
func (t *windowTabs) joinOpenersGroup(frame *tabFrame, tab *appTab, behind bool) {
	if tab.opener == nil || tab.opener.group == nil || frame.index(tab.opener) < 0 {
		return
	}
	group := tab.opener.group
	t.setTabsGroup(frame, []*appTab{tab}, group)
	if behind && group.collapsed {
		group.collapsed = false
	}
}

// dropEmptyGroups forgets the window's groups no tab is in, and keeps each
// group's tabs one run: a tab of a group found apart from its run is in
// none (Notion's normalizeTabGroups).
func (t *windowTabs) dropEmptyGroups(frame *tabFrame) {
	ended := map[*tabGroup]bool{}
	var last *tabGroup
	for _, tab := range frame.section.tabs {
		if tab.pinned || tab.group != nil && (ended[tab.group] || !containsGroup(frame.groups, tab.group)) {
			tab.group = nil
		}
		if last != nil && tab.group != last {
			ended[last] = true
		}
		last = tab.group
	}
	held := frame.heldGroups()
	kept := frame.groups[:0]
	for _, group := range frame.groups {
		if held[group] {
			kept = append(kept, group)
		}
	}
	frame.groups = kept
}

func containsGroup(groups []*tabGroup, group *tabGroup) bool {
	for _, each := range groups {
		if each == group {
			return true
		}
	}
	return false
}

// groupTitle is a group's name, or Notion's words for an untitled one.
func groupTitle(group *tabGroup) string {
	if name := strings.TrimSpace(group.name); name != "" {
		return name
	}
	return tabWords()["untitledGroup"]
}

// savedGroup is a group as kept with its window, or with tabs closed to open
// again.
type savedGroup struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Color     string `json:"color,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Collapsed bool   `json:"collapsed,omitempty"`
}

func savedGroupOf(group *tabGroup) savedGroup {
	return savedGroup{ID: group.id, Name: group.name, Color: group.color, Icon: group.icon, Collapsed: group.collapsed}
}

// groupFromSaved is a group as kept, brought back under a new id.
func (t *windowTabs) groupFromSaved(saved savedGroup) *tabGroup {
	t.seq++
	color := saved.Color
	if !knownGroupColor(color) {
		color = "default"
	}
	name := saved.Name
	if len([]rune(name)) > maxGroupName {
		name = string([]rune(name)[:maxGroupName])
	}
	return &tabGroup{id: "group-" + strconv.Itoa(t.seq), name: name, color: color, icon: groupIconOf(saved.Icon), collapsed: saved.Collapsed}
}

// groupIconOf is an icon a group may take: an emoji, or one of the app's
// icons as its picture (an SVG data address, drawn in the group's color by
// its shape alone), or none — Notion's folder.
func groupIconOf(icon string) string {
	icon = strings.TrimSpace(icon)
	if strings.HasPrefix(icon, groupIconPicture) {
		if len(icon) > maxGroupIconPicture || strings.ContainsAny(icon, "\"\\\n\r") {
			return ""
		}
		return icon
	}
	if icon == "" || len(icon) > 32 || strings.ContainsAny(icon, "<>\"'\\/:") {
		return ""
	}
	return icon
}

// A group's picture icon is an SVG data address up to this long.
const (
	groupIconPicture    = "data:image/svg+xml,"
	maxGroupIconPicture = 16 << 10
)

// regroup puts tabs brought back into the groups they were kept in, made
// anew in the window (or found there already, by their kept id), and keeps
// each group one run.
func (t *windowTabs) regroup(frame *tabFrame, tabs []*appTab, kept []savedTab, groups []savedGroup) {
	made := map[string]*tabGroup{}
	for i, tab := range tabs {
		if i >= len(kept) || kept[i].Group == "" || tab.pinned {
			continue
		}
		id := kept[i].Group
		group := made[id]
		if group == nil {
			for _, saved := range groups {
				if saved.ID == id {
					group = t.groupFromSaved(saved)
				}
			}
			if group == nil {
				continue
			}
			made[id] = group
			frame.groups = append(frame.groups, group)
		}
		tab.group = group
	}
	t.dropEmptyGroups(frame)
}

// The tab spaces earlier versions of the app kept (Notion 7.42.3's, shared
// by all windows) come back as groups: each space's tabs at the end of the
// window that showed it, else of the first window, under its name and icon.

// savedSpace is a tab space as kept by an earlier version.
type savedSpace struct {
	ID    string     `json:"id"`
	Title string     `json:"title"`
	Icon  string     `json:"icon,omitempty"`
	Tabs  []savedTab `json:"tabs"`
}

// spacesAsGroups moves kept tab spaces into the kept windows as groups.
func spacesAsGroups(frames []*savedFrame, spaces []*savedSpace) []*savedFrame {
	if len(spaces) == 0 {
		return frames
	}
	if len(frames) == 0 {
		frames = []*savedFrame{{}}
	}
	for index, space := range spaces {
		if space == nil || len(space.Tabs) == 0 {
			continue
		}
		to := frames[0]
		for _, frame := range frames {
			if frame.Section != "" && frame.Section == space.ID {
				to = frame
				break
			}
		}
		id := "space-" + strconv.Itoa(index)
		color := newGroupColors[len(to.Groups)%len(newGroupColors)]
		to.Groups = append(to.Groups, savedGroup{ID: id, Name: strings.TrimSpace(space.Title), Color: color, Icon: space.Icon})
		// The window showing the space shows the tab it last showed there.
		shown, showing := to.Shown[space.ID]
		showing = showing && to.Section == space.ID
		if showing {
			for i := range to.Tabs {
				to.Tabs[i].Active = false
			}
		}
		for i, tab := range space.Tabs {
			tab.Group, tab.Pinned, tab.Active = id, false, showing && i == shown
			to.Tabs = append(to.Tabs, tab)
		}
	}
	for _, frame := range frames {
		frame.Section, frame.Shown = "", nil
	}
	return frames
}
