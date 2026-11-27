//go:build ios || android

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// A phone has neither a background to serve in nor a proxy setting.
func windowServices() []application.Service { return nil }

// A phone app doesn't stay behind its window.
func startBackground(*application.App, *application.WebviewWindow, *localServer, func() bool) {}
