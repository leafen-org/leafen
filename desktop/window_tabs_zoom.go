//go:build (darwin && !ios) || windows

package main

import "math"

// The pages' zoom and the strip's height, as Notion 7.44.1's app has them:
// View › Zoom In and Zoom Out step every page's zoom by 0.1 between 0.5 and
// 4.9 and Actual Size takes it back to 1 (its appController.zoomIn, zoomOut
// and ELECTRON_ZOOM_*), and its tab bar zooms with the pages: 40 high at 1,
// 40 × the zoom rounded up else (its virtualTabBarHeightSelector), its
// traffic lights centered in it and its Windows buttons as high (measured in
// its Windows app 2026-11-24: 36 at 0.9, 32 at 0.8, 48 at 1.2).

const (
	stripBaseHeight = 40
	zoomStepSize    = 0.1
	zoomLeast       = 0.5
	zoomMost        = 4.9
)

// nextZoom is the zoom a step in (change > 0), out (< 0) or back (0) leads to,
// kept to tenths.
func nextZoom(now float64, change int) float64 {
	if now <= 0 {
		now = 1
	}
	switch {
	case change > 0:
		now += zoomStepSize
	case change < 0:
		now -= zoomStepSize
	default:
		return 1
	}
	return math.Round(min(zoomMost, max(zoomLeast, now))*10) / 10
}

// stripHeightAt is the strip's height in points at a zoom.
func stripHeightAt(zoom float64) int {
	if zoom <= 0 {
		zoom = 1
	}
	return int(math.Ceil(stripBaseHeight*zoom - 1e-6))
}

// zoomFactor is the pages' zoom, 1 until one was asked.
func (t *windowTabs) zoomFactor() float64 {
	if t.zoom <= 0 {
		return 1
	}
	return t.zoom
}

// stripHeight is the strip's height in points (pixels at 96 dpi on
// Windows) at the pages' zoom.
func stripHeight() int { return stripHeightAt(desktopTabs.zoomFactor()) }

// stripCSSHeight is the strip's height in its own and the pages' CSS pixels,
// which the zoom scales alike.
func stripCSSHeight() float64 { return float64(stripHeight()) / desktopTabs.zoomFactor() }

// tabsZoom zooms every tab's page and every window's strip in, out or back
// to their size alike, as Notion's app zooms all its pages and tab bars, and
// sets each window's strip and pages again at the strip's new height.
func tabsZoom(change int) {
	tabsAsk(func(t *windowTabs, _ *tabFrame, _ *appTab) {
		t.zoom = nextZoom(t.zoomFactor(), change)
		for _, each := range t.allTabs() {
			if each.window != nil {
				setTabZoom(each.window, t.zoom)
			}
		}
		for _, frame := range t.frames {
			t.zoomStrip(frame)
		}
	})
}

// zoomStrip gives a frame's strip the pages' zoom and its height, and puts
// its traffic lights or buttons and its shown page under it again.
func (t *windowTabs) zoomStrip(frame *tabFrame) {
	if !frame.adopted || frame.window == nil {
		return
	}
	setTabZoom(frame.window, t.zoomFactor())
	frame.lights = adoptFrameView(frame.window)
	if tab := frame.active; tab != nil && tab.window != nil && tab.host == frame {
		showTabView(frame, tab, false)
	}
	t.drawStrip(frame)
}
