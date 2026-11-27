//go:build !ios && !android

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// The folders a computer keeps in step with a cloud drive — Google Drive,
// iCloud Drive, Dropbox and OneDrive — found where each app puts them, so the
// backup settings offer them first (2026-10-17: Leafen keeps no backups; a
// backup goes to a place the person already has). Only where they are: the
// app talks to none of these services. Where each app puts them:
//   - macOS: Google Drive, Dropbox and OneDrive under ~/Library/CloudStorage
//     (GoogleDrive-<account> holding "My Drive", Dropbox[-…], OneDrive-…),
//     iCloud Drive at ~/Library/Mobile Documents/com~apple~CloudDocs.
//   - Windows: OneDrive at %OneDriveConsumer% and %OneDriveCommercial%,
//     Google Drive as a drive holding "My Drive", iCloud Drive in the user
//     folder ("iCloud Drive", "iCloudDrive" before version 14).
//   - Dropbox everywhere names its folders in info.json (~/.dropbox, or
//     %APPDATA%\Dropbox and %LOCALAPPDATA%\Dropbox).

// SyncFolder is a cloud drive's folder on this computer.
type SyncFolder struct {
	// Service is google-drive, icloud-drive, dropbox or onedrive.
	Service string `json:"service"`
	// Account tells one of a service's folders from another: the Google
	// account, the OneDrive organization, the Dropbox folder's own name.
	Account string `json:"account"`
	Path    string `json:"path"`
}

// googleDriveSharedFolders are the folders beside My Drive in a Google Drive,
// which a backup can't go into.
var googleDriveSharedFolders = []string{"Shared drives", "Other computers", "공유 드라이브", "다른 컴퓨터"}

// syncFoldersHomeEnvironment points a development build at another home to
// look in, as LEAFEN_DESKTOP_DATA_DIR points it at other data.
const syncFoldersHomeEnvironment = "LEAFEN_DESKTOP_SYNC_HOME"

func findSyncFolders() []SyncFolder {
	home := os.Getenv(syncFoldersHomeEnvironment)
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return nil
		}
	}
	return syncFoldersIn(runtime.GOOS, home, os.Getenv)
}

func syncFoldersIn(goos, home string, env func(string) string) []SyncFolder {
	found := []SyncFolder{}
	add := func(service, account, path string) {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return
		}
		path = filepath.Clean(path)
		if slices.ContainsFunc(found, func(folder SyncFolder) bool { return strings.EqualFold(folder.Path, path) }) {
			return
		}
		found = append(found, SyncFolder{Service: service, Account: account, Path: path})
	}
	switch goos {
	case "darwin":
		storage := filepath.Join(home, "Library", "CloudStorage")
		entries, _ := os.ReadDir(storage)
		for _, entry := range entries {
			name := entry.Name()
			switch {
			case strings.HasPrefix(name, "GoogleDrive-"):
				if drive := googleMyDrive(filepath.Join(storage, name)); drive != "" {
					add("google-drive", strings.TrimPrefix(name, "GoogleDrive-"), drive)
				}
			case name == "Dropbox" || strings.HasPrefix(name, "Dropbox-"):
				add("dropbox", strings.TrimPrefix(strings.TrimPrefix(name, "Dropbox"), "-"), filepath.Join(storage, name))
			case strings.HasPrefix(name, "OneDrive-") && !strings.HasPrefix(name, "OneDrive-SharedLibraries"):
				account := strings.TrimPrefix(name, "OneDrive-")
				if account == "Personal" {
					account = ""
				}
				add("onedrive", account, filepath.Join(storage, name))
			}
		}
		add("icloud-drive", "", filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs"))
	case "windows":
		if personal := env("OneDriveConsumer"); personal != "" {
			add("onedrive", "", personal)
		}
		if work := env("OneDriveCommercial"); work != "" {
			add("onedrive", strings.TrimSpace(strings.TrimPrefix(filepath.Base(work), "OneDrive -")), work)
		}
		for letter := 'C'; letter <= 'Z'; letter++ {
			if drive := googleMyDrive(string(letter) + `:\`); drive != "" {
				add("google-drive", "", drive)
			}
		}
		add("icloud-drive", "", filepath.Join(home, "iCloud Drive"))
		add("icloud-drive", "", filepath.Join(home, "iCloudDrive"))
	}
	dropboxInfo := []string{filepath.Join(home, ".dropbox", "info.json")}
	if goos == "windows" {
		dropboxInfo = []string{filepath.Join(env("APPDATA"), "Dropbox", "info.json"), filepath.Join(env("LOCALAPPDATA"), "Dropbox", "info.json")}
	}
	for _, path := range dropboxInfo {
		for _, folder := range dropboxFolders(path) {
			account := filepath.Base(folder)
			if account == "Dropbox" {
				account = ""
			}
			add("dropbox", account, folder)
		}
	}
	return found
}

// googleMyDrive is the My Drive folder in a Google Drive, or "": the folder
// named so, or the one folder beside the shared ones when the app names it
// in another language.
func googleMyDrive(drive string) string {
	entries, err := os.ReadDir(drive)
	if err != nil {
		return ""
	}
	others := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if name == "My Drive" {
			return filepath.Join(drive, name)
		}
		if entry.IsDir() && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "$") &&
			!slices.Contains(googleDriveSharedFolders, name) {
			others = append(others, name)
		}
	}
	// A plain disk has other folders too; only a Google Drive's root holds
	// one folder with the shared ones.
	if len(others) == 1 && len(entries) <= 4 && slices.ContainsFunc(entries, func(entry os.DirEntry) bool {
		return slices.Contains(googleDriveSharedFolders, entry.Name())
	}) {
		return filepath.Join(drive, others[0])
	}
	return ""
}

// dropboxFolders reads the folders Dropbox's info.json names, one for each
// account on this computer.
func dropboxFolders(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	accounts := map[string]struct {
		Path string `json:"path"`
	}{}
	if json.Unmarshal(data, &accounts) != nil {
		return nil
	}
	folders := []string{}
	for _, key := range []string{"personal", "business"} {
		if folder := accounts[key].Path; folder != "" {
			folders = append(folders, folder)
		}
	}
	return folders
}
