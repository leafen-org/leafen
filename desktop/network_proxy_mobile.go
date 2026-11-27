//go:build ios || android

package main

// A phone app goes out as its system lets it; it has no proxy setting.
func applyAppProxy() error { return nil }
