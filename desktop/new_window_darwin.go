//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework WebKit

#include <stdbool.h>

void leafenRouteNewWindows(void *window);
bool leafenWindowOpen(void *window);
*/
import "C"

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// WebKit asks the window's delegate for a new window — window.open, a link
// meant for a new tab, in the page or any frame in it — and drops the
// request when the delegate has no answer, as Wails' has none (v3.0.0-beta.27
// to v3.1.0): links and "새 탭에서 열기" opened nothing (2026-11-08). The
// window's delegate is given the answer once its page has loaded, and the
// answer is the app's rule (new_window.go, new_window_darwin.m). A tab's page
// may be in its frame by then, so its web view is the one its window keeps.
func routeNewWindows(window *application.WebviewWindow) {
	var first sync.Once
	window.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func(*application.WindowEvent) {
		first.Do(func() {
			if native := window.NativeWindow(); native != nil {
				C.leafenRouteNewWindows(native)
			}
		})
	})
}

// windowOpen says whether an app window is open: ordered in on screen,
// however covered, or minimized to the Dock. Wails' IsVisible says whether
// any of it shows, which a window behind another app's doesn't.
func windowOpen(window application.Window) bool {
	native := window.NativeWindow()
	return native != nil && bool(C.leafenWindowOpen(native))
}

// leafenNewWindowKind tells the window what an address is (newWindowKindOf).
//
//export leafenNewWindowKind
func leafenNewWindowKind(address *C.char) C.int {
	kind, _ := newWindowKindOf(C.GoString(address))
	return C.int(kind)
}

// leafenOpenNewWindow opens an address outside the app or one of its pages —
// in a tab of the window in front, shown at once when active says — after
// the window's main thread has gone on, and tells what it is.
//
//export leafenOpenNewWindow
func leafenOpenNewWindow(address *C.char, active C.int) C.int {
	text := C.GoString(address)
	kind, _ := newWindowKindOf(text)
	if kind == newWindowOutside || kind == newWindowAppPage {
		go func() { _ = openNewWindowAs(text, nil, active != 0, false) }()
	}
	return C.int(kind)
}
