package main

import "errors"

// FolderChooser asks for a folder with the system's dialog, as the backup
// settings of the app's own server take one: that server runs on this
// computer, so a folder here is one it can write.
type FolderChooser struct{}

var errNoFolderDialog = errors.New("this device offers no folder dialog to the app")

func newFolderChooser() *FolderChooser { return &FolderChooser{} }

// Choose answers the folder picked, or "" when the person cancelled.
func (*FolderChooser) Choose(title string) (string, error) {
	return chooseFolder(title)
}

// SyncFolders answers the cloud drives' folders on this computer, offered
// first as places for backups.
func (*FolderChooser) SyncFolders() []SyncFolder {
	return findSyncFolders()
}
