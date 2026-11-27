//go:build android

package main

import "errors"

// Android's window saves through the app's own Downloads store
// (LeafenFiles.java).
func chooseSavePath(string) (string, error) {
	return "", errors.New("the app's own Downloads store saves files here")
}

func finishSave(string) error { return nil }
