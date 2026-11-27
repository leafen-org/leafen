package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Bringing a lost server back at a new one's first start (2026-11-06): the
// setup screen's small link under the name (2026-11-19) opens the restore,
// so a server whose machine is gone comes back without first making a
// workspace only to throw it away — by the person here, or left to an agent
// (setup_restore_agent.go). The person names the place the
// old server backed up to; the screen lists its continuous replica and its
// snapshots, and the one chosen is put together and switched in at the next
// start, as a restore asked for in the app is (backup_restore_in_app.go).
// It is open only while the server isn't set up, behind the setup code where
// one is asked — from outside the server's network (setup_code.go): who may
// set the server up may fill it from a backup.

type setupRestoreInput struct {
	SetupCode      string `json:"setupCode"`
	Target         string `json:"target"`
	Folder         string `json:"folder"`
	WebDAVURL      string `json:"webdavUrl"`
	WebDAVUsername string `json:"webdavUsername"`
	WebDAVPassword string `json:"webdavPassword"`
	S3Endpoint     string `json:"s3Endpoint"`
	S3Region       string `json:"s3Region"`
	S3Bucket       string `json:"s3Bucket"`
	S3Prefix       string `json:"s3Prefix"`
	S3AccessKey    string `json:"s3AccessKey"`
	S3SecretKey    string `json:"s3SecretKey"`
	S3PathStyle    bool   `json:"s3PathStyle"`
	// What opens sealed backups: their recovery code or password.
	Code     string `json:"code"`
	Password string `json:"password"`
	// What to restore: "replica", or a snapshot's name.
	Choice string `json:"choice"`
}

type setupRestorePlace struct {
	Replica   administratorReplica                   `json:"replica"`
	Snapshots []administratorScheduledBackupSnapshot `json:"snapshots"`
}

type setupRestoreResult struct {
	Restart string `json:"restart"`
	// The pass this browser signs in with once the restored server starts
	// (setup_restore_sign_in.go).
	SignIn string `json:"signIn,omitempty"`
	// A restore still being put together answers with how far it has got
	// and the watch its screen follows it by (setup_restore_apart.go).
	Watch    string                       `json:"watch,omitempty"`
	Progress *administratorBackupProgress `json:"progress,omitempty"`
}

const setupRestoreChoiceReplica = "replica"

func registerSetupRestoreRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/setup/restore/place", readSetupRestorePlace)
	event.Router.POST(apiRoot+"/setup/restore", requestSetupRestore)
	event.Router.POST(apiRoot+"/setup/restore/sign-in", signInAfterSetupRestore)
	event.Router.POST(apiRoot+"/setup/restore/progress", readSetupRestoreProgress)
	registerSetupRestoreAgentRoutes(event)
}

// place is the backup place the input names, with its secrets as given: a
// folder (the one holding "Leafen backups", or that folder itself), a
// WebDAV folder, or S3-compatible storage.
func (input setupRestoreInput) place(app core.App) (storedScheduledBackup, bool) {
	place := storedScheduledBackup{Target: strings.TrimSpace(input.Target)}
	if place.Target == "" {
		place.Target = scheduledBackupTargetFolder
	}
	switch place.Target {
	case scheduledBackupTargetFolder:
		// A phone's picked folder is the app's name for it, not a path.
		if len(input.Folder) <= maxScheduledBackupFolderLength && deviceBackupFolderNamed(input.Folder) {
			place.Folder = input.Folder
			break
		}
		folder := filepath.Clean(strings.TrimSpace(input.Folder))
		if input.Folder == "" || !filepath.IsAbs(folder) || len(folder) > maxScheduledBackupFolderLength ||
			pathWithin(app.DataDir(), folder) {
			return place, false
		}
		if filepath.Base(folder) == scheduledBackupRootName {
			folder = filepath.Dir(folder)
		}
		place.Folder = folder
	case scheduledBackupTargetWebDAV:
		place.WebDAVURL, place.WebDAVUsername = strings.TrimSpace(input.WebDAVURL), strings.TrimSpace(input.WebDAVUsername)
		place.WebDAVPassword = input.WebDAVPassword
		if place.WebDAVURL == "" || len(place.WebDAVURL) > maxScheduledBackupWebDAVURLLength ||
			len(place.WebDAVUsername) > maxScheduledBackupWebDAVUsernameLength || len(place.WebDAVPassword) > maxScheduledBackupPasswordLength {
			return place, false
		}
	case scheduledBackupTargetS3:
		place.S3Endpoint, place.S3Region = strings.TrimSpace(input.S3Endpoint), strings.TrimSpace(input.S3Region)
		place.S3Bucket, place.S3AccessKey = strings.TrimSpace(input.S3Bucket), strings.TrimSpace(input.S3AccessKey)
		place.S3Prefix = strings.Trim(strings.TrimSpace(input.S3Prefix), "/")
		place.S3Secret, place.S3PathStyle = input.S3SecretKey, input.S3PathStyle
		if place.S3Region == "" {
			place.S3Region = "us-east-1"
		}
		if place.S3Endpoint == "" || place.S3Bucket == "" || len(place.S3Endpoint) > maxScheduledBackupWebDAVURLLength ||
			len(place.S3Region) > maxScheduledBackupS3FieldLength || len(place.S3Bucket) > maxScheduledBackupS3FieldLength ||
			len(place.S3Prefix) > maxScheduledBackupS3FieldLength || len(place.S3AccessKey) > maxScheduledBackupS3FieldLength ||
			len(place.S3Secret) > maxScheduledBackupPasswordLength {
			return place, false
		}
	default:
		return place, false
	}
	return place, len(input.Code) <= maxScheduledBackupPasswordLength && len(input.Password) <= maxScheduledBackupPasswordLength
}

// The place's secrets come as typed, not sealed in a database.
func setupRestoreSecretAsGiven(value string) (string, error) { return value, nil }

func (input setupRestoreInput) secret() scheduledBackupSecret {
	return scheduledBackupSecret{Code: strings.TrimSpace(input.Code), Password: input.Password}
}

var (
	// errSetupRestoreNothing is a folder that holds no Leafen backups.
	errSetupRestoreNothing = errors.New("the folder holds no Leafen backups")
	// errSetupRestoreAnswered is a request already answered with why it can't go on.
	errSetupRestoreAnswered = errors.New("the request was answered")
)

// setupRestoreRefusal is why a first-setup restore can't go on, as the
// screen's routes and an agent's restore tools (setup_restore_agent.go)
// both answer it.
type setupRestoreRefusal struct {
	status  int
	code    string
	message string
	cause   error
}

func (refusal *setupRestoreRefusal) Error() string { return refusal.message }

func refuseSetupRestore(status int, code, message string, cause error) *setupRestoreRefusal {
	return &setupRestoreRefusal{status: status, code: code, message: message, cause: cause}
}

// answerSetupRestoreError answers a request with why the restore can't go on.
func answerSetupRestoreError(event *core.RequestEvent, err error) error {
	var refusal *setupRestoreRefusal
	if errors.As(err, &refusal) {
		return apiErrorFrom(event, refusal.status, refusal.code, refusal.message, refusal.cause)
	}
	return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't restore the backup.", err)
}

// openSetupRestorePlace binds the input, lets the first setup through and
// opens the place the input names (openSetupRestoreStore).
func openSetupRestorePlace(event *core.RequestEvent) (setupRestoreInput, storedScheduledBackup, scheduledBackupStore, error) {
	input := setupRestoreInput{}
	if err := event.BindBody(&input); err != nil {
		_ = apiError(event, http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "Name the place the backups are in.")
		return input, storedScheduledBackup{}, nil, errSetupRestoreAnswered
	}
	if !admitFirstSetup(event, input.SetupCode) {
		return input, storedScheduledBackup{}, nil, errSetupRestoreAnswered
	}
	// Read apart from the request: a restore may go on past its answer.
	place, store, err := openSetupRestoreStore(context.WithoutCancel(event.Request.Context()), event.App, input)
	if err != nil && !errors.Is(err, errSetupRestoreNothing) {
		_ = answerSetupRestoreError(event, err)
		return input, place, nil, errSetupRestoreAnswered
	}
	return input, place, store, err
}

// openSetupRestoreStore opens the place the input names. A folder that is
// there but holds no backups is not opened (opening one would make its
// backup folder), and says so with errSetupRestoreNothing.
func openSetupRestoreStore(ctx context.Context, app core.App, input setupRestoreInput) (storedScheduledBackup, scheduledBackupStore, error) {
	place, ok := input.place(app)
	if !ok {
		return place, nil, refuseSetupRestore(http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "Name the place the backups are in.", nil)
	}
	if place.Target == scheduledBackupTargetFolder {
		if store, ok, err := openDeviceBackupFolder(place.Folder); ok {
			if err != nil {
				return place, nil, refuseSetupRestore(http.StatusNotFound, errorCodeScheduledBackupFolderUnavailable, "That folder can't be read.", nil)
			}
			store, err = openDeviceRestoreFolder(store)
			return place, store, err
		}
		if info, err := os.Stat(place.Folder); err != nil || !info.IsDir() {
			return place, nil, refuseSetupRestore(http.StatusNotFound, errorCodeScheduledBackupFolderUnavailable, "That folder can't be read.", nil)
		}
		if info, err := os.Stat(filepath.Join(place.Folder, scheduledBackupRootName)); err != nil || !info.IsDir() {
			return place, nil, errSetupRestoreNothing
		}
	}
	store, err := openScheduledBackupStoreWith(ctx, place, setupRestoreSecretAsGiven, scheduledBackupSpool(app))
	if err != nil {
		return place, nil, refuseSetupRestore(http.StatusServiceUnavailable, snapshotListErrorCode(err), "Couldn't read the backup place.", nil)
	}
	return place, store, nil
}

// listSetupRestorePlace is what the place holds to restore: its continuous
// replica, if it has one, and its snapshots, newest first.
func listSetupRestorePlace(ctx context.Context, app core.App, input setupRestoreInput, store scheduledBackupStore) (setupRestorePlace, error) {
	result := setupRestorePlace{Replica: administratorReplica{Owner: replicaOwnerNone}, Snapshots: []administratorScheduledBackupSnapshot{}}
	snapshots, err := listBackupSnapshotsIn(store, input.secret())
	if err != nil {
		return result, refuseSetupRestore(http.StatusServiceUnavailable, snapshotListErrorCode(err), "Couldn't read the backups.", nil)
	}
	result.Snapshots = snapshots
	if source, err := readReplicaSource(store, input.secret()); err == nil {
		result.Replica = source.describe(ctx, app)
	}
	return result, nil
}

func readSetupRestorePlace(event *core.RequestEvent) error {
	input, _, store, err := openSetupRestorePlace(event)
	if errors.Is(err, errSetupRestoreNothing) {
		return event.JSON(http.StatusOK, setupRestorePlace{Replica: administratorReplica{Owner: replicaOwnerNone},
			Snapshots: []administratorScheduledBackupSnapshot{}})
	}
	if err != nil {
		return nil
	}
	defer closeScheduledBackupStore(store)
	result, err := listSetupRestorePlace(event.Request.Context(), event.App, input, store)
	if err != nil {
		return answerSetupRestoreError(event, err)
	}
	return event.JSON(http.StatusOK, result)
}

// requestSetupRestore restores what the screen chose and starts the server
// again on it, apart from the request (setup_restore_apart.go).
func requestSetupRestore(event *core.RequestEvent) error {
	input, place, store, err := openSetupRestorePlace(event)
	if errors.Is(err, errSetupRestoreNothing) {
		return apiError(event, http.StatusNotFound, errorCodeScheduledBackupSnapshotUnavailable, "There are no Leafen backups in that folder.")
	}
	if err != nil {
		return nil
	}
	return requestSetupRestoreApart(event, input, place, store)
}

// restartAfterSetupRestore starts the server again on what was restored,
// where it starts itself again.
func restartAfterSetupRestore(app core.App, restart string) {
	if restart != restoreRestartAutomatic {
		return
	}
	time.AfterFunc(restoreRestartDelay, func() {
		if err := restartForRestore(app); err != nil {
			logDurableJobDiagnostic(app, errorCodeScheduledBackupRestoreFailed, newDiagnosticReference(), "",
				"setup-restore", "restore-restart", err)
		}
	})
}

// stageSetupRestore puts the chosen snapshot, or the replica, together
// beside the data and leaves the note the next start follows. What the fresh
// server held stays aside, as a restore keeps the data from before.
func stageSetupRestore(ctx context.Context, app core.App, input setupRestoreInput, place storedScheduledBackup, store scheduledBackupStore) error {
	choice := strings.TrimSpace(input.Choice)
	if choice != setupRestoreChoiceReplica && !restoreSnapshotNameValid(choice) {
		return refuseSetupRestore(http.StatusBadRequest, errorCodeScheduledBackupInputInvalid, "Choose a backup to restore.", nil)
	}
	work := restoreWorkDir(app.DataDir())
	if pending, _, err := readRestoreStatus(app.DataDir()); err != nil {
		return refuseSetupRestore(http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't read the restore state.", err)
	} else if pending != nil {
		return refuseSetupRestore(http.StatusConflict, errorCodeScheduledBackupRestorePending, "A restore waits for the server to start again.", nil)
	}
	if app.Store().Has(core.StoreKeyActiveBackup) {
		return refuseSetupRestore(http.StatusConflict, errorCodeScheduledBackupBusy, "A backup or restore is running.", nil)
	}
	app.Store().Set(core.StoreKeyActiveBackup, "leafen-setup-restore")
	defer app.Store().Remove(core.StoreKeyActiveBackup)
	// How far it has got shows on the screen and in the agent's status, as
	// a restore in the app does (backup_progress.go).
	progress := startBackupProgress(app, backupProgressStoreKey, backupProgressKindRestore, "")
	defer endBackupProgress(app, backupProgressStoreKey, progress)
	if choice == setupRestoreChoiceReplica {
		return stageSetupReplicaRestore(withBackupProgress(ctx, progress), app, store, place, input.secret(), work)
	}
	source, err := openScheduledBackupSnapshotWith(store, scheduledBackupRootName,
		scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupSnapshotsName, choice), "", input.secret())
	if err != nil {
		return refuseSetupRestore(http.StatusNotFound, errorCodeScheduledBackupSnapshotUnavailable, "That backup can't be opened.", nil)
	}
	source.progress = progress
	// The restored server keeps backing up where the person found its
	// backups: a folder may be mounted at another path on this machine.
	keepFolder := func(staged string) error {
		if place.Target != scheduledBackupTargetFolder {
			return nil
		}
		return keepBackupFolderAfterRestore(filepath.Join(staged, dataDatabaseFile), keptBackupFolder(place, store))
	}
	var refused restoreRefused
	if err := stageRestore(work, source, choice, time.Now(), keepFolder); errors.As(err, &refused) {
		return refuseSetupRestore(http.StatusUnprocessableEntity, refused.codeOr(errorCodeScheduledBackupSnapshotUnavailable),
			"That backup can't be restored here: "+refused.message+".", nil)
	} else if errors.Is(err, errRestoreAssemble) {
		return setupRestoreAssembleRefusal(app, "restore-assemble", err, errorCodeScheduledBackupSnapshotUnavailable,
			"That backup doesn't hold what its list says")
	} else if err != nil {
		return refuseSetupRestore(http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't put the backup together.", err)
	}
	return nil
}

// stageSetupReplicaRestore puts the place's replica together, takes it over
// and leaves the note, as a replica restore asked for in the app does
// (replica_restore.go).
func stageSetupReplicaRestore(ctx context.Context, app core.App, store scheduledBackupStore, place storedScheduledBackup,
	secret scheduledBackupSecret, work string) error {
	source, err := readReplicaSource(store, secret)
	if err != nil {
		return refuseSetupRestore(http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't read the backup place.", nil)
	}
	if source.owner == nil {
		return refuseSetupRestore(http.StatusNotFound, errorCodeReplicaUnavailable, "There's no replica in this place.", nil)
	}
	if !source.readable {
		return refuseSetupRestore(http.StatusUnprocessableEntity, errorCodeReplicaUnavailable, "The backup password doesn't open this replica.", nil)
	}
	staged := filepath.Join(work, restoreStagedName)
	if err := errors.Join(os.RemoveAll(staged), os.MkdirAll(staged, 0o700)); err != nil {
		return refuseSetupRestore(http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't put the replica together.", err)
	}
	if _, _, err := source.assemble(ctx, staged); err != nil {
		_ = os.RemoveAll(staged)
		return setupRestoreAssembleRefusal(app, "replica-assemble", err, errorCodeReplicaUnavailable, "The replica can't be put together")
	}
	backupProgressFrom(ctx).step(backupProgressStepFinishing, 0, 0)
	var refused restoreRefused
	if err := stagedRestoreRefusal(staged); errors.As(err, &refused) {
		_ = os.RemoveAll(staged)
		return refuseSetupRestore(http.StatusUnprocessableEntity, refused.codeOr(errorCodeReplicaUnavailable),
			"The replica can't be restored here: "+refused.message+".", nil)
	}
	if place.Target == scheduledBackupTargetFolder {
		if err := keepBackupFolderAfterRestore(filepath.Join(staged, dataDatabaseFile), keptBackupFolder(place, store)); err != nil {
			_ = os.RemoveAll(staged)
			return refuseSetupRestore(http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't put the replica together.", err)
		}
	}
	if err := source.takeOver(app.DataDir(), replicaPlace(place)); err != nil {
		_ = os.RemoveAll(staged)
		return refuseSetupRestore(http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't take the replica over.", nil)
	}
	now := time.Now()
	if err := leaveRestoreNote(staged, work, pendingRestore{
		Kind: restoreKindRestore, Phase: restorePhaseReady, Snapshot: replicaRestoreName(now),
		Source: restoreStagedName, Keep: restoreBeforePrefix + restoreStamp(now), RequestedAt: now.UnixMilli(), TakesOver: true,
	}); err != nil {
		_ = os.RemoveAll(staged)
		return refuseSetupRestore(http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't put the replica together.", err)
	}
	return nil
}

// setupRestoreAssembleRefusal is a backup that couldn't be put together:
// what its place or this server's disk said, where that is why, else the
// backup itself — with the reason after the message and in the log
// (backup_failure_reason.go).
func setupRestoreAssembleRefusal(app core.App, stage string, err error, backupCode, message string) error {
	code, status := backupCode, http.StatusUnprocessableEntity
	if placeCode := restorePlaceFailureCode(err); placeCode != "" {
		code, status, message = placeCode, http.StatusServiceUnavailable, "Couldn't read the backup from its place"
		if placeCode == errorCodeScheduledBackupNoSpace {
			status, message = http.StatusInsufficientStorage, "There's no room on this server to put the backup together"
		}
	}
	reason := backupFailureReason(err)
	logBackupFailure(app, code, "setup-restore", stage, err, reason)
	if reason != "" {
		message += ": " + reason
	}
	return refuseSetupRestore(status, code, message+".", err)
}
