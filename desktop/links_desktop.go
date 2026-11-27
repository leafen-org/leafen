//go:build (darwin && !ios) || windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// singleInstance keeps one app running for its data: starting it again —
// as the system does on Windows to open a leafen: link — hands the new
// start's link to the running app and brings its window forward. A
// development build pointed at other data is its own app.
func singleInstance() *application.SingleInstanceOptions {
	id := signInReturnScheme
	if dir := os.Getenv(localDataDirEnvironment); dir != "" {
		digest := sha256.Sum256([]byte(dir))
		id += ".dev-" + hex.EncodeToString(digest[:6])
	}
	return &application.SingleInstanceOptions{
		UniqueID: id,
		OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
			opened := false
			for _, arg := range data.Args {
				if strings.Contains(arg, "://") {
					links.opened(arg)
					opened = true
				}
			}
			if window := links.window; !opened && !showFront() && window != nil {
				window.Show()
				window.Focus()
			}
		},
	}
}
