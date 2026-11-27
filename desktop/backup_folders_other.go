//go:build !ios && !android

package main

// A computer's backup folder is a path the app's own server writes; there
// is nothing more to open.
func registerDeviceBackupFolders() {}
