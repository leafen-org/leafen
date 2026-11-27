//go:build (darwin && !ios) || windows

package main

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const dataFolderOpens = true

// openDataFolder shows a folder in Finder, Explorer or the file manager.
func openDataFolder(dir string) error {
	if app := application.Get(); app != nil {
		return app.Env.OpenFileManager(dir, false)
	}
	return errors.New("the app isn't running")
}
