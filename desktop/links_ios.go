//go:build ios

package main

import "C"

// leafenLinkOpened takes a link iOS opened the app with, from the scene
// delegate (build/ios/main.m).
//
//export leafenLinkOpened
func leafenLinkOpened(link *C.char) {
	links.opened(C.GoString(link))
}
