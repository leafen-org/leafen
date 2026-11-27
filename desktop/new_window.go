//go:build (darwin && !ios) || windows

package main

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// What the app does when one of its windows asks for a new window or tab —
// a link meant for a new tab, window.open, "새 탭에서 열기", a ⌘-pressed link —
// as Notion's app does: an address outside the app (a site, mail, a phone
// number) opens with the computer's own browser or app, and one of the app's
// own pages opens in a new tab of the window it came from (window_tabs.go) —
// behind the shown one, in front with ⇧. A window the page fills itself (a
// file's or PDF's preview, a sign-in it then points elsewhere) is a plain
// window of the app, as a browser's pop-up. Anything else opens nothing.
//
// The rule is this file's alone. Requests reach it from the window's own page
// (WindowOpener, web/src/lib/newWindowRequests.ts) and from the web view
// itself, which hears every new-window request in the window's frames: WebKit
// on macOS (new_window_darwin.go), which also hears every pressed link to an
// address the window can't show, and WebView2 on Windows
// (new_window_windows.go). Wails gives neither of these hooks, so the app
// sets them on the window itself.

// newWindowKind is what an address asked for in a new window is. The values
// are also new_window_darwin.m's.
type newWindowKind int

const (
	newWindowRefused newWindowKind = iota
	newWindowOutside
	newWindowAppPage
	newWindowPageMade
)

// newWindowKindOf says what an address is: the app's own page is at the
// packaged client's origin (web/src/lib/packagedOrigins.ts), a custom scheme
// on macOS and a reserved host name on Windows.
func newWindowKindOf(address string) (newWindowKind, *url.URL) {
	address = strings.TrimSpace(address)
	if address == "" || address == "about:blank" {
		return newWindowPageMade, nil
	}
	if len(address) > maxLinkLength {
		return newWindowRefused, nil
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return newWindowRefused, nil
	}
	web := parsed.Scheme == "http" || parsed.Scheme == "https"
	switch {
	case parsed.Scheme == "wails", web && strings.EqualFold(parsed.Hostname(), "wails.localhost"):
		return newWindowAppPage, parsed
	case web && parsed.Host != "":
		return newWindowOutside, parsed
	case (parsed.Scheme == "mailto" || parsed.Scheme == "tel") && parsed.Opaque != "":
		return newWindowOutside, parsed
	case parsed.Scheme == "about" || parsed.Scheme == "blob":
		return newWindowPageMade, parsed
	}
	return newWindowRefused, nil
}

// openNewWindow opens an address outside the app or one of its pages as the
// rule says, beside the window that asked (from, or else the window in
// front); the window opens the pages it fills itself.
func openNewWindow(address string, from application.Window) error {
	return openNewWindowAs(address, from, false, false)
}

// openNewWindowAs opens it so: a page of the app's in front when active says,
// at the end of the tabs when atEnd says (a search for a new tab).
func openNewWindowAs(address string, from application.Window, active, atEnd bool) error {
	kind, parsed := newWindowKindOf(address)
	switch kind {
	case newWindowOutside:
		return application.Get().Browser.OpenURL(parsed.String())
	case newWindowAppPage:
		openAppPage(appPagePath(parsed), from, active, atEnd)
		return nil
	}
	return errors.New("only a site, mail, a phone number or one of the app's pages opens in a new window")
}

// appPagePath is the path in the app of one of its pages' addresses.
func appPagePath(parsed *url.URL) string {
	page := url.URL{Path: parsed.Path, RawPath: parsed.RawPath, RawQuery: parsed.RawQuery, Fragment: parsed.Fragment}
	if page.Path == "" {
		page.Path = "/"
	}
	return page.String()
}

// WindowOpener is what the window's page asks to open in a new window or tab.
type WindowOpener struct{}

// Open opens an address in a new window or tab as the app's rule says; a page
// of the app's opens behind the asking one, or in front when active says, and
// at the end of the tabs when atEnd says (web/src/lib/newWindowRequests.ts).
func (*WindowOpener) Open(ctx context.Context, address string, active, atEnd bool) error {
	from, _ := ctx.Value(application.WindowKey).(application.Window)
	return openNewWindowAs(address, from, active, atEnd)
}

// OpenWindow opens one of the app's pages in a new window of its own, as a
// page's right-press menu "새 창에서 링크 열기" asks (text_menu_darwin.m).
func (*WindowOpener) OpenWindow(address string) error {
	kind, parsed := newWindowKindOf(address)
	if kind != newWindowAppPage {
		return errors.New("only one of the app's pages opens in a window of its own")
	}
	path := appPagePath(parsed)
	onMain(func() { desktopTabs.newFrame(&savedFrame{Tabs: []savedTab{{Path: path}}}, true) })
	return nil
}

// anotherWindowOpen says whether an app window other than this one is open:
// on screen, behind others or minimized, not hidden.
func anotherWindowOpen(window application.Window) bool {
	app := application.Get()
	if app == nil {
		return false
	}
	for _, other := range app.Window.GetAll() {
		if other.ID() != window.ID() && windowOpen(other) {
			return true
		}
	}
	return false
}
