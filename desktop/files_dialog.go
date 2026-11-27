//go:build !ios && !android

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// chooseSavePath asks where the file goes with the system's save dialog.
func chooseSavePath(name string) (string, error) {
	return application.Get().Dialog.SaveFile().
		SetFilename(name).
		CanCreateDirectories(true).
		PromptForSingleSelection()
}

// finishSave has nothing more to do: the file is where the person chose.
func finishSave(string) error { return nil }

// chooseFolder asks for a folder with the system's open dialog.
func chooseFolder(title string) (string, error) {
	return application.Get().Dialog.OpenFile().
		SetTitle(title).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true).
		PromptForSingleSelection()
}
