//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework WebKit -framework UniformTypeIdentifiers

#include <stdbool.h>

void leafenPrepareWebViews(void);
void leafenTextMenuLanguage(bool korean);
void leafenToggleInspector(void *window);
void leafenShowConsole(void *window);
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

// The app's web views on macOS do what a browser does and Wails' WKWebView
// doesn't (v3.0.0-beta.27 to v3.1.0): a page's window.print() opens the system's print
// panel, a press after typing reaches the page as one click, and a press
// still allows what it allows — a video's full screen — after the app has
// run a script in the page, and a misspelt word is underlined while typing
// is never corrected, as in Notion's app, and a page and each of its frames
// draw as they load (web_view_darwin.m); a right press opens Notion's app's
// short menu (text_menu_darwin.m); a file input's open panel dims what its
// accept list doesn't take (open_panel_darwin.m). All are set
// once, before the first window is made, for every window and pop-up the
// app opens.
func init() {
	C.leafenPrepareWebViews()
}

// textMenuLanguage puts a page's right-press menu in the window's language.
func textMenuLanguage(korean bool) {
	C.leafenTextMenuLanguage(C.bool(korean))
}

// toggleDevTools opens a page in the web inspector, or closes it there.
func toggleDevTools(window *application.WebviewWindow) {
	if native := window.NativeWindow(); native != nil {
		C.leafenToggleInspector(native)
	}
}

// openConsole opens a page in the web inspector on its console (트러블슈팅 ›
// 콘솔 열기).
func openConsole(window *application.WebviewWindow) {
	if native := window.NativeWindow(); native != nil {
		C.leafenShowConsole(native)
	}
}
