package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/benbjohnson/litestream"
	"github.com/pocketbase/pocketbase/core"
)

// A standby server (decided 2026-10-17): a second Leafen server given the
// primary's connection code follows the primary's continuous replica — the
// database through Litestream's following restore, the files through the
// replica's list — into a folder of its own, beats beside it, and watches the
// primary's beat (replica_lease.go). When that beat stands still long
// enough, or when an administrator says so, it takes the replica over and
// switches the followed data in at its next start, the way a restore does;
// it then goes on as the primary. Its version must not be older than the
// primary's: a newer one takes over and upgrades the data as it starts,
// which is how a pair updates with only a moment's pause. Migration 547
// keeps the standby's settings beside the backup place's.

const (
	standbyCodePrefix   = "leafen-standby-1:"
	standbyFolderName   = ".leafen-standby"
	standbyTakeoverName = "takeover.json"
	standbyDismissName  = "standby-dismissed.json"
	// The standby beats, reads the primary's beat and its file list this
	// often; while taking over it goes on following long enough for the
	// primary it replaces to read the new owner.
	standbyBeatEvery  = replicaBeatEvery
	standbyWatchEvery = 2 * time.Second
	standbyFilesEvery = 5 * time.Second
	standbyFinalWait  = replicaOwnerCheckEvery + 2*time.Second
)

// standbyCode is what the connection code carries: the place, its keys,
// the backups' password, and the primary's address.
type standbyCode struct {
	Target         string `json:"target"`
	Folder         string `json:"folder,omitempty"`
	WebDAVURL      string `json:"webdavUrl,omitempty"`
	WebDAVUsername string `json:"webdavUsername,omitempty"`
	WebDAVPassword string `json:"webdavPassword,omitempty"`
	S3Endpoint     string `json:"s3Endpoint,omitempty"`
	S3Region       string `json:"s3Region,omitempty"`
	S3Bucket       string `json:"s3Bucket,omitempty"`
	S3Prefix       string `json:"s3Prefix,omitempty"`
	S3AccessKey    string `json:"s3AccessKey,omitempty"`
	S3SecretKey    string `json:"s3SecretKey,omitempty"`
	S3PathStyle    bool   `json:"s3PathStyle,omitzero"`
	Password       string `json:"password,omitempty"`
	// BackupKey and BackupKeyRecord are the primary's backup key and its
	// record (scheduled_backup_protection.go), in place of the password of
	// backups a password alone seals.
	BackupKey       string `json:"backupKey,omitempty"`
	BackupKeyRecord string `json:"backupKeyRecord,omitempty"`
	PrimaryOrigin   string `json:"primaryOrigin,omitempty"`
	PrimaryVersion  string `json:"primaryVersion"`
	// PairSecret signs the pair's direct checks (replica_pair_check.go).
	PairSecret string `json:"pairSecret,omitempty"`
	// JoinToken is this code's; a newer code closes it (replica_pair_check.go).
	JoinToken string `json:"joinToken,omitempty"`
}

func standbyFolder(app core.App) string {
	return filepath.Join(app.DataDir(), core.LocalBackupsDirName, standbyFolderName)
}

// standbyStatus is what the screen shows of the pair on either side.
type standbyStatus struct {
	PrimaryOrigin  string
	PrimaryVersion string
	Followed       uint64
	FollowedAt     time.Time
	PrimaryStill   time.Duration
	SeenPrimary    bool
	ErrorCode      string
	TakingOver     bool
	DirectCheck    string
}

// standbySession follows one primary's replica.
type standbySession struct {
	runner  *replicationRunner
	place   string
	holder  string
	store   scheduledBackupStore
	key     *scheduledBackupKey
	keyID   string
	folder  string
	cancel  context.CancelFunc
	done    chan error
	watch   beatWatch
	beat    int64
	primary replicaLease
	// followed is the last transaction followed, and when it changed.
	followed   uint64
	followedAt time.Time

	lastBeat  time.Time
	lastWatch time.Time
	lastFiles time.Time
	applied   map[string]string
	// The pair's direct check (replica_pair_check.go): the secret, the
	// primary's address from the code, the last check and what it found.
	pairSecret    string
	primaryOrigin string
	lastAsk       time.Time
	direct        string
}

func (runner *replicationRunner) startStandby(ctx context.Context, stored storedScheduledBackup) (*standbySession, error) {
	app := runner.app
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
	if err != nil || source.owner == nil || !source.readable {
		closeScheduledBackupStore(store)
		if err == nil {
			err = errStandbyReplicaUnreadable
		}
		return nil, err
	}
	holder, err := loadReplicaHolder(app.DataDir())
	if err != nil {
		closeScheduledBackupStore(store)
		return nil, err
	}
	session := &standbySession{runner: runner, place: replicaPlace(stored), holder: holder.Holder, store: store, key: source.key,
		keyID: source.keyID, folder: standbyFolder(app), applied: map[string]string{}, beat: time.Now().UnixMilli(),
		pairSecret: holder.PairSecret, primaryOrigin: stored.StandbyPrimaryOrigin}
	if err := os.MkdirAll(filepath.Join(session.folder, "files"), 0o700); err != nil {
		session.close()
		return nil, err
	}
	session.loadApplied()
	followCtx, cancel := context.WithCancel(context.Background())
	session.cancel, session.done = cancel, make(chan error, 1)
	go func() {
		replica := followingReplica(app, source.client())
		options := litestream.NewRestoreOptions()
		options.OutputPath = filepath.Join(session.folder, dataDatabaseFile)
		options.Follow = true
		options.FollowInterval = time.Second
		err := replica.Restore(followCtx, options)
		// What was followed can't go on from the replica as it is now: the copy
		// is only a copy, so it is read anew rather than asked for again forever.
		if err != nil && strings.Contains(err.Error(), "cannot resume follow mode") {
			removeFollowedCopy(options.OutputPath)
			err = replica.Restore(followCtx, options)
		}
		session.done <- err
	}()
	return session, nil
}

func removeFollowedCopy(path string) {
	for _, suffix := range []string{"", "-txid", "-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}

// tick beats, watches the primary, and brings the files along; it answers
// whether the primary is gone long enough to take over.
func (session *standbySession) tick(ctx context.Context, now time.Time) (bool, error) {
	select {
	case err := <-session.done:
		// Following stopped by itself: start again.
		session.done <- err
		if err == nil {
			err = errors.New("following stopped")
		}
		return false, err
	default:
	}
	dismissed := map[string]string{}
	if present, err := readReplicaJSON(session.store, standbyDismissName, &dismissed); err == nil && present && dismissed["holder"] == session.holder {
		return false, errStandbyDismissed
	}
	if now.Sub(session.lastBeat) >= standbyBeatEvery {
		session.lastBeat = now
		session.beat++
		followed, _ := litestream.ReadTXIDFile(filepath.Join(session.folder, dataDatabaseFile))
		if uint64(followed) != session.followed {
			session.followed, session.followedAt = uint64(followed), now
		}
		if err := writeReplicaJSON(session.store, replicaStandbyName, replicaStandbyBeat{Holder: session.holder, Beat: session.beat,
			Version: ReleaseVersion(), Origin: configuredPublicOrigin(), Followed: uint64(followed), PairedAt: now.UnixMilli()}); err != nil {
			return false, err
		}
	}
	gone := false
	if now.Sub(session.lastWatch) >= standbyWatchEvery {
		session.lastWatch = now
		lease := replicaLease{}
		present, err := readReplicaJSON(session.store, replicaLeaseName, &lease)
		if err != nil {
			return false, err
		}
		owner, err := readReplicaOwner(session.store)
		if err != nil {
			return false, err
		}
		if owner != nil && owner.Holder == session.holder {
			// Taken over already (a takeover cut off before its restart).
			return true, nil
		}
		// The primary's beat, by this server's clock: standing still from
		// when this server first saw it, or since it last moved.
		if present && owner != nil && lease.Holder == owner.Holder {
			session.primary = lease
			gone = session.watch.see(lease.Holder, lease.Beat, now) >= replicaTakeOverAfter
		}
	}
	// A primary whose beat is late hears from its standby directly, and
	// goes on through a place's outage while it does; the first check, as
	// the standby starts, says on the screen whether the pair reach each
	// other at all, and one unanswered is asked again now and then.
	late := session.watch.still(now) >= 2*replicaBeatEvery && now.Sub(session.lastAsk) >= replicaBeatEvery
	if session.primary.Holder != "" && (late || replicaPairRecheckDue(session.direct, session.lastAsk, now)) {
		session.lastAsk = now
		session.askPrimary(ctx, session.primaryOrigin)
	}
	if now.Sub(session.lastFiles) >= standbyFilesEvery {
		session.lastFiles = now
		if err := session.syncFiles(); err != nil {
			return gone, err
		}
	}
	return gone, nil
}

var errStandbyDismissed = errors.New("the primary let this standby go")

// errStandbyReplicaUnreadable is a place with no primary's replica a standby
// can open.
var errStandbyReplicaUnreadable = errors.New("the primary's replica isn't there, or doesn't open")

// syncFiles brings the followed files to the replica's list.
func (session *standbySession) syncFiles() error {
	held, err := readReplicaFileList(session.store, scheduledBackupRootName, session.key)
	if err != nil {
		return err
	}
	root := filepath.Join(session.folder, "files")
	changed := false
	for path, entry := range held {
		if session.applied[path] == entry.SHA256 {
			continue
		}
		content, err := session.key.reader(session.store, scheduledBackupBlobStorePath(scheduledBackupRootName, session.key.blobName(entry.SHA256)))
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(path))
		partial := target + ".partial"
		_ = os.Remove(partial)
		err = os.MkdirAll(filepath.Dir(target), 0o700)
		if err == nil {
			err = restoreScheduledBackupFile(content, partial, entry)
		}
		content.Close()
		if err == nil {
			err = os.Rename(partial, target)
		}
		if err != nil {
			_ = os.Remove(partial)
			return err
		}
		session.applied[path] = entry.SHA256
		changed = true
	}
	for path := range session.applied {
		if _, ok := held[path]; !ok {
			_ = os.Remove(filepath.Join(root, filepath.FromSlash(path)))
			delete(session.applied, path)
			changed = true
		}
	}
	if changed {
		return session.saveApplied()
	}
	return nil
}

func (session *standbySession) appliedPath() string {
	return filepath.Join(session.folder, "applied.json")
}

func (session *standbySession) loadApplied() {
	_ = readRestoreFile(session.appliedPath(), &session.applied)
	if session.applied == nil {
		session.applied = map[string]string{}
	}
}

func (session *standbySession) saveApplied() error {
	return writeRestoreFile(session.folder, "applied.json", session.applied)
}

func (session *standbySession) close() {
	if session.cancel != nil {
		session.cancel()
		<-session.done
		session.cancel = nil
	}
	closeScheduledBackupStore(session.store)
}

// takeOver makes the standby the primary: it names itself the replica's
// owner, follows long enough for the primary it replaces to stop, brings the
// files along, and leaves the followed data for the next start to switch in.
func (runner *replicationRunner) takeOver(ctx context.Context, session *standbySession, stored storedScheduledBackup) error {
	app := runner.app
	if NewerReleaseVersion(session.primary.Version, ReleaseVersion()) {
		return errStandbyOlder
	}
	// The primary's data from before the schema baseline isn't taken over:
	// this server wouldn't start on it, after the primary had stopped.
	if err := schemaHistoryRefusal(session.folder); errors.Is(err, errSchemaHistoryBeforeBaseline) {
		return err
	}
	// A primary that still answers hands over first, so nothing it took is
	// lost; one that doesn't answer in time is taken over as it stands.
	var ready replicaHandoverReady
	if session.primary.Holder != "" && session.watch.still(time.Now()) < replicaPauseAfter {
		nonce, err := randomHex(16)
		if err != nil {
			return err
		}
		// An answer left from an earlier asking goes first: what comes after
		// answers this one, carrying its nonce, or none from a primary of a
		// version before nonces (a pair updates the standby first).
		if err := store404(session.store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), replicaHandoverReadyName))); err != nil {
			return err
		}
		if err := writeReplicaJSON(session.store, replicaHandoverName, replicaHandover{Holder: session.holder, Nonce: nonce}); err != nil {
			return err
		}
		for deadline := time.Now().Add(replicaHandoverWait); time.Now().Before(deadline); {
			answer := replicaHandoverReady{}
			if present, err := readReplicaJSON(session.store, replicaHandoverReadyName, &answer); err == nil && present &&
				answer.Holder == session.primary.Holder && (answer.Nonce == nonce || answer.Nonce == "") {
				ready = answer
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	if err := writeReplicaOwner(session.store, replicaOwner{Holder: session.holder, Origin: configuredPublicOrigin(),
		KeyID: session.keyID, Since: time.Now().UnixMilli()}); err != nil {
		return err
	}
	for _, name := range []string{replicaHandoverName, replicaHandoverReadyName} {
		_ = store404(session.store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), name)))
	}
	holder, err := loadReplicaHolder(app.DataDir())
	if err != nil {
		return err
	}
	holder.Claimed, holder.Paired = replicaPlace(stored), false
	if err := saveReplicaHolder(app.DataDir(), holder); err != nil {
		return err
	}
	// Follow on until the handed-over transaction is here, or, from a
	// primary that didn't answer, long enough for it to read the new owner.
	wait, step := standbyFinalWait, standbyFinalWait
	if ready.TXID > 0 {
		wait, step = replicaHandoverWait, 250*time.Millisecond
	}
	followed := filepath.Join(session.folder, dataDatabaseFile)
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); {
		if txid, err := litestream.ReadTXIDFile(followed); err == nil && ready.TXID > 0 && uint64(txid) >= ready.TXID {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(step):
		}
	}
	session.cancel()
	if err := <-session.done; err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("following the replica: %w", err)
	}
	session.cancel = nil
	if err := session.syncFiles(); err != nil {
		return err
	}
	_ = store404(session.store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), replicaStandbyName)))
	work := restoreWorkDir(app.DataDir())
	staged := filepath.Join(work, restoreStagedName)
	if err := errors.Join(os.RemoveAll(staged), os.MkdirAll(work, 0o700)); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(session.folder, "files"), staged); err != nil {
		return err
	}
	for _, name := range []string{dataDatabaseFile, dataDatabaseFile + "-wal", dataDatabaseFile + "-shm"} {
		if err := os.Rename(filepath.Join(session.folder, name), filepath.Join(staged, name)); err != nil &&
			(name == dataDatabaseFile || !errors.Is(err, fs.ErrNotExist)) {
			return err
		}
	}
	if stored.Target == scheduledBackupTargetFolder {
		if err := keepBackupFolderAfterRestore(filepath.Join(staged, dataDatabaseFile), stored.Folder); err != nil {
			return err
		}
	}
	now := time.Now()
	if err := leaveRestoreNote(staged, work, pendingRestore{
		Kind: restoreKindRestore, Phase: restorePhaseReady, Snapshot: "standby-" + now.UTC().Format("20060102T150405Z"),
		Source: restoreStagedName, Keep: restoreBeforePrefix + restoreStamp(now), RequestedAt: now.UnixMilli(), TakesOver: true,
	}, func() error {
		return writeRestoreFile(work, standbyTakeoverName, map[string]any{"at": now.UnixMilli(), "from": session.primary.Origin})
	}); err != nil {
		return err
	}
	_ = os.RemoveAll(session.folder)
	app.Logger().Warn("standby: took the replica over; starting again as the primary", "from", session.primary.Origin)
	go func() {
		time.Sleep(restoreRestartDelay)
		if err := restartForRestore(app); err != nil {
			app.Logger().Error("standby: couldn't start again to take over", "error", err)
		}
	}()
	return nil
}

var errStandbyOlder = errors.New("this standby is older than the primary")

func store404(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// takeoverMailOrigin is the primary's address in brackets after "the
// primary", lead before them, or nothing for a primary with no outside
// address (2026-11-11: the mail said "(no address)").
func takeoverMailOrigin(lead, from string) string {
	if from == "" {
		return ""
	}
	return lead + "(" + from + ")"
}

// announceTakeover tells administrators, once, after a standby took over.
func announceTakeover(app core.App) {
	work := restoreWorkDir(app.DataDir())
	marker := filepath.Join(work, standbyTakeoverName)
	takeover := map[string]any{}
	if readRestoreFile(marker, &takeover) != nil {
		return
	}
	if err := os.Rename(marker, filepath.Join(work, "announced-"+standbyTakeoverName)); err != nil {
		return
	}
	settings, err := loadAdministratorSettings(app)
	if err != nil || !notificationEmailConfigured(settings) {
		return
	}
	from, _ := takeover["from"].(string)
	_ = mailServerAdministrators(app, settings, "standby-takeover-alert-failed", func(recipient, language string) []byte {
		var subject, body string
		if language == accountLanguageKo {
			subject = "리펜 예비 서버로 전환했습니다"
			body = "주 서버" + takeoverMailOrigin("", from) + "가 응답하지 않아 예비 서버로 전환했습니다. 마지막 몇 초의 변경이 빠졌을 수 있습니다.\r\n" +
				"옛 주 서버는 다시 켜져도 변경을 받지 않습니다. 설정 → 서버 → 예비 서버에서 새 예비 서버를 추가하세요.\r\n"
		} else {
			subject = "Leafen’s standby server took over"
			body = "The primary" + takeoverMailOrigin(" ", from) + " stopped answering, so the standby took over. Changes from the last few seconds may be missing.\r\n" +
				"The old primary takes no changes if it comes back. Pair a new standby in Settings → Server → Standby server.\r\n"
		}
		if origin := configuredPublicOrigin(); origin != "" {
			body += "\r\n" + origin + "\r\n"
		}
		return serverAdministratorMail(settings, recipient, subject, body)
	})
}
