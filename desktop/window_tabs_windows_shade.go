//go:build windows

package main

import "github.com/wailsapp/wails/v3/pkg/w32"

// shadedCaption lays the shade a window over the frame's shown page lays on
// it (web/src/lib/desktopTabShade.ts) over the ground and line under the
// frame's buttons too, their marks staying as they are, as Notion 7.44.1's
// Windows app shades its title bar overlay (measured 2026-11-24: #111111
// under its import window, the marks still #ada9a3).
func shadedCaption(hwnd w32.HWND, bar, border rgb) (rgb, rgb) {
	chrome := frameChromes[hwnd]
	if chrome == nil {
		return bar, border
	}
	for _, frame := range desktopTabs.frames {
		if frame.window == chrome.window && frame.active != nil && frame.active.shade != "" {
			return cssColorOn(frame.active.shade, bar, bar), cssColorOn(frame.active.shade, border, border)
		}
	}
	return bar, border
}
