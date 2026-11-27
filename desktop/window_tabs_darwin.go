//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework WebKit

#include <stdbool.h>
#include <stdlib.h>

int leafenTabsAdoptFrame(void *frame, int stripHeight, double zoom);
void leafenTabsSetZoom(void *window, double zoom);
void leafenTabsContentSize(void *frame, int *width, int *height);
void leafenTabsPlaceFrame(void *frame, bool hasFrame, double x, double y, double width, double height, bool hasPoint, int px, int py);
bool leafenTabsFrameRect(void *frame, double *x, double *y, double *width, double *height);
bool leafenTabsStripPoint(void *frame, int px, int py, int stripHeight, int *x);
void leafenTabsAddView(void *frame, void *tab, int stripHeight);
void leafenTabsShowView(void *frame, void *tab, int stripHeight, bool focus);
void leafenTabsFocusView(void *frame, void *tab);
void leafenTabsReleaseView(void *tab);
void leafenTabsKeepHidden(void *tab);
void leafenTabsStarting(void *tab, bool starting);
int leafenTabsHistory(void *tab);
void leafenTabsGo(void *tab, bool forward);
char *leafenTabsHistoryList(void *tab, bool forward);
*/
import "C"

import (
	"encoding/json"
	"math"
	"strconv"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// stripPlatform tells the strip whose window it is in (tab_strip.js).
const stripPlatform = "mac"

// pageKeepsHistory: WebKit lists a tab's page's history (tabHistoryList).
const pageKeepsHistory = false

// The frame and its tabs' web views on macOS (window_tabs_darwin.m). Each
// runs on the main thread, where the tabs live.

// frameOptions are a frame's: an app window whose page is the strip, its
// title bar left to the strip under the traffic lights, and the tab keys
// Notion's app has without a menu item (window_tabs_strip.go).
func frameOptions() application.WebviewWindowOptions {
	options := appWindowOptions(stripPage)
	options.Hidden = true
	options.Mac.TitleBar = application.MacTitleBar{AppearsTransparent: true, HideTitle: true, FullSizeContent: true}
	options.KeyBindings = tabKeyBindings()
	return options
}

// tabWindowOptions are a tab's own window's, which never shows: its page's
// keys reach the menu bar and the frame's bindings.
func tabWindowOptions(path string) application.WebviewWindowOptions {
	options := appWindowOptions(path)
	options.Hidden = true
	return options
}

// watchFrame calls ready once the frame's strip page has loaded, and
// fullscreen as the frame enters or leaves full screen.
func watchFrame(window *application.WebviewWindow, ready func(), fullscreen func(bool)) {
	window.RegisterHook(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) { ready() })
	window.OnWindowEvent(events.Mac.WindowDidEnterFullScreen, func(*application.WindowEvent) { fullscreen(true) })
	window.OnWindowEvent(events.Mac.WindowDidExitFullScreen, func(*application.WindowEvent) { fullscreen(false) })
}

// tabKeyBindings are the tab keys Notion's app has without a menu item of
// their own, and ⌃Tab, ⌃⇧Tab, which a page's web view would otherwise take:
// ⌘⌥← ⌘⌥→ and ⌘⇧[ ⌘⇧] step through the tabs, ⌘1–⌘8 show the tab at that
// place and ⌘9 the last, ⌘⇧W closes the window.
func tabKeyBindings() map[string]func(application.Window) {
	step := func(by int) func(application.Window) { return func(application.Window) { tabsStep(by) } }
	bindings := map[string]func(application.Window){
		"ctrl+tab": step(1), "ctrl+shift+tab": step(-1),
		"cmdorctrl+alt+left": step(-1), "cmdorctrl+alt+right": step(1),
		"cmdorctrl+shift+[": step(-1), "cmdorctrl+shift+]": step(1),
		"cmdorctrl+shift+w": func(application.Window) { tabsCloseWindow() },
	}
	for number := 1; number <= 9; number++ {
		bindings["cmdorctrl+"+strconv.Itoa(number)] = func(application.Window) { tabsNumber(number) }
	}
	return bindings
}

// stripSaidHere takes what only a Mac strip says: nothing beyond the rest.
func (t *windowTabs) stripSaidHere(*tabFrame, stripMessage) {}

// paintFrameChrome: a Mac frame's traffic lights are the system's own.
func paintFrameChrome(*application.WebviewWindow) {}

// adoptFrameView sets the strip along the frame's top and its traffic lights
// in the strip, as Notion's; it says how wide their room is.
func adoptFrameView(frame *application.WebviewWindow) int {
	if native := frame.NativeWindow(); native != nil {
		// Notion's lights' room in the tab bar keeps its points below a zoom
		// of 1 and grows with it above (its getTrafficLightBlankAreaWidth).
		room := float64(C.leafenTabsAdoptFrame(native, C.int(stripHeight()), C.double(desktopTabs.zoomFactor())))
		return int(math.Round(room / min(1, desktopTabs.zoomFactor())))
	}
	return 0
}

// placeFrame puts a frame where it was kept, or where a tab dragged out of
// another window was let go.
func placeFrame(frame *application.WebviewWindow, kept *savedFrame) {
	native := frame.NativeWindow()
	if native == nil {
		return
	}
	var x, y, width, height C.double
	var px, py C.int
	if kept.Frame != nil {
		x, y, width, height = C.double(kept.Frame[0]), C.double(kept.Frame[1]), C.double(kept.Frame[2]), C.double(kept.Frame[3])
	}
	if kept.At != nil {
		px, py = C.int(kept.At[0]), C.int(kept.At[1])
	}
	C.leafenTabsPlaceFrame(native, C.bool(kept.Frame != nil), x, y, width, height, C.bool(kept.At != nil), px, py)
}

// frameRect is where a frame is on the screen, to keep.
func frameRect(frame *application.WebviewWindow) *[4]float64 {
	native := frame.NativeWindow()
	if native == nil {
		return nil
	}
	var x, y, width, height C.double
	if !bool(C.leafenTabsFrameRect(native, &x, &y, &width, &height)) {
		return nil
	}
	return &[4]float64{float64(x), float64(y), float64(width), float64(height)}
}

// stripPointIn says whether a screen point a strip gave (from the top left
// of the main screen) is over a frame's strip, and where along it.
func stripPointIn(frame *application.WebviewWindow, px, py int) (int, bool) {
	native := frame.NativeWindow()
	if native == nil {
		return 0, false
	}
	var x C.int
	if !bool(C.leafenTabsStripPoint(native, C.int(px), C.int(py), C.int(stripHeight()), &x)) {
		return 0, false
	}
	// Points along the window, as the zoomed strip's CSS pixels.
	return int(math.Round(float64(x) / desktopTabs.zoomFactor())), true
}

// frameMaximized: a Mac window's zoom isn't kept.
func frameMaximized(*application.WebviewWindow) bool { return false }

// addTabView puts a tab's web view in its frame, behind the shown one.
func addTabView(frame *tabFrame, tab *appTab) {
	if outer, inner := frame.window.NativeWindow(), tab.window.NativeWindow(); outer != nil && inner != nil {
		C.leafenTabsAddView(outer, inner, C.int(stripHeight()))
		tab.host = frame
	}
}

// showTabView shows a tab's web view in its frame, the frame's other tabs
// behind it, with the keyboard when focus says.
func showTabView(frame *tabFrame, tab *appTab, focus bool) {
	if outer, inner := frame.window.NativeWindow(), tab.window.NativeWindow(); outer != nil && inner != nil {
		C.leafenTabsShowView(outer, inner, C.int(stripHeight()), C.bool(focus))
		tab.host = frame
	}
}

// focusTabView gives the shown tab's page the keyboard back after a press on
// the strip took it.
func focusTabView(frame *tabFrame, tab *appTab) {
	if tab.window == nil {
		return
	}
	if outer, inner := frame.window.NativeWindow(), tab.window.NativeWindow(); outer != nil && inner != nil {
		C.leafenTabsFocusView(outer, inner)
	}
}

// releaseTabView takes a tab's web view out of its frame, back into the
// tab's own window, before that window closes or the tab moves.
func releaseTabView(window *application.WebviewWindow) {
	if native := window.NativeWindow(); native != nil {
		C.leafenTabsReleaseView(native)
	}
}

// wakeTabPage keeps a tab's page in view to WebKit as it starts, under the
// shown tab's page, which covers it (window_tabs_start.go).
func wakeTabPage(window *application.WebviewWindow) {
	if native := window.NativeWindow(); native != nil {
		C.leafenTabsStarting(native, true)
	}
}

// restTabPage ends that: a page not shown is hidden again.
func restTabPage(window *application.WebviewWindow) {
	if native := window.NativeWindow(); native != nil {
		C.leafenTabsStarting(native, false)
	}
}

// keepTabWindowHidden keeps a tab's own window from ever showing — Wails
// shows every hidden window when the Dock icon is pressed with none on
// screen; the tab shows in its frame instead.
func keepTabWindowHidden(window *application.WebviewWindow) {
	if native := window.NativeWindow(); native != nil {
		C.leafenTabsKeepHidden(native)
	}
}

// tabHistory says whether the tab's page can go back and forward.
func tabHistory(window *application.WebviewWindow) (bool, bool) {
	native := window.NativeWindow()
	if native == nil {
		return false, false
	}
	can := int(C.leafenTabsHistory(native))
	return can&1 != 0, can&2 != 0
}

// goTabHistory goes back or forward in the tab's page.
func goTabHistory(window *application.WebviewWindow, forward bool) {
	if native := window.NativeWindow(); native != nil {
		C.leafenTabsGo(native, C.bool(forward))
	}
}

// tabHistoryList is the tab's page's history one way: back from the newest
// entry, or forward from the nearest.
func tabHistoryList(window *application.WebviewWindow, forward bool) []historyEntry {
	native := window.NativeWindow()
	if native == nil {
		return nil
	}
	text := C.leafenTabsHistoryList(native, C.bool(forward))
	defer C.free(unsafe.Pointer(text))
	var entries []historyEntry
	if json.Unmarshal([]byte(C.GoString(text)), &entries) != nil {
		return nil
	}
	return entries
}

// setTabZoom gives a page — a tab's, or a frame's strip — a zoom as Notion's
// app zooms them (window_tabs_zoom.go), by WebKit's page zoom: the page lays
// itself out again at that size, below 1 too, where Wails' own magnifies it
// as a pinch does and keeps it at 1 or more.
func setTabZoom(window *application.WebviewWindow, zoom float64) {
	if native := window.NativeWindow(); native != nil {
		C.leafenTabsSetZoom(native, C.double(zoom))
	}
}

// frameContentSize is the frame's content's size in points.
func frameContentSize(frame *application.WebviewWindow) (int, int) {
	var width, height C.int
	if native := frame.NativeWindow(); native != nil {
		C.leafenTabsContentSize(native, &width, &height)
	}
	return int(width), int(height)
}
