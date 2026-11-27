//go:build ios || android

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// A phone's window opens no other window: there is no web view request to
// route (new_window.go is the computers').
func routeNewWindows(*application.WebviewWindow) {}
