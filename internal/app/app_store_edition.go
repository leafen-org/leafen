//go:build appstore

package app

// The Mac App Store edition (the desktop's -tags appstore,
// desktop/store_appstore.go), whose own server leaves the app platform out:
// apps come with the app downloaded from Leafen.
const appStoreEdition = true
