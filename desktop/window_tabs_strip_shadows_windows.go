//go:build windows

package main

// The soft shadow of the strip's menus on Windows, as Notion 7.44.1's tab
// bar's menus cast theirs over its page (measured in its Windows app
// 2026-11-24). The strip grown over the window keeps only its bar and the
// menus' places (window_tabs_strip_room_windows.go): a WebView2 hosted in a
// window of its own, as Wails hosts it, covers what is under it and cannot
// blend a shadow over another. So the strip casts the shadow where it draws
// — over its bar — and the shown page, under the menus, casts the rest
// from the same boxes in its own colors and shadow
// (web/src/components/desktop/DesktopStripMenus.tsx): seen together, the
// menus' shadow as the Mac's, where the strip blends over the page itself.

// stripShadowsEvent gives the page under the strip's menus their boxes, in
// the strip's CSS pixels, and the strip's height in them.
const stripShadowsEvent = "leafen:strip-shadows"

// tellMenuShadows has the page under the frame's strip menus cast their
// shadow (none once they close, as the page hears of it).
func tellMenuShadows(frame *tabFrame, boxes []stripBox) {
	page := frame.active
	if frame.menu != nil && frame.menu.page != nil {
		page = frame.menu.page
	}
	if page == nil || page.window == nil {
		return
	}
	if boxes == nil {
		boxes = []stripBox{}
	}
	page.window.EmitEvent(stripShadowsEvent, map[string]any{"window": page.window.Name(), "boxes": boxes, "strip": stripCSSHeight()})
}
