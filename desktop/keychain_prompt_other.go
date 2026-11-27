//go:build !darwin || ios

package main

// Only a Mac's keychain stops a call for a prompt the window can't see.
func systemPromptShowing() bool { return false }

func readWebKitKey() {}
