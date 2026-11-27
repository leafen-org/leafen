//go:build darwin && !ios

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"leafen.org/leafen/internal/app"
)

// The Mac app updates itself (decided 2026-10-17): it fetches a newer version
// the list names in the background and checks it — the list's SHA-256, then
// macOS's own checks of who made it: Leafen's team's code signature and
// notarization (update_fetch.go) — and keeps it ready. The switch happens the
// next time the app starts, or now when the person restarts: a helper waits
// for the app to end, keeps a copy of the data and the old app, puts the new
// one in place and starts it (update_helper.go). When the new version doesn't
// come up within two minutes — or within half an hour while its server says
// it is bringing the data up to date — the helper puts the old app and data
// back, starts it and says so, and that version is skipped.

// updatesNoticeOnly: the Mac app installs a newer version itself.
const updatesNoticeOnly = false

const (
	updateHealthWait = 2 * time.Minute
	// A version whose server is bringing its data up to date says so every
	// updateProgressEvery (server.go); the helper waits on while it heard so
	// within updateProgressFresh, up to updateHealthWaitUpdating in all.
	updateProgressFresh      = 30 * time.Second
	updateHealthWaitUpdating = 30 * time.Minute
	updateMaxBytes           = 1 << 30
	updateEventStaged        = "leafen:update-staged"
	updateStagedFile         = "staged.json"
	updateRolledBack         = "rolled-back.json"
	updatePendingHealth      = "pending-health.json"
	// The copies a switch keeps beside the app for a moment.
	updateSiblingNew = ".leafen-new"
	updateSiblingOld = ".leafen-old"
)

// StagedUpdate is a newer version fetched, checked and ready to switch to.
type StagedUpdate struct {
	Version  string `json:"version"`
	Security bool   `json:"security"`
	From     string `json:"from"`
	Kind     string `json:"kind"`
	// Payload is the new app in the updates folder; Target where the
	// running app is installed.
	Payload string `json:"payload"`
	Target  string `json:"target"`
}

// RolledBack says a version didn't come up and the old one came back.
type RolledBack struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
}

// updaterPlatform is what the Mac's updater keeps: the version ready to
// switch to, and while a newer one downloads, its bytes so far and in all,
// for the app menu's "업데이트 다운로드 중(n%)" (app_menu_bar_darwin.go).
type updaterPlatform struct {
	staged         *StagedUpdate
	downloading    bool
	received, size int64
}

// SetEnabled turns updating itself on or off; turning it off also lets go
// of a version already fetched, so nothing changes at the next start.
func (updater *Updater) SetEnabled(enabled bool) error {
	dir, err := updatesDir()
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, updateSettingsFile), map[string]bool{"enabled": enabled}); err != nil {
		return err
	}
	// The app menu's update line comes or goes at once.
	defer redrawUpdateLine()
	if enabled {
		return nil
	}
	updater.mu.Lock()
	staged, desktop := updater.staged, updater.app
	updater.staged = nil
	updater.mu.Unlock()
	_ = os.Remove(filepath.Join(dir, updateStagedFile))
	if staged != nil {
		_ = os.RemoveAll(filepath.Join(dir, staged.Version))
	}
	if desktop != nil {
		desktop.Event.Emit(updateEventStaged, nil)
	}
	return nil
}

// Staged is the version ready to switch to, or nil.
func (updater *Updater) Staged() *StagedUpdate {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	return updater.staged
}

// Downloading says how far a newer version has downloaded, in whole
// percent rounded as Notion's app rounds it, while one is on its way: from
// when the list names it until it is checked and ready, or has failed.
func (updater *Updater) Downloading() (int, bool) {
	updater.mu.Lock()
	defer updater.mu.Unlock()
	return downloadPercent(updater.received, updater.size), updater.downloading
}

func downloadPercent(received, size int64) int {
	if size <= 0 {
		return 0
	}
	return int(min(max((received*200+size)/(size*2), 0), 100))
}

// followDownload keeps how far the download is and draws the app menu's
// update line again when its whole percent moves or the download starts or
// ends.
func (updater *Updater) followDownload(downloading bool, received, size int64) {
	updater.mu.Lock()
	moved := downloading != updater.downloading || downloadPercent(received, size) != downloadPercent(updater.received, updater.size)
	updater.downloading, updater.received, updater.size = downloading, received, size
	updater.mu.Unlock()
	if moved {
		redrawUpdateLine()
	}
}

// menuLine is the app menu's update line while updating is on: "restart"
// once a newer version is ready, "downloading" with its whole percent while
// one is on its way, else "check" (app_menu_desktop.go updateLine).
func (updater *Updater) menuLine() (string, int) {
	if updater.Staged() != nil {
		return "restart", 0
	}
	if percent, downloading := updater.Downloading(); downloading {
		return "downloading", percent
	}
	return "check", 0
}

// TakeRolledBack answers, once, a version that didn't come up.
func (updater *Updater) TakeRolledBack() *RolledBack {
	dir, err := updatesDir()
	if err != nil {
		return nil
	}
	rolled := &RolledBack{}
	if !readJSON(filepath.Join(dir, updateRolledBack), rolled) {
		return nil
	}
	_ = os.Remove(filepath.Join(dir, updateRolledBack))
	return rolled
}

// RestartNow switches to the ready version now: the helper takes over and
// the app ends.
func (updater *Updater) RestartNow() error {
	if updater.Staged() == nil {
		return errors.New("no new version is ready")
	}
	if err := startUpdateHelper(false); err != nil {
		return err
	}
	updater.mu.Lock()
	desktop := updater.app
	updater.mu.Unlock()
	if desktop != nil {
		desktop.Quit()
	}
	return nil
}

// ReadyAtStart says a version made ready earlier waits to start instead of
// this one.
func (updater *Updater) ReadyAtStart() bool {
	dir, err := updatesDir()
	if err != nil {
		return false
	}
	staged := &StagedUpdate{}
	if !readJSON(filepath.Join(dir, updateStagedFile), staged) || !app.NewerReleaseVersion(staged.Version, app.ReleaseVersion()) {
		return false
	}
	enabled, err := updater.Enabled()
	return err == nil && enabled["enabled"]
}

// HandOffAtStart starts the helper instead of the app, answering whether it
// did; the app then ends. The new version starts as this one was: behind
// its window when it was opened at login.
func (updater *Updater) HandOffAtStart(background bool) bool {
	return startUpdateHelper(background) == nil
}

// MarkUpdating tells a helper waiting on this version that its server is
// still bringing the data up to date.
func (updater *Updater) MarkUpdating() {
	dir, err := updatesDir()
	if err != nil {
		return
	}
	pending := &StagedUpdate{}
	if readJSON(filepath.Join(dir, updatePendingHealth), pending) && pending.Version == app.ReleaseVersion() {
		_ = os.WriteFile(filepath.Join(dir, "updating-"+pending.Version), []byte("updating"), 0o600)
	}
}

// MarkHealthy tells a helper waiting on this version that it came up.
func (updater *Updater) MarkHealthy() {
	dir, err := updatesDir()
	if err != nil {
		return
	}
	pending := &StagedUpdate{}
	if readJSON(filepath.Join(dir, updatePendingHealth), pending) && pending.Version == app.ReleaseVersion() {
		_ = os.WriteFile(filepath.Join(dir, "healthy-"+pending.Version), []byte("ok"), 0o600)
	}
}

// atStart clears what an earlier switch left and takes up a version
// fetched before, as Run starts.
func (updater *Updater) atStart(dir string) {
	// What an earlier switch left: the helper's copy, and a version that
	// didn't start.
	if _, err := os.Stat(filepath.Join(dir, updatePendingHealth)); errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(filepath.Join(dir, "update-helper"))
		// A switch cut off before its last step leaves the other copy
		// beside the app (update_helper.go swapInto).
		if target, err := installedApp(); err == nil {
			_ = os.RemoveAll(target + updateSiblingNew)
			_ = os.RemoveAll(target + updateSiblingOld)
		}
		if failed, err := filepath.Glob(filepath.Join(dir, "failed-*")); err == nil {
			for _, path := range failed {
				_ = os.RemoveAll(path)
			}
		}
	}
	staged := &StagedUpdate{}
	enabled, _ := updater.Enabled()
	if enabled["enabled"] && readJSON(filepath.Join(dir, updateStagedFile), staged) && app.NewerReleaseVersion(staged.Version, app.ReleaseVersion()) {
		updater.mu.Lock()
		updater.staged = staged
		updater.mu.Unlock()
	}
}

// checkNow looks for a newer version at once, as the app menu's "업데이트
// 확인" asks (app_menu_desktop.go): it answers the newer version the list
// names, or nil when this one is the latest, and fetches the newer one in the
// background as the six-hourly check does.
func (updater *Updater) checkNow(ctx context.Context) (*app.DesktopRelease, error) {
	release, err := updater.newerRelease(ctx, &http.Client{Timeout: 30 * time.Second})
	if err != nil || release == nil {
		return nil, err
	}
	go func() {
		if err := updater.check(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "leafen update:", err)
		}
	}()
	return release, nil
}

// check fetches a newer version the list names and keeps it ready, one check
// at a time: the six-hourly one, or the menu's.
func (updater *Updater) check(ctx context.Context) error {
	updater.checking.Lock()
	defer updater.checking.Unlock()
	client := &http.Client{Timeout: 30 * time.Second}
	release, err := updater.newerRelease(ctx, client)
	if err != nil || release == nil {
		return err
	}
	dir, err := updatesDir()
	if err != nil {
		return err
	}
	if staged := updater.Staged(); staged != nil && staged.Version == release.Version {
		return nil
	}
	var asset *app.DesktopAsset
	for index := range release.Assets {
		if release.Assets[index].OS == runtime.GOOS && release.Assets[index].Arch == runtime.GOARCH {
			asset = &release.Assets[index]
		}
	}
	if asset == nil {
		return nil
	}
	target, err := installedApp()
	if err != nil {
		return err
	}
	// The menu's line counts the download from here until the version is
	// ready or has failed, as Notion's counts from when its list names one.
	updater.followDownload(true, 0, asset.Size)
	defer updater.followDownload(false, 0, 0)
	versionDir := filepath.Join(dir, release.Version)
	_ = os.RemoveAll(versionDir)
	if err := os.MkdirAll(versionDir, 0o700); err != nil {
		return err
	}
	download := filepath.Join(versionDir, "download")
	received := func(bytes int64) { updater.followDownload(true, bytes, asset.Size) }
	if err := fetchVerified(ctx, client, asset, download, received); err != nil {
		_ = os.RemoveAll(versionDir)
		return err
	}
	payload, err := unpackUpdate(download, filepath.Join(versionDir, "payload"))
	if err == nil {
		err = verifyPublisher(payload)
	}
	if err != nil {
		_ = os.RemoveAll(versionDir)
		return err
	}
	_ = os.Remove(download)
	staged := &StagedUpdate{Version: release.Version, Security: release.Security, From: app.ReleaseVersion(),
		Kind: asset.Kind, Payload: payload, Target: target}
	if err := writeJSON(filepath.Join(dir, updateStagedFile), staged); err != nil {
		return err
	}
	updater.mu.Lock()
	previous := updater.staged
	updater.staged = staged
	desktop := updater.app
	updater.mu.Unlock()
	// A newer version replaces one fetched before.
	if previous != nil && previous.Version != staged.Version {
		_ = os.RemoveAll(filepath.Join(dir, previous.Version))
	}
	if desktop != nil {
		desktop.Event.Emit(updateEventStaged, staged)
	}
	return nil
}

// startUpdateHelper starts a copy of this program as the helper that
// switches versions once the app has ended.
func startUpdateHelper(background bool) error {
	dir, err := updatesDir()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	helper := filepath.Join(dir, "update-helper")
	if err := copyFile(executable, helper, 0o700); err != nil {
		return err
	}
	args := []string{"apply-update", "--pid", fmt.Sprint(os.Getpid())}
	if background {
		args = append(args, "--background")
	}
	command := exec.Command(helper, args...)
	command.Env = os.Environ()
	detach(command)
	return command.Start()
}

func copyFile(from, to string, mode os.FileMode) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		return err
	}
	return target.Close()
}
