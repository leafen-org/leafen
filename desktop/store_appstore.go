//go:build appstore

package main

// The Mac App Store edition (package-desktop.mjs --store), which the store
// updates: it carries no updater of its own, as the Microsoft Store's app
// (packaged_windows.go).
const appStoreEdition = true
