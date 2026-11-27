//go:build !darwin || ios

package main

import (
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Windows opens the app at login with "--background"
// (background_windows.go); a phone has no login item.
const windowWaitsForLaunch = false

func watchLoginLaunch() {}

func openedAtLogin() bool { return len(os.Args) > 1 && os.Args[1] == "--background" }

func showUnlessOpenedAtLogin(*application.WebviewWindow) {}
