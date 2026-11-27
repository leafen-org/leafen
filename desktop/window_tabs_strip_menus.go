//go:build (darwin && !ios) || windows

package main

import (
	"context"
	"encoding/json"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The strip's menus, as Notion 7.44.1's tab bar draws them (its web
// client's "탭 작업" and "탭 그룹 작업" menus, measured in its Windows app
// 2026-11-24): a tab's right press opens its menu at the press, over the
// strip — 250 wide — with the link, a copy, pinning, "탭을 다음 위치로 이동"
// (the window's groups, checked for its own; "그룹에서 제거", "신규 그룹", "새
// 창"), then the closes and opening the last closed again, which the strip
// draws itself, grown over the window while it is open (tab_strip_menu.js,
// window_tabs_strip_room*.go); a group head's right press opens the group's
// menu under it — 220 wide, from the head's foot over the strip — its name,
// icon and color, "그룹 해제" and "그룹 닫기", which the strip draws the same
// way (tab_strip_group_menu.js) but for the icon picker, the shown page's
// over it (web/src/components/desktop/DesktopStripMenus.tsx), the strip
// leaving its place to it. Both say back what was chosen (the strip's
// "menu-pick" and "group-say", the page's WindowTabs.StripMenu).

// stripMenuEvent tells the shown page that a strip menu is open over it
// (kind "open"; its presses close the menu, as they reach Notion's menu
// over its page), that a group's icon picker is to stand over it ("icon"),
// or that all of it closed ("").
const stripMenuEvent = "leafen:strip-menu"

// stripMenu is a frame's strip's menu, a tab's or a group's, the page shown
// under it and whether that page's icon picker is open over it; from is the
// tab a group's menu was made from ("신규 그룹"), which takes the keys back
// as it closes (window_tabs_strip_keys.go).
type stripMenu struct {
	kind    string
	tab     *appTab
	group   *tabGroup
	from    *appTab
	page    *appTab
	picking bool
}

// stripRect is a place on the strip, from the strip's top left.
type stripRect struct {
	Left   int `json:"left"`
	Right  int `json:"right"`
	Top    int `json:"top"`
	Bottom int `json:"bottom"`
}

// stripBox is a menu's box on the strip as drawn, in its CSS pixels from
// its top left.
type stripBox struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// stripRoom is what a strip grown over its window keeps of it while a menu
// of it is open (window_tabs_strip_room*): the menus' places (rects) and
// the holes it leaves in them to the page's icon picker over a group's menu;
// through, that a press outside the menus and the strip goes on to the page
// (a tab's menu, as Notion's lets it); shadows, the menus' boxes as drawn.
type stripRoom struct {
	rects, holes []stripRect
	shadows      []stripBox
	through      bool
}

// menuGroup is a group as the menus show it.
type menuGroup struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Icon  string `json:"icon"`
}

func menuGroupOf(group *tabGroup) menuGroup {
	return menuGroup{ID: group.id, Name: group.name, Color: group.color, Icon: group.icon}
}

// openTabMenu has the strip open a tab's menu at its point, with what each
// item may do now (Notion's VirtualTabContextMenu), the strip grown over the
// window first; its first line is lit, opened by a press or a key.
func (t *windowTabs) openTabMenu(frame *tabFrame, tab *appTab, x, y int) {
	if frame.window == nil || frame.index(tab) < 0 {
		return
	}
	at := frame.index(tab)
	others, before, after := false, false, false
	for i, each := range frame.section.tabs {
		if i == at || each.pinned {
			continue
		}
		others = true
		before = before || i < at
		after = after || i > at
	}
	groups := make([]menuGroup, 0, len(frame.groups))
	for _, group := range frame.groups {
		groups = append(groups, menuGroupOf(group))
	}
	current := ""
	if tab.group != nil {
		current = tab.group.id
	}
	t.closeStripMenu(frame, keysStay)
	frame.menu = &stripMenu{kind: "tab", tab: tab}
	t.openStripMenu(frame, map[string]any{
		"kind": "tab", "x": x, "y": y, "tab": tab.id, "pinned": tab.pinned, "group": current, "groups": groups,
		"can": map[string]bool{"closeOthers": others, "closeBefore": before, "closeAfter": after,
			"reopen": len(t.closed) > 0, "newWindow": len(frame.section.tabs) > 1},
	})
}

// openGroupMenu has the strip open a group's menu under its head, the
// strip grown over the window first and given the keys for its name; its
// "그룹 닫기" is off while the group has every tab of the window. from is the
// tab it was made from, if one.
func (t *windowTabs) openGroupMenu(frame *tabFrame, group *tabGroup, head stripRect, from *appTab) {
	if frame.window == nil || !containsGroup(frame.groups, group) {
		return
	}
	t.closeStripMenu(frame, keysStay)
	frame.menu = &stripMenu{kind: "group", group: group, from: from}
	// The keys first: a Mac web view given them drops its page's focus,
	// which the menu then puts in its name.
	focusStrip(frame)
	t.openStripMenu(frame, map[string]any{
		"kind": "group", "head": head, "group": menuGroupOf(group),
		"canClose": len(frame.groupTabs(group)) < len(frame.section.tabs),
	})
	t.drawStrip(frame)
}

// openStripMenu grows the strip over the window and has it draw a menu,
// told the window's room in its CSS pixels.
func (t *windowTabs) openStripMenu(frame *tabFrame, menu map[string]any) {
	through := frame.menu.kind == "tab"
	growStrip(frame, true, stripRoom{through: through})
	width, height := frameContentSize(frame.window)
	zoom := t.zoomFactor()
	menu["rtl"], menu["width"], menu["height"] = t.look.RTL, float64(width)/zoom, float64(height)/zoom
	said, _ := json.Marshal(menu)
	frame.window.ExecJS("window.leafenStrip&&leafenStrip.openMenu(" + string(said) + ")")
	frame.menu.page = frame.active
	tellStripPage(frame.menu, map[string]any{"kind": "open", "through": through})
}

// tellStripPage tells the page under a strip menu what stands over it.
func tellStripPage(menu *stripMenu, said map[string]any) {
	if page := menu.page; page != nil && page.window != nil {
		said["window"] = page.window.Name()
		page.window.EmitEvent(stripMenuEvent, said)
	}
}

// openGroupPicker has the shown page open its icon picker for the group's
// menu under the menu's icon (at, from the strip's top left), with the keys.
func (t *windowTabs) openGroupPicker(frame *tabFrame, at stripRect) {
	menu := frame.menu
	if menu == nil || menu.kind != "group" || menu.page == nil || menu.page.window == nil {
		return
	}
	menu.picking = true
	// The keys first, for the picker's filter to keep them (openGroupMenu).
	focusTabView(frame, menu.page)
	tellStripPage(menu, map[string]any{"kind": "icon", "at": at, "strip": stripCSSHeight(), "rtl": t.look.RTL,
		"group": menuGroupOf(menu.group)})
}

// closeGroupPicker closes the page's icon picker over a group's menu, or
// takes that it closed itself; the menu's name takes the keys again.
func (t *windowTabs) closeGroupPicker(frame *tabFrame) {
	menu := frame.menu
	if menu == nil || !menu.picking {
		return
	}
	menu.picking = false
	tellStripPage(menu, map[string]any{"kind": "open"})
	focusStrip(frame)
	frame.window.ExecJS("window.leafenStrip&&leafenStrip.pickerAt(null)")
}

// closeStripMenu closes the frame's strip menu, and the page's picker over
// it; the strip goes back to its height, the card of the tab the pointer
// rests on shows again, and the keys go where keys says
// (window_tabs_strip_keys.go).
func (t *windowTabs) closeStripMenu(frame *tabFrame, keys stripKeys) {
	menu := frame.menu
	if menu == nil {
		return
	}
	frame.menu = nil
	tellStripPage(menu, map[string]any{"kind": ""})
	if frame.window != nil {
		// Where the pointer is, for the card of the tab it rests on
		// (tab_strip_hover.js): the strip, shrinking back, has lost it.
		pointer := []byte("null")
		if x, y, ok := stripPointer(frame); ok {
			pointer, _ = json.Marshal(map[string]float64{"x": x, "y": y})
		}
		frame.window.ExecJS("window.leafenStrip&&leafenStrip.closeMenu(" + string(pointer) + ")")
		growStrip(frame, false, stripRoom{})
		if keys == keysToPage && frame.active != nil {
			focusTabView(frame, frame.active)
		}
	}
	t.drawStrip(frame)
	if keys == keysToOpener {
		ringStrip(frame, openerRing(frame, menu))
	}
}

// stripMenuMessage is what a strip menu chose — the strip's, or what the
// page under it said (DesktopStripMenus.tsx): that it was pressed, its icon
// picker's pick, its place (Rect, from the page's top left) or that it
// closed; or that the page's history menu closed (Forward, the forward
// button's).
type stripMenuMessage struct {
	Do      string     `json:"do"`
	Tab     string     `json:"tab"`
	Group   string     `json:"group"`
	Name    string     `json:"name"`
	Color   string     `json:"color"`
	Icon    string     `json:"icon"`
	Rect    *stripRect `json:"rect"`
	Forward bool       `json:"forward"`
	// Keys says a pick was made by a key (Enter), not a press.
	Keys bool `json:"keys"`
}

// StripMenu takes what the page under a strip menu said.
func (*WindowTabs) StripMenu(ctx context.Context, said stripMenuMessage) {
	caller, _ := ctx.Value(application.WindowKey).(application.Window)
	if caller == nil {
		return
	}
	onMain(func() {
		frame, page := desktopTabs.frameOf(caller)
		if frame != nil && page != nil && page == frame.active && page.window != nil && page.window.ID() == caller.ID() {
			desktopTabs.stripMenuSaid(frame, said)
		}
	})
}

func (t *windowTabs) stripMenuSaid(frame *tabFrame, said stripMenuMessage) {
	tab := frame.tabByID(said.Tab)
	group := frame.groupByID(said.Group)
	switch said.Do {
	case "pressed":
		// The page under the menu was pressed (Windows, where it takes the
		// presses outside the menu, and the Mac's a tab's menu lets through):
		// the menu closes, as Notion's — a tab's for the press to go on in
		// the page, which keeps the keys; a group's having taken it.
		keys := keysToOpener
		if frame.menu != nil && frame.menu.kind == "tab" {
			keys = keysToPage
		}
		t.closeStripMenu(frame, keys)
		return
	case "historyClosed":
		t.historyClosed(frame, said.Forward)
		return
	case "picker":
		// The page's picker stands here over the menu: the strip leaves it the place.
		if menu := frame.menu; menu != nil && menu.picking && said.Rect != nil {
			top := stripCSSHeight()
			at, _ := json.Marshal(map[string]float64{"left": float64(said.Rect.Left), "right": float64(said.Rect.Right),
				"top": float64(said.Rect.Top) + top, "bottom": float64(said.Rect.Bottom) + top})
			frame.window.ExecJS("window.leafenStrip&&leafenStrip.pickerAt(" + string(at) + ")")
		}
		return
	case "pickerClosed":
		t.closeGroupPicker(frame)
		return
	case "rename":
		if group != nil {
			name := []rune(said.Name)
			group.name = string(name[:min(len(name), maxGroupName)])
			t.changed(frame)
		}
		return
	case "color":
		if group != nil && knownGroupColor(said.Color) {
			group.color = said.Color
			t.changed(frame)
		}
		return
	case "icon":
		if group != nil {
			group.icon = groupIconOf(said.Icon)
			t.changed(frame)
		}
		return
	}
	// A pick: the menu closes, the pick is done and the keys go where
	// Notion's go (window_tabs_strip_keys.go).
	menu := frame.menu
	var first *appTab
	if group != nil {
		if tabs := frame.groupTabs(group); len(tabs) > 0 {
			first = tabs[0]
		}
	}
	t.closeStripMenu(frame, keysStay)
	defer func() {
		if menu == nil || !containsFrame(t.frames, frame) {
			return
		}
		ringAfterPick(frame, menu, said.Do, first, said.Keys)
	}()
	switch said.Do {
	case "ungroup":
		if group != nil {
			t.ungroup(frame, group)
		}
	case "closeGroup":
		if group != nil {
			t.closeGroup(frame, group)
		}
	}
	if tab == nil {
		return
	}
	switch said.Do {
	case "copyLink":
		t.copyTabLink(frame, tab)
	case "duplicate":
		t.duplicate(frame, tab)
	case "pin":
		t.pin(frame, tab, !tab.pinned)
	case "moveToGroup":
		if group != nil {
			t.setTabsGroup(frame, []*appTab{tab}, group)
			t.changed(frame)
		}
	case "removeFromGroup":
		t.removeFromGroups(frame, []*appTab{tab})
		t.changed(frame)
	case "newGroup":
		made := t.newGroup(frame, []*appTab{tab})
		t.changed(frame)
		// Its menu opens under its head as the strip draws it, its name to
		// type, the tab giving it the keys back as it closes.
		id, _ := json.Marshal(made.id)
		from, _ := json.Marshal(tab.id)
		frame.window.ExecJS("window.leafenStrip&&leafenStrip.openGroupMenu(" + string(id) + "," + string(from) + ")")
	case "newWindow":
		t.moveTab(frame, tab, nil, frame.index(tab), nil)
	case "close":
		t.closeTabs(frame, []*appTab{tab})
	case "closeOthers":
		t.closeAround(frame, tab, "others")
	case "closeBefore":
		t.closeAround(frame, tab, "before")
	case "closeAfter":
		t.closeAround(frame, tab, "after")
	case "reopen":
		t.reopen()
	}
}

// copyTabLink copies the link of a tab's page: by its own page, or by the
// shown page for a tab whose page hasn't opened since the app started.
func (t *windowTabs) copyTabLink(frame *tabFrame, tab *appTab) {
	if tab.window != nil {
		tab.window.EmitEvent(copyPageLinkEvent, tab.window.Name())
	} else if shown := frame.active; shown != nil && shown.window != nil {
		shown.window.EmitEvent(copyTabLinkEvent, map[string]string{"window": shown.window.Name(), "path": tab.path, "title": tab.title})
	}
}
