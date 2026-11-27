package app

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/benbjohnson/litestream"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// Restoring from the continuous replica (decided 2026-10-17): another server
// given the same backup place puts the replica's database and files
// together — a few seconds behind the server that wrote it — and takes the
// replica over, so that server, back again, takes no more changes. From the
// screen it goes the way a snapshot restore does: put together beside the
// data, switched in at the next start, the data from before kept to undo
// it. From the command line, into a fresh data folder:
//
//	leafen restore --replica --input "/path/to/Leafen backups"

// replicaSource is a place's replica opened to read.
type replicaSource struct {
	store    scheduledBackupStore
	key      *scheduledBackupKey
	keyID    string
	sealed   bool
	readable bool
	owner    *replicaOwner
}

// openReplicaSource opens the schedule's place to read its replica.
func openReplicaSource(ctx context.Context, app core.App, stored storedScheduledBackup) (*replicaSource, error) {
	store, err := openScheduledBackupStore(ctx, app, stored)
	if err != nil {
		return nil, err
	}
	secret, err := scheduledBackupSecretOf(app, stored)
	if err != nil {
		closeScheduledBackupStore(store)
		return nil, err
	}
	source, err := readReplicaSource(store, secret)
	if err != nil {
		closeScheduledBackupStore(store)
		return nil, err
	}
	return source, nil
}

// readReplicaSource reads a replica's owner and how it is sealed: a sealed
// one is readable with a secret that opens the key it was sealed under.
func readReplicaSource(store scheduledBackupStore, secret scheduledBackupSecret) (*replicaSource, error) {
	source := &replicaSource{store: store, readable: true}
	owner, err := readReplicaOwner(store)
	if err != nil {
		return nil, err
	}
	source.owner = owner
	if owner != nil && owner.KeyID != "" {
		source.sealed = true
		source.keyID = owner.KeyID
		keys, err := scheduledBackupKeysIn(store, scheduledBackupRootName, secret)
		for _, key := range keys {
			if err == nil && key.id == owner.KeyID {
				source.key = key
			}
		}
		if source.key == nil {
			source.readable = false
			return source, nil
		}
	}
	return source, nil
}

func (source *replicaSource) close() { closeScheduledBackupStore(source.store) }

func (source *replicaSource) client() *replicaStoreClient {
	return &replicaStoreClient{store: source.store, root: scheduledBackupStoreJoin(replicaRoot(), replicaDatabaseName), key: source.key}
}

// describe is what the screen shows of the replica.
func (source *replicaSource) describe(ctx context.Context, app core.App) administratorReplica {
	described := administratorReplica{Owner: replicaOwnerNone, Sealed: source.sealed, Readable: source.readable}
	if source.owner == nil {
		return described
	}
	described.Present = true
	described.Owner = replicaOwnerOther
	if holder, err := loadReplicaHolder(app.DataDir()); err == nil && holder.Holder == source.owner.Holder {
		described.Owner = replicaOwnerThis
	}
	described.OwnerOrigin = source.owner.Origin
	if !source.readable {
		return described
	}
	if latest, txid, err := replicaLatestChange(ctx, source.client()); err == nil && txid > 0 {
		described.LastChangeAt = latest.UTC().Format(time.RFC3339)
	} else if err == nil {
		described.Present = false
	}
	return described
}

// assemble writes the replica's data folder into target: the database as
// of its last change, then every file of its list, each checked.
func (source *replicaSource) assemble(ctx context.Context, target string) (int, int64, error) {
	if !source.readable {
		return 0, 0, errScheduledBackupPassword
	}
	replica := litestream.NewReplicaWithClient(nil, source.client())
	options := litestream.NewRestoreOptions()
	options.OutputPath = filepath.Join(target, dataDatabaseFile)
	options.IntegrityCheck = litestream.IntegrityCheckQuick
	// Restored as a whole by litestream: its step shows, its bytes don't.
	progress := backupProgressFrom(ctx)
	progress.step(backupProgressStepDatabase, 0, 1)
	if err := replica.Restore(ctx, options); err != nil {
		return 0, 0, err
	}
	progress.done(0, 1)
	files, bytes, err := assembleReplicaFiles(source.store, scheduledBackupRootName, source.key, target, progress)
	if err != nil {
		return 0, 0, err
	}
	if info, err := os.Stat(options.OutputPath); err == nil {
		bytes += info.Size()
	}
	return files, bytes, nil
}

// takeOver names this server the replica's owner, so the one that wrote it
// takes no more changes once it reads the name.
func (source *replicaSource) takeOver(dataDir string, place string) error {
	holder, err := loadReplicaHolder(dataDir)
	if err != nil {
		return err
	}
	if err := writeReplicaOwner(source.store, replicaOwner{Holder: holder.Holder, Origin: configuredPublicOrigin(),
		KeyID: source.keyID, Since: time.Now().UnixMilli()}); err != nil {
		return err
	}
	holder.Claimed = place
	return saveReplicaHolder(dataDir, holder)
}

// requestReplicaRestore puts the place's replica together beside the data,
// takes the replica over and leaves the note the next start follows, then
// starts the server again where it can.
func requestReplicaRestore(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	schedule, err := loadScheduledBackup(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed, "Couldn't read the backup schedule.", err)
	}
	if !schedule.targetSet() {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupInputInvalid, "Choose where backups go first.")
	}
	work := restoreWorkDir(event.App.DataDir())
	if pending, _, err := readRestoreStatus(event.App.DataDir()); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't read the restore state.", err)
	} else if pending != nil {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupRestorePending, "A restore waits for the server to start again.")
	}
	// One backup or restore at a time; the restore holds it until it ends,
	// past the answer if need be.
	if event.App.Store().Has(core.StoreKeyActiveBackup) {
		return apiError(event, http.StatusConflict, errorCodeScheduledBackupBusy, "A backup or restore is running.")
	}
	event.App.Store().Set(core.StoreKeyActiveBackup, "leafen-replica-restore")
	handed := false
	defer func() {
		if !handed {
			event.App.Store().Remove(core.StoreKeyActiveBackup)
		}
	}()
	// Read apart from the request, which may be answered first.
	source, err := openReplicaSource(context.WithoutCancel(event.Request.Context()), event.App, schedule)
	if err != nil {
		return apiErrorFrom(event, http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't read the backup place.", err)
	}
	defer func() {
		if !handed {
			source.close()
		}
	}()
	if source.owner == nil {
		return apiError(event, http.StatusNotFound, errorCodeReplicaUnavailable, "There's no replica in this place.")
	}
	if !source.readable {
		return apiError(event, http.StatusUnprocessableEntity, errorCodeReplicaUnavailable, "The backup password doesn't open this replica.")
	}
	// A key's call asks its person once the replica is there and opens
	// (user_key_acting_ask.go); whether it can be put together shows only as
	// it is.
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	// Put together apart from the request, showing how far it has got
	// (backup_restore_background.go).
	handed = true
	return runRestoreAnswering(event, stagedRestore{
		operation: "replica",
		stage: func(ctx context.Context, progress *backupProgress) error {
			return stageReplicaRestore(withBackupProgress(ctx, progress), event.App, source, schedule, work)
		},
		release: source.close,
		codeOf:  replicaRestoreFailureCode,
		secrets: scheduledBackupSecrets(event.App, schedule),
		answer: func(err error, reason string) error {
			if err == nil {
				return answerRestoreRequest(event, schedule)
			}
			message := "Couldn't put the replica together"
			var refusal replicaRestoreRefusal
			if errors.As(err, &refusal) {
				message = strings.TrimSuffix(refusal.message, ".")
				if refusal.cause == nil {
					// The refusal says why itself.
					reason = ""
				}
			}
			return answerRestoreFailure(event, replicaRestoreFailureCode(err), message, reason)
		},
	})
}

// replicaRestoreFailureCode is why a replica couldn't be restored: what its
// place or this server's disk said, else its refusal's code.
func replicaRestoreFailureCode(err error) string {
	if code := restorePlaceFailureCode(err); code != "" {
		return code
	}
	var refusal replicaRestoreRefusal
	if errors.As(err, &refusal) {
		return refusal.code
	}
	return errorCodeScheduledBackupRestoreFailed
}

// replicaRestoreRefusal is why a replica couldn't be restored, as its request
// answers it.
type replicaRestoreRefusal struct {
	status  int
	code    string
	message string
	cause   error
}

func (refusal replicaRestoreRefusal) Error() string { return refusal.message }

func (refusal replicaRestoreRefusal) Unwrap() error { return refusal.cause }

// stageReplicaRestore puts the replica together beside the data, takes it
// over and leaves the note the next start follows.
func stageReplicaRestore(ctx context.Context, app core.App, source *replicaSource, schedule storedScheduledBackup, work string) error {
	staged := filepath.Join(work, restoreStagedName)
	if err := errors.Join(os.RemoveAll(staged), os.MkdirAll(staged, 0o700)); err != nil {
		return replicaRestoreRefusal{http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't put the replica together.", err}
	}
	if _, _, err := source.assemble(ctx, staged); err != nil {
		_ = os.RemoveAll(staged)
		return replicaRestoreRefusal{http.StatusUnprocessableEntity, errorCodeReplicaUnavailable, "The replica can't be put together.", err}
	}
	backupProgressFrom(ctx).step(backupProgressStepFinishing, 0, 0)
	var refused restoreRefused
	if err := stagedRestoreRefusal(staged); errors.As(err, &refused) {
		_ = os.RemoveAll(staged)
		return replicaRestoreRefusal{http.StatusUnprocessableEntity, refused.codeOr(errorCodeReplicaUnavailable),
			"The replica can't be restored here: " + refused.message + ".", nil}
	}
	if schedule.Target == scheduledBackupTargetFolder {
		if err := keepBackupFolderAfterRestore(filepath.Join(staged, dataDatabaseFile), schedule.Folder); err != nil {
			_ = os.RemoveAll(staged)
			return replicaRestoreRefusal{http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't put the replica together.", err}
		}
	}
	// Taken over before the note: a start that finds the note runs on the
	// replica this server now holds.
	if err := source.takeOver(app.DataDir(), replicaPlace(schedule)); err != nil {
		_ = os.RemoveAll(staged)
		return replicaRestoreRefusal{http.StatusServiceUnavailable, errorCodeScheduledBackupFolderUnavailable, "Couldn't take the replica over.", nil}
	}
	now := time.Now()
	if err := leaveRestoreNote(staged, work, pendingRestore{
		Kind: restoreKindRestore, Phase: restorePhaseReady, Snapshot: replicaRestoreName(now),
		Source: restoreStagedName, Keep: restoreBeforePrefix + restoreStamp(now), RequestedAt: now.UnixMilli(), TakesOver: true,
	}); err != nil {
		_ = os.RemoveAll(staged)
		return replicaRestoreRefusal{http.StatusInternalServerError, errorCodeScheduledBackupRestoreFailed, "Couldn't put the replica together.", err}
	}
	return nil
}

// replicaRestoreName names a restore from the replica where a snapshot's
// name goes.
func replicaRestoreName(now time.Time) string {
	return "replica-" + now.UTC().Format("20060102T150405Z")
}

// restoreReplicaIntoFreshRoot restores a replica from the command line: from
// a "Leafen backups" folder given, or else from the place the backup
// environment names, into a fresh data folder, then takes it over.
func restoreReplicaIntoFreshRoot(ctx context.Context, pb *pocketbase.PocketBase, input string, secret scheduledBackupSecret) (restoreReceipt, error) {
	var store scheduledBackupStore
	place, folderGiven := "", ""
	if strings.TrimSpace(input) != "" {
		folder, err := filepath.Abs(strings.TrimSpace(input))
		if err != nil {
			return restoreReceipt{}, err
		}
		if pathWithin(pb.DataDir(), folder) {
			return restoreReceipt{}, errors.New("input path must be outside the Leafen data root")
		}
		// The folder given is "Leafen backups" itself; the store is the
		// folder holding it.
		if filepath.Base(folder) != scheduledBackupRootName {
			folder = filepath.Join(folder, scheduledBackupRootName)
		}
		if _, err := os.Stat(filepath.Join(folder, replicaFolderName)); errors.Is(err, fs.ErrNotExist) {
			return restoreReceipt{}, errors.New("the folder holds no replica")
		}
		store = newFolderBackupStore(filepath.Dir(folder))
		folderGiven = filepath.Dir(folder)
		place = replicaPlace(storedScheduledBackup{Target: scheduledBackupTargetFolder, Folder: filepath.Dir(folder)})
	} else {
		stored, err := loadScheduledBackup(pb)
		if err != nil {
			return restoreReceipt{}, err
		}
		if !stored.SetByEnvironment {
			return restoreReceipt{}, errors.New("give the backup folder with --input, or name the place in the backup environment")
		}
		if store, err = openScheduledBackupStore(ctx, pb, stored); err != nil {
			return restoreReceipt{}, err
		}
		place = replicaPlace(stored)
	}
	defer closeScheduledBackupStore(store)
	source, err := readReplicaSource(store, secret)
	if err != nil {
		return restoreReceipt{}, err
	}
	if source.owner == nil {
		return restoreReceipt{}, errors.New("there's no replica in this place")
	}
	if !source.readable {
		if secret.empty() {
			return restoreReceipt{}, errScheduledBackupSealed
		}
		return restoreReceipt{}, errScheduledBackupPassword
	}
	if err := requireFreshRestoreTarget(pb); err != nil {
		return restoreReceipt{}, err
	}
	var bytes int64
	dataRoot, err := restoreDataRoot(pb, func(extractedRoot string) error {
		var assembleErr error
		_, bytes, assembleErr = source.assemble(ctx, extractedRoot)
		if assembleErr != nil {
			return assembleErr
		}
		if folderGiven != "" {
			if err := keepBackupFolderAfterRestore(filepath.Join(extractedRoot, dataDatabaseFile), folderGiven); err != nil {
				return err
			}
		}
		// The restored folder carries this server's mark, now the owner's.
		return source.takeOver(extractedRoot, place)
	})
	if err != nil {
		return restoreReceipt{}, err
	}
	return restoreReceipt{Status: "restored", Path: dataRoot, Bytes: bytes}, nil
}
