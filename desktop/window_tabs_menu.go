//go:build (darwin && !ios) || windows

package main

// menuEntry is an item of a menu the app shows itself — the Windows app's
// own (app_menu_windows.go): its words and keys, whether it is off, a
// submenu or a line between items; do runs as it is pressed.
type menuEntry struct {
	label     string
	keys      string
	off       bool
	separator bool
	sub       []menuEntry
	do        func()
}
