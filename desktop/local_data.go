package main

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// localDataDirEnvironment points a development build at other data than the
// person's own.
const localDataDirEnvironment = "LEAFEN_DESKTOP_DATA_DIR"

// localDataDir is where the app keeps its local workspace, made if it
// isn't there yet.
func localDataDir() (string, error) {
	dir, err := localDataPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// localDataPath is where the app keeps its local workspace: the person's
// own application data, closed to other users of the machine; on a phone,
// the app's own storage.
func localDataPath() (string, error) {
	if dir := os.Getenv(localDataDirEnvironment); dir != "" {
		return dir, nil
	}
	if storage := application.Mobile.StoragePath(); storage != "" {
		return filepath.Join(storage, "local"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Leafen", "local"), nil
}

const localProfilePath = "/leafen-local/profile"

// singlePageFiles serves the web build, and its page for any address that
// isn't one of its files, the page routing it itself. The page is served
// as itself too when asked for by name, as Android's window asks, where the
// file server would redirect.
func singlePageFiles(dist fs.FS) http.Handler {
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
		if info, err := fs.Stat(dist, name); err != nil || info.IsDir() || name == "index.html" {
			request = request.Clone(request.Context())
			request.URL.Path = "/"
		}
		files.ServeHTTP(response, request)
	})
}
