//go:build leafen_devweb

package web

import (
	"fmt"
	"os"
	"path/filepath"
)

// For development only. A server built with the tag leafen_devweb serves the
// web build from the folder LEAFEN_DEV_WEB_DIST names (a tree's web/dist)
// instead of the one it embeds, reading each file when it is asked for: a
// change to the web app shows after a fresh `vite build` and a reload, with
// no Go build and no server restart. The third-party notices still come from
// the edition's own embedded file (embed.go).
//
// Release builds are made without the tag: this file is not compiled into
// them, so they serve only what they embed and never read the variable.
// Time presses and run the full checked build with a release build; a
// development build differs in where its files come from, and it stops a
// start over a table the product export has no class for, which a release
// build only logs (internal/app/schema_migration_export_classes.go).

const devWebDistVariable = "LEAFEN_DEV_WEB_DIST"

func init() {
	folder := os.Getenv(devWebDistVariable)
	if folder == "" {
		fmt.Fprintf(os.Stderr, "leafen: development web build: %s is not set, serving the embedded web build\n", devWebDistVariable)
		return
	}
	absolute, err := filepath.Abs(folder)
	if err == nil {
		_, err = os.Stat(filepath.Join(absolute, "index.html"))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "leafen: development web build: %s=%s holds no web build: %v\n", devWebDistVariable, folder, err)
		os.Exit(2)
	}
	diskDist = os.DirFS(absolute)
	fmt.Fprintf(os.Stderr, "leafen: development web build: serving web files from %s\n", absolute)
}
