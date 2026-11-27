//go:build (darwin && !ios) || windows

package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// What the menu bar, the keyboard and the rest of the app ask of the tabs
// (app_menu_bar_darwin.go); each runs on the main thread.

// frontPage is the page the menu bar's commands are for: the shown tab of
// the window in front.
func frontPage() application.Window {
	return application.InvokeSyncWithResult(func() application.Window {
		if _, tab := desktopTabs.front(); tab != nil && tab.window != nil {
			return tab.window
		}
		return nil
	})
}

// showFront brings the window in front forward, its shown tab ready — after
// a link, a notification or the Dock asked for the app.
func showFront() bool {
	return application.InvokeSyncWithResult(func() bool {
		frame, _ := desktopTabs.front()
		if frame == nil {
			return false
		}
		// One whose strip isn't in place yet shows once it is (adopt).
		if !frame.adopted {
			frame.show = true
			return true
		}
		frame.window.Show()
		frame.window.Focus()
		return true
	})
}

// reopenWindows answers the Dock icon pressed with none of the app's windows
// on screen: its hidden windows come back — the last one closed while the
// app kept running in the background, or all of them after it opened at
// login — the one last in front focused; with none, a new one. A minimized
// one the system brings back itself. Only the windows: the command search's
// and the tabs' pages stay where they are.
func reopenWindows() {
	onMain(func() {
		t := desktopTabs
		front, _ := t.front()
		if front == nil {
			t.newFrame(&savedFrame{Tabs: []savedTab{{Path: "/"}}}, true)
			return
		}
		for _, frame := range t.frames {
			if frame.window != nil && frame.adopted && windowOpen(frame.window) {
				return
			}
		}
		for _, frame := range t.frames {
			if frame != front {
				showFrame(frame, false)
			}
		}
		showFrame(front, true)
	})
}

// showFrame shows a hidden window, once its strip is in place (adopt).
func showFrame(frame *tabFrame, focus bool) {
	if !frame.adopted {
		frame.show = true
		return
	}
	frame.window.Show()
	if focus {
		frame.window.Focus()
	}
}

// openAppPage opens one of the app's pages a page asked for in a new tab
// beside it (a ⌘-pressed link behind, with ⇧ in front), or at the end.
func openAppPage(path string, from application.Window, active, atEnd bool) {
	onMain(func() {
		t := desktopTabs
		frame, tab := t.frameOf(from)
		if frame == nil {
			frame, tab = t.front()
		}
		if frame == nil {
			t.newFrame(&savedFrame{Tabs: []savedTab{{Path: path}}}, true)
			return
		}
		t.openTab(frame, path, tab, active, atEnd)
	})
}

// openNewAppWindow opens a new window on the start page (파일 › 새 창).
func openNewAppWindow(application.Window) {
	onMain(func() { desktopTabs.newFrame(&savedFrame{Tabs: []savedTab{{Path: "/"}}}, true) })
}

// tabsAsk runs fn with the window in front and its shown tab.
func tabsAsk(fn func(t *windowTabs, frame *tabFrame, tab *appTab)) {
	onMain(func() {
		if frame, tab := desktopTabs.front(); frame != nil {
			fn(desktopTabs, frame, tab)
		}
	})
}

func tabsNew() { tabsAsk(func(t *windowTabs, frame *tabFrame, _ *appTab) { t.newTabSearch(frame) }) }

func tabsReopen() { onMain(desktopTabs.reopen) }

func tabsCloseTab() {
	tabsAsk(func(t *windowTabs, frame *tabFrame, tab *appTab) {
		if tab != nil {
			t.closeTabs(frame, []*appTab{tab})
		}
	})
}

func tabsCloseWindow() {
	tabsAsk(func(t *windowTabs, frame *tabFrame, _ *appTab) { t.closeAsked(frame) })
}

func tabsStep(by int) { tabsAsk(func(t *windowTabs, frame *tabFrame, _ *appTab) { t.step(frame, by) }) }

func tabsNumber(number int) {
	tabsAsk(func(t *windowTabs, frame *tabFrame, _ *appTab) { t.selectNumber(frame, number) })
}

// tabsQuitWithoutSaving ends the app keeping no tabs for its next start.
func tabsQuitWithoutSaving() {
	onMain(func() {
		desktopTabs.keepNone = true
		go application.Get().Quit()
	})
}

// tabsReload reloads the shown tab's page (보기 › 다시 불러오기).
func tabsReload(force bool) {
	tabsAsk(func(_ *windowTabs, _ *tabFrame, tab *appTab) {
		if tab == nil || tab.window == nil {
			return
		}
		if force {
			tab.window.ForceReload()
		} else {
			tab.window.Reload()
		}
	})
}

// tabsInspect opens the shown tab's page in the web inspector, or closes it
// there (보기 › 개발자 도구 토글, as Notion's app's).
func tabsInspect() {
	tabsAsk(func(_ *windowTabs, _ *tabFrame, tab *appTab) {
		if tab != nil && tab.window != nil {
			toggleDevTools(tab.window)
		}
	})
}
