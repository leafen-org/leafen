//go:build ios

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework UIKit -framework UniformTypeIdentifiers -framework Foundation
#include <stdlib.h>

void leafenChooseBackupFolder(const char *identifier);
char *leafenOpenBackupFolder(const char *bookmark);
void leafenCloseBackupFolder(const char *path);
*/
import "C"

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"leafen.org/leafen/internal/app"
)

// On iOS a backup folder is picked in the Files picker and kept as a
// security-scoped bookmark: the folder's name is "ios-folder:" with its
// shown name and the bookmark, and a backup opens the bookmark for the
// folder's path while it runs.

const iosFolderPrefix = "ios-folder:"

var (
	iosFolderChoices sync.Map // ID → chan string
	iosFolderCount   atomic.Int64
)

// chooseFolder shows the Files picker for a folder and answers its name,
// or "" when the person cancelled.
func chooseFolder(string) (string, error) {
	id := strconv.FormatInt(iosFolderCount.Add(1), 10)
	chosen := make(chan string, 1)
	iosFolderChoices.Store(id, chosen)
	defer iosFolderChoices.Delete(id)
	identifier := C.CString(id)
	defer C.free(unsafe.Pointer(identifier))
	C.leafenChooseBackupFolder(identifier)
	return <-chosen, nil
}

//export leafenBackupFolderChosen
func leafenBackupFolderChosen(identifier *C.char, name *C.char, bookmark *C.char) {
	waiting, ok := iosFolderChoices.Load(C.GoString(identifier))
	if !ok {
		return
	}
	folder := ""
	if mark := C.GoString(bookmark); mark != "" {
		folder = iosFolderPrefix + url.QueryEscape(C.GoString(name)) + ":" + mark
	}
	waiting.(chan string) <- folder
}

func registerDeviceBackupFolders() {
	app.SetDeviceBackupFolders(func(name string) (app.DeviceBackupFolder, bool, error) {
		rest, ok := strings.CutPrefix(name, iosFolderPrefix)
		if !ok {
			return nil, false, nil
		}
		_, bookmark, found := strings.Cut(rest, ":")
		if !found || bookmark == "" {
			return nil, true, errors.New("the folder's bookmark is missing")
		}
		mark := C.CString(bookmark)
		defer C.free(unsafe.Pointer(mark))
		opened := C.leafenOpenBackupFolder(mark)
		if opened == nil {
			return nil, true, errors.New("the folder's bookmark no longer opens it")
		}
		path := C.GoString(opened)
		C.free(unsafe.Pointer(opened))
		return app.NewPathBackupFolder(path, func() {
			value := C.CString(path)
			defer C.free(unsafe.Pointer(value))
			C.leafenCloseBackupFolder(value)
		}), true, nil
	})
}
