//go:build ios || android

package main

import "errors"

// A phone's app keeps its data where no file manager shows it.
const dataFolderOpens = false

func openDataFolder(string) error {
	return errors.New("a phone doesn't show the app's data folder")
}
