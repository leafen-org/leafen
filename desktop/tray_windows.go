//go:build windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// setTaskbarIcon gives the tray the icon for the taskbar's color — dark on a
// light taskbar, light on a dark one — and gives it again when Windows' mode
// changes. The taskbar and its overflow follow Windows' mode
// (SystemUsesLightTheme), not the apps' mode. Wails (beta.27 to 3.1.0) keeps a light
// and a dark icon of its own, but on a tray already running (SystemTray.New
// runs it at once) SetIcon and SetDarkModeIcon each copy the new icon into
// the other while the two share one, so whichever came second stood for both:
// a light taskbar showed the white icon (2026-11-21, the test VM's overflow).
// Given one icon at a time, Wails' own swap has nothing to swap.
func setTaskbarIcon(desktop *application.App, tray *application.SystemTray) {
	give := func() {
		if w32.IsSystemCurrentlyDarkMode() {
			tray.SetIcon(trayDarkTaskbar)
		} else {
			tray.SetIcon(trayLightTaskbar)
		}
	}
	give()
	desktop.Event.OnApplicationEvent(events.Windows.SystemThemeChanged, func(*application.ApplicationEvent) { give() })
}
