//go:build (darwin && !ios) || windows

package main

import (
	"encoding/json"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// stripSidebar is the sidebar of the page shown: whether it has one, open or
// folded and how wide, for the strip's part over it (Notion's tab bar takes
// the sidebar's width and has no sidebar button where there is no sidebar).
type stripSidebar struct {
	Shown    bool `json:"shown"`
	Expanded bool `json:"expanded"`
	Width    int  `json:"width"`
	// Inbox and New are the sidebar head's buttons the strip draws over it,
	// as Notion's tab bar with the switcher at the sidebar's foot: "" when the
	// page has none, else "on", "open" or "off" (and New "ask", a guest's).
	Inbox string `json:"inbox"`
	New   string `json:"new"`
}

// stripLook is the pages' colors for the strip — their sidebar, page, border,
// text and icon colors, and their menus' look (web/src/lib/desktopTabs.ts) —
// their direction and the tab menu's words in their language.
type stripLook struct {
	Colors map[string]string `json:"colors"`
	Words  map[string]string `json:"words,omitempty"`
	Dark   bool              `json:"dark"`
	RTL    bool              `json:"rtl"`
	// Groups are each group color's colors in the strip
	// (web/src/lib/tabGroupColors.ts).
	Groups map[string]map[string]string `json:"groups,omitempty"`
}

type stripTab struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Icon   string `json:"icon"`
	Pinned bool   `json:"pinned"`
	Active bool   `json:"active"`
	// Group is the id of its group, if one.
	Group string `json:"group,omitempty"`
}

// stripGroup is a group as the strip draws it (window_tabs_groups.go): its
// menu open draws its head pressed.
type stripGroup struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Icon      string `json:"icon"`
	Collapsed bool   `json:"collapsed"`
	Menu      bool   `json:"menu"`
}

// tabWords are the strip's and the tab menu's words in the window's
// language, as Notion's app says them (its ko-KR and en-US messages).
func tabWords() map[string]string {
	desktopBackground.mu.Lock()
	korean := strings.HasPrefix(desktopBackground.language, "ko")
	desktopBackground.mu.Unlock()
	words := map[string][2]string{
		"newTab":       {"New Tab", "새 탭"},
		"back":         {"Back", "뒤로가기"},
		"forward":      {"Forward", "앞으로 가기"},
		"openSidebar":  {"Open Sidebar", "사이드바 열기"},
		"closeSidebar": {"Close Sidebar", "사이드바 닫기"},
		"closeTab":     {"Close Tab, {title}", "{title} 탭 닫기"},
		"untitled":     {"Tab", "탭"},
		"sidebarKey":   {"Cmd+\\", "Cmd+\\"},
		"appMenu":      {"App Menu", "앱 메뉴"},
		"copyLink":     {"Copy Link", "링크 복사"},
		"duplicate":    {"Duplicate Tab", "탭 복제"},
		"pin":          {"Pin Tab", "탭 고정"},
		"unpin":        {"Unpin Tab", "탭 고정 해제"},
		"reopen":       {"Reopen Last Closed Tab", "마지막으로 닫은 탭 다시 열기"},
		"close":        {"Close Tab", "탭 닫기"},
		"closeOthers":  {"Close Other Tabs", "다른 탭 닫기"},
		"closeLeft":    {"Close Tabs to the Left", "왼쪽 탭 닫기"},
		"closeRight":   {"Close Tabs to the Right", "오른쪽 탭 닫기"},
		// The sidebar head's buttons on the strip (Notion's desktop.tabBar).
		"inbox":   {"Inbox", "수신함"},
		"compose": {"New", "신규"},
		// Groups of tabs (window_tabs_groups.go), in Notion's words.
		"untitledGroup": {"Untitled group", "이름 없는 그룹"},
		"groupHead":     {"{name}, {count} tabs", "{name}, {count}개의 탭"},
		"groupHeadOne":  {"{name}, 1 tab", "{name}, 1개의 탭"},
		"expandGroup":   {"Expand {name}", "{name} 펼치기"},
		"collapseGroup": {"Collapse {name}", "{name} 접기"},
	}
	if stripPlatform == "windows" {
		words["sidebarKey"] = [2]string{"Ctrl+\\", "Ctrl+\\"}
	}
	said := make(map[string]string, len(words))
	for key, pair := range words {
		if korean {
			said[key] = pair[1]
		} else {
			said[key] = pair[0]
		}
	}
	return said
}

// drawStrip tells the frame's strip what to show.
func (t *windowTabs) drawStrip(frame *tabFrame) {
	if !frame.adopted || frame.window == nil {
		return
	}
	// The shown tab's sidebar, as its page last told, wherever it was told:
	// a tab dragged into a new window keeps its sidebar's head on the strip.
	sidebar := frame.sidebar
	if tab := frame.active; tab != nil && tab.sidebar != nil {
		sidebar = *tab.sidebar
	}
	state := map[string]any{
		"window": frame.window.Name(), "dark": t.look.Dark, "rtl": t.look.RTL, "colors": t.look.Colors, "groupLooks": t.look.Groups,
		"sidebar": sidebar, "lights": frame.lights, "fullscreen": frame.fullscreen,
		"unread": t.unread, "labels": tabWords(), "platform": stripPlatform, "maximized": frameMaximized(frame.window),
		"strip": stripCSSHeight(), "words": t.look.Words,
	}
	tabs := make([]stripTab, 0, len(frame.section.tabs))
	for _, tab := range frame.section.tabs {
		shown := stripTab{ID: tab.id, Title: tab.title, Icon: tab.icon, Pinned: tab.pinned, Active: tab == frame.active}
		if tab.group != nil {
			shown.Group = tab.group.id
		}
		tabs = append(tabs, shown)
	}
	state["tabs"] = tabs
	groups := make([]stripGroup, 0, len(frame.groups))
	for _, group := range frame.groups {
		menu := frame.menu != nil && frame.menu.group == group
		groups = append(groups, stripGroup{ID: group.id, Name: group.name, Color: group.color, Icon: group.icon,
			Collapsed: group.collapsed, Menu: menu})
	}
	state["groups"] = groups
	if tab := frame.active; tab != nil && tab.window != nil {
		state["back"], state["forward"] = tabCanGo(tab)
		state["shade"] = tab.shade
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	frame.window.ExecJS("window.leafenStrip&&leafenStrip.set(" + string(data) + ")")
	// The window's own buttons beside the strip take its colors (Windows).
	paintFrameChrome(frame.window)
}

// stripMessage is what the strip says the person did (tab_strip.js).
type stripMessage struct {
	Do    string `json:"do"`
	ID    string `json:"id"`
	From  string `json:"from"`
	Index int    `json:"index"`
	// Group is the group a tab dropped goes into, if one.
	Group string `json:"group"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	// Right is a hovered tab's or a pressed button's right edge; Bottom a
	// pressed button's foot.
	Right  int  `json:"right"`
	Bottom int  `json:"bottom"`
	Meta   bool `json:"meta"`
	// Keys says a menu's line was picked by a key (Enter), not a press.
	Keys bool `json:"keys"`
	// Rects are the places a menu the strip draws takes over the window
	// (tab_strip_menu.js), Holes those it leaves to the page's icon picker
	// over a group's menu; a group's menu's first rect is its icon's.
	Rects []stripRect `json:"rects"`
	Holes []stripRect `json:"holes"`
	// Shadows are the menus' boxes as drawn, whose shadow the page draws
	// under them on Windows (window_tabs_strip_shadows_windows.go).
	Shadows []stripBox `json:"shadows"`
	// Name and Color are what a group's menu says of its group as it changes.
	Name  string `json:"name"`
	Color string `json:"color"`
}

// windowMessage takes a frame's strip's messages; other pages say nothing
// this way.
func windowMessage(window application.Window, message string, _ *application.OriginInfo) {
	text, ok := strings.CutPrefix(message, "leafen-tabs:")
	if !ok {
		return
	}
	var said stripMessage
	if json.Unmarshal([]byte(text), &said) != nil {
		return
	}
	onMain(func() { desktopTabs.stripSaid(window, said) })
}

func (t *windowTabs) stripSaid(window application.Window, said stripMessage) {
	var frame *tabFrame
	for _, each := range t.frames {
		if each.window != nil && each.window.ID() == window.ID() {
			frame = each
		}
	}
	if frame == nil {
		return
	}
	tab := frame.tabByID(said.ID)
	// Anything else done on the strip closes its menu, the keys staying.
	switch said.Do {
	case "ready", "focus", "hover", "unhover", "menu", "group-menu", "menu-room", "group-say", "group-icon", "picker-close",
		"menu-pick", "menu-closed":
	default:
		t.closeStripMenu(frame, keysStay)
	}
	switch said.Do {
	case "ready":
		t.drawStrip(frame)
	case "focus":
		if frame.active != nil {
			focusTabView(frame, frame.active)
		}
	case "select":
		if tab != nil {
			t.selectTab(frame, tab)
		}
	case "close":
		if tab != nil {
			t.closeTabs(frame, []*appTab{tab})
		}
	case "new":
		t.newTabSearch(frame)
	case "drop":
		if tab != nil {
			t.dropTab(frame, tab, said.Index, frame.groupByID(said.Group))
		}
	case "drop-group":
		if group := frame.groupByID(said.ID); group != nil {
			t.moveGroup(frame, group, said.Index)
		}
	case "fold":
		if group := frame.groupByID(said.ID); group != nil {
			t.foldGroup(frame, group, !group.collapsed)
		}
	case "group-menu":
		if group := frame.groupByID(said.ID); group != nil {
			t.openGroupMenu(frame, group, stripRect{Left: said.X, Right: said.Right, Top: said.Y, Bottom: said.Bottom}, frame.tabByID(said.From))
		}
	case "sidebar":
		if tab := frame.active; tab != nil && tab.window != nil {
			tab.window.EmitEvent(toggleSidebarEvent, tab.window.Name())
		}
	case "head":
		// The shown page opens its inbox, or its "새로" menu under the
		// strip's button, whose place the page takes as its own, and takes
		// the keys with it: Escape closes what opened, as Notion's app's
		// inbox, rather than going to the strip pressed (2026-11-15, a
		// press the strip's mouseup did not hand back).
		if tab := frame.active; tab != nil && tab.window != nil {
			focusTabView(frame, tab)
			tab.window.EmitEvent(sidebarHeadEvent, map[string]any{
				"window": tab.window.Name(), "action": said.ID,
				"x": said.X, "right": said.Right, "y": said.Y, "bottom": said.Bottom,
			})
		}
	case "back", "forward":
		if said.Meta {
			t.openHistoryInNewTab(frame, said.Do == "forward")
		} else if tab := frame.active; tab != nil && tab.window != nil {
			goTab(tab, said.Do == "forward")
		}
	case "history":
		t.showHistoryMenu(frame, said.ID == "forward")
	case "hover":
		t.previewTab(frame, tab, said.X, said.Right)
	case "unhover":
		t.previewTab(frame, nil, 0, 0)
	case "menu":
		if tab != nil {
			t.openTabMenu(frame, tab, said.X, said.Y)
		}
	case "menu-room":
		if frame.menu != nil {
			growStrip(frame, true, stripRoom{rects: said.Rects, holes: said.Holes, shadows: said.Shadows, through: frame.menu.kind == "tab"})
		}
	case "group-say":
		// A group's menu changed its name or color as it goes, still open.
		if frame.menu != nil && frame.menu.kind == "group" {
			t.stripMenuSaid(frame, stripMenuMessage{Do: said.ID, Group: said.Group, Name: said.Name, Color: said.Color})
		}
	case "group-icon":
		if len(said.Rects) > 0 {
			t.openGroupPicker(frame, said.Rects[0])
		}
	case "picker-close":
		t.closeGroupPicker(frame)
	case "menu-pick":
		// The tab's menu chose (its "id" what, "from" the tab; keys, by Enter), and closed.
		t.stripMenuSaid(frame, stripMenuMessage{Do: said.ID, Tab: said.From, Group: said.Group, Keys: said.Keys})
	case "menu-closed":
		// Escape, Enter in a group's name or a press outside it: what it
		// opened from takes the keys; the window going behind, the page.
		keys := keysToOpener
		if said.ID == "blur" {
			keys = keysToPage
		}
		t.closeStripMenu(frame, keys)
	case "insert":
		t.insertFrom(frame, said)
	case "insert-group":
		t.insertGroupFrom(frame, said)
	case "out":
		if tab != nil {
			t.dropOutside(frame, tab, said.X, said.Y)
		}
	case "group-out":
		if group := frame.groupByID(said.ID); group != nil {
			t.dropGroupOutside(frame, group, said.X, said.Y)
		}
	default:
		t.stripSaidHere(frame, said)
	}
}

// insertFrom takes a tab dragged from another window's strip into this
// one, at the place it was dropped, into the group there if one.
func (t *windowTabs) insertFrom(to *tabFrame, said stripMessage) {
	for _, from := range t.frames {
		if from.window != nil && from.window.Name() == said.From {
			if tab := from.tabByID(said.ID); tab != nil {
				t.moveTab(from, tab, to, said.Index, nil)
				if group := to.groupByID(said.Group); group != nil && to.index(tab) >= 0 {
					t.setTabsGroup(to, []*appTab{tab}, group)
					t.changed(to)
				}
			}
			return
		}
	}
}

// insertGroupFrom takes a group dragged from another window's strip into
// this one, at the place it was dropped.
func (t *windowTabs) insertGroupFrom(to *tabFrame, said stripMessage) {
	for _, from := range t.frames {
		if from.window != nil && from.window.Name() == said.From {
			if group := from.groupByID(said.ID); group != nil {
				t.moveGroupTo(from, group, to, said.Index, nil)
			}
			return
		}
	}
}

// newTabSearch opens search for a new tab in the shown page, as Notion's app
// does for ⌘T and "+": the page found opens in a new tab at the end. A page
// without search (signing in, the first run) gets a new tab on the start
// page. The page takes the keys with its search, as with the strip's head's
// buttons: given ⌘T while a tab of the strip held them, its search showed a
// caret and the letters typed went to the tab (the Windows VM 2026-11-24).
func (t *windowTabs) newTabSearch(frame *tabFrame) {
	if tab := frame.active; tab != nil && tab.window != nil && tab.searchReady {
		focusTabView(frame, tab)
		tab.window.EmitEvent(newTabSearchEvent, tab.window.Name())
		return
	}
	t.openTab(frame, "/", frame.active, true, true)
}

// newTabSearchEvent asks a page to open search for a new tab.
const newTabSearchEvent = "leafen:new-tab-search"

// sidebarHeadEvent asks a page for what its sidebar head's button on the
// strip opens (SidebarTopActions.tsx).
const sidebarHeadEvent = "leafen:sidebar-head"

// copyTabLinkEvent asks the shown page to copy the link of another tab's
// page, one not opened yet since the app started.
const copyTabLinkEvent = "leafen:copy-tab-link"
