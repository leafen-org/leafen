//go:build (!darwin || ios) && !windows

package main

import (
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// A phone has no login item.
const windowWaitsForLaunch = false

func watchLoginLaunch() {}

func openedAtLogin() bool { return len(os.Args) > 1 && os.Args[1] == "--background" }

func showUnlessOpenedAtLogin(*application.WebviewWindow) {}

func windowsOptions() application.WindowsOptions { return application.WindowsOptions{} }
