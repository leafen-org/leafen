//go:build !windows

package main

// Off Windows (packaged_windows.go) only the Mac App Store edition comes in
// a store's package, which the store updates; the others update themselves
// where they are installed.
func packagedApp() bool { return appStoreEdition }
