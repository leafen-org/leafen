package main

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The window opens at this size, or smaller on a screen that can't hold it.
const windowWidth, windowHeight = 1280, 820

// windowFit keeps the window inside the work area of the screen it opens on.
// Windows opens a window taller than the work area with its title bar above
// the screen, where it can't be moved or closed (a 1280×800 screen,
// 2026-10-02). The app knows its screens when its services start, just before
// it makes the window.
type windowFit struct{ window *application.WebviewWindow }

func (fit *windowFit) ServiceStartup(context.Context, application.ServiceOptions) error {
	screen := application.Get().Screen.GetPrimary()
	if fit.window == nil || screen == nil || screen.WorkArea.Width <= 0 || screen.WorkArea.Height <= 0 {
		return nil
	}
	if area := screen.WorkArea; area.Width < windowWidth || area.Height < windowHeight {
		fit.window.SetSize(min(windowWidth, area.Width), min(windowHeight, area.Height))
	}
	return nil
}

// appWindowOptions are an app window's, opening on the page at path: the
// first window's, and each one a page asks to open in a new window
// (new_window.go).
func appWindowOptions(path string) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "Leafen",
		Width:            windowWidth,
		Height:           windowHeight,
		URL:              path,
		BackgroundColour: lastWindowBackground(),
		// A video's own full-screen control, as in a browser: WebKit leaves it out
		// unless the page may go full screen (seen 2026-11-05: the player had none).
		Mac: application.MacWindow{
			WebviewPreferences: application.MacWebviewPreferences{
				FullscreenEnabled: application.Enabled,
				// Tab goes to every button, box and link, as in Chrome, and ⌥Tab
				// to text fields alone: WebKit's own default, under the system's
				// keyboard navigation turned off, gives Tab to text fields only
				// (seen 2026-11-15: a screen of buttons took no Tab at all).
				TabFocusesLinks:             application.Enabled,
				ApplicationNameForUserAgent: safariUserAgentName(),
				// A video site's player starts after its press has passed, which a
				// WebKit view holding media for a gesture refuses: YouTube stayed on
				// its picture (seen 2026-11-05). A browser lets a page play once used.
				EnableAutoplayWithoutUserAction: application.Enabled,
			},
		},
	}
}
