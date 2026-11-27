//go:build (darwin && !ios) || windows

package main

import (
	"strconv"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The Mac and Windows apps' windows hold tabs, as Notion's app's windows do
// (7.42.3, its tab code read 2026-11-10: tabs renderer, NativeTabBehavior,
// the File and Window menus). A window is a frame the person moves and
// resizes, whose own web view is the strip of tabs along its top
// (tab_strip.html, window_tabs_strip.go); each tab is a whole page of the
// app in a web view of its own, made in a window of its own that never
// shows, and shown in the frame under the strip (window_tabs_darwin.m,
// window_tabs_windows.go). Only the frame's chosen tab
// shows; the others keep their pages behind it. The tabs are kept as they
// change and come back when the app starts again (window_tabs_frames.go).
//
// Everything here runs on the app's main thread (onMain): the frames, their
// tabs and their web views are the main thread's.

// tabsInWindows says the app's windows hold tabs on this computer.
const tabsInWindows = true

// appTab is one tab: the page it shows, as its page last told, and the window
// its web view was made in — none yet for a tab brought back from the last
// time until it is first chosen.
type appTab struct {
	id     string
	path   string
	title  string
	icon   string
	pinned bool
	// signIn is the sign-in its page shows — its server and the account on
	// it — as its page last told, which it opens with again
	// (window_tabs_kept.go).
	signIn string
	// said is what its page last told of its name and icon (window_tabs_names.go).
	said tabSaid
	// back is how many entries back its history goes, as its page last told
	// (window_tabs_page.go); -1 till it tells.
	back int
	// opener is the tab it was opened from: tabs opened from one follow it
	// in turn, as Notion's "after children".
	opener *appTab
	// group is the group of its window's tabs it is in, if one
	// (window_tabs_groups.go).
	group *tabGroup
	// shade is the shade a window over its page lays on it, as its page last
	// told, which its window's strip takes while it shows.
	shade  string
	window *application.WebviewWindow
	// host is the frame its web view is in.
	host *tabFrame
	// searchReady says its page opens search for a new tab (⌘T).
	searchReady bool
	// sidebar is the sidebar its page last told of, which the strip of the
	// window it shows in draws (a window it moves to too).
	sidebar *stripSidebar
	// unusedSince is when it was last the shown tab of its window, or
	// opened behind; a tab long unused has its page put away (unloadTabs).
	unusedSince time.Time
	// shown is when it was last chosen to show, in the order of choosing
	// (closeTabs shows the last chosen of those left).
	shown int64
	// starting is its page's web view while it starts in view, and
	// startDrawn says its page has drawn its first screen meanwhile
	// (window_tabs_start.go).
	starting   *application.WebviewWindow
	startDrawn bool
	// ringDrawn: what of the strip takes the keys back once its page has
	// drawn its first screen (a copy's tab, window_tabs_strip_keys.go).
	ringDrawn stripRing
	// awakeUntil is when its page, kept in view behind the shown tab to
	// follow a step passed there, rests again (window_tabs_start.go).
	awakeUntil time.Time
	// closing lets its window's own closing go through.
	closing atomic.Bool
}

// tabSection is a window's tabs, in the strip's order.
type tabSection struct {
	tabs []*appTab
}

// tabFrame is one window: its tabs, the groups they are in, in the order
// they were made (window_tabs_groups.go), and the tab shown.
type tabFrame struct {
	window     *application.WebviewWindow
	section    *tabSection
	groups     []*tabGroup
	active     *appTab
	focused    int64
	adopted    bool
	show       bool
	fullscreen bool
	// hideWindowed: closed in full screen, it hides once out of it.
	hideWindowed bool
	lights       int
	sidebar      stripSidebar
	// menu is the strip's menu its shown page draws, while one is open
	// (window_tabs_strip_menus.go).
	menu    *stripMenu
	saved   *savedFrame
	closing atomic.Bool
}

// windowTabs is every frame the app has open, front ones first by when they
// were last in front, and what was closed, newest last.
type windowTabs struct {
	frames  []*tabFrame
	closed  []closedTabs
	seq     int
	focus   int64
	choices int64
	look    stripLook
	unread  int
	zoom    float64
	// quitting: the app is ending, and its frames close with it.
	quitting bool
	// keepNone: the app ends without keeping its tabs (탭 저장 없이 종료).
	keepNone bool
}

var desktopTabs = &windowTabs{}

// onMain runs fn on the app's main thread, after what is there already.
func onMain(fn func()) { application.InvokeAsync(fn) }

// newTab makes a tab, in the sign-in of the tab in front, as Notion's app
// opens a new tab in its account (window_tabs_kept.go).
func (t *windowTabs) newTab(path string) *appTab {
	t.seq++
	if path == "" {
		path = "/"
	}
	tab := &appTab{id: "tab-" + strconv.Itoa(t.seq), path: path, back: -1, unusedSince: time.Now()}
	if _, front := t.front(); front != nil {
		tab.signIn = front.signIn
	}
	return tab
}

// frameOf is the frame a window is, or holds as a tab, with the tab.
func (t *windowTabs) frameOf(window application.Window) (*tabFrame, *appTab) {
	if window == nil {
		return nil, nil
	}
	for _, frame := range t.frames {
		if frame.window != nil && frame.window.ID() == window.ID() {
			return frame, frame.active
		}
	}
	for _, frame := range t.frames {
		for _, each := range frame.section.tabs {
			if each.window != nil && each.window.ID() == window.ID() {
				return frame, each
			}
		}
	}
	return nil, nil
}

// allTabs is every tab the app has, each window's.
func (t *windowTabs) allTabs() []*appTab {
	var tabs []*appTab
	for _, frame := range t.frames {
		tabs = append(tabs, frame.section.tabs...)
	}
	return tabs
}

// shownTabs are the frame's shown tab, which keeps its page (unloadTabs).
func (frame *tabFrame) shownTabs() []*appTab {
	if frame.active != nil {
		return []*appTab{frame.active}
	}
	return nil
}

// front is the frame in front — the key window's, else the one last in
// front — and its chosen tab.
func (t *windowTabs) front() (*tabFrame, *appTab) {
	if app := application.Get(); app != nil {
		if frame, _ := t.frameOf(app.Window.Current()); frame != nil {
			return frame, frame.active
		}
	}
	var last *tabFrame
	for _, frame := range t.frames {
		if last == nil || frame.focused > last.focused {
			last = frame
		}
	}
	if last == nil {
		return nil, nil
	}
	return last, last.active
}

func (frame *tabFrame) index(tab *appTab) int {
	for i, each := range frame.section.tabs {
		if each == tab {
			return i
		}
	}
	return -1
}

func (frame *tabFrame) tabByID(id string) *appTab {
	for _, tab := range frame.section.tabs {
		if tab.id == id {
			return tab
		}
	}
	return nil
}

// insert puts a tab in the frame: at the end, or after the tab it was opened
// from and the tabs opened from that one before it, in its group; pinned
// tabs stay first.
func (frame *tabFrame) insert(tab *appTab, atEnd bool) {
	at := len(frame.section.tabs)
	if !atEnd && tab.opener != nil {
		if from := frame.index(tab.opener); from >= 0 {
			at = from + 1
			for at < len(frame.section.tabs) && frame.section.tabs[at].opener == tab.opener {
				at++
			}
		}
	}
	frame.insertAt(tab, at)
}

func (frame *tabFrame) insertAt(tab *appTab, at int) {
	pinned := 0
	for _, each := range frame.section.tabs {
		if each.pinned {
			pinned++
		}
	}
	if tab.pinned {
		at = min(at, pinned)
	} else {
		at = max(at, pinned)
	}
	at = max(0, min(at, len(frame.section.tabs)))
	frame.section.tabs = append(frame.section.tabs, nil)
	copy(frame.section.tabs[at+1:], frame.section.tabs[at:])
	frame.section.tabs[at] = tab
	frame.takeNeighbours(at)
}

func (frame *tabFrame) remove(tab *appTab) int {
	at := frame.index(tab)
	if at >= 0 {
		frame.section.tabs = append(frame.section.tabs[:at], frame.section.tabs[at+1:]...)
	}
	for _, each := range frame.section.tabs {
		if each.opener == tab {
			each.opener = nil
		}
	}
	return at
}

// openTab opens a page in a new tab of the frame: shown at once, or behind
// the one shown (a ⌘-pressed link).
func (t *windowTabs) openTab(frame *tabFrame, path string, from *appTab, active, atEnd bool) *appTab {
	tab := t.newTab(path)
	tab.opener = from
	if from != nil {
		tab.signIn = from.signIn
	}
	frame.insert(tab, atEnd)
	if !atEnd {
		t.joinOpenersGroup(frame, tab, !active)
	}
	if frame.adopted {
		t.tabWindow(frame, tab)
	}
	if active {
		t.selectTab(frame, tab)
	} else {
		t.changed(frame)
	}
	return tab
}

// selectTab shows a tab of its frame, making its page first if it has none.
func (t *windowTabs) selectTab(frame *tabFrame, tab *appTab) {
	if frame.index(tab) < 0 {
		return
	}
	if left := frame.active; left != nil && left != tab {
		left.unusedSince = time.Now()
		// What the page left showed for the strip goes with it.
		t.closeStripMenu(frame, keysStay)
	}
	frame.active = tab
	t.choices++
	tab.shown = t.choices
	if frame.adopted {
		if window := t.tabWindow(frame, tab); window != nil {
			showTabView(frame, tab, true)
			titleWindow(frame)
		}
	}
	t.changed(frame)
}

// step shows the tab so many places along, round the ends (⌃Tab, ⌃⇧Tab).
func (t *windowTabs) step(frame *tabFrame, by int) {
	at := frame.index(frame.active)
	if at < 0 || len(frame.section.tabs) == 0 {
		return
	}
	at = ((at+by)%len(frame.section.tabs) + len(frame.section.tabs)) % len(frame.section.tabs)
	t.selectTab(frame, frame.section.tabs[at])
}

// selectNumber shows the tab at a place (⌘1–⌘8), or the last (⌘9).
func (t *windowTabs) selectNumber(frame *tabFrame, number int) {
	if len(frame.section.tabs) == 0 {
		return
	}
	at := number - 1
	if number == 9 {
		at = len(frame.section.tabs) - 1
	}
	if at >= 0 && at < len(frame.section.tabs) {
		t.selectTab(frame, frame.section.tabs[at])
	}
}

// closeTabs closes tabs of a frame, kept to open again as one: the shown
// one's place goes to the tab after it, else the last. A frame left with no
// tab closes as a window does.
func (t *windowTabs) closeTabs(frame *tabFrame, closing []*appTab) {
	if len(closing) == 0 {
		return
	}
	if len(closing) >= len(frame.section.tabs) {
		t.closeAsked(frame)
		return
	}
	gone := closedTabs{frame: frame, groups: savedGroupsOf(closing)}
	for _, tab := range closing {
		gone.tabs = append(gone.tabs, savedTabOf(tab, frame.index(tab), tab == frame.active))
	}
	t.remember(gone)
	if shut := frame.active; shut != nil && containsTab(closing, shut) {
		t.selectTab(frame, nextShown(frame, shut, closing))
	}
	for _, tab := range closing {
		frame.remove(tab)
		t.dropTabWindow(tab)
	}
	t.dropEmptyGroups(frame)
	t.changed(frame)
}

func containsTab(tabs []*appTab, tab *appTab) bool {
	for _, each := range tabs {
		if each == tab {
			return true
		}
	}
	return false
}

// closeAround closes the frame's other tabs, or those before or after one;
// pinned tabs stay, as in Notion's tab menu.
func (t *windowTabs) closeAround(frame *tabFrame, tab *appTab, which string) {
	at := frame.index(tab)
	var closing []*appTab
	for i, each := range frame.section.tabs {
		if each == tab || each.pinned {
			continue
		}
		if which == "others" || which == "before" && i < at || which == "after" && i > at {
			closing = append(closing, each)
		}
	}
	t.closeTabs(frame, closing)
}

// dropTabWindow ends a tab's page: its web view leaves the frame and its own
// window closes with it.
func (t *windowTabs) dropTabWindow(tab *appTab) {
	window := tab.window
	if window == nil {
		return
	}
	tab.window, tab.host = nil, nil
	releaseTabView(window)
	tab.closing.Store(true)
	window.Close()
}

func containsFrame(frames []*tabFrame, frame *tabFrame) bool {
	for _, each := range frames {
		if each == frame {
			return true
		}
	}
	return false
}

// changed draws the frame's strip again and keeps the tabs for next time.
func (t *windowTabs) changed(frame *tabFrame) {
	if frame != nil {
		t.drawStrip(frame)
	}
	t.keep()
}
