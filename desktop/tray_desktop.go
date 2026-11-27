//go:build (darwin && !ios) || windows

package main

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// trayWanted is what the menu bar's or tray's icon is there for: this
// computer serving, or command search, as Notion's icon (TrayController,
// 7.43.0: shown on a Mac while command search and "메뉴 모음에 Notion 표시"
// are on, always on Windows, where it is the way back to an app kept in the
// background).
type trayWanted struct {
	serving, commandSearch, menuBarIcon bool
	// Windows' 시작 설정, as the settings are.
	openAtLogin, keepRunning bool
	label, language, key     string
}

// showTray puts the app's item in the menu bar or tray, or takes it away;
// it runs on the app's main thread. Its press and its menu are Notion's
// icon's (TrayController and buildTrayMenuTemplate, 7.43.0), in the
// window's language: what the app is doing while it serves, 새 창, the
// command search's 명령어 검색 토글 and 명령어 검색 단축키 변경, on a Mac 메뉴
// 모음에 리펜 표시, on Windows 시작 설정 ▸ and 트러블슈팅 ▸, and 리펜 종료.
func (background *Background) showTray(wanted trayWanted) {
	show := wanted.serving || (wanted.commandSearch && wanted.menuBarIcon) || runtime.GOOS == "windows"
	if !show {
		if background.tray != nil {
			background.tray.Destroy()
			background.tray, background.status, background.trayKey = nil, nil, ""
		}
		return
	}
	// One icon, its menu made anew when what it lists changes.
	key := fmt.Sprint(wanted.serving, wanted.commandSearch, wanted.menuBarIcon, wanted.language, wanted.key)
	if runtime.GOOS == "windows" {
		key += fmt.Sprint(wanted.openAtLogin, wanted.keepRunning)
	}
	if background.tray != nil && background.trayKey == key {
		if background.status != nil {
			background.status.SetLabel(wanted.label)
		}
		return
	}
	if background.tray == nil {
		// New puts the item in the menu bar or tray at once while the app
		// runs (Wails beta.27 to 3.1.0 runs it there). Running it again made a second,
		// empty item, and Destroy then took that one away and left the icon:
		// turned off, the icon stayed, and each time it was put back there
		// was one more (seen 2026-11-13).
		tray := background.app.SystemTray.New()
		if runtime.GOOS == "darwin" {
			// Notion's size, which Wails' own SetTemplateIcon does not keep.
			if !setMenuBarIcon(trayMenuBar) {
				tray.SetTemplateIcon(trayMenuBar)
			}
		} else {
			setTaskbarIcon(background.app, tray)
		}
		tray.SetTooltip("Leafen")
		background.tray = tray
	}
	background.tray.SetMenu(background.trayMenu(wanted))
	background.tray.OnClick(background.trayPress(wanted))
	background.trayKey = key
}

// trayPress is what pressing the icon does, as Notion's icon: the command
// search while that is on; else, on Windows, the app's window back (Notion's
// handleActivate), and on a Mac the menu — the icon there is up only to say
// the computer serves, as Notion's shows its menu while it shows a meeting.
// No handler leaves the press to the menu (Wails opens it then); the icon's
// right press opens the menu either way.
func (background *Background) trayPress(wanted trayWanted) func() {
	switch {
	case wanted.commandSearch:
		return func() { commandSearchPressed(true) }
	case runtime.GOOS == "windows":
		return background.Open
	}
	return nil
}

// trayMenu is the icon's menu for what it is there for, in Notion's order:
// 새 창 opens a new window as the menu bar's 파일 › 새 창 (a window kept in
// the background stays hidden, as in Notion's app).
func (background *Background) trayMenu(wanted trayWanted) *application.Menu {
	words := trayWords(wanted.language)
	menu := application.NewMenu()
	background.status = nil
	if wanted.serving {
		background.status = menu.Add(wanted.label).SetEnabled(false)
		menu.AddSeparator()
	}
	menu.Add(words.newWindow).OnClick(func(*application.Context) { openNewAppWindow(nil) })
	menu.AddSeparator()
	if wanted.commandSearch {
		toggle := menu.Add(words.toggle).OnClick(func(*application.Context) { commandSearchPressed(true) })
		if key, ok := parseHotKey(wanted.key); ok {
			toggle.SetAccelerator(key.accelerator())
		}
		menu.Add(words.changeKey).OnClick(func(*application.Context) { openCommandSearchSettings() })
		menu.AddSeparator()
		// The icon also stays while the computer serves, the setting off.
		if runtime.GOOS == "darwin" {
			menu.AddCheckbox(words.menuBar, wanted.menuBarIcon).OnClick(func(*application.Context) {
				background.change(func(settings *backgroundSettings) { settings.MenuBarIcon = !wanted.menuBarIcon })
			})
		}
	}
	if runtime.GOOS == "windows" {
		background.launchMenu(menu.AddSubmenu(words.launch), wanted, words)
		menu.AddSeparator()
		// Notion's tray carries its help menu's 트러블슈팅, the same items.
		troubleshooting := menu.AddSubmenu(words.troubleshooting)
		for _, item := range troubleshootingItems(strings.HasPrefix(wanted.language, "ko")) {
			troubleshooting.Add(item.label).OnClick(func(*application.Context) { item.do() })
		}
		menu.AddSeparator()
	}
	menu.Add(words.quit).OnClick(func(*application.Context) { background.app.Quit() })
	return menu
}

// launchMenu is Windows' 시작 설정, as Notion's tray has it: 로그인 시 리펜
// 열기 and 빠른 검색 활성화, then 백그라운드에서 계속 실행 or 즉시 표시 —
// whether closing the last window hides it or ends the app.
func (background *Background) launchMenu(launch *application.Menu, wanted trayWanted, words trayMenuWords) {
	launch.AddCheckbox(words.openAtLogin, wanted.openAtLogin).OnClick(func(*application.Context) {
		background.change(func(settings *backgroundSettings) { settings.OpenAtLogin = !wanted.openAtLogin })
	})
	launch.AddCheckbox(words.quickSearch, wanted.commandSearch).OnClick(func(*application.Context) {
		background.change(func(settings *backgroundSettings) { settings.CommandSearch = !wanted.commandSearch })
	})
	launch.AddSeparator()
	launch.AddRadio(words.keepInBackground, wanted.keepRunning).OnClick(func(*application.Context) {
		background.change(func(settings *backgroundSettings) { settings.KeepRunning = true })
	})
	launch.AddRadio(words.showImmediately, !wanted.keepRunning).OnClick(func(*application.Context) {
		background.change(func(settings *backgroundSettings) { settings.KeepRunning = false })
	})
}

// trayWords are the icon's menu's, in the window's language (Notion's
// menuBarIcon.menu.*, and its menu bar's 새 창 and 트러블슈팅).
type trayMenuWords struct {
	newWindow, toggle, changeKey, menuBar, quit                         string
	launch, openAtLogin, quickSearch, keepInBackground, showImmediately string
	troubleshooting                                                     string
}

func trayWords(language string) trayMenuWords {
	if strings.HasPrefix(language, "ko") {
		return trayMenuWords{"새 창", "명령어 검색 토글", "명령어 검색 단축키 변경", "메뉴 모음에 리펜 표시", "리펜 종료",
			"시작 설정", "로그인 시 리펜 열기", "빠른 검색 활성화", "백그라운드에서 계속 실행", "즉시 표시", "트러블슈팅"}
	}
	return trayMenuWords{"New Window", "Toggle Command Search", "Change Command Search Shortcut", "Show Leafen in menu bar", "Quit Leafen",
		"Launch Preferences", "Open Leafen at Login", "Enable Quick Search", "Keep in Background", "Show Immediately", "Troubleshooting"}
}

// change changes one of the settings from the icon's menu, as the window's
// settings do (Set), off the main thread.
func (background *Background) change(edit func(*backgroundSettings)) {
	background.mu.Lock()
	settings := background.settings
	background.mu.Unlock()
	edit(&settings)
	go func() { _, _ = background.Set(settings) }()
}
