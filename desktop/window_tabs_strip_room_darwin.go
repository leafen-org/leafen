//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework WebKit

#include <stdbool.h>

void leafenTabsGrowStrip(void *frame, bool grow, int stripHeight);
void leafenTabsStripHoles(void *frame, double *holes, int holeCount, double *solids, int solidCount);
void leafenTabsFocusStrip(void *frame);
bool leafenTabsStripPointer(void *frame, double *x, double *y);
*/
import "C"

import "math"

// growStrip grows the frame's strip over its whole window while a menu it
// draws is open, or puts it back along the top: see-through where it draws
// nothing, the pages showing under it (tab_strip_menu.js). A Mac view is
// drawn as it shows, so the menus' places matter not here for drawing; the
// holes it leaves to the page's icon picker over a group's menu let presses
// there through to the page, and a tab's menu lets through every press
// outside the strip's bar and the menus (window_tabs_strip_room_darwin.m).
func growStrip(frame *tabFrame, grow bool, room stripRoom) {
	native := frame.window.NativeWindow()
	if native == nil {
		return
	}
	C.leafenTabsGrowStrip(native, C.bool(grow), C.int(stripHeight()))
	var holes, solids []C.double
	if grow {
		holes = pointRects(room.holes)
		if room.through {
			// The bar, as wide as any window.
			bar := stripRect{Right: 1 << 20, Bottom: int(math.Ceil(stripCSSHeight()))}
			solids = pointRects(append([]stripRect{bar}, room.rects...))
		}
	}
	C.leafenTabsStripHoles(native, firstOf(holes), C.int(len(holes)/4), firstOf(solids), C.int(len(solids)/4))
}

// pointRects are the strip's places (its CSS pixels) as x, y, width and
// height in points: the CSS pixels times the zoom.
func pointRects(rects []stripRect) []C.double {
	zoom := desktopTabs.zoomFactor()
	values := make([]C.double, 0, 4*len(rects))
	for _, rect := range rects {
		values = append(values, C.double(float64(rect.Left)*zoom), C.double(float64(rect.Top)*zoom),
			C.double(float64(rect.Right-rect.Left)*zoom), C.double(float64(rect.Bottom-rect.Top)*zoom))
	}
	return values
}

func firstOf(values []C.double) *C.double {
	if len(values) == 0 {
		return nil
	}
	return &values[0]
}

// focusStrip gives the frame's strip the keys, for a group's menu's name.
func focusStrip(frame *tabFrame) {
	if native := frame.window.NativeWindow(); native != nil {
		C.leafenTabsFocusStrip(native)
	}
}

// stripPointer is where the pointer is on the frame's strip, in its CSS
// pixels from its top left, while it is over the window.
func stripPointer(frame *tabFrame) (float64, float64, bool) {
	native := frame.window.NativeWindow()
	if native == nil {
		return 0, 0, false
	}
	var x, y C.double
	if !C.leafenTabsStripPointer(native, &x, &y) {
		return 0, 0, false
	}
	zoom := desktopTabs.zoomFactor()
	return float64(x) / zoom, float64(y) / zoom, true
}
