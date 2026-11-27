package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// scheduledBackupContainerPath is where Leafen's own image keeps a folder for
// backups: compose.yaml and the launcher mount the "leafen-backups" volume
// there (Dockerfile, docs/self-hosting.md "Backup"). A variable, so a check
// outside a container can name another.
var scheduledBackupContainerPath = "/backups"

// scheduledBackupContainerFolder is that folder when the server runs in a
// container that has it, else "": in a container a path of the computer it
// runs on means nothing, and the screens' folder fields give this one as
// their example and name it when they refuse a path (2026-11-18: a Docker
// server's backup screen gave a Mac path as its example and refused one
// without saying the server runs in a container or where to write).
func scheduledBackupContainerFolder() string {
	if !runningInContainer() && strings.TrimSpace(os.Getenv(containerRuntimeEnvironment)) != "1" {
		return ""
	}
	if info, err := os.Stat(scheduledBackupContainerPath); err != nil || !info.IsDir() {
		return ""
	}
	return scheduledBackupContainerPath
}

// scheduledBackupFolderRefusalFor is what the refusal of folder says: a
// folder a phone picked was there, so the phone couldn't write in it.
func scheduledBackupFolderRefusalFor(folder string) string {
	if deviceBackupFolderNamed(folder) {
		return "The phone couldn't write to the chosen folder: choose another one."
	}
	return scheduledBackupFolderRefusal()
}

// scheduledBackupFolderRefusal is what a refused backup folder's answer says,
// with the container's folder where the server runs in one.
func scheduledBackupFolderRefusal() string {
	refusal := "The backup folder must be a full path outside the data folder that the server can write to."
	if container := scheduledBackupContainerFolder(); container != "" {
		refusal += " This server runs in a container, where a path of the computer it runs on is not there: use " +
			container + ", the folder mounted for backups, or a folder in it."
	}
	return refusal
}

// targetSet is whether the schedule names where its backups go.
func (stored storedScheduledBackup) targetSet() bool {
	switch stored.Target {
	case scheduledBackupTargetWebDAV:
		return stored.WebDAVURL != ""
	case scheduledBackupTargetS3:
		return stored.S3Bucket != "" && stored.S3Endpoint != ""
	}
	return stored.Folder != ""
}

// scheduledBackupS3Secret is the S3 storage's secret key: the
// environment's where it sets the schedule, else the saved one.
func scheduledBackupS3Secret(app core.App, stored storedScheduledBackup) (string, error) {
	return environmentOr(stored, scheduledBackupS3SecretKeyEnv, stored.S3Secret, administratorDecrypter(app))
}

// scheduledBackupWebDAVPassword is the WebDAV server's password: the
// environment's where it sets the schedule, else the saved one.
func scheduledBackupWebDAVPassword(app core.App, stored storedScheduledBackup) (string, error) {
	return environmentOr(stored, scheduledBackupWebDAVPasswordEnv, stored.WebDAVPassword, administratorDecrypter(app))
}

// openScheduledBackupStore opens where the schedule's backups go.
func openScheduledBackupStore(ctx context.Context, app core.App, stored storedScheduledBackup) (scheduledBackupStore, error) {
	return openScheduledBackupStoreWith(ctx, stored, administratorDecrypter(app), scheduledBackupSpool(app))
}

// openScheduledBackupStoreWith opens it with the saved secrets opened by
// decrypt and WebDAV's files waiting in spool, as the startup recovery
// screen does while the database won't open.
func openScheduledBackupStoreWith(ctx context.Context, stored storedScheduledBackup, decrypt func(string) (string, error), spool string) (scheduledBackupStore, error) {
	if stored.Target == scheduledBackupTargetS3 {
		secret, err := environmentOr(stored, scheduledBackupS3SecretKeyEnv, stored.S3Secret, decrypt)
		if err != nil {
			return nil, err
		}
		return newS3BackupStore(ctx, stored.S3Endpoint, stored.S3Region, stored.S3Bucket, stored.S3Prefix,
			stored.S3AccessKey, secret, stored.S3PathStyle)
	}
	if stored.Target != scheduledBackupTargetWebDAV {
		// A folder a phone keeps opens through the app that embeds the
		// server (scheduled_backup_device_folders.go).
		if store, ok, err := openDeviceBackupFolder(stored.Folder); ok {
			return store, err
		}
		return newFolderBackupStore(stored.Folder), nil
	}
	password, err := environmentOr(stored, scheduledBackupWebDAVPasswordEnv, stored.WebDAVPassword, decrypt)
	if err != nil {
		return nil, err
	}
	return newWebDAVBackupStore(ctx, stored.WebDAVURL, stored.WebDAVUsername, password, spool)
}

// environmentOr is a saved secret: the environment's where it sets the
// schedule, else the saved one opened.
func environmentOr(stored storedScheduledBackup, name, cipher string, decrypt func(string) (string, error)) (string, error) {
	if stored.SetByEnvironment {
		return os.Getenv(name), nil
	}
	return decrypt(cipher)
}

// administratorDecrypter opens what the server saved sealed with its
// administrator settings key.
func administratorDecrypter(app core.App) func(string) (string, error) {
	return func(cipher string) (string, error) { return decryptAdministratorSetting(app, cipher) }
}

// scheduledBackupSpool is where files wait on their way to a WebDAV server:
// Leafen's own work folder, which backups leave out (data_folder_temp.go).
func scheduledBackupSpool(app core.App) string {
	return leafenTempDir(app.DataDir())
}

// checkScheduledBackupFolder makes the backup folder's own folder there and
// writes a file to see that it can. A path must be named in full and lie
// outside the data folder, and the folder it is in must be there already:
// only its last step is made, so a mistyped path is refused rather than
// backed up to somewhere nobody looks (2026-11-06).
func checkScheduledBackupFolder(app core.App, folder string) error {
	if folder == "" || len(folder) > maxScheduledBackupFolderLength {
		return errors.New(errorCodeScheduledBackupFolderInvalid)
	}
	// A folder a phone keeps is checked by writing there.
	if store, ok, err := openDeviceBackupFolder(folder); ok {
		if err != nil || checkDeviceBackupFolder(store) != nil {
			return errors.New(errorCodeScheduledBackupFolderInvalid)
		}
		return nil
	}
	if !filepath.IsAbs(folder) {
		return errors.New(errorCodeScheduledBackupFolderInvalid)
	}
	if pathWithin(app.DataDir(), folder) {
		return errors.New(errorCodeScheduledBackupFolderInvalid)
	}
	folder = filepath.Clean(folder)
	if parent, err := os.Stat(filepath.Dir(folder)); err != nil || !parent.IsDir() {
		return errors.New(errorCodeScheduledBackupFolderInvalid)
	}
	if err := os.Mkdir(folder, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return errors.New(errorCodeScheduledBackupFolderInvalid)
	}
	root := filepath.Join(folder, scheduledBackupRootName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return errors.New(errorCodeScheduledBackupFolderInvalid)
	}
	probe, err := os.CreateTemp(root, scheduledBackupIncomingPrefix+"probe-*")
	if err != nil {
		return errors.New(errorCodeScheduledBackupFolderInvalid)
	}
	name := probe.Name()
	return errors.Join(probe.Close(), os.Remove(name))
}

// checkScheduledBackupWebDAV makes the backups' own folder on the WebDAV
// server and writes a file there to see that it can: the answer names a
// refused user name or password apart from an address it can't use.
func checkScheduledBackupWebDAV(ctx context.Context, app core.App, address, username, password string) error {
	ctx, cancel := context.WithTimeout(ctx, scheduledBackupWebDAVCheckTimeout)
	defer cancel()
	store, err := newWebDAVBackupStore(ctx, address, username, password, scheduledBackupSpool(app))
	if err != nil {
		return err
	}
	probe := scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupIncomingPrefix+"probe-"+security.RandomString(12))
	err = store.MakeDir(scheduledBackupRootName)
	if err == nil {
		err = scheduledBackupStoreWriteAll(store, probe, []byte("probe"))
	}
	if err == nil {
		err = store.RemoveAll(probe)
	}
	if errors.Is(err, errWebDAVBackupUnauthorized) {
		return err // with what the server answered
	}
	if err != nil {
		// What the server answered goes with it (backup_failure_reason.go).
		return errors.Join(err, errors.New(errorCodeScheduledBackupWebDAVInvalid))
	}
	return nil
}

// checkScheduledBackupS3 writes an object where the backups would go and
// removes it: the answer names refused keys apart from storage it can't
// use (a wrong endpoint, region or bucket).
func checkScheduledBackupS3(ctx context.Context, stored storedScheduledBackup, secret string) error {
	ctx, cancel := context.WithTimeout(ctx, s3BackupCheckTimeout)
	defer cancel()
	store, err := newS3BackupStore(ctx, stored.S3Endpoint, stored.S3Region, stored.S3Bucket, stored.S3Prefix,
		stored.S3AccessKey, secret, stored.S3PathStyle)
	if err != nil {
		return errors.Join(err, errS3BackupInvalid)
	}
	defer store.Close()
	probe := scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupIncomingPrefix+"probe-"+security.RandomString(12))
	err = scheduledBackupStoreWriteAll(store, probe, []byte("probe"))
	if err == nil {
		err = store.RemoveAll(probe)
	}
	if errors.Is(err, errS3BackupUnauthorized) {
		return err // with what the storage answered
	}
	if err != nil {
		// What the storage answered goes with it (backup_failure_reason.go).
		return errors.Join(err, errS3BackupInvalid)
	}
	return nil
}

// replacedScheduledBackupSecret is a kept secret after a save: cleared,
// replaced by one given (encrypted), or kept as it was.
func replacedScheduledBackupSecret(app core.App, kept, given string, clear bool) (string, error) {
	if clear {
		return "", nil
	}
	if given == "" {
		return kept, nil
	}
	return encryptAdministratorSetting(app, cipherScheduledBackup, given)
}
