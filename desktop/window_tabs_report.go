//go:build (darwin && !ios) || windows

package main

import (
	"context"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The service a tab's page calls: what it shows now for its tab and the
// strip (Report), the sign-in it starts in and the zoom it asks for. The strip
// itself, and what it says the person did, is window_tabs_strip.go.

// WindowTabs is what a tab's page tells the app about itself and asks of it.
type WindowTabs struct{}

// tabReport is a page's own state for its tab and the strip.
type tabReport struct {
	Path    string        `json:"path"`
	Title   string        `json:"title"`
	Page    string        `json:"page"`
	Icon    string        `json:"icon"`
	Search  bool          `json:"search"`
	Sidebar *stripSidebar `json:"sidebar"`
	Unread  *int          `json:"unread"`
	Look    *stripLook    `json:"look"`
	// Drawn says its first screen is drawn whole (window_tabs_start.go).
	Drawn bool `json:"drawn"`
	// Back is how many entries back its history goes (window_tabs_page.go).
	Back *int `json:"back"`
	// SignIn is the sign-in it shows, kept with its tab (window_tabs_kept.go).
	SignIn *string `json:"signIn"`
	// Shade is the shade a window over the page lays on it, "" with none,
	// which goes on over the strip (web/src/lib/desktopTabShade.ts).
	Shade *string `json:"shade"`
}

// Report takes what a tab's page shows now: its address, its name and icon,
// whether it can search for a new tab, its sidebar, the inbox's unread count
// and the pages' colors, and says whether its tab is pinned.
func (*WindowTabs) Report(ctx context.Context, report tabReport) bool {
	caller, _ := ctx.Value(application.WindowKey).(application.Window)
	if caller == nil {
		return false
	}
	onMain(func() {
		t := desktopTabs
		frame, tab := t.frameOf(caller)
		if frame == nil || tab == nil || tab.window == nil || tab.window.ID() != caller.ID() {
			return
		}
		if strings.HasPrefix(report.Path, "/") && !strings.HasPrefix(report.Path, "//") && len(report.Path) <= maxLinkLength {
			tab.path = report.Path
		}
		// Its page's name and icon go to every tab of that page (window_tabs_names.go).
		shared := t.takeSaid(tab, report.Title, report.Icon, report.Page)
		if report.Drawn {
			t.drawn(tab)
		}
		tab.searchReady = report.Search
		if report.Back != nil {
			tab.back = *report.Back
		}
		if report.SignIn != nil && len(*report.SignIn) <= maxSignIn {
			tab.signIn = *report.SignIn
		}
		if report.Shade != nil && len(*report.Shade) <= 64 {
			tab.shade = *report.Shade
		}
		if report.Sidebar != nil {
			told := *report.Sidebar
			tab.sidebar = &told
			if tab == frame.active {
				frame.sidebar = told
			}
		}
		if report.Unread != nil {
			t.unread = *report.Unread
		}
		if report.Look != nil && len(report.Look.Colors) > 0 {
			t.look = *report.Look
		}
		if tab == frame.active {
			titleWindow(frame)
		}
		if report.Look != nil || report.Unread != nil || shared {
			for _, each := range t.frames {
				if shared {
					titleWindow(each)
				}
				t.drawStrip(each)
			}
			t.keep()
			return
		}
		t.changed(frame)
	})
	// Whether its tab is pinned, which sends the pages it goes to into new
	// tabs (tellPinned tells it again as that changes).
	return application.InvokeSyncWithResult(func() bool {
		_, tab := desktopTabs.frameOf(caller)
		return tab != nil && tab.window != nil && tab.window.ID() == caller.ID() && tab.pinned
	})
}

// SignIn is the sign-in a tab's page starts in as it first starts: the one
// its tab kept, or "" for the page to start in the one used last
// (window_tabs_kept.go).
func (*WindowTabs) SignIn(ctx context.Context) string {
	caller, _ := ctx.Value(application.WindowKey).(application.Window)
	if caller == nil {
		return ""
	}
	return application.InvokeSyncWithResult(func() string {
		_, tab := desktopTabs.frameOf(caller)
		if tab == nil || tab.window == nil || tab.window.ID() != caller.ID() {
			return ""
		}
		return tab.signIn
	})
}

// Zoom zooms every tab's page in, out or back to its size, as a page asks
// for a key the app's own keys can't name (⌃= and ⌃- on Windows,
// web/src/lib/desktopTabs.ts).
func (*WindowTabs) Zoom(change int) { tabsZoom(change) }

// tabServices are what a tab's page calls (WindowTabs).
func tabServices() []application.Service {
	return []application.Service{application.NewService(&WindowTabs{})}
}
