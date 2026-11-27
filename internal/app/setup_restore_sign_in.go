package app

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The browser that restored a lost server at its first start signs in once,
// as the restored server's owner (user decision 2026-11-07): the restored data
// knows none of the browser's sign-ins when every device that had one is gone
// with the old server, and who may set a server up may fill it from a backup
// and so come in as its owner. The restore gives that browser a pass whose
// hash waits beside the data, kept in place as the restored data comes in
// (restoreStaysPut), for half an hour and one use.
const setupRestoreSignInName = "setup-restore-sign-in.json"

const setupRestoreSignInLifetime = 30 * time.Minute

type setupRestoreSignInNote struct {
	Hash      string `json:"hash"`
	ExpiresAt int64  `json:"expiresAt"`
}

type setupRestoreSignInInput struct {
	Pass string `json:"pass"`
}

var errSetupRestoreSignInInvalid = errors.New("setup restore login pass is not valid")

// newSetupRestoreSignIn leaves the note for a pass and gives the pass.
func newSetupRestoreSignIn(dataDir string, now time.Time) (string, error) {
	pass, err := newSetupRestoreSignInPass()
	if err != nil {
		return "", err
	}
	return pass, leaveSetupRestoreSignIn(dataDir, mcpTokenHash(pass), now)
}

// newSetupRestoreSignInPass is a new pass, not yet noted: a screen that left
// the restore to an agent holds it while the agent restores
// (setup_restore_agent.go).
func newSetupRestoreSignInPass() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

// leaveSetupRestoreSignIn leaves the note for the pass whose hash is given.
func leaveSetupRestoreSignIn(dataDir, hash string, now time.Time) error {
	note, err := json.Marshal(setupRestoreSignInNote{Hash: hash, ExpiresAt: now.Add(setupRestoreSignInLifetime).UnixMilli()})
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, setupRestoreSignInName)
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, note, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// takeSetupRestoreSignIn checks the pass against the note and ends the note
// either way it was found: a pass is good once.
func takeSetupRestoreSignIn(dataDir, pass string, now time.Time) error {
	path := filepath.Join(dataDir, setupRestoreSignInName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return errSetupRestoreSignInInvalid
	}
	note := setupRestoreSignInNote{}
	if json.Unmarshal(raw, &note) != nil || note.Hash == "" {
		_ = os.Remove(path)
		return errSetupRestoreSignInInvalid
	}
	if now.UnixMilli() >= note.ExpiresAt {
		_ = os.Remove(path)
		return errSetupRestoreSignInInvalid
	}
	if subtle.ConstantTimeCompare([]byte(note.Hash), []byte(mcpTokenHash(pass))) != 1 {
		return errSetupRestoreSignInInvalid
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return nil
}

// restoredServerOwner is the restored server's first administrator: the one
// who set it up.
func restoredServerOwner(app core.App) (*core.Record, error) {
	userID := ""
	err := app.DB().NewQuery(`SELECT [[user_id]] FROM {{leafen_administrators}} ORDER BY [[created_at]], [[user_id]] LIMIT 1`).Row(&userID)
	if errors.Is(err, sql.ErrNoRows) || userID == "" {
		return nil, errSetupRestoreSignInInvalid
	}
	if err != nil {
		return nil, err
	}
	return app.FindRecordById("users", userID)
}

// signInAfterSetupRestore signs the restoring browser in as the restored
// server's owner with its pass, once.
func signInAfterSetupRestore(event *core.RequestEvent) error {
	input := setupRestoreSignInInput{}
	_ = event.BindBody(&input)
	pass := strings.TrimSpace(input.Pass)
	invalid := func() error {
		return apiError(event, http.StatusUnauthorized, errorCodeDeviceCodeInvalid, "This login has ended. Log in from a device or with a code.")
	}
	if pass == "" || len(pass) > 128 {
		return invalid()
	}
	if configured, err := administratorConfigured(event.App); err != nil || !configured {
		return invalid()
	}
	if err := takeSetupRestoreSignIn(event.App.DataDir(), pass, time.Now()); err != nil {
		if errors.Is(err, errSetupRestoreSignInInvalid) {
			return invalid()
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeDeviceCodeFailed, "Couldn't log in.", err)
	}
	owner, err := restoredServerOwner(event.App)
	if err != nil {
		return invalid()
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return writeAdministratorSession(event, owner, "")
}
