package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pocketbase/pocketbase/tools/security"
)

// The desktop and phone apps' startup recovery screen (user decision
// 2026-10-21): when the app's own server can't start, it says why in the
// words a person acts on and offers the last backup to go back to. The
// database may not open then, so the server notes its last backup beside the
// data after each one: the schedule's place and what opens it, sealed as the
// database keeps them — the key that opens those is the data folder's own
// (.secrets), so the note shows nothing the folder didn't hold already.

const lastBackupNoteName = "last-backup.json"

// Why the app's own server couldn't start, as the screen tells it.
const (
	StartupDataLocked  = "locked"
	StartupDataDamaged = "damaged"
	StartupDiskFull    = "disk_full"
)

// ErrBackupNeedsSecret is a sealed backup the data folder's own key doesn't
// open: its recovery code or password does.
var ErrBackupNeedsSecret = errors.New("the backup needs its recovery code or password")

// ErrRestoreWaiting is a restore already waiting for the next start.
var ErrRestoreWaiting = errors.New("a restore waits for the server to start again")

// lastBackupNote is the last backup the server made: when, its snapshot and
// list's SHA-256, and the schedule it was made under.
type lastBackupNote struct {
	MadeAt   int64                 `json:"madeAt"`
	Snapshot string                `json:"snapshot"`
	SHA256   string                `json:"sha256"`
	Schedule storedScheduledBackup `json:"schedule"`
}

func writeLastBackupNote(dataDir string, schedule storedScheduledBackup, outcome scheduledBackupOutcome, at time.Time) error {
	return writeRestoreFile(restoreWorkDir(dataDir), lastBackupNoteName, lastBackupNote{
		MadeAt: at.UnixMilli(), Snapshot: outcome.Snapshot, SHA256: outcome.ManifestSHA256, Schedule: schedule,
	})
}

func readLastBackupNote(dataDir string) (lastBackupNote, bool) {
	note := lastBackupNote{}
	if err := readRestoreFile(filepath.Join(restoreWorkDir(dataDir), lastBackupNoteName), &note); err != nil ||
		note.MadeAt == 0 || !restoreSnapshotNameValid(note.Snapshot) || !note.Schedule.targetSet() {
		return lastBackupNote{}, false
	}
	return note, true
}

// startupDiskRoom is the free space under which a start that failed is the
// disk's doing: a full disk fails as SQLite unable to open its files (14),
// since it can't make the side files a database opens with, and with this
// little left freeing room is what to do anyway.
const startupDiskRoom = 64 << 20

// StartupFailureReason names why the data in dataDir couldn't open: another
// program holding the database, the database damaged, or the disk full; ""
// for anything else.
func StartupFailureReason(dataDir string, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, syscall.ENOSPC) {
		return StartupDiskFull
	}
	// SQLite's result codes, the primary one in the low byte.
	var coded interface{ Code() int }
	if errors.As(err, &coded) {
		switch coded.Code() & 0xff {
		case 5, 6: // SQLITE_BUSY, SQLITE_LOCKED
			return StartupDataLocked
		case 11, 26: // SQLITE_CORRUPT, SQLITE_NOTADB
			return StartupDataDamaged
		case 13: // SQLITE_FULL
			return StartupDiskFull
		}
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "database is locked"), strings.Contains(text, "database table is locked"):
		return StartupDataLocked
	case strings.Contains(text, "malformed"), strings.Contains(text, "not a database"):
		return StartupDataDamaged
	case strings.Contains(text, "disk is full"), strings.Contains(text, "no space left"), strings.Contains(text, "not enough space on the disk"):
		return StartupDiskFull
	}
	if free, _, ok := dataDiskSpace(dataDir); ok && free < startupDiskRoom {
		return StartupDiskFull
	}
	return ""
}

// LastBackup says when the server's last backup was made, the one the
// recovery screen restores, if it noted one.
func LastBackup(dataDir string) (time.Time, bool) {
	note, ok := readLastBackupNote(dataDir)
	if !ok {
		return time.Time{}, false
	}
	return time.UnixMilli(note.MadeAt), true
}

// RestoreLastBackup puts the server's last backup together beside the data
// while the server is off and leaves the note its next start follows, as a
// restore asked for in the app does: the data held now goes aside, kept to
// undo the restore. A sealed backup opens with what the data folder holds,
// else with the recovery code or password given.
func RestoreLastBackup(ctx context.Context, dataDir, code, password string) error {
	note, ok := readLastBackupNote(dataDir)
	if !ok {
		return errors.New("no backup is noted")
	}
	if pending, _, err := readRestoreStatus(dataDir); err != nil {
		return err
	} else if pending != nil {
		return ErrRestoreWaiting
	}
	decrypt := dataFolderDecrypter(dataDir)
	secret, err := scheduledBackupSecretWith(note.Schedule, decrypt)
	if err != nil {
		secret = scheduledBackupSecret{}
	}
	if code = strings.TrimSpace(code); code != "" {
		secret.Code = code
	}
	if password != "" {
		secret.Password = password
	}
	store, err := openScheduledBackupStoreWith(ctx, note.Schedule, decrypt, leafenTempDir(dataDir))
	if err != nil {
		return err
	}
	defer closeScheduledBackupStore(store)
	path := scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupSnapshotsName, note.Snapshot)
	source, err := openScheduledBackupSnapshotWith(store, scheduledBackupRootName, path, note.SHA256, secret)
	if err != nil {
		if !scheduledBackupStoreExists(store, scheduledBackupStoreJoin(path, scheduledBackupManifestName)) &&
			scheduledBackupManifestPresent(store, path) {
			return errors.Join(ErrBackupNeedsSecret, err)
		}
		return err
	}
	return stageRestore(restoreWorkDir(dataDir), source, note.Snapshot, time.Now())
}

// dataFolderDecrypter opens what the server saved sealed with its
// administrator settings key, read from the data folder without the
// database: none there opens nothing.
func dataFolderDecrypter(dataDir string) func(string) (string, error) {
	key := ""
	if raw, err := os.ReadFile(filepath.Join(dataDir, administratorSecretDirectory, administratorSecretFilename)); err == nil {
		key, _, _ = parseAdministratorSettingsSecret(raw)
	}
	return func(cipher string) (string, error) {
		if cipher == "" {
			return "", nil
		}
		if key == "" {
			return "", errors.New("the data folder holds no settings key")
		}
		plain, err := security.Decrypt(cipher, key)
		return string(plain), err
	}
}
