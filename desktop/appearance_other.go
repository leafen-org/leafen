//go:build !ios

package main

// Elsewhere the system draws the status bar, if any.
func setStatusBarText(bool) {}
