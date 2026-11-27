//go:build !windows

package main

// Elsewhere the system shows why the app couldn't open its window.
func reportFatal(error) {}
