//go:build windows

package main

import (
	"context"
	"net/http"
	"time"

	"leafen.org/leafen/internal/app"
)

// Windows updates only through the Microsoft Store (decided 2026-11-17).
// Leafen buys no Windows code signing certificate, so no check here could
// tell a program Leafen made from any other signed one, and the zip outside
// the store doesn't install a newer version by itself: when the list names
// one, the app says so on the band at the top of its window and in its menu's
// "업데이트 확인", and opens the release page, from which the person takes it
// from the store or as a new zip. The store's own app is updated by the
// store and reads no list (Updater.Run).

// updatesNoticeOnly: the Windows zip only says a newer version is out.
const updatesNoticeOnly = true

// updateEventAvailable tells the window a newer version is out.
const updateEventAvailable = "leafen:update-available"

// AvailableUpdate is a newer version the list names, for the person to
// install: its version, whether it is a security release, and its release
// page (a Leafen release page, app.ReadDesktopRelease).
type AvailableUpdate struct {
	Version  string `json:"version"`
	Security bool   `json:"security"`
	NotesURL string `json:"notesUrl"`
}

// updaterPlatform is what the Windows updater keeps: the newer version out,
// once a check has found one.
type updaterPlatform struct {
	available *AvailableUpdate
}

// Available is the newer version out, or nil.
func (updater *Updater) Available() *AvailableUpdate {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	return updater.available
}

// OpenDownload opens the newer version's release page in the browser.
func (updater *Updater) OpenDownload() error {
	updater.mu.Lock()
	available, desktop := updater.available, updater.app
	updater.mu.Unlock()
	if available == nil || desktop == nil {
		return nil
	}
	return desktop.Browser.OpenURL(available.NotesURL)
}

// menuLine is the app menu's update line while looking is on: always
// "업데이트 확인", which says what it found (app_menu_windows.go).
func (updater *Updater) menuLine() (string, int) { return "check", 0 }

// atStart has nothing to take up: no version is kept here.
func (updater *Updater) atStart(string) {}

// checkNow looks for a newer version at once, as the app menu's "업데이트
// 확인" asks, and answers it, or nil when this one is the latest.
func (updater *Updater) checkNow(ctx context.Context) (*app.DesktopRelease, error) {
	updater.checking.Lock()
	defer updater.checking.Unlock()
	release, err := updater.newerRelease(ctx, &http.Client{Timeout: 30 * time.Second})
	if err == nil && release != nil {
		updater.announce(release)
	}
	return release, err
}

// check looks for a newer version, as the six-hourly look does, and says
// when one is out.
func (updater *Updater) check(ctx context.Context) error {
	_, err := updater.checkNow(ctx)
	return err
}

// announce keeps the newer version and tells the window, when it is not the
// one already told.
func (updater *Updater) announce(release *app.DesktopRelease) {
	available := &AvailableUpdate{Version: release.Version, Security: release.Security, NotesURL: release.NotesURL}
	updater.mu.Lock()
	same := updater.available != nil && *updater.available == *available
	updater.available = available
	desktop := updater.app
	updater.mu.Unlock()
	if !same && desktop != nil {
		desktop.Event.Emit(updateEventAvailable, available)
	}
}

// Nothing waits to start in place of this version, and no helper switches
// versions.
func (*Updater) ReadyAtStart() bool       { return false }
func (*Updater) HandOffAtStart(bool) bool { return false }
func (*Updater) MarkHealthy()             {}
func (*Updater) MarkUpdating()            {}
func runUpdateHelper([]string) int        { return 2 }
