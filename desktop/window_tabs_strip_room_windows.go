//go:build windows

package main

import (
	"math"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

// The strip grown over the window while a menu it draws is open
// (tab_strip_menu.js), as Notion's tab bar's menus stand over its page: the
// strip's web view takes the window's whole client in front of the pages,
// and its window region keeps only the strip and the menus drawn, but the
// holes it leaves to the page's icon picker over a group's menu, so the
// pages show, and take presses, everywhere else — a web view on Windows
// covers what is under it, see-through or not. The menus' soft shadow past
// their ring, which the region cuts off, the shown page draws under them
// (window_tabs_strip_shadows_windows.go).

var (
	gdi32               = windows.NewLazySystemDLL("gdi32.dll")
	procCreateRectRgn   = gdi32.NewProc("CreateRectRgn")
	procCreateRoundRgn  = gdi32.NewProc("CreateRoundRectRgn")
	procCombineRgn      = gdi32.NewProc("CombineRgn")
	procDeleteObject    = gdi32.NewProc("DeleteObject")
	procSetWindowRgn    = windows.NewLazySystemDLL("user32.dll").NewProc("SetWindowRgn")
	stripRooms          = map[w32.HWND]stripRoom{}
	stripViews          = map[w32.HWND]w32.HWND{}
	menuCornerCSSPixels = 10
	procWindowFromPoint = user32.NewProc("WindowFromPoint")
	procGetAncestor     = user32.NewProc("GetAncestor")
)

const (
	regionOr   = 2
	regionDiff = 4
	gaParent   = 1
	gaRoot     = 2
)

// growStrip grows the frame's strip over its window, clipped to the strip
// and the menus' places less their holes (CSS pixels of the strip), or
// puts it back.
func growStrip(frame *tabFrame, grow bool, room stripRoom) {
	hwnd := hwndOf(frame.window)
	if hwnd == 0 {
		return
	}
	if grow {
		stripRooms[hwnd] = room
	} else {
		delete(stripRooms, hwnd)
	}
	placeFrameViews(hwnd)
	if grow {
		tellMenuShadows(frame, room.shadows)
	}
}

// stripBounds is where the strip's web view goes in the frame: along the
// title bar but the buttons' room, or over the whole client while it draws
// a menu, its region set first so nothing of the pages is covered meanwhile.
func stripBounds(hwnd w32.HWND, client *w32.RECT) w32.RECT {
	band := w32.RECT{Right: max(0, client.Right-scaled(hwnd, captionWidth)), Bottom: scaled(hwnd, stripHeight())}
	room, grown := stripRooms[hwnd]
	view := stripView(hwnd, band)
	if view == 0 {
		return band
	}
	if !grown {
		procSetWindowRgn.Call(uintptr(view), 0, 1)
		return band
	}
	region := newRegion(band)
	scale := dpiScale(hwnd) * desktopTabs.zoomFactor()
	corner := int32(float64(menuCornerCSSPixels*2) * scale)
	for _, rect := range room.rects {
		// A pixel round each menu for its ring.
		part, _, _ := procCreateRoundRgn.Call(uintptr(int32(float64(rect.Left-1)*scale)), uintptr(int32(float64(rect.Top-1)*scale)),
			uintptr(int32(float64(rect.Right+1)*scale)+1), uintptr(int32(float64(rect.Bottom+1)*scale)+1), uintptr(corner), uintptr(corner))
		if part != 0 {
			procCombineRgn.Call(region, region, part, regionOr)
			procDeleteObject.Call(part)
		}
	}
	for _, hole := range room.holes {
		part := newRegion(w32.RECT{Left: int32(float64(hole.Left) * scale), Top: int32(float64(hole.Top) * scale),
			Right: int32(math.Ceil(float64(hole.Right) * scale)), Bottom: int32(math.Ceil(float64(hole.Bottom) * scale))})
		if part != 0 {
			procCombineRgn.Call(region, region, part, regionDiff)
			procDeleteObject.Call(part)
		}
	}
	// The system keeps the region from here on.
	procSetWindowRgn.Call(uintptr(view), region, 1)
	w32.SetWindowPos(view, 0, 0, 0, 0, 0, w32.SWP_NOMOVE|w32.SWP_NOSIZE|w32.SWP_NOACTIVATE)
	return *client
}

func newRegion(rect w32.RECT) uintptr {
	region, _, _ := procCreateRectRgn.Call(uintptr(rect.Left), uintptr(rect.Top), uintptr(rect.Right), uintptr(rect.Bottom))
	return region
}

// stripView is the window the strip's WebView2 draws in: the frame's child
// at its client's top left, the pages' standing under the strip; found
// once, while the strip is along the top.
func stripView(hwnd w32.HWND, band w32.RECT) w32.HWND {
	if view := stripViews[hwnd]; view != 0 && w32.IsWindow(view) {
		return view
	}
	left, top := w32.ClientToScreen(hwnd, 0, 0)
	for child := w32.GetWindow(hwnd, w32.GW_CHILD); child != 0; child = w32.GetWindow(child, w32.GW_HWNDNEXT) {
		rect := w32.GetWindowRect(child)
		if rect == nil || !w32.IsWindowVisible(child) || int(rect.Left) != left || int(rect.Top) != top {
			continue
		}
		if rect.Bottom-rect.Top == band.Bottom {
			stripViews[hwnd] = child
			return child
		}
	}
	return 0
}

// focusStrip gives the frame's strip the keys, for a group's menu's name or
// the ring a menu closing gives back (window_tabs_strip_keys.go), unless it
// holds them, as a Mac strip is given them (window_tabs_strip_room_darwin.m):
// a WebView2 handed the keys it holds blurs its page and focuses it again,
// and that blur, reaching the strip after the ring it was told to draw,
// took the ring away (tab_strip_ring.js) — measured in the Windows VM
// 2026-11-24: a blur and a focus 10–25 ms after each Escape closing a tab's
// menu, and a ring drawn before the blur gone.
func focusStrip(frame *tabFrame) {
	if hwnd := hwndOf(frame.window); hwnd != 0 && isStripView(hwnd, viewWithKeys(hwnd)) {
		return
	}
	if controller := controllerOf(frame.window); controller != nil {
		controller.call(controllerMoveFocus, 0)
	}
}

// isStripView says whether a child of the frame is the strip's web view:
// the one stripView found, or else the one along the client's top, where
// the pages' never are — they stand under the strip, or have no height in
// a window with none. Known so before a menu of the strip has grown it and
// stripView looked for it: the keys a frame takes as it is focused go to
// the strip's web view (Wails gives its own web view the keys at
// WM_SETFOCUS), and were taken for a page's holding them, so they stayed on
// the strip after the app started and at every focus till a menu of the
// strip opened (the Windows VM 2026-11-24).
func isStripView(hwnd, view w32.HWND) bool {
	if view == 0 {
		return false
	}
	if known := stripViews[hwnd]; known != 0 && w32.IsWindow(known) {
		return view == known
	}
	rect := w32.GetWindowRect(view)
	_, top := w32.ClientToScreen(hwnd, 0, 0)
	return rect != nil && int(rect.Top) == top && rect.Bottom > rect.Top
}

// viewWithKeys is the frame's child the keys are in — the strip's web view
// or a tab's page's — or 0 when they are in none of them.
func viewWithKeys(hwnd w32.HWND) w32.HWND {
	for at := w32.GetFocus(); at != 0; {
		parent, _, _ := procGetAncestor.Call(uintptr(at), gaParent)
		if w32.HWND(parent) == hwnd {
			return at
		}
		at = w32.HWND(parent)
	}
	return 0
}

// frameContentSize is the frame's client's size in pixels at 96 dpi.
func frameContentSize(frame *application.WebviewWindow) (int, int) {
	hwnd := hwndOf(frame)
	client := w32.GetClientRect(hwnd)
	if hwnd == 0 || client == nil {
		return 0, 0
	}
	scale := dpiScale(hwnd)
	return int(float64(client.Right) / scale), int(float64(client.Bottom) / scale)
}

// stripPointer is where the pointer is on the frame's strip — at its
// client's top left — in its CSS pixels, while it is over the window and
// not over another one in front of it.
func stripPointer(frame *tabFrame) (float64, float64, bool) {
	hwnd := hwndOf(frame.window)
	x, y, ok := w32.GetCursorPos()
	if hwnd == 0 || !ok {
		return 0, 0, false
	}
	under, _, _ := procWindowFromPoint.Call(uintptr(uint32(int32(x))) | uintptr(uint32(int32(y)))<<32)
	if root, _, _ := procGetAncestor.Call(under, gaRoot); w32.HWND(root) != hwnd {
		return 0, 0, false
	}
	left, top, ok := w32.ScreenToClient(hwnd, x, y)
	if !ok {
		return 0, 0, false
	}
	scale := dpiScale(hwnd) * desktopTabs.zoomFactor()
	return float64(left) / scale, float64(top) / scale, true
}
