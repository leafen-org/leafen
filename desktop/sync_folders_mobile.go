//go:build ios || android

package main

// SyncFolder is a cloud drive's folder on a computer; a phone reaches its
// drives through the folder picker instead.
type SyncFolder struct {
	Service string `json:"service"`
	Account string `json:"account"`
	Path    string `json:"path"`
}

func findSyncFolders() []SyncFolder { return nil }
