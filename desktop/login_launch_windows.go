//go:build windows

package main

import (
	"os"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Windows opens the app at login with "--background"
// (background_windows.go), its windows made hidden.
const windowWaitsForLaunch = false

func watchLoginLaunch() {}

func openedAtLogin() bool { return len(os.Args) > 1 && os.Args[1] == "--background" }

func showUnlessOpenedAtLogin(*application.WebviewWindow) {}

// windowsOptions keep a start behind its windows out of the front until a
// window of the app's shows, as a Mac's is kept (login_launch_darwin.go).
// Wails activates each window it makes as it centers it, and puts in front a
// window given the focus (WM_SETFOCUS), which WebView2 gives the window it
// starts in; so the app opened at sign-in put windows no one could see in
// front of what the person was doing, and their keys went to them. While it
// holds, the app's windows are made unable to be activated and a hidden one
// takes the focus without coming forward; the first window shown — from the
// tray, a link, a second start — ends the hold, and the app comes forward
// with it as it would have.
func windowsOptions() application.WindowsOptions {
	held := openedAtLogin()
	var made []w32.HWND
	release := func() {
		held = false
		for _, window := range made {
			if w32.IsWindow(window) {
				w32.SetWindowLong(window, w32.GWL_EXSTYLE, uint32(w32.GetWindowLong(window, w32.GWL_EXSTYLE))&^w32.WS_EX_NOACTIVATE)
			}
		}
		made = nil
	}
	return application.WindowsOptions{
		WndProcInterceptor: func(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
			if !held {
				return 0, false
			}
			window := w32.HWND(hwnd)
			switch msg {
			case w32.WM_NCCREATE:
				// A window of the app's own: not the tray's (a message-only
				// window) nor the hidden one Wails runs its calls through.
				if create := *(**w32.CREATESTRUCT)(unsafe.Pointer(&lParam)); create.Parent == 0 && create.Style&w32.WS_DISABLED == 0 {
					w32.SetWindowLong(window, w32.GWL_EXSTYLE, uint32(w32.GetWindowLong(window, w32.GWL_EXSTYLE))|w32.WS_EX_NOACTIVATE)
					made = append(made, window)
				}
			case w32.WM_SHOWWINDOW:
				if wParam != 0 {
					release()
				}
			case w32.WM_WINDOWPOSCHANGING:
				if position := *(**w32.WINDOWPOS)(unsafe.Pointer(&lParam)); position.Flags&w32.SWP_SHOWWINDOW != 0 {
					release()
				}
			case w32.WM_SETFOCUS:
				if !w32.IsWindowVisible(window) {
					return 0, true
				}
			}
			return 0, false
		},
	}
}
