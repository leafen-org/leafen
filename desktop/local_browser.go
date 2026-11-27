package main

import (
	"errors"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// deviceCodeShape is how a device code shows (internal/app/account_device_codes.go).
var deviceCodeShape = regexp.MustCompile(`^[0-9A-Z]{4}-[0-9A-Z]{4}$`)

// LocalBrowser is the app's "Open in a browser" (the multi-server decision,
// 2026-10-20): this computer's browser opens the app's own server at its
// address here, on the page of a one-time device code the window made,
// which signs that browser in as a device of the window's account.
type LocalBrowser struct {
	local *localServer
}

// OpenDevicePage opens the app's own server's page for a device code in the
// system's browser. Only such a page opens.
func (browser *LocalBrowser) OpenDevicePage(code string) error {
	if !deviceCodeShape.MatchString(code) {
		return errors.New("only a device code's page opens")
	}
	_, err, address, _ := browser.local.current()
	if err != nil || address == "" {
		return errors.New("Leafen's own server isn't running")
	}
	return application.Get().Browser.OpenURL(strings.TrimSuffix(address, "/") + "/device/" + code)
}
