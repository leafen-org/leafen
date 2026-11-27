//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Carbon

#include <stdbool.h>

bool leafenCommandSearchKey(bool on, int code, int modifiers);
void leafenBringAppForward(void);
void leafenCenterOnPointerScreen(void *window);
*/
import "C"

import (
	"log"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The Mac's command search key: a Carbon hot key the system gives the app
// while another app is in front (command_search_darwin.m); the app's search
// window centers on the screen under the pointer, as Notion's.

// hotKeyCodes are the keys' virtual key codes (Carbon's kVK_ANSI_… and the
// rest), by the names Notion's setting writes them with.
var hotKeyCodes = map[string]int{
	"a": 0x00, "s": 0x01, "d": 0x02, "f": 0x03, "h": 0x04, "g": 0x05, "z": 0x06, "x": 0x07,
	"c": 0x08, "v": 0x09, "b": 0x0B, "q": 0x0C, "w": 0x0D, "e": 0x0E, "r": 0x0F, "y": 0x10,
	"t": 0x11, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15, "6": 0x16, "5": 0x17, "=": 0x18,
	"9": 0x19, "7": 0x1A, "-": 0x1B, "8": 0x1C, "0": 0x1D, "]": 0x1E, "o": 0x1F, "u": 0x20,
	"[": 0x21, "i": 0x22, "p": 0x23, "l": 0x25, "j": 0x26, "'": 0x27, "k": 0x28, ";": 0x29,
	"\\": 0x2A, ",": 0x2B, "/": 0x2C, "n": 0x2D, "m": 0x2E, ".": 0x2F, "`": 0x32,
	"space": 0x31, "return": 0x24, "backspace": 0x33, "delete": 0x75,
	"left": 0x7B, "right": 0x7C, "down": 0x7D, "up": 0x7E,
	"f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76, "f5": 0x60, "f6": 0x61,
	"f7": 0x62, "f8": 0x64, "f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F,
}

// Carbon's modifier bits.
const (
	carbonCmdKey     = 0x0100
	carbonShiftKey   = 0x0200
	carbonOptionKey  = 0x0800
	carbonControlKey = 0x1000
)

// setCommandSearchKey takes the key for the app, or gives it back; false
// when the system refused it (another app holds it).
func setCommandSearchKey(key hotKey, on bool) bool {
	modifiers := 0
	if key.command {
		modifiers |= carbonCmdKey
	}
	if key.shift {
		modifiers |= carbonShiftKey
	}
	if key.option {
		modifiers |= carbonOptionKey
	}
	if key.control {
		modifiers |= carbonControlKey
	}
	taken := true
	application.InvokeSync(func() {
		taken = bool(C.leafenCommandSearchKey(C.bool(on), C.int(hotKeyCodes[key.key]), C.int(modifiers)))
	})
	if !taken {
		log.Printf("command search: the system refused %s", key.key)
	}
	return taken
}

//export leafenCommandSearchPressed
func leafenCommandSearchPressed() {
	go commandSearchPressed(false)
}

// bringAppForward puts the app in front of the one in use.
func bringAppForward() { C.leafenBringAppForward() }

// centerOnPointerScreen puts a window in the middle of the screen under the
// pointer.
func centerOnPointerScreen(native unsafe.Pointer) {
	if native != nil {
		C.leafenCenterOnPointerScreen(native)
	}
}
