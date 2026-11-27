//go:build windows

package main

import (
	"reflect"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// The Windows app's windows hold tabs as the Mac app's do (window_tabs.go),
// as Notion's Windows app's do (7.42.3, the same tab code as its Mac app's):
// a window is a frame whose own web view is the strip of tabs along its
// top, in its title bar, with the window's own buttons beside it
// (window_tabs_windows_frame.go); each tab is a whole page of the app in a
// WebView2 of its own, made in a window that never shows, and shown in the
// frame under the strip. A WebView2 moves to another window by WebView2's
// own way (its controller's ParentWindow), and is placed and shown there.
// Each runs on the main thread, where the tabs live.

const (
	// stripPlatform tells the strip whose window it is in (tab_strip.js).
	stripPlatform = "windows"
	// pageKeepsHistory: WebView2 lists no history to the app, so a tab's
	// page lists its own (web/src/lib/desktopTabPage.ts).
	pageKeepsHistory = true
)

// Places in the COM method tables of WebView2.h.
const (
	controllerPutIsVisible        = 4
	controllerPutBounds           = 6
	controllerMoveFocus           = 12
	controllerGetParentWindow     = 21
	controllerPutParentWindow     = 22
	controllerNotifyParentChanged = 23
	webViewGetCanGoBack           = 38
	webViewGetCanGoForward        = 39
	webViewGoBack                 = 40
	webViewGoForward              = 41
)

// frameOptions are a frame's: an app window whose page is the strip, its
// title bar given to the strip (window_tabs_windows_frame.go), and the
// app's keys (app_menu_windows.go).
func frameOptions() application.WebviewWindowOptions {
	options := appWindowOptions(stripPage)
	options.Hidden = true
	options.KeyBindings = tabKeyBindings()
	return options
}

// tabWindowOptions are a tab's own window's, which never shows, nor in the
// taskbar; its page's keys are the app's.
func tabWindowOptions(path string) application.WebviewWindowOptions {
	options := appWindowOptions(path)
	options.Hidden = true
	options.Windows.HiddenOnTaskbar = true
	options.KeyBindings = tabKeyBindings()
	return options
}

// watchFrame calls ready once the frame's strip page has loaded, and
// fullscreen as the frame enters or leaves full screen.
func watchFrame(window *application.WebviewWindow, ready func(), fullscreen func(bool)) {
	window.RegisterHook(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) { ready() })
	window.OnWindowEvent(events.Windows.WindowFullscreen, func(*application.WindowEvent) { fullscreen(true) })
	window.OnWindowEvent(events.Windows.WindowUnFullscreen, func(*application.WindowEvent) { fullscreen(false) })
}

// adoptFrameView gives the frame's title bar to the strip and the window's
// buttons (window_tabs_windows_frame.go); it says how far in the strip's
// first button starts, Notion's 12.
func adoptFrameView(frame *application.WebviewWindow) int {
	takeTitleBar(frame)
	// Again at the zoom's strip height (window_tabs_zoom.go): the strip, the
	// pages under it and the buttons, as tall as Notion's titleBarOverlay.
	if hwnd := hwndOf(frame); hwnd != 0 {
		placeFrameViews(hwnd)
		paintFrameChrome(frame)
	}
	return 12
}

// wailsChromiumPart finds a part of a window's WebView2 where Wails keeps
// it, unexported (v3.0.0-beta.27 to v3.1.0: WebviewWindow.impl.chromium.<field>), as
// Wails offers no way to it, with its holder (the chromium, made anew with
// a new WebView2); nil when the window has none yet or Wails keeps it
// elsewhere. new_window_windows.go finds the web view this way too.
func wailsChromiumPart(window *application.WebviewWindow, field, kind string) (holder unsafe.Pointer, part *comObject) {
	if window == nil {
		return nil, nil
	}
	value := reflect.ValueOf(window).Elem().FieldByName("impl")
	for _, name := range []string{"chromium", field} {
		if value.Kind() == reflect.Interface {
			value = value.Elem()
		}
		if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
			return nil, nil
		}
		holder = value.UnsafePointer()
		value = value.Elem().FieldByName(name)
	}
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Type().String() != kind {
		return nil, nil
	}
	return holder, (*comObject)(value.UnsafePointer())
}

// controllerOf is a window's WebView2 controller, which places it.
func controllerOf(window *application.WebviewWindow) *comObject {
	_, controller := wailsChromiumPart(window, "controller", "*edge.ICoreWebView2Controller")
	return controller
}

func (object *comObject) call(place int, args ...uintptr) uintptr {
	result, _, _ := syscall.SyscallN(object.method(place), append([]uintptr{uintptr(unsafe.Pointer(object))}, args...)...)
	return result
}

// putBounds places a WebView2 in its window: a RECT goes by value, in two
// registers on arm64 and by its address on amd64 (as Wails' own SetSize).
func putBounds(controller *comObject, rect w32.RECT) {
	if runtime.GOARCH == "arm64" {
		words := (*[2]uintptr)(unsafe.Pointer(&rect))
		controller.call(controllerPutBounds, words[0], words[1])
		return
	}
	controller.call(controllerPutBounds, uintptr(unsafe.Pointer(&rect)))
}

func hwndOf(window *application.WebviewWindow) w32.HWND {
	if window == nil {
		return 0
	}
	return w32.HWND(uintptr(window.NativeWindow()))
}

// dpiScale is the window's pixels per CSS pixel.
func dpiScale(hwnd w32.HWND) float64 {
	if dpi := w32.GetDpiForWindow(hwnd); dpi > 0 {
		return float64(dpi) / 96
	}
	return 1
}

func scaled(hwnd w32.HWND, length int) int32 {
	return int32(float64(length)*dpiScale(hwnd) + 0.5)
}

// tabViewRect is where a tab's page shows in its frame: under the strip.
func tabViewRect(frame w32.HWND) w32.RECT {
	rect := w32.RECT{}
	if client := w32.GetClientRect(frame); client != nil {
		rect = *client
	}
	rect.Top = min(scaled(frame, stripHeight()), rect.Bottom)
	return rect
}

// attachTabView puts a tab's WebView2 in its frame under the strip unless it
// is there, and says whether it is.
func attachTabView(frame *tabFrame, tab *appTab) bool {
	controller, host := controllerOf(tab.window), hwndOf(frame.window)
	if controller == nil || host == 0 {
		return false
	}
	var parent uintptr
	controller.call(controllerGetParentWindow, uintptr(unsafe.Pointer(&parent)))
	if parent != uintptr(host) {
		controller.call(controllerPutParentWindow, uintptr(host))
	}
	putBounds(controller, tabViewRect(host))
	tab.host = frame
	return true
}

func showTabWebView(tab *appTab, shown bool) {
	if controller := controllerOf(tab.window); controller != nil {
		on := uintptr(0)
		if shown {
			on = 1
		}
		controller.call(controllerPutIsVisible, on)
	}
}

// placeFrame puts a frame where it was kept, maximized if it was, or where
// a tab dragged out of another window was let go.
func placeFrame(frame *application.WebviewWindow, kept *savedFrame) {
	hwnd := hwndOf(frame)
	if hwnd == 0 {
		return
	}
	if box := kept.Frame; box != nil && box[2] >= 200 && box[3] >= 200 {
		rect := w32.RECT{Left: int32(box[0]), Top: int32(box[1]), Right: int32(box[0] + box[2]), Bottom: int32(box[1] + box[3])}
		if w32.MonitorFromRect(&rect, w32.MONITOR_DEFAULTTONULL) != 0 {
			placement := w32.WINDOWPLACEMENT{RcNormalPosition: rect, ShowCmd: w32.SW_HIDE}
			placement.Length = uint32(unsafe.Sizeof(placement))
			visible := w32.IsWindowVisible(hwnd)
			if visible {
				placement.ShowCmd = w32.SW_SHOWNORMAL
				if kept.Maximized {
					placement.ShowCmd = w32.SW_SHOWMAXIMIZED
				}
			}
			w32.SetWindowPlacement(hwnd, &placement)
			if !visible && kept.Maximized {
				maximizeOnShow(hwnd)
			}
		}
	}
	if at := kept.At; at != nil {
		// A tab dragged out opens in a window the size of the one it left,
		// its corner where it was let go, as Notion 7.44.1's Windows app
		// opens one (measured at two drops). The page's screen point is in
		// CSS pixels; the window's left edge lies the resizing border
		// before what shows.
		scale := dpiScale(hwnd)
		x, y := int(float64(at[0])*scale)-int(resizeBorder(hwnd)), int(float64(at[1])*scale)
		flags := uint(w32.SWP_NOZORDER | w32.SWP_NOACTIVATE)
		width, height := 0, 0
		if from := hwndOf(kept.From); from != 0 && !w32.IsZoomed(from) {
			rect := w32.GetWindowRect(from)
			width, height = int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
		} else {
			flags |= w32.SWP_NOSIZE
		}
		w32.SetWindowPos(hwnd, 0, x, y, width, height, flags)
	}
}

// frameRect is the frame's place when not maximized, to keep.
func frameRect(frame *application.WebviewWindow) *[4]float64 {
	hwnd := hwndOf(frame)
	if hwnd == 0 || frame.IsFullscreen() {
		return nil
	}
	placement := w32.WINDOWPLACEMENT{}
	placement.Length = uint32(unsafe.Sizeof(placement))
	if !w32.GetWindowPlacement(hwnd, &placement) {
		return nil
	}
	rect := placement.RcNormalPosition
	return &[4]float64{float64(rect.Left), float64(rect.Top), float64(rect.Right - rect.Left), float64(rect.Bottom - rect.Top)}
}

// stripPointIn says whether a screen point a strip gave (CSS pixels) is
// over a frame's strip, and where along it.
func stripPointIn(frame *application.WebviewWindow, px, py int) (int, bool) {
	hwnd := hwndOf(frame)
	if hwnd == 0 || !w32.IsWindowVisible(hwnd) {
		return 0, false
	}
	scale := dpiScale(hwnd)
	x, y := int(float64(px)*scale), int(float64(py)*scale)
	left, top := w32.ClientToScreen(hwnd, 0, 0)
	client := w32.GetClientRect(hwnd)
	if client == nil || x < left || x >= left+int(client.Right) || y < top || y >= top+int(scaled(hwnd, stripHeight())) {
		return 0, false
	}
	// Pixels along the window, as the zoomed strip's CSS pixels.
	return int(float64(x-left) / scale / desktopTabs.zoomFactor()), true
}

// frameMaximized says whether the frame is maximized, to keep.
func frameMaximized(frame *application.WebviewWindow) bool {
	hwnd := hwndOf(frame)
	return hwnd != 0 && w32.IsZoomed(hwnd)
}

// addTabView puts a tab's web view in its frame, behind the shown one.
func addTabView(frame *tabFrame, tab *appTab) {
	if attachTabView(frame, tab) {
		showTabWebView(tab, false)
	}
}

// showTabView shows a tab's web view in its frame, the frame's other tabs
// behind it, with the keyboard when focus says.
func showTabView(frame *tabFrame, tab *appTab, focus bool) {
	if !attachTabView(frame, tab) {
		return
	}
	showTabWebView(tab, true)
	for _, other := range desktopTabs.allTabs() {
		if other != tab && other.host == frame && other.window != nil {
			showTabWebView(other, false)
		}
	}
	if focus {
		focusTabView(frame, tab)
	}
}

// focusTabView gives the shown tab's page the keyboard, unless the page
// showing in the frame holds it already: handed again, its page would blur
// and focus again, as the strip's would (focusStrip).
func focusTabView(frame *tabFrame, tab *appTab) {
	if tab.window == nil || tab.host != frame {
		return
	}
	if hwnd := hwndOf(frame.window); hwnd != 0 {
		if view := viewWithKeys(hwnd); view != 0 && !isStripView(hwnd, view) && w32.IsWindowVisible(view) {
			return
		}
	}
	if controller := controllerOf(tab.window); controller != nil {
		controller.call(controllerMoveFocus, 0)
	}
}

// releaseTabView takes a tab's web view out of its frame, back into the
// tab's own window, before that window closes or the tab moves.
func releaseTabView(window *application.WebviewWindow) {
	if controller, own := controllerOf(window), hwndOf(window); controller != nil && own != 0 {
		controller.call(controllerPutIsVisible, 0)
		controller.call(controllerPutParentWindow, uintptr(own))
	}
}

// wakeTabPage and restTabPage: a tab's page not shown starts as Chrome's
// tabs behind do in WebView2 (window_tabs_start.go; not measured on Windows).
func wakeTabPage(*application.WebviewWindow) {}

func restTabPage(*application.WebviewWindow) {}

// keepTabWindowHidden: a tab's own window never shows (it is made hidden and
// off the taskbar). Wails hides a hidden window's WebView2 once its first
// page has loaded, and makes a new one after its browser process ends: each
// load puts the tab's page back in its frame, shown if it is the shown tab.
func keepTabWindowHidden(window *application.WebviewWindow) {
	window.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
		onMain(func() {
			for _, tab := range desktopTabs.allTabs() {
				if tab.window == window && tab.host != nil && containsFrame(desktopTabs.frames, tab.host) {
					frame := tab.host
					if attachTabView(frame, tab) {
						showTabWebView(tab, frame.active == tab)
					}
				}
			}
		})
	})
}

// tabHistory says whether the tab's page can go back and forward.
func tabHistory(window *application.WebviewWindow) (bool, bool) {
	_, webView := webView2Of(window)
	if webView == nil {
		return false, false
	}
	var back, forward int32
	webView.call(webViewGetCanGoBack, uintptr(unsafe.Pointer(&back)))
	webView.call(webViewGetCanGoForward, uintptr(unsafe.Pointer(&forward)))
	return back != 0, forward != 0
}

// goTabHistory goes back or forward in the tab's page.
func goTabHistory(window *application.WebviewWindow, forward bool) {
	if _, webView := webView2Of(window); webView != nil {
		if forward {
			webView.call(webViewGoForward)
		} else {
			webView.call(webViewGoBack)
		}
	}
}

// tabHistoryList: WebView2 lists no history (pageKeepsHistory).
func tabHistoryList(*application.WebviewWindow, bool) []historyEntry { return nil }
