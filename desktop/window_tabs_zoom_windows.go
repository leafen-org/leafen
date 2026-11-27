//go:build windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// controllerPutZoomFactor is put_ZoomFactor's place in WebView2.h's
// ICoreWebView2Controller.
const controllerPutZoomFactor = 8

// setTabZoom gives a page — a tab's, or a frame's strip — a zoom through its
// WebView2 controller (putZoomFactor, by the processor's own way to pass it
// a double), the same factor for every window, as Wails' own keeps a
// frameless window's at 1 or more (v3.1.0; window_tabs_zoom.go has the
// steps); without one, Wails' way.
func setTabZoom(window *application.WebviewWindow, zoom float64) {
	if controller := controllerOf(window); controller != nil && putZoomFactor(controller, zoom) {
		return
	}
	window.SetZoom(zoom)
}
