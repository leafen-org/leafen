//go:build darwin && !ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#include <stdbool.h>
#include <stdlib.h>

bool leafenKeepCopiedHTML(const char *text, const char *html);
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Pasteboard is what the Mac app's page asks of the general pasteboard after
// a copy (web/src/lib/desktopClipboard.ts): its own HTML put back in place
// of WebKit's re-serialized one (pasteboard_darwin.m).
type Pasteboard struct{}

// KeepCopiedHTML puts the HTML the page copied with text back on the
// pasteboard, while it still holds that copy; it says whether it did.
func (*Pasteboard) KeepCopiedHTML(text, html string) bool {
	return application.InvokeSyncWithResult(func() bool {
		plain := C.CString(text)
		defer C.free(unsafe.Pointer(plain))
		page := C.CString(html)
		defer C.free(unsafe.Pointer(page))
		return bool(C.leafenKeepCopiedHTML(plain, page))
	})
}
