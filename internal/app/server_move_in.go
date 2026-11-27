package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
	"golang.org/x/net/webdav"
)

// The new server's side of a move (server_move.go): one receiving place at a
// time, open for an hour to the code's secret alone, as WebDAV the old
// server's backup store already speaks; then the snapshot put together and
// restored at the next start.

const serverMoveInStoreKey = "leafenServerMoveIn"

type serverMoveIn struct {
	app   core.App
	mu    sync.Mutex
	slot  *serverMoveInSlot
	locks webdav.LockSystem
	// refused is the error code of the last move this server refused to
	// take (restoreRefused): the screen says why until a new code is made
	// or the note is put away.
	refused string
}

type serverMoveInSlot struct {
	code    serverMoveCode
	encoded string
	state   string
	restart string
	// failed is the error code of the last finish that failed here for a
	// reason other than a refusal: the place stays open to the same code
	// for the old server to send again, and the screen says why and offers
	// to cancel.
	failed string
}

func serverMoveInOf(app core.App) *serverMoveIn {
	return app.Store().GetOrSet(serverMoveInStoreKey, func() any {
		return &serverMoveIn{app: app, locks: webdav.NewMemLS()}
	}).(*serverMoveIn)
}

func (in *serverMoveIn) folder(id string) string {
	return filepath.Join(in.app.DataDir(), core.LocalBackupsDirName, serverMoveInDir, id)
}

// current is the open place, closing one that ran out unused.
func (in *serverMoveIn) current() *serverMoveInSlot {
	if in.slot != nil && in.slot.state == serverMoveInWaiting && time.Now().UnixMilli() > in.slot.code.Expires {
		_ = os.RemoveAll(in.folder(in.slot.code.ID))
		in.slot = nil
	}
	return in.slot
}

func (in *serverMoveIn) project() administratorServerMoveIn {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.projectHeld()
}

// projectHeld is project with the lock held.
func (in *serverMoveIn) projectHeld() administratorServerMoveIn {
	if note := readServerMovedIn(in.app.DataDir()); note != nil {
		return administratorServerMoveIn{State: serverMoveInMoved, From: note.From, MovedAt: time.UnixMilli(note.At).UTC().Format(time.RFC3339),
			Expected: note.Expected, Actual: countServerContents(in.app)}
	}
	projected := administratorServerMoveIn{State: serverMoveInNone, People: countServerContents(in.app).People, ErrorCode: in.refused}
	if slot := in.current(); slot != nil {
		projected.State, projected.Restart, projected.ErrorCode = slot.state, slot.restart, slot.failed
		projected.ExpiresAt = time.UnixMilli(slot.code.Expires).UTC().Format(time.RFC3339)
		if slot.state == serverMoveInWaiting {
			projected.Code = slot.encoded
		}
	}
	return projected
}

func (in *serverMoveIn) read(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	return event.JSON(http.StatusOK, in.project())
}

// open makes the receiving place and its code. Only a server no one else
// uses takes a whole server: it is replaced, its own data kept aside for 7
// days as after any restore. A server with people moves a workspace instead.
func (in *serverMoveIn) open(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	if countServerContents(event.App).People > 1 {
		return apiError(event, http.StatusConflict, errorCodeServerMoveNotEmpty,
			"Other people use this server; a whole server moves only to one no one else uses yet.")
	}
	if pending, _, err := readRestoreStatus(event.App.DataDir()); err != nil || pending != nil || dataMovedAway(event.App) ||
		serverMoveOutOf(event.App).busy() {
		return apiError(event, http.StatusConflict, errorCodeServerMoveBusy, "This server is moving or restoring now.")
	}
	if backups, err := loadScheduledBackup(event.App); err == nil && backups.StandbyEnabled {
		return apiError(event, http.StatusConflict, errorCodeServerMoveBusy, "This server is a standby; stop following first.")
	}
	// A key's call asks its person once this server can take one (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if slot := in.current(); slot != nil && slot.state != serverMoveInWaiting {
		return apiError(event, http.StatusConflict, errorCodeServerMoveBusy, "A server is moving here now.")
	} else if slot != nil {
		_ = os.RemoveAll(in.folder(slot.code.ID))
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerMoveFailed, "Couldn't make the code.", err)
	}
	code := serverMoveCode{To: serverMoveRequestOrigin(event.Request), ID: security.RandomStringWithAlphabet(20, "abcdefghijklmnopqrstuvwxyz0123456789"),
		Secret: hex.EncodeToString(secret), Release: ReleaseVersion(), Expires: time.Now().Add(serverMoveCodeFor).UnixMilli()}
	if public := requestOrigin(event.Request); public != "" && public != code.To {
		code.Also = public
	}
	if code.To == "" {
		code.To, code.Also = code.Also, ""
	}
	if err := os.MkdirAll(in.folder(code.ID), 0o700); err != nil || code.To == "" {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerMoveFailed, "Couldn't make the receiving place.", err)
	}
	in.slot = &serverMoveInSlot{code: code, encoded: encodeServerMoveCode(code), state: serverMoveInWaiting}
	in.refused = ""
	return event.JSON(http.StatusOK, in.projectHeld())
}

// close takes the receiving place back — one waiting, or one receiving that
// a finish failed in or an old server stopped sending to; the old server's
// move then stops and it takes changes again — or puts away the note of a
// move taken in. A place being restored is past taking back.
func (in *serverMoveIn) close(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	in.mu.Lock()
	if slot := in.current(); slot != nil && slot.state != serverMoveInRestoring {
		_ = os.RemoveAll(in.folder(slot.code.ID))
		in.slot = nil
	}
	_ = os.Remove(filepath.Join(event.App.DataDir(), serverMovedInName))
	in.refused = ""
	defer in.mu.Unlock()
	return event.JSON(http.StatusOK, in.projectHeld())
}

// held is the place a request names when it carries the place's secret.
func (in *serverMoveIn) held(event *core.RequestEvent) *serverMoveInSlot {
	id, secret, ok := event.Request.BasicAuth()
	if !ok || id != event.Request.PathValue("id") {
		return nil
	}
	slot := in.current()
	if slot == nil || slot.code.ID != id || subtle.ConstantTimeCompare([]byte(secret), []byte(slot.code.Secret)) != 1 {
		return nil
	}
	return slot
}

func serverMoveRefused(event *core.RequestEvent) error {
	event.Response.Header().Set("WWW-Authenticate", `Basic realm="leafen-move"`)
	return apiError(event, http.StatusUnauthorized, errorCodeServerMoveCodeInvalid, "That move code isn't open here.")
}

// info tells the old server this server's version before it stops.
func (in *serverMoveIn) info(event *core.RequestEvent) error {
	in.mu.Lock()
	slot := in.held(event)
	in.mu.Unlock()
	if slot == nil {
		return serverMoveRefused(event)
	}
	schema := 0
	_ = event.App.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&schema)
	return event.JSON(http.StatusOK, map[string]any{"release": ReleaseVersion(), "schemaVersion": schema, "state": slot.state})
}

// dav is the receiving place itself.
func (in *serverMoveIn) dav(event *core.RequestEvent) error {
	in.mu.Lock()
	slot := in.held(event)
	if slot != nil && (slot.state == serverMoveInWaiting || slot.state == serverMoveInReceiving) {
		// An old server sending again after a failed finish starts afresh.
		slot.state, slot.failed = serverMoveInReceiving, ""
	}
	in.mu.Unlock()
	if slot == nil || slot.state != serverMoveInReceiving {
		return serverMoveRefused(event)
	}
	handler := &webdav.Handler{Prefix: apiRoot + "/server-move/in/" + slot.code.ID + "/dav",
		FileSystem: webdav.Dir(in.folder(slot.code.ID)), LockSystem: in.locks}
	handler.ServeHTTP(event.Response, event.Request)
	return nil
}

type serverMoveFinishInput struct {
	Snapshot string                        `json:"snapshot"`
	SHA256   string                        `json:"sha256"`
	From     string                        `json:"from"`
	Counts   administratorServerMoveCounts `json:"counts"`
	// Holder is the old server's replica mark, sealed: the new server goes
	// on with its replica, as the same server in another place.
	Holder string `json:"holder,omitempty"`
}

// finish puts the snapshot together, every file checked against its list,
// and has the next start move it in; a server that can starts again now.
func (in *serverMoveIn) finish(event *core.RequestEvent) error {
	in.mu.Lock()
	slot := in.held(event)
	if slot == nil || slot.state != serverMoveInReceiving {
		in.mu.Unlock()
		return serverMoveRefused(event)
	}
	slot.state = serverMoveInRestoring
	in.mu.Unlock()
	// A finish that fails leaves the place receiving, for the old server to
	// send again with the same code, and keeps why for this server's screen.
	fail := func(status int, code, message string, cause error) error {
		in.mu.Lock()
		slot.state, slot.failed = serverMoveInReceiving, code
		in.mu.Unlock()
		return apiErrorFrom(event, status, code, message, cause)
	}
	input := serverMoveFinishInput{}
	if err := event.BindBody(&input); err != nil || !restoreSnapshotNameValid(strings.TrimSpace(input.Snapshot)) {
		return fail(http.StatusBadRequest, errorCodeServerMoveFailed, "Name the snapshot that was sent.", nil)
	}
	if event.App.Store().Has(core.StoreKeyActiveBackup) {
		return fail(http.StatusConflict, errorCodeServerMoveBusy, "A backup or restore is running.", nil)
	}
	event.App.Store().Set(core.StoreKeyActiveBackup, "leafen-server-move")
	defer event.App.Store().Remove(core.StoreKeyActiveBackup)
	folder := in.folder(slot.code.ID)
	source, err := openScheduledBackupSnapshotWith(newFolderBackupStore(folder), scheduledBackupRootName,
		scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupSnapshotsName, strings.TrimSpace(input.Snapshot)),
		strings.TrimSpace(input.SHA256), passwordBackupSecret(slot.code.Secret))
	if err != nil {
		return fail(http.StatusUnprocessableEntity, errorCodeServerMoveIncomplete, "The snapshot that was sent can't be opened.", nil)
	}
	work := restoreWorkDir(event.App.DataDir())
	staged := filepath.Join(work, restoreStagedName)
	if err := errors.Join(os.RemoveAll(staged), os.MkdirAll(staged, 0o700)); err != nil {
		return fail(http.StatusInternalServerError, errorCodeServerMoveFailed, "Couldn't put the snapshot together.", err)
	}
	if _, err := source.assemble(staged); err != nil {
		_ = os.RemoveAll(staged)
		logDurableJobDiagnostic(event.App, errorCodeServerMoveFailed, newDiagnosticReference(), "", "server-move", "assemble", err)
		return fail(http.StatusUnprocessableEntity, errorCodeServerMoveIncomplete, "The snapshot doesn't hold what its list says.", nil)
	}
	// Data this server can't take isn't taken by trying again: the place
	// closes, and this server's screen says why (the old server's says only
	// that the move stopped).
	var refused restoreRefused
	if err := stagedRestoreRefusal(staged); errors.As(err, &refused) {
		_ = os.RemoveAll(staged)
		code := refused.codeOr(errorCodeServerMoveFailed)
		in.mu.Lock()
		_ = os.RemoveAll(in.folder(slot.code.ID))
		in.slot, in.refused = nil, code
		in.mu.Unlock()
		return apiError(event, http.StatusUnprocessableEntity, code, "This server can't take that data: "+refused.message+".")
	}
	holder := []byte(nil)
	if input.Holder != "" {
		opened, err := openForMove(slot.code.Secret, input.Holder)
		if err != nil || json.Unmarshal(opened, &replicaHolder{}) != nil {
			return fail(http.StatusBadRequest, errorCodeServerMoveFailed, "The replica mark that was sent can't be opened.", nil)
		}
		holder = opened
	}
	now := time.Now()
	note := serverMovedIn{From: serverMoveOrigin(input.From), At: now.UnixMilli(), Expected: input.Counts}
	if err := leaveRestoreNote(staged, work, pendingRestore{
		Kind: restoreKindRestore, Phase: restorePhaseReady, Snapshot: strings.TrimSpace(input.Snapshot), SHA256: source.manifestSHA256,
		Source: restoreStagedName, Keep: restoreBeforePrefix + restoreStamp(now), RequestedAt: now.UnixMilli(),
	}, func() error { return writeRestoreFile(event.App.DataDir(), serverMovedInName, note) }); err != nil {
		_ = os.RemoveAll(staged)
		return fail(http.StatusInternalServerError, errorCodeServerMoveFailed, "Couldn't put the snapshot together.", err)
	}
	// The mark stays through the restore as this machine's own, so it is
	// put in place of this server's before.
	if holder != nil {
		if err := writeRestoreFile(event.App.DataDir(), replicaHolderName, json.RawMessage(holder)); err != nil {
			return fail(http.StatusInternalServerError, errorCodeServerMoveFailed, "Couldn't keep the replica mark.", err)
		}
	}
	_ = os.RemoveAll(folder)
	restart := restoreRestartMode(event.App)
	in.mu.Lock()
	slot.restart = restart
	in.mu.Unlock()
	if err := event.JSON(http.StatusAccepted, map[string]string{"restart": restart}); err != nil {
		return err
	}
	if restart == restoreRestartAutomatic {
		app := event.App
		time.AfterFunc(restoreRestartDelay, func() {
			if err := restartForRestore(app); err != nil {
				logDurableJobDiagnostic(app, errorCodeServerMoveFailed, newDiagnosticReference(), "", "server-move", "restart", err)
			}
		})
	}
	return nil
}
