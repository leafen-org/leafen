package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The old server's side of a move (server_move.go): given the new server's
// code, it checks the new server answers and is no older, stops taking
// changes and lets go of its address, writes its snapshot into the
// receiving place and asks the new server to restore it. Until the new
// server has the snapshot, a failure takes the stop back.

const (
	serverMoveOutStoreKey = "leafenServerMoveOut"
	serverMoveAskTimeout  = 15 * time.Second
	// Putting a large snapshot together on the new server takes a while.
	serverMoveFinishTimeout = 30 * time.Minute
)

type serverMoveOut struct {
	app    core.App
	mu     sync.Mutex
	status administratorServerMoveOut
}

func serverMoveOutOf(app core.App) *serverMoveOut {
	return app.Store().GetOrSet(serverMoveOutStoreKey, func() any {
		return &serverMoveOut{app: app, status: administratorServerMoveOut{State: serverMoveOutNone}}
	}).(*serverMoveOut)
}

func (out *serverMoveOut) busy() bool {
	out.mu.Lock()
	defer out.mu.Unlock()
	return out.status.State == serverMoveOutCopying || out.status.State == serverMoveOutFinishing
}

func (out *serverMoveOut) set(change func(*administratorServerMoveOut)) {
	out.mu.Lock()
	change(&out.status)
	out.mu.Unlock()
}

func (out *serverMoveOut) read(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	fence := readReplicaFence(event.App.DataDir())
	out.mu.Lock()
	// A server kept apart after its move takes changes again.
	if out.status.State == serverMoveOutMoved && fence == nil {
		out.status = administratorServerMoveOut{State: serverMoveOutNone}
	}
	status := out.status
	out.mu.Unlock()
	if status.State == serverMoveOutNone && fence != nil {
		status.To = fence.MovedTo
	}
	return event.JSON(http.StatusOK, status)
}

// serverMoveAsk calls the new server's receiving place with its secret.
func serverMoveAsk(ctx context.Context, code serverMoveCode, origin, method, path string, payload any, timeout time.Duration) (*http.Response, error) {
	var reader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	request, err := http.NewRequestWithContext(ctx, method, origin+apiRoot+"/server-move/in/"+code.ID+path, reader)
	if err != nil {
		cancel()
		return nil, err
	}
	request.SetBasicAuth(code.ID, code.Secret)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	body := response.Body
	response.Body = struct {
		io.Reader
		io.Closer
	}{body, closerFunc(func() error { defer cancel(); return body.Close() })}
	return response, nil
}

type closerFunc func() error

func (close closerFunc) Close() error { return close() }

// start checks the code and the new server, then moves.
func (out *serverMoveOut) start(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	input := administratorServerMoveOutInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeServerMoveCodeInvalid, "That isn't a move code.")
	}
	if strings.TrimSpace(input.Code) == "" {
		return refuseMissingInput(event, errorCodeServerMoveCodeInvalid, "code", "the move code the new server shows.")
	}
	code, err := decodeServerMoveCode(input.Code, time.Now())
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeServerMoveCodeInvalid, "That isn't a move code, or it ran out; make a new one on the new server.")
	}
	if slot := serverMoveInOf(event.App).slotID(); slot == code.ID {
		return apiError(event, http.StatusConflict, errorCodeServerMoveSameServer, "That code is this server's own; give it to the server that moves here.")
	}
	if pending, _, err := readRestoreStatus(event.App.DataDir()); err != nil || pending != nil || dataMovedAway(event.App) || out.busy() ||
		event.App.Store().Has(core.StoreKeyActiveBackup) {
		return apiError(event, http.StatusConflict, errorCodeServerMoveBusy, "This server is moving, restoring or backing up now.")
	}
	// The new server must answer, at the address the code names or the other.
	answer := struct {
		Release       string `json:"release"`
		SchemaVersion int    `json:"schemaVersion"`
	}{}
	reached := ""
	for _, origin := range []string{code.To, code.Also} {
		if origin == "" {
			continue
		}
		response, err := serverMoveAsk(event.Request.Context(), code, origin, http.MethodGet, "", nil, serverMoveAskTimeout)
		if err != nil {
			continue
		}
		status := response.StatusCode
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&answer)
		_ = response.Body.Close()
		if status == http.StatusUnauthorized || status == http.StatusNotFound {
			return apiError(event, http.StatusConflict, errorCodeServerMoveCodeInvalid, "The new server no longer takes that code; make a new one there.")
		}
		if status == http.StatusOK && decodeErr == nil {
			reached = origin
			break
		}
	}
	if reached == "" {
		return apiError(event, http.StatusBadGateway, errorCodeServerMoveUnreachable, "Couldn't reach the new server from this one at "+code.To+".")
	}
	schema := 0
	_ = event.App.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&schema)
	if NewerReleaseVersion(ReleaseVersion(), answer.Release) || schema > answer.SchemaVersion {
		return apiError(event, http.StatusConflict, errorCodeServerMoveVersionOlder, "The new server is older than this one; update it first.")
	}
	// A key's call asks its person once the new server answered and can take
	// this one's data (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	code.To = reached
	from := firstNonEmpty(configuredPublicOrigin(), serverMoveRequestOrigin(event.Request))
	out.set(func(status *administratorServerMoveOut) {
		*status = administratorServerMoveOut{State: serverMoveOutCopying, To: reached, Release: answer.Release,
			StartedAt: time.Now().UTC().Format(time.RFC3339), Counts: countServerContents(event.App)}
	})
	go out.run(code, from)
	out.mu.Lock()
	status := out.status
	out.mu.Unlock()
	return event.JSON(http.StatusAccepted, status)
}

func (in *serverMoveIn) slotID() string {
	in.mu.Lock()
	defer in.mu.Unlock()
	if slot := in.current(); slot != nil {
		return slot.code.ID
	}
	return ""
}

// run stops changes, sends the snapshot and has the new server restore it.
func (out *serverMoveOut) run(code serverMoveCode, from string) {
	app := out.app
	fail := func(stage string, err error) {
		logDurableJobDiagnostic(app, errorCodeServerMoveFailed, newDiagnosticReference(), "", "server-move", stage, err)
		_ = os.Remove(filepath.Join(app.DataDir(), replicaFencedName))
		forgetDataMoved()
		if settings, loadErr := loadServerTunnelSettings(app); loadErr == nil {
			_ = serverTunnelOf(app).apply(settings)
		}
		out.set(func(status *administratorServerMoveOut) {
			status.State, status.ErrorCode = serverMoveOutFailed, errorCodeServerMoveFailed
			if stage == "busy" {
				status.ErrorCode = errorCodeServerMoveBusy
			}
		})
	}
	if app.Store().Has(core.StoreKeyActiveBackup) {
		fail("busy", errors.New("a backup or restore is running"))
		return
	}
	app.Store().Set(core.StoreKeyActiveBackup, "leafen-server-move")
	defer app.Store().Remove(core.StoreKeyActiveBackup)
	// From here nothing changes here: not a request, not a live edit, and
	// the address goes to the new server with the data.
	if err := writeRestoreFile(app.DataDir(), replicaFencedName, replicaFence{MovedTo: code.To, At: time.Now().UnixMilli()}); err != nil {
		fail("fence", err)
		return
	}
	forgetDataMoved()
	if settings, err := loadServerTunnelSettings(app); err == nil {
		tunnel := serverTunnelOf(app)
		tunnel.stop()
		if settings.Mode != serverTunnelOff && settings.Mode != "" {
			tunnel.set(serverTunnelStateOff, "", remoteAccessHeld(app))
		}
		tunnel.noteAddressChange(settings)
	}
	time.Sleep(serverMoveSettle)
	counts := countServerContents(app)
	out.set(func(status *administratorServerMoveOut) { status.Counts = counts })
	ctx := context.Background()
	store, err := newWebDAVBackupStore(ctx, code.To+apiRoot+"/server-move/in/"+code.ID+"/dav/", code.ID, code.Secret, scheduledBackupSpool(app))
	if err != nil {
		fail("store", err)
		return
	}
	outcome, err := writeScheduledBackupSnapshotWith(ctx, app, store, 1, passwordBackupSecret(code.Secret), time.Now())
	if err != nil {
		fail("snapshot", err)
		return
	}
	out.set(func(status *administratorServerMoveOut) { status.State = serverMoveOutFinishing })
	finish := serverMoveFinishInput{Snapshot: outcome.Snapshot, SHA256: outcome.ManifestSHA256, From: from, Counts: counts}
	// A server that replicated hands its replica on with its mark.
	if mark, err := os.ReadFile(filepath.Join(app.DataDir(), replicaHolderName)); err == nil {
		if finish.Holder, err = sealForMove(code.Secret, mark); err != nil {
			fail("holder", err)
			return
		}
	}
	response, err := serverMoveAsk(ctx, code, code.To, http.MethodPost, "/finish", finish, serverMoveFinishTimeout)
	if err != nil {
		fail("finish", err)
		return
	}
	status := response.StatusCode
	_ = response.Body.Close()
	if status != http.StatusAccepted {
		fail("finish", errors.New("the new server answered "+http.StatusText(status)))
		return
	}
	// The mark is the new server's now: kept here too, this server could
	// later take the replica it no longer holds for its own.
	if finish.Holder != "" {
		_ = os.Remove(filepath.Join(app.DataDir(), replicaHolderName))
	}
	out.set(func(status *administratorServerMoveOut) { status.State = serverMoveOutMoved })
	app.Logger().Warn("server move: this server's data moved; it takes no more changes", "movedTo", code.To)
}
