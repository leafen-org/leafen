package app

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The data epoch names which history the server's data is in. A restore,
// or taking one back, puts the data into another history (an earlier
// point, then edits from there), so it takes a new epoch; a device that
// knew another one holds copies from a history the server no longer has,
// and its writes, or its live edits, would carry that history back in.
// The server refuses them, and the device lets its copies go once its
// person agrees.
const (
	dataEpochStoreKey = "leafenDataEpoch"
	dataEpochHeader   = "X-Leafen-Data-Epoch"
	dataEpochQuery    = "epoch"
	// A restore made by the command line leaves this in the new data
	// folder, as the server isn't running to change the epoch itself.
	restoreEpochMarkerName = "renew-data-epoch"
)

// loadDataEpoch reads the epoch and keeps it at hand for the requests.
func loadDataEpoch(app core.App) (string, error) {
	epoch := ""
	if err := app.DB().NewQuery(`SELECT [[epoch]] FROM {{leafen_data_epoch}} WHERE [[id]] = 'current'`).Row(&epoch); err != nil {
		return "", err
	}
	app.Store().Set(dataEpochStoreKey, epoch)
	return epoch, nil
}

// renewDataEpoch gives the data a new epoch, after a restore or its undo.
func renewDataEpoch(app core.App, now time.Time) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	epoch := hex.EncodeToString(random)
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_data_epoch}} SET [[epoch]] = {:epoch}, [[changed_at]] = {:now}
		WHERE [[id]] = 'current'`).Bind(dbx.Params{"epoch": epoch, "now": now.UnixMilli()}).Execute(); err != nil {
		return "", err
	}
	app.Store().Set(dataEpochStoreKey, epoch)
	return epoch, nil
}

// currentDataEpoch is the epoch the server is in, as read at its start.
func currentDataEpoch(app core.App) string {
	epoch, _ := app.Store().Get(dataEpochStoreKey).(string)
	return epoch
}

// markRestoredDataRootForNewEpoch leaves the note that makes a restored
// data folder's first start take a new epoch.
func markRestoredDataRootForNewEpoch(dataRoot string) error {
	work := restoreWorkDir(dataRoot)
	if err := os.MkdirAll(work, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(work, restoreEpochMarkerName), []byte("restored\n"), 0o600)
}

// takeRestoreEpochMarker is whether the data folder holds that note, which
// it then lets go.
func takeRestoreEpochMarker(dataDir string) bool {
	path := filepath.Join(restoreWorkDir(dataDir), restoreEpochMarkerName)
	if _, err := os.Stat(path); err != nil {
		return false
	}
	_ = os.Remove(path)
	return true
}

// guardDataEpoch refuses a write, or a live connection, from a device that
// names another epoch than the server's: its copies are of another
// history. A device that names none (older clients, other programs) is
// let through.
func guardDataEpoch(event *core.RequestEvent) error {
	request := event.Request
	if !strings.HasPrefix(request.URL.Path, apiRoot+"/") {
		return event.Next()
	}
	live := strings.HasPrefix(request.URL.Path, apiRoot+"/collaboration/")
	if !live && (request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions) {
		return event.Next()
	}
	// Asking for a file's link changes nothing, and a device that put off
	// taking the new data still shows its pages to copy from, images too.
	if strings.HasPrefix(request.URL.Path, apiRoot+"/files/") && strings.HasSuffix(request.URL.Path, "/capability") {
		return event.Next()
	}
	named := request.Header.Get(dataEpochHeader)
	if live {
		named = request.URL.Query().Get(dataEpochQuery)
	}
	current := currentDataEpoch(event.App)
	if named == "" || current == "" || named == current {
		return event.Next()
	}
	return apiError(event, http.StatusConflict, errorCodeDataEpochChanged,
		"This server was restored from a backup; this device's copies are from before.")
}

func registerDataEpochGuard(event *core.ServeEvent) error {
	if _, err := loadDataEpoch(event.App); err != nil {
		return err
	}
	event.Router.BindFunc(guardDataEpoch)
	return nil
}
