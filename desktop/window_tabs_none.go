//go:build ios || android

package main

import (
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Only the Mac and Windows apps' windows hold tabs (window_tabs.go); a phone
// shows one page.
const tabsInWindows = false

// firstWindow isn't asked for where windows hold no tabs.
func firstWindow(*application.App) *application.WebviewWindow { return nil }

// windowMessage: no page here speaks to the app this way.
func windowMessage(application.Window, string, *application.OriginInfo) {}

// frontPage has no tab to give: the window in front is the page's own.
func frontPage() application.Window { return nil }

// showFront has no tabs' window to show.
func showFront() bool { return false }

// tabServices: no tabs, so nothing for a tab's page to call.
func tabServices() []application.Service { return nil }

// tabStripFiles: no strip to serve.
func tabStripFiles() http.Handler { return nil }
