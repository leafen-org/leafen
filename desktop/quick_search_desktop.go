//go:build (darwin && !ios) || windows

package main

import (
	"net/url"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// The command search's own window, as Notion's app's (QuickSearchController,
// 7.43.0): 755 wide and 700 high, frameless, over every app and every space,
// left out of Mission Control and the window cycle — on a Mac a panel that
// takes the keys without bringing the app forward. It holds the app's page
// at its search alone (web/src/lib/quickSearchWindow.ts), comes up in the
// middle of the screen under the pointer, and goes away when it loses the
// keys, when Esc closes its search or when a result opens — in a tab of the
// app's window in front, the one already showing that page if there is one,
// the app brought forward. It is made the first time it is asked for and
// kept; until its page has opened its search it waits, at most 1.5 seconds,
// as Notion's.

const (
	quickSearchName           = "quick-search"
	quickSearchWidth          = 755
	quickSearchHeight         = 700
	quickSearchPath           = "/?leafen-quick-search=1"
	quickSearchHiddenEvent    = "leafen:quick-search-hidden"
	quickSearchShowFallback   = 1500 * time.Millisecond
	commandSearchSettingsPart = "computer"
)

// quickSearchWindow is the window and where it stands; the main thread's.
type quickSearchWindow struct {
	window  *application.WebviewWindow
	shown   bool
	ready   bool
	waiting bool
	asked   int
}

var quickSearch = &quickSearchWindow{}

func quickSearchOptions() application.WebviewWindowOptions {
	options := appWindowOptions(quickSearchPath)
	options.Name = quickSearchName
	options.Title = "Leafen - Command Search"
	options.Width, options.Height = quickSearchWidth, quickSearchHeight
	options.MinWidth, options.MaxWidth, options.MinHeight = quickSearchWidth, quickSearchWidth, quickSearchHeight
	options.Frameless = true
	options.Hidden = true
	options.AlwaysOnTop = true
	options.Mac.WindowClass = application.MacWindowClassPanel
	options.Mac.PanelPreferences = application.MacPanelPreferences{FloatingPanel: true, NonActivating: true}
	options.Mac.CollectionBehavior = application.MacWindowCollectionBehaviorMoveToActiveSpace |
		application.MacWindowCollectionBehaviorFullScreenAuxiliary |
		application.MacWindowCollectionBehaviorIgnoresCycle |
		application.MacWindowCollectionBehaviorTransient
	options.Windows.HiddenOnTaskbar = true
	return options
}

// make makes the window, hidden, its page loading.
func (q *quickSearchWindow) make() {
	window := application.Get().Window.NewWithOptions(quickSearchOptions())
	q.window, q.ready = window, false
	routeNewWindows(window)
	window.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		onMain(q.hide)
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if !tabsQuitting.Load() {
			event.Cancel()
			onMain(q.hide)
		}
	})
}

// toggle brings the window up, or takes it away when it is up.
func (q *quickSearchWindow) toggle() {
	if q.shown {
		q.hide()
		return
	}
	q.show()
}

func (q *quickSearchWindow) show() {
	if q.window == nil {
		q.make()
	}
	q.shown = true
	centerOnPointerScreen(q.window.NativeWindow())
	if q.ready {
		q.reveal()
		return
	}
	q.waiting = true
	q.asked++
	asked := q.asked
	time.AfterFunc(quickSearchShowFallback, func() {
		onMain(func() {
			if q.waiting && q.asked == asked {
				q.reveal()
			}
		})
	})
}

// reveal shows the window, its search open anew.
func (q *quickSearchWindow) reveal() {
	q.waiting = false
	q.window.Show()
	q.window.Focus()
	application.Get().Event.Emit(openSearchEvent, q.window.Name())
}

func (q *quickSearchWindow) hide() {
	if q.window == nil || !q.shown {
		return
	}
	q.shown, q.waiting = false, false
	q.window.Hide()
	application.Get().Event.Emit(quickSearchHiddenEvent, q.window.Name())
}

// openPage opens one of the app's pages a result named: in the tab of the
// window in front already showing it, else in a new tab at the end of that
// window, shown; the app comes forward (Notion's
// openURLOptionallySurfacingExistingTab).
func (q *quickSearchWindow) openPage(path string) {
	q.hide()
	frame, _ := desktopTabs.front()
	if frame == nil {
		desktopTabs.newFrame(&savedFrame{Tabs: []savedTab{{Path: path}}}, true)
		bringAppForward()
		return
	}
	if tab := tabShowing(frame, path); tab != nil {
		desktopTabs.selectTab(frame, tab)
	} else {
		desktopTabs.openTab(frame, path, nil, true, true)
	}
	if frame.adopted {
		frame.window.Show()
		frame.window.Focus()
	} else {
		frame.show = true
	}
	bringAppForward()
}

// tabShowing is the frame's tab showing the page at path, for a path that
// names a page alone (no row, block or view in it).
func tabShowing(frame *tabFrame, path string) *appTab {
	if strings.ContainsAny(path, "?#") {
		return nil
	}
	for _, tab := range frame.section.tabs {
		if tab.path == path {
			return tab
		}
	}
	return nil
}

// QuickSearch is what the command search window's page asks.
type QuickSearch struct{}

// Ready says its page has opened its search: a window waiting for it shows.
func (*QuickSearch) Ready() {
	onMain(func() {
		q := quickSearch
		q.ready = true
		if q.waiting {
			q.reveal()
		}
	})
}

// Hide takes the window away (Esc, its search closed).
func (*QuickSearch) Hide() { onMain(quickSearch.hide) }

// Open opens one of the app's pages a result names, as openPage says.
func (*QuickSearch) Open(address string) {
	kind, parsed := newWindowKindOf(address)
	if kind != newWindowAppPage {
		if strings.HasPrefix(address, "/") {
			parsed, _ = url.Parse(address)
		} else {
			return
		}
	}
	path := appPagePath(parsed)
	onMain(func() { quickSearch.openPage(path) })
}

// EditShortcut opens 설정 › 이 컴퓨터 at the command search's key in the
// window in front, as the search's foot and the icon's menu ask (Notion's
// quick-search-open-shortcut-settings, openUserSettings).
func (*QuickSearch) EditShortcut() { openCommandSearchSettings() }

func openCommandSearchSettings() {
	onMain(func() {
		quickSearch.hide()
		frame, tab := desktopTabs.front()
		if frame == nil {
			desktopTabs.newFrame(&savedFrame{Tabs: []savedTab{{Path: "/"}}}, true)
			bringAppForward()
			return
		}
		if frame.adopted {
			frame.window.Show()
			frame.window.Focus()
		}
		bringAppForward()
		if tab != nil && tab.window != nil {
			application.Get().Event.Emit(openSettingsEvent, map[string]string{
				"window": tab.window.Name(), "section": "servers", "part": commandSearchSettingsPart,
			})
		}
	})
}
