//go:build (darwin && !ios) || windows

package main

import (
	"errors"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Command search, as Notion's app's (its QuickSearchController, tray and
// shortcut setting, 7.43.0, read 2026-11-13): a key for the whole computer —
// ⇧⌘K on a Mac, Ctrl+Shift+K on Windows, or the one chosen in 설정 › 이
// 컴퓨터 — and a press of the app's icon in the menu bar or tray. With one of
// the app's windows in front it opens that window's search; otherwise the
// app's own search window comes up over the app in use
// (quick_search_desktop.go). The key is the system's to give: a Carbon hot
// key on a Mac (command_search_darwin.go), RegisterHotKey on Windows
// (command_search_windows.go) — neither asks the person for a permission.

// openSearchEvent asks a window's page to open its search
// (web/src/lib/desktopMenuCommands.ts).
const openSearchEvent = "leafen:open-search"

// errCommandSearchKeyTaken says the system gave the key to another app.
var errCommandSearchKeyTaken = errors.New("command search key taken")

// hotKey is a key with its modifiers, written as Notion writes its shortcut
// setting: "shift+command+k" on a Mac, "shift+ctrl+k" on Windows.
type hotKey struct {
	key                             string
	shift, control, option, command bool
}

// accelerator is the key as a menu shows it.
func (key hotKey) accelerator() string {
	parts := []string{}
	if key.control {
		parts = append(parts, "ctrl")
	}
	if key.option {
		parts = append(parts, "option")
	}
	if key.shift {
		parts = append(parts, "shift")
	}
	if key.command {
		parts = append(parts, "cmd")
	}
	return strings.Join(append(parts, key.key), "+")
}

// defaultCommandSearchKey is Notion's Shift+CommandOrControl+K.
func defaultCommandSearchKey() string {
	if runtime.GOOS == "darwin" {
		return "shift+command+k"
	}
	return "shift+ctrl+k"
}

// parseHotKey reads a written key; a key needs Option, Control or Command on
// a Mac, Control or Alt on Windows, as Notion's setting asks.
func parseHotKey(text string) (hotKey, bool) {
	var key hotKey
	for _, part := range strings.Split(strings.ToLower(strings.TrimSpace(text)), "+") {
		switch part {
		case "shift":
			key.shift = true
		case "control", "ctrl":
			key.control = true
		case "option", "alt":
			key.option = true
		case "command", "meta", "cmd":
			key.command = true
		case "":
			return hotKey{}, false
		default:
			if key.key != "" {
				return hotKey{}, false
			}
			key.key = part
		}
	}
	if key.key == "" || !(key.control || key.option || (key.command && runtime.GOOS == "darwin")) {
		return hotKey{}, false
	}
	if _, ok := hotKeyCodes[key.key]; !ok {
		return hotKey{}, false
	}
	return key, true
}

// commandSearchKeyText is the key the settings hold, or the default.
func commandSearchKeyText(settings backgroundSettings) string {
	if settings.CommandSearchShortcut != "" {
		return settings.CommandSearchShortcut
	}
	return defaultCommandSearchKey()
}

// takeCommandSearchKey takes the key for the whole computer, or gives it back
// when off; errCommandSearchKeyTaken when the system refused it.
func takeCommandSearchKey(on bool, text string) error {
	if !on {
		setCommandSearchKey(hotKey{}, false)
		return nil
	}
	key, ok := parseHotKey(text)
	if !ok {
		return errors.New("command search key not usable")
	}
	if !setCommandSearchKey(key, true) {
		return errCommandSearchKeyTaken
	}
	return nil
}

// commandSearchPressed is the key or the icon pressed: one of the app's
// windows in front opens its own search (Notion: openSearchModalInNotion),
// else the app's search window comes up or goes away.
func commandSearchPressed(fromIcon bool) {
	onMain(func() {
		if !fromIcon {
			if frame, tab := desktopTabs.front(); frame != nil && tab != nil && tab.window != nil && frame.window.IsFocused() {
				application.Get().Event.Emit(openSearchEvent, tab.window.Name())
				return
			}
		}
		quickSearch.toggle()
	})
}
