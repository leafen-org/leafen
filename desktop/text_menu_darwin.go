//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework WebKit

#include <stdlib.h>

char *leafenTextMenuSpelling(const char *context, long at, long length);
void leafenTextMenuLearn(const char *word);
void leafenTextMenuEdit(void *window, int action);
void leafenTextMenuLookUp(void *window, const char *word, double x, double y, double size);
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// TextMenu is what the Mac app's page asks of the app for the right press's
// menu it draws itself (web/src/lib/desktopTextMenu.ts), as Notion's app's
// page asks Electron: a word's spelling, the dictionary, the web view's own
// cut, copy, paste and spelling switch, and the Mac's look-up
// (text_menu_darwin.m). Each runs on the main thread.
type TextMenu struct{}

// TextMenuSpelling is a word's spelling: whether spelling is checked as one
// types, whether the word is misspelt, and its corrections.
type TextMenuSpelling struct {
	Checking bool     `json:"checking"`
	Misspelt bool     `json:"misspelt"`
	Guesses  []string `json:"guesses"`
}

// Spelling says whether spelling is checked and whether the word at
// [at, at+length) of text (UTF-16 units) is misspelt there, and what it may
// be instead.
func (*TextMenu) Spelling(text string, at, length int) TextMenuSpelling {
	answer := application.InvokeSyncWithResult(func() string {
		whole := C.CString(text)
		defer C.free(unsafe.Pointer(whole))
		found := C.leafenTextMenuSpelling(whole, C.long(at), C.long(length))
		defer C.free(unsafe.Pointer(found))
		return C.GoString(found)
	})
	var spelling TextMenuSpelling
	_ = json.Unmarshal([]byte(answer), &spelling)
	if spelling.Guesses == nil {
		spelling.Guesses = []string{}
	}
	return spelling
}

// Learn adds a word to the Mac's dictionary ("사전에 추가").
func (*TextMenu) Learn(word string) {
	application.InvokeSync(func() {
		text := C.CString(word)
		defer C.free(unsafe.Pointer(text))
		C.leafenTextMenuLearn(text)
	})
}

var textMenuEdits = map[string]C.int{"cut": 0, "copy": 1, "paste": 2, "spelling": 3, "selectAll": 4}

// Edit cuts, copies or pastes in the asking page as ⌘X ⌘C ⌘V do, turns
// spelling on or off ("spelling"), or chooses all as WebKit does where the
// focus is ("selectAll": the menu's 모두 선택 for a ⌘A the page leaves).
func (*TextMenu) Edit(ctx context.Context, action string) error {
	edit, known := textMenuEdits[action]
	window, _ := ctx.Value(application.WindowKey).(application.Window)
	if !known || window == nil {
		return errors.New("no such edit, or no window asked")
	}
	application.InvokeSync(func() {
		if native := window.NativeWindow(); native != nil {
			C.leafenTextMenuEdit(native, edit)
		}
	})
	return nil
}

// LookUp shows the Mac's look-up for a word whose text starts at x, y on its
// baseline in the asking page, in its font size.
func (*TextMenu) LookUp(ctx context.Context, word string, x, y, size float64) {
	window, _ := ctx.Value(application.WindowKey).(application.Window)
	if window == nil || word == "" {
		return
	}
	application.InvokeSync(func() {
		native := window.NativeWindow()
		if native == nil {
			return
		}
		text := C.CString(word)
		defer C.free(unsafe.Pointer(text))
		C.leafenTextMenuLookUp(native, text, C.double(x), C.double(y), C.double(size))
	})
}

// textMenuServices: the page's menu, and its copies' HTML
// (pasteboard_darwin.go), ask the Mac app alone.
func textMenuServices() []application.Service {
	return []application.Service{application.NewService(&TextMenu{}), application.NewService(&Pasteboard{})}
}
