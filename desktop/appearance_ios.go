//go:build ios

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// On a dark page the status bar's text is light, on a light one dark.
func setStatusBarText(dark bool) {
	if dark {
		application.IOS.SetStatusBar(`{"style":"light"}`)
		return
	}
	application.IOS.SetStatusBar(`{"style":"dark"}`)
}
