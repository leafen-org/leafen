package app

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pocketbase/pocketbase"
)

// restoreScheduledBackupSnapshot puts a folder snapshot in a fresh data
// folder's place: the database unpacked, and every file it names from the
// contents beside it, each checked against its SHA-256. A SHA-256 given is
// the one its list file (snapshot.json, or snapshot.json.sealed) must have; a
// sealed snapshot opens with the password it was made under.
func restoreScheduledBackupSnapshot(pb *pocketbase.PocketBase, snapshot, expectedSHA256 string, secret scheduledBackupSecret) (restoreReceipt, error) {
	if pathWithin(pb.DataDir(), snapshot) {
		return restoreReceipt{}, errors.New("input path must be outside the Leafen data root")
	}
	source, err := openScheduledBackupSnapshot(snapshot, expectedSHA256, secret)
	if err != nil {
		return restoreReceipt{}, err
	}
	if err := requireFreshRestoreTarget(pb); err != nil {
		return restoreReceipt{}, err
	}
	var bytes int64
	dataRoot, err := restoreDataRoot(pb, func(extractedRoot string) error {
		if bytes, err = source.assemble(extractedRoot); err != nil {
			return err
		}
		// It starts knowing it is the backup it was restored from
		// (restored_last_backup.go).
		return noteRestoredBackup(filepath.Join(extractedRoot, dataDatabaseFile), source, filepath.Base(snapshot))
	})
	if err != nil {
		return restoreReceipt{}, err
	}
	return restoreReceipt{Status: "restored", Path: dataRoot, Bytes: bytes, SHA256: source.manifestSHA256}, nil
}

// scheduledBackupSnapshotSource is a snapshot opened to restore: the store
// it is in, its place there and the backup root's its contents and keys sit
// in, the key a sealed one opens with, and its checked list.
type scheduledBackupSnapshotSource struct {
	store          scheduledBackupStore
	snapshot       string
	root           string
	key            *scheduledBackupKey
	manifest       scheduledBackupManifest
	manifestSHA256 string
	// What a restore asked for in the app shows of its progress, if any.
	progress *backupProgress
}

// openScheduledBackupSnapshot opens a snapshot folder on the server's own
// file system, its contents and keys two folders up (in "Leafen backups"
// where the server wrote it).
func openScheduledBackupSnapshot(snapshot, expectedSHA256 string, secret scheduledBackupSecret) (scheduledBackupSnapshotSource, error) {
	store := newFolderBackupStore(filepath.Dir(filepath.Dir(snapshot)))
	relative := scheduledBackupStoreJoin(filepath.Base(filepath.Dir(snapshot)), filepath.Base(snapshot))
	return openScheduledBackupSnapshotWith(store, "", relative, expectedSHA256, secret)
}

// openScheduledBackupSnapshotIn reads a snapshot's list with a password.
func openScheduledBackupSnapshotIn(store scheduledBackupStore, root, snapshot, expectedSHA256, password string) (scheduledBackupSnapshotSource, error) {
	return openScheduledBackupSnapshotWith(store, root, snapshot, expectedSHA256, passwordBackupSecret(password))
}

// errScheduledBackupSealed is a sealed snapshot or replica given nothing to
// open it with.
var errScheduledBackupSealed = errors.New("the backup is encrypted: give its recovery code in " + scheduledBackupRecoveryCodeEnv +
	" or --recovery-code-file, or its password in " + scheduledBackupPasswordEnv + " or --password-file")

// openScheduledBackupSnapshotWith reads a snapshot's list: the plain one, or
// the sealed one with whichever key the secret opens it was made under.
func openScheduledBackupSnapshotWith(store scheduledBackupStore, root, snapshot, expectedSHA256 string, secret scheduledBackupSecret) (scheduledBackupSnapshotSource, error) {
	source := scheduledBackupSnapshotSource{store: store, snapshot: snapshot, root: root}
	raw, err := scheduledBackupStoreReadAll(store, scheduledBackupStoreJoin(snapshot, scheduledBackupManifestName))
	if errors.Is(err, fs.ErrNotExist) {
		if secret.empty() {
			return source, errScheduledBackupSealed
		}
		keys, keysErr := scheduledBackupKeysIn(store, root, secret)
		if keysErr != nil {
			return source, keysErr
		}
		if len(keys) == 0 {
			return source, errScheduledBackupPassword
		}
		// Each key names the list its own way: the one whose list is there
		// and opens sealed the snapshot.
		for _, key := range keys {
			raw, err = scheduledBackupStoreReadAll(store, scheduledBackupStoreJoin(snapshot, key.fileName(scheduledBackupManifestName)))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return source, err
			}
			if _, decodeErr := decodeScheduledBackupManifest(raw, key); decodeErr == nil {
				source.key = key
				break
			}
		}
		if source.key == nil {
			return source, errScheduledBackupPassword
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return source, errors.New("the folder is not a Leafen folder snapshot")
	}
	if err != nil {
		return source, err
	}
	digest := sha256.Sum256(raw)
	source.manifestSHA256 = hex.EncodeToString(digest[:])
	if expectedSHA256 != "" && expectedSHA256 != source.manifestSHA256 {
		return source, errors.New("snapshot SHA-256 receipt does not match")
	}
	if source.manifest, err = decodeScheduledBackupManifest(raw, source.key); err != nil {
		return source, err
	}
	return source, nil
}

// assemble writes the snapshot's data folder into target: the database
// unpacked and every file from the contents, each checked as it is written.
func (source scheduledBackupSnapshotSource) assemble(target string) (int64, error) {
	key, manifest, progress := source.key, source.manifest, source.progress
	total := manifest.Database.Bytes
	for _, file := range manifest.Files {
		total += file.Bytes
	}
	progress.step(backupProgressStepDatabase, total, len(manifest.Files)+1)
	packed, err := key.reader(source.store, scheduledBackupStoreJoin(source.snapshot, key.fileName(scheduledBackupDatabaseName)))
	if err != nil {
		return 0, err
	}
	defer packed.Close()
	unpacked, err := gzip.NewReader(packed)
	if err != nil {
		return 0, err
	}
	if err := restoreScheduledBackupFile(progress.reader(unpacked), filepath.Join(target, "data.db"), manifest.Database); err != nil {
		return 0, err
	}
	progress.done(0, 1)
	progress.step(backupProgressStepFiles, 0, 0)
	bytes := manifest.Database.Bytes
	for _, file := range manifest.Files {
		content, err := key.reader(source.store, scheduledBackupBlobStorePath(source.root, key.blobName(file.SHA256)))
		if err != nil {
			return 0, errors.Join(errors.New("the snapshot's content for "+file.Path+" is missing"), err)
		}
		path := filepath.Join(target, filepath.FromSlash(file.Path))
		err = errors.Join(os.MkdirAll(filepath.Dir(path), 0o700), restoreScheduledBackupFile(progress.reader(content), path, file))
		content.Close()
		if err != nil {
			return 0, err
		}
		progress.done(0, 1)
		bytes += file.Bytes
	}
	return bytes, nil
}

func restoreScheduledBackupFile(source io.Reader, target string, entry scheduledBackupEntry) error {
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), source)
	if err := errors.Join(copyErr, output.Sync(), output.Close()); err != nil {
		return err
	}
	if written != entry.Bytes || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
		return errors.New("the snapshot's " + entry.Path + " does not match its SHA-256")
	}
	return nil
}
