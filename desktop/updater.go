//go:build (darwin && !ios) || windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"leafen.org/leafen/internal/app"
)

// The app reads the signed desktop update list when it starts and every six
// hours. On macOS it updates itself (decided 2026-10-17, updater_darwin.go):
// it fetches a newer version in the background, checks it and switches to it
// the next time it starts, or now when the person restarts. Windows updates
// only through the Microsoft Store (decided 2026-11-17: Leafen buys no
// Windows code signing certificate, so nothing could tell Leafen's program
// from another signed one): the store updates its app (packaged_windows.go),
// and the zip outside the store only says a newer version is out and opens
// the download page (updater_windows.go). Servers in Docker or on a NAS never
// update themselves, nor does the Mac App Store's app.
// LEAFEN_DESKTOP_AUTO_UPDATE=off, or the person's switch on macOS, keeps the
// app as it is and says nothing.

const (
	autoUpdateEnv      = "LEAFEN_DESKTOP_AUTO_UPDATE"
	updateCheckEvery   = 6 * time.Hour
	updatesFolder      = "updates"
	updateSettingsFile = "settings.json"
	updateSkipFile     = "skip.json"
)

// Updater is the app's own updater, bound for the window. What it keeps
// besides the lock and the app is its system's (updaterPlatform).
type Updater struct {
	mu  sync.Mutex
	app *application.App
	// Held while a check reads the list, so the menu's and the six-hourly
	// one never run at once.
	checking sync.Mutex
	updaterPlatform
}

func newUpdater() *Updater { return &Updater{} }

// runningUpdater is the app's updater, for its menu's "업데이트 확인"
// (app_menu_desktop.go).
var runningUpdater *Updater

func (updater *Updater) setApp(desktop *application.App) {
	updater.mu.Lock()
	updater.app = desktop
	updater.mu.Unlock()
	runningUpdater = updater
}

func updatesDir() (string, error) {
	data, err := localDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(data, updatesFolder)
	return dir, os.MkdirAll(dir, 0o700)
}

func readJSON(path string, target any) bool {
	data, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(data, target) == nil
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Enabled says whether the app looks for newer versions, whether the
// environment decides it (a company's deployment) or a store updates the app,
// which leaves the window no switch to show, and whether the person installs a
// newer version themselves ("manual": the Windows zip, which only says one is
// out).
func (updater *Updater) Enabled() (map[string]bool, error) {
	if packagedApp() {
		return map[string]bool{"enabled": false, "managed": true, "store": true}, nil
	}
	off := strings.EqualFold(strings.TrimSpace(os.Getenv(autoUpdateEnv)), "off")
	if updatesNoticeOnly {
		return map[string]bool{"enabled": !off, "managed": off, "manual": true}, nil
	}
	if off {
		return map[string]bool{"enabled": false, "managed": true}, nil
	}
	dir, err := updatesDir()
	if err != nil {
		return nil, err
	}
	settings := map[string]bool{"enabled": true}
	readJSON(filepath.Join(dir, updateSettingsFile), &settings)
	return map[string]bool{"enabled": settings["enabled"], "managed": false}, nil
}

// Run looks for a newer version now and every six hours.
func (updater *Updater) Run(ctx context.Context) {
	if packagedApp() {
		return
	}
	if dir, err := updatesDir(); err == nil {
		updater.atStart(dir)
	}
	for {
		err := updater.check(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "leafen update:", err)
		}
		// The last check and why it failed stay beside the updates, for the
		// person or a supporter to read.
		if dir, dirErr := updatesDir(); dirErr == nil {
			last := map[string]string{"at": time.Now().UTC().Format(time.RFC3339)}
			if err != nil {
				last["error"] = err.Error()
			}
			_ = writeJSON(filepath.Join(dir, "last-check.json"), last)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(updateCheckEvery):
		}
	}
}

// newerRelease reads the update list and answers its version when it is newer
// than this one and not one that didn't come up here, or nil.
func (updater *Updater) newerRelease(ctx context.Context, client *http.Client) (*app.DesktopRelease, error) {
	if enabled, err := updater.Enabled(); err != nil || !enabled["enabled"] {
		return nil, err
	}
	listURL := app.DesktopReleaseListURL()
	if listURL == "" {
		return nil, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the update list didn't answer (%d)", response.StatusCode)
	}
	// The list host's time stands in for a clock set back (app.ReleaseListNow).
	release, err := app.ReadDesktopRelease(raw, app.ReleaseListNow(time.Now(), response))
	if err != nil {
		return nil, err
	}
	if !app.NewerReleaseVersion(release.Version, app.ReleaseVersion()) {
		return nil, nil
	}
	dir, err := updatesDir()
	if err != nil {
		return nil, err
	}
	skip := map[string]string{}
	if readJSON(filepath.Join(dir, updateSkipFile), &skip) && skip["version"] == release.Version {
		return nil, nil
	}
	return &release, nil
}
