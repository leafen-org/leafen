//go:build (darwin && !ios) || windows

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// windowServices are what only a computer's window calls: the background it
// keeps serving in, the network proxy setting, what opens its new windows
// and tabs (new_window.go), the command search's window
// (quick_search_desktop.go), and what the Mac page's right-press menu and
// its copies ask (text_menu_darwin.go).
func windowServices() []application.Service {
	return append([]application.Service{
		application.NewService(desktopBackground),
		application.NewService(desktopNetworkProxy),
		application.NewService(&WindowOpener{}),
		application.NewService(&QuickSearch{}),
	}, textMenuServices()...)
}

// desktopBackground keeps the app running while this computer serves
// (background_desktop.go).
var desktopBackground = &Background{}

// desktopNetworkProxy is the proxy setting the window changes.
var desktopNetworkProxy = &NetworkProxy{}

// startBackground gives the background its window and server.
func startBackground(desktop *application.App, window *application.WebviewWindow, local *localServer, hidden func() bool) {
	desktopBackground.start(desktop, window, local, hidden)
}
