//go:build (darwin && !ios) || windows

package main

import (
	_ "embed"
	"net/http"
)

// The strip's page and its scripts (tab_strip.html), which the app serves
// to its frames.

var (
	//go:embed tab_strip.html
	tabStripHTML []byte
	//go:embed tab_strip.js
	tabStripJS []byte
	//go:embed tab_strip_drag.js
	tabStripDragJS []byte
	//go:embed tab_strip_groups.js
	tabStripGroupsJS []byte
	//go:embed tab_strip_motion.js
	tabStripMotionJS []byte
	//go:embed tab_strip_menu.js
	tabStripMenuJS []byte
	//go:embed tab_strip_group_menu.js
	tabStripGroupMenuJS []byte
	//go:embed tab_strip_ring.js
	tabStripRingJS []byte
	//go:embed tab_strip_hover.js
	tabStripHoverJS []byte
)

// tabStripFiles serves the strip's page to the frames (server.go).
func tabStripFiles() http.Handler {
	files := map[string][2]any{
		stripPage:                               {"text/html; charset=utf-8", tabStripHTML},
		"/desktop-tabs/tab_strip.js":            {"text/javascript; charset=utf-8", tabStripJS},
		"/desktop-tabs/tab_strip_drag.js":       {"text/javascript; charset=utf-8", tabStripDragJS},
		"/desktop-tabs/tab_strip_groups.js":     {"text/javascript; charset=utf-8", tabStripGroupsJS},
		"/desktop-tabs/tab_strip_motion.js":     {"text/javascript; charset=utf-8", tabStripMotionJS},
		"/desktop-tabs/tab_strip_menu.js":       {"text/javascript; charset=utf-8", tabStripMenuJS},
		"/desktop-tabs/tab_strip_group_menu.js": {"text/javascript; charset=utf-8", tabStripGroupMenuJS},
		"/desktop-tabs/tab_strip_ring.js":       {"text/javascript; charset=utf-8", tabStripRingJS},
		"/desktop-tabs/tab_strip_hover.js":      {"text/javascript; charset=utf-8", tabStripHoverJS},
		// The app's mark for the Windows strip's app menu button, drawn as a
		// 16px mask: the menu bar's leaf (background_desktop.go).
		"/desktop-tabs/mark.png": {"image/png", trayMenuBar},
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		file, ok := files[request.URL.Path]
		if !ok {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", file[0].(string))
		response.Header().Set("Cache-Control", "no-store")
		_, _ = response.Write(file[1].([]byte))
	})
}
