package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A first-setup restore left to an agent (user decision 2026-11-19): the
// setup screen's "에이전트에게 맡기기" gives the person a sentence to paste to
// their agent, with a restore code. The code is the screen's own: only a
// request the first setup lets through (admitFirstSetup) gets one, and it
// opens nothing but this one restore on a server with no administrator —
// finding the backups in a place, and restoring one of them. It can't make
// the first administrator, read anything else or write anything else, and it
// ends when the restore is under way, when the server gets an administrator
// by any way, when the screen goes back, or half an hour after it was made.
// It travels only in request bodies and tool arguments — never in an
// address — and is kept here only as its hash. The backups' password or
// recovery code and the storage's keys are typed by the person on the
// screen, never through the agent: the agent's calls name the place only,
// and the restore uses what the screen sent. Once the agent restores, the
// screen, which holds the restore's sign-in pass from the start, signs in as
// the restored server's owner, as after a restore of its own
// (setup_restore_sign_in.go). An agent elsewhere reaches the server through
// an outside way opened for the restore alone (setup_restore_agent_outside.go),
// over MCP or this JSON API (setup_restore_agent_mcp.go).

// How long a restore code lasts unused (a variable for tests).
var setupRestoreAgentLifetime = 30 * time.Minute

const (
	setupRestoreAgentStoreKey   = "leafen.setupRestoreAgent"
	setupRestoreAgentWatchEvery = 2 * time.Second
	setupRestoreAgentPath       = apiRoot + "/setup/restore/agent"

	errorCodeSetupRestoreCodeInvalid = "LEAFEN_SETUP_RESTORE_CODE_INVALID"

	// Why the last restore code ended, as the screen says it.
	setupRestoreAgentEndedExpired  = "expired"
	setupRestoreAgentEndedSetUp    = "set-up"
	setupRestoreAgentEndedClosed   = "closed"
	setupRestoreAgentEndedRestored = "restored"
)

// setupRestoreAgentPlace is where the backups are, as the agent names it:
// no password or key, which the screen sends.
type setupRestoreAgentPlace struct {
	Target         string `json:"target"`
	Folder         string `json:"folder,omitempty"`
	WebDAVURL      string `json:"webdavUrl,omitempty"`
	WebDAVUsername string `json:"webdavUsername,omitempty"`
	S3Endpoint     string `json:"s3Endpoint,omitempty"`
	S3Region       string `json:"s3Region,omitempty"`
	S3Bucket       string `json:"s3Bucket,omitempty"`
	S3Prefix       string `json:"s3Prefix,omitempty"`
	S3PathStyle    bool   `json:"s3PathStyle,omitzero"`
}

// setupRestoreAgentSecrets is what the person typed on the screen.
type setupRestoreAgentSecrets struct {
	WebDAVPassword string `json:"webdavPassword"`
	S3AccessKey    string `json:"s3AccessKey"`
	S3SecretKey    string `json:"s3SecretKey"`
	// The backups' password or recovery code, in one field as the screen's own restore takes it.
	Secret string `json:"secret"`
}

// setupRestoreAgentTyped says which of the secrets the person typed.
type setupRestoreAgentTyped struct {
	WebDAVPassword bool `json:"webdavPassword"`
	S3AccessKey    bool `json:"s3AccessKey"`
	S3SecretKey    bool `json:"s3SecretKey"`
	Secret         bool `json:"secret"`
}

// setupRestoreAgentFound is what the agent's last look found.
type setupRestoreAgentFound struct {
	Replica   bool `json:"replica"`
	Snapshots int  `json:"snapshots"`
	// Some of it opens only with the backups' password or recovery code.
	Sealed bool `json:"sealed"`
}

type setupRestoreAgentSession struct {
	codeHash, signInHash string
	expires              time.Time
	outside              string
	place                setupRestoreAgentPlace
	placed               bool
	secrets              setupRestoreAgentSecrets
	found                *setupRestoreAgentFound
	agentSeen            bool
	running              bool
	failure              string
	restart              string
	stop                 chan struct{}
}

type setupRestoreAgentKeeper struct {
	mu      sync.Mutex
	session *setupRestoreAgentSession
	ended   string
}

func setupRestoreAgentOf(app core.App) *setupRestoreAgentKeeper {
	return app.Store().GetOrSet(setupRestoreAgentStoreKey, func() any { return &setupRestoreAgentKeeper{} }).(*setupRestoreAgentKeeper)
}

// input is the restore input the agent's place and the screen's secrets make.
func (session *setupRestoreAgentSession) input(choice string) setupRestoreInput {
	place, secrets := session.place, session.secrets
	return setupRestoreInput{
		Target: place.Target, Folder: place.Folder, WebDAVURL: place.WebDAVURL, WebDAVUsername: place.WebDAVUsername,
		WebDAVPassword: secrets.WebDAVPassword, S3Endpoint: place.S3Endpoint, S3Region: place.S3Region, S3Bucket: place.S3Bucket,
		S3Prefix: place.S3Prefix, S3AccessKey: secrets.S3AccessKey, S3SecretKey: secrets.S3SecretKey, S3PathStyle: place.S3PathStyle,
		Code: strings.TrimSpace(secrets.Secret), Password: secrets.Secret, Choice: choice,
	}
}

func setupRestoreAgentSecret() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

// begin ends any restore code before and makes a new one, with the screen's
// sign-in pass and, where asked, the outside way for it.
func (keeper *setupRestoreAgentKeeper) begin(app core.App, outside string, now time.Time) (code, pass string, expires time.Time, err error) {
	if code, err = setupRestoreAgentSecret(); err != nil {
		return "", "", time.Time{}, err
	}
	if pass, err = newSetupRestoreSignInPass(); err != nil {
		return "", "", time.Time{}, err
	}
	keeper.end(app, setupRestoreAgentEndedClosed)
	session := &setupRestoreAgentSession{codeHash: mcpTokenHash(code), signInHash: mcpTokenHash(pass),
		expires: now.Add(setupRestoreAgentLifetime), outside: outside, stop: make(chan struct{})}
	keeper.mu.Lock()
	keeper.session, keeper.ended = session, ""
	keeper.mu.Unlock()
	if outside != "" {
		openSetupRestoreOutside(app, outside)
	}
	// Until it ends, or the server stops (server_background.go).
	serverBackgroundOf(app).run("restore code watch", func(ctx context.Context) { keeper.watch(ctx, app, session) })
	return code, pass, session.expires, nil
}

// end ends the restore code there is, saying why, and closes its outside way.
func (keeper *setupRestoreAgentKeeper) end(app core.App, why string) {
	keeper.mu.Lock()
	session := keeper.session
	if session != nil {
		keeper.session, keeper.ended = nil, why
		close(session.stop)
	}
	keeper.mu.Unlock()
	if session != nil && session.outside != "" {
		closeSetupRestoreOutside(app)
	}
}

// watch ends the code when its time is up or the server got an
// administrator, whichever way.
func (keeper *setupRestoreAgentKeeper) watch(ctx context.Context, app core.App, session *setupRestoreAgentSession) {
	ticker := time.NewTicker(setupRestoreAgentWatchEvery)
	defer ticker.Stop()
	for {
		select {
		case <-session.stop:
			return
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			keeper.mu.Lock()
			current, restoring := keeper.session == session, session.running || session.restart != ""
			keeper.mu.Unlock()
			if !current {
				return
			}
			if now.After(session.expires) && !restoring {
				keeper.end(app, setupRestoreAgentEndedExpired)
				return
			}
			if configured, err := administratorConfigured(app); err == nil && configured && !restoring {
				keeper.end(app, setupRestoreAgentEndedSetUp)
				return
			}
		}
	}
}

// admit finds the session a restore code opens: one that is the current
// code, not past its time nor used, on a server with no administrator. A
// read (reading) still finds it once the restore is done, to say so.
func (keeper *setupRestoreAgentKeeper) admit(ctx context.Context, app core.App, code string, reading bool) (*setupRestoreAgentSession, error) {
	configured, err := administratorConfiguredContext(ctx, app)
	if err != nil {
		return nil, refuseSetupRestore(http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the setup state.", err)
	}
	if configured {
		keeper.end(app, setupRestoreAgentEndedSetUp)
		return nil, refuseSetupRestore(http.StatusConflict, errorCodeSetupClosed,
			"This Leafen is set up already, so its restore code has ended: the person logs in on the screen.", nil)
	}
	invalid := refuseSetupRestore(http.StatusUnauthorized, errorCodeSetupRestoreCodeInvalid,
		"This restore code isn't valid any more. Ask the person for a new one: on Leafen's setup screen, "+
			"Restore from a backup → Leave it to an agent.", nil)
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 128 {
		return nil, invalid
	}
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	session := keeper.session
	if session == nil || subtle.ConstantTimeCompare([]byte(session.codeHash), []byte(mcpTokenHash(code))) != 1 {
		return nil, invalid
	}
	if session.restart != "" && !reading {
		return nil, refuseSetupRestore(http.StatusConflict, errorCodeScheduledBackupRestorePending,
			"The restore is done and the server is starting again on it, so the restore code has ended.", nil)
	}
	if time.Now().After(session.expires) && !session.running {
		return nil, invalid
	}
	session.agentSeen = true
	return session, nil
}

// statusFor is what the agent is told of the restore its code is for; on a
// server set up already — by the restore, or by its first administrator
// made another way — that it is, whatever the code.
func (keeper *setupRestoreAgentKeeper) statusFor(ctx context.Context, app core.App, code string) (setupRestoreAgentStatus, error) {
	if configured, err := administratorConfiguredContext(ctx, app); err == nil && configured {
		keeper.end(app, setupRestoreAgentEndedSetUp)
		return setupRestoreAgentStatus{State: "set-up", Next: "Leafen is set up now, so the restore code has ended. If the restore " +
			"was run, the server started on it. " + setupRestoreAgentCheck}, nil
	}
	session, err := keeper.admit(ctx, app, code, true)
	if err != nil {
		return setupRestoreAgentStatus{}, err
	}
	keeper.mu.Lock()
	defer keeper.mu.Unlock()
	status := session.status()
	if session.running {
		// How far it has got (backup_progress.go).
		status.Progress = runningBackupProgress(app, backupProgressStoreKey).snapshot()
	}
	return status, nil
}

// setupRestoreAgentCheck is how the agent sees that the restore came back:
// itself, through its own connection to the restored server, rather than
// asking the person (2026-11-22, the user: everything a person can do, an
// agent does to the end). Its connection to the old Leafen comes back with
// the backup; without one it connects again, the person's hand only
// allowing that connection's code (mcp_device_connections.go). The screen
// signs the person in by itself (setup_restore_sign_in.go).
const setupRestoreAgentCheck = "Check it yourself rather than asking the person: once the server answers at its own address " +
	"(GET /api/leafen/v1/health), call leafen_list_workspaces through your connection to it — the one you had to the old Leafen, " +
	"which comes back with the backup. Left to do then: which workspaces are back, to them. Without such a connection (made after this backup, or never), " +
	"connect again: `leafen connect --name \"<your name>\"` on the server's computer, else POST /api/leafen/v1/mcp/device-connections " +
	"{\"name\": \"<your name>\"} at its address and follow its answer — allowing its code is the one step that is theirs: left to do, " +
	"just that, marked, to them. The setup screen logs them in by itself; only if it shows Leafen's login instead do they log in there with " +
	"the old server's account."

// setupRestoreAgentStatus is what an agent is told of the restore.
type setupRestoreAgentStatus struct {
	// waiting (for the agent to name the place), ready (backups found),
	// restoring, or restored (the server starts again on it).
	State     string                  `json:"state"`
	ExpiresAt int64                   `json:"expiresAt"`
	Place     *setupRestoreAgentPlace `json:"place,omitempty"`
	Found     *setupRestoreAgentFound `json:"found,omitempty"`
	// What the person typed on the screen: which, never the values.
	Typed   setupRestoreAgentTyped `json:"typed"`
	Failure string                 `json:"failure,omitempty"`
	Next    string                 `json:"next"`
	// How far a restore under way has got: its step, bytes and files.
	Progress *administratorBackupProgress `json:"progress,omitempty"`
}

func (session *setupRestoreAgentSession) typed() setupRestoreAgentTyped {
	return setupRestoreAgentTyped{WebDAVPassword: session.secrets.WebDAVPassword != "", S3AccessKey: session.secrets.S3AccessKey != "",
		S3SecretKey: session.secrets.S3SecretKey != "", Secret: session.secrets.Secret != ""}
}

// status says where the restore is; the caller holds the keeper's lock.
func (session *setupRestoreAgentSession) status() setupRestoreAgentStatus {
	status := setupRestoreAgentStatus{State: "waiting", ExpiresAt: session.expires.UnixMilli(), Found: session.found,
		Typed: session.typed(), Failure: session.failure}
	if session.placed {
		place := session.place
		status.Place = &place
	}
	switch {
	case session.restart != "":
		status.State = "restored"
		status.Next = "The restore is done. Leafen starts again on the restored data (with the desktop app or on Windows, when the person " +
			"opens it again) and the setup screen logs the person in as the restored server's owner. The restore code has ended, " +
			"and so has any outside way opened for it; the restored server's own address and agent connections come back from the backup. " +
			setupRestoreAgentCheck
	case session.running:
		status.State = "restoring"
		status.Next = "The backup is being put together: progress says its step and the bytes and files done of all it has. " +
			"Left to do: how far it has got, to the person, and leafen_restore_status again in a few seconds."
	case session.found != nil:
		status.State = "ready"
		status.Next = session.nextAfterFound()
	default:
		status.Next = "Ask the person where the old Leafen backed up to — a folder the server's computer reads, a WebDAV server or " +
			"S3-compatible storage — and name that place with leafen_restore_find. Don't ask for passwords or keys in the " +
			"conversation: the person types them on the setup screen."
	}
	return status
}

// nextAfterFound says what the person types on the screen, or what to restore.
func (session *setupRestoreAgentSession) nextAfterFound() string {
	asks := []string{}
	typed := session.typed()
	switch session.place.Target {
	case scheduledBackupTargetWebDAV:
		if !typed.WebDAVPassword {
			asks = append(asks, "the WebDAV password")
		}
	case scheduledBackupTargetS3:
		if !typed.S3AccessKey || !typed.S3SecretKey {
			asks = append(asks, "the access key and secret key")
		}
	}
	if session.found.Sealed && !typed.Secret {
		asks = append(asks, "the backups' password or recovery code")
	}
	if !session.found.Replica && session.found.Snapshots == 0 {
		if len(asks) > 0 {
			return "Nothing could be read here yet. Ask the person to type " + strings.Join(asks, " and ") +
				" in the fields that show on Leafen's setup screen; then look again with leafen_restore_find."
		}
		return "There are no Leafen backups in this place. Ask the person where else the old Leafen backed up to."
	}
	if len(asks) > 0 {
		return "Some of it opens only once the person types " + strings.Join(asks, " and ") +
			" in the fields that show on Leafen's setup screen; then look again with leafen_restore_find."
	}
	if session.found.Replica {
		return "Here are a continuous replica, which holds the latest changes, and snapshots, earlier points. Left to do: that to the " +
			"person, and once they choose, leafen_restore_run for it."
	}
	return "Snapshots are here. Left to do: which, newest first, to the person, and once they choose, leafen_restore_run for it."
}

// find looks in the place the agent names, with what the screen typed.
func (keeper *setupRestoreAgentKeeper) find(ctx context.Context, app core.App, session *setupRestoreAgentSession,
	place setupRestoreAgentPlace) (setupRestorePlace, setupRestoreAgentStatus, error) {
	place.Target = strings.TrimSpace(place.Target)
	if place.Target == "" {
		place.Target = scheduledBackupTargetFolder
	}
	keeper.mu.Lock()
	if session.running {
		keeper.mu.Unlock()
		return setupRestorePlace{}, setupRestoreAgentStatus{}, refuseSetupRestore(http.StatusConflict, errorCodeScheduledBackupBusy, "The restore is under way.", nil)
	}
	session.place, session.placed, session.found, session.failure = place, true, nil, ""
	input := session.input("")
	keeper.mu.Unlock()
	result := setupRestorePlace{Replica: administratorReplica{Owner: replicaOwnerNone}, Snapshots: []administratorScheduledBackupSnapshot{}}
	_, store, err := openSetupRestoreStore(ctx, app, input)
	if err == nil {
		result, err = listSetupRestorePlace(ctx, app, input, store)
		closeScheduledBackupStore(store)
	}
	if err != nil && !errors.Is(err, errSetupRestoreNothing) {
		keeper.mu.Lock()
		session.failure = err.Error()
		status := session.status()
		keeper.mu.Unlock()
		return result, status, err
	}
	found := &setupRestoreAgentFound{Replica: result.Replica.Present, Sealed: result.Replica.Present && !result.Replica.Readable}
	for _, snapshot := range result.Snapshots {
		found.Snapshots++
		found.Sealed = found.Sealed || !snapshot.Readable
	}
	keeper.mu.Lock()
	session.found = found
	status := session.status()
	keeper.mu.Unlock()
	return result, status, nil
}

// run restores the choice in the background, so that a long one outlasts no
// agent's call: leafen_restore_status follows it.
func (keeper *setupRestoreAgentKeeper) run(app core.App, session *setupRestoreAgentSession, choice string) (setupRestoreAgentStatus, error) {
	choice = strings.TrimSpace(choice)
	if choice != setupRestoreChoiceReplica && !restoreSnapshotNameValid(choice) {
		return setupRestoreAgentStatus{}, refuseSetupRestore(http.StatusBadRequest, errorCodeScheduledBackupInputInvalid,
			`Choose "replica" or a snapshot's name from leafen_restore_find.`, nil)
	}
	keeper.mu.Lock()
	if session.running {
		keeper.mu.Unlock()
		return setupRestoreAgentStatus{}, refuseSetupRestore(http.StatusConflict, errorCodeScheduledBackupBusy, "The restore is under way.", nil)
	}
	if !session.placed {
		keeper.mu.Unlock()
		return setupRestoreAgentStatus{}, refuseSetupRestore(http.StatusBadRequest, errorCodeScheduledBackupInputInvalid,
			"Name the place first with leafen_restore_find.", nil)
	}
	session.running, session.failure = true, ""
	input := session.input(choice)
	status := session.status()
	keeper.mu.Unlock()
	go keeper.restore(app, session, input)
	return status, nil
}

func (keeper *setupRestoreAgentKeeper) restore(app core.App, session *setupRestoreAgentSession, input setupRestoreInput) {
	ctx := context.Background()
	place, store, err := openSetupRestoreStore(ctx, app, input)
	if errors.Is(err, errSetupRestoreNothing) {
		err = refuseSetupRestore(http.StatusNotFound, errorCodeScheduledBackupSnapshotUnavailable, "There are no Leafen backups in that folder.", nil)
	}
	if err == nil {
		err = stageSetupRestore(ctx, app, input, place, store)
		closeScheduledBackupStore(store)
	}
	keeper.mu.Lock()
	session.running = false
	if err != nil {
		session.failure = err.Error()
		keeper.mu.Unlock()
		return
	}
	restart := restoreRestartMode(app)
	session.restart = restart
	keeper.ended = setupRestoreAgentEndedRestored
	keeper.mu.Unlock()
	if err := leaveSetupRestoreSignIn(app.DataDir(), session.signInHash, time.Now()); err != nil {
		// The restore goes ahead; the screen signs in from a device instead.
		app.Logger().Warn("leafen setup restore sign-in pass failed", "error", err)
	}
	// The outside way ends with the restore: at once where the server
	// waits to be started again, else as it starts again, a moment after
	// the agent heard.
	if restart != restoreRestartAutomatic && session.outside != "" {
		time.AfterFunc(restoreRestartDelay, func() { closeSetupRestoreOutside(app) })
	}
	restartAfterSetupRestore(app, restart)
}
