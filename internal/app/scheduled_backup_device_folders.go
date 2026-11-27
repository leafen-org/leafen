package app

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync/atomic"
	"time"
)

// A phone keeps a folder a person picks through its document providers,
// not as a path the server can open (user decision 2026-10-03: the mobile
// apps pick backup folders with the system's file picker). The phone app
// that embeds the server names such a folder by a string of its own (an iOS
// bookmark, an Android document tree) and opens it here; backups to it, the
// restore list, a restore at setup and the startup recovery's restore then
// work as with any folder.

// DeviceBackupFolder is such a folder, opened. Paths are slash-separated
// and relative to it; a missing one answers fs.ErrNotExist.
type DeviceBackupFolder interface {
	MakeDir(path string) error
	// Create writes a new file, there under its name once Close returns
	// without an error; it fails when one is there.
	Create(path string) (io.WriteCloser, error)
	Open(path string) (io.ReadCloser, error)
	Stat(path string) (DeviceBackupEntry, error)
	// Rename moves a file or folder within the folder, failing when the
	// target is there.
	Rename(from, to string) error
	List(path string) ([]DeviceBackupEntry, error)
	RemoveAll(path string) error
	// Replace writes a file whole over the one there, if any.
	Replace(path string, content []byte) error
	// Close lets the device's access to the folder go.
	Close() error
}

// DeviceBackupEntry is a file or folder in a DeviceBackupFolder.
type DeviceBackupEntry struct {
	Name     string
	Dir      bool
	Size     int64
	Modified time.Time
}

// OpenDeviceBackupFolder opens a folder the device keeps by the name the
// app gave it, answering false for a name that isn't one of its own.
type OpenDeviceBackupFolder func(name string) (DeviceBackupFolder, bool, error)

var deviceBackupFolders atomic.Pointer[OpenDeviceBackupFolder]

// SetDeviceBackupFolders lets the app that embeds the server open the
// folders its device keeps.
func SetDeviceBackupFolders(open OpenDeviceBackupFolder) {
	deviceBackupFolders.Store(&open)
}

// NewPathBackupFolder is a folder reached by a path once the device has
// opened it (as an iOS bookmark is); release runs on Close.
func NewPathBackupFolder(root string, release func()) DeviceBackupFolder {
	return &pathBackupFolder{store: newFolderBackupStore(root), release: release}
}

type pathBackupFolder struct {
	store   folderBackupStore
	release func()
}

func (folder *pathBackupFolder) MakeDir(path string) error { return folder.store.MakeDir(path) }
func (folder *pathBackupFolder) Create(path string) (io.WriteCloser, error) {
	return folder.store.Create(path)
}
func (folder *pathBackupFolder) Open(path string) (io.ReadCloser, error) {
	return folder.store.Open(path)
}
func (folder *pathBackupFolder) Stat(path string) (DeviceBackupEntry, error) {
	entry, err := folder.store.Stat(path)
	return DeviceBackupEntry(entry), err
}
func (folder *pathBackupFolder) Rename(from, to string) error { return folder.store.Rename(from, to) }
func (folder *pathBackupFolder) List(path string) ([]DeviceBackupEntry, error) {
	entries, err := folder.store.List(path)
	listed := make([]DeviceBackupEntry, 0, len(entries))
	for _, entry := range entries {
		listed = append(listed, DeviceBackupEntry(entry))
	}
	return listed, err
}
func (folder *pathBackupFolder) RemoveAll(path string) error { return folder.store.RemoveAll(path) }
func (folder *pathBackupFolder) Replace(path string, content []byte) error {
	return folder.store.Replace(path, content)
}
func (folder *pathBackupFolder) Close() error {
	if folder.release != nil {
		folder.release()
		folder.release = nil
	}
	return nil
}

// openDeviceBackupFolder opens a schedule's folder through the embedding
// app when it is one of the device's own.
func openDeviceBackupFolder(name string) (scheduledBackupStore, bool, error) {
	open := deviceBackupFolders.Load()
	if open == nil || *open == nil {
		return nil, false, nil
	}
	folder, ok, err := (*open)(name)
	if !ok {
		return nil, false, nil
	}
	if err != nil {
		// A picked folder that no longer opens (its bookmark or permission
		// gone) is one the backup can't reach, said as such.
		return nil, true, errors.Join(errors.New(errorCodeScheduledBackupFolderUnavailable), fs.ErrPermission, err)
	}
	return deviceBackupStore{folder}, true, nil
}

// deviceBackupFolderNamed is whether a folder is one the device keeps
// (named by the app, not a path), whether or not it opens now.
func deviceBackupFolderNamed(name string) bool {
	store, ok, _ := openDeviceBackupFolder(name)
	if store != nil {
		closeScheduledBackupStore(store)
	}
	return ok
}

// deviceBackupStore is a device folder as the backups' store.
type deviceBackupStore struct{ folder DeviceBackupFolder }

func (store deviceBackupStore) MakeDir(path string) error { return store.folder.MakeDir(path) }
func (store deviceBackupStore) Create(path string) (io.WriteCloser, error) {
	return store.folder.Create(path)
}
func (store deviceBackupStore) Open(path string) (io.ReadCloser, error) {
	return store.folder.Open(path)
}
func (store deviceBackupStore) Stat(path string) (scheduledBackupStoreEntry, error) {
	entry, err := store.folder.Stat(path)
	return scheduledBackupStoreEntry(entry), err
}
func (store deviceBackupStore) Rename(from, to string) error { return store.folder.Rename(from, to) }
func (store deviceBackupStore) List(path string) ([]scheduledBackupStoreEntry, error) {
	entries, err := store.folder.List(path)
	listed := make([]scheduledBackupStoreEntry, 0, len(entries))
	for _, entry := range entries {
		listed = append(listed, scheduledBackupStoreEntry(entry))
	}
	return listed, err
}
func (store deviceBackupStore) RemoveAll(path string) error { return store.folder.RemoveAll(path) }
func (store deviceBackupStore) Replace(path string, content []byte) error {
	return store.folder.Replace(path, content)
}

// Sync has nothing more to do: a device folder keeps a file once it closes.
func (store deviceBackupStore) Sync(string) error { return nil }
func (store deviceBackupStore) Close() error      { return store.folder.Close() }

// checkDeviceBackupFolder writes a file where a device folder's backups go
// and removes it.
func checkDeviceBackupFolder(store scheduledBackupStore) error {
	defer closeScheduledBackupStore(store)
	if err := store.MakeDir(scheduledBackupRootName); err != nil {
		return err
	}
	probe := scheduledBackupStoreJoin(scheduledBackupRootName, scheduledBackupIncomingPrefix+"probe")
	_ = store.RemoveAll(probe)
	if err := scheduledBackupStoreWriteAll(store, probe, []byte("probe")); err != nil {
		return err
	}
	if err := store.RemoveAll(probe); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// backupRootFolderStore is a picked folder that is "Leafen backups" itself,
// as the folder holding it: a restore at setup takes either (as a path's
// last step "Leafen backups" is dropped), and a phone's picked folder has no
// folder above it to open instead.
type backupRootFolderStore struct{ scheduledBackupStore }

func (store backupRootFolderStore) inner(path string) string {
	if path == scheduledBackupRootName {
		return ""
	}
	return strings.TrimPrefix(path, scheduledBackupRootName+"/")
}

func (store backupRootFolderStore) MakeDir(path string) error {
	return store.scheduledBackupStore.MakeDir(store.inner(path))
}
func (store backupRootFolderStore) Create(path string) (io.WriteCloser, error) {
	return store.scheduledBackupStore.Create(store.inner(path))
}
func (store backupRootFolderStore) Open(path string) (io.ReadCloser, error) {
	return store.scheduledBackupStore.Open(store.inner(path))
}
func (store backupRootFolderStore) Stat(path string) (scheduledBackupStoreEntry, error) {
	return store.scheduledBackupStore.Stat(store.inner(path))
}
func (store backupRootFolderStore) Rename(from, to string) error {
	return store.scheduledBackupStore.Rename(store.inner(from), store.inner(to))
}
func (store backupRootFolderStore) List(path string) ([]scheduledBackupStoreEntry, error) {
	return store.scheduledBackupStore.List(store.inner(path))
}
func (store backupRootFolderStore) RemoveAll(path string) error {
	return store.scheduledBackupStore.RemoveAll(store.inner(path))
}
func (store backupRootFolderStore) Replace(path string, content []byte) error {
	return store.scheduledBackupStore.Replace(store.inner(path), content)
}
func (store backupRootFolderStore) Sync(path string) error {
	return store.scheduledBackupStore.Sync(store.inner(path))
}
func (store backupRootFolderStore) Close() error {
	closeScheduledBackupStore(store.scheduledBackupStore)
	return nil
}

// openDeviceRestoreFolder opens a picked folder to restore from at setup:
// the folder holding "Leafen backups", or that folder itself; with neither,
// errSetupRestoreNothing.
func openDeviceRestoreFolder(store scheduledBackupStore) (scheduledBackupStore, error) {
	if entry, err := store.Stat(scheduledBackupRootName); err == nil && entry.Dir {
		return store, nil
	}
	if entry, err := store.Stat(scheduledBackupSnapshotsName); err == nil && entry.Dir {
		return backupRootFolderStore{store}, nil
	}
	if entry, err := store.Stat(replicaFolderName); err == nil && entry.Dir {
		return backupRootFolderStore{store}, nil
	}
	closeScheduledBackupStore(store)
	return nil, errSetupRestoreNothing
}

// keptBackupFolder is the folder a server restored at setup goes on backing
// up into: where its backups were found, but not a phone's picked "Leafen
// backups" itself, which would hold another "Leafen backups" (the restored
// server keeps the folder it had, and its screen says if that fails).
func keptBackupFolder(place storedScheduledBackup, store scheduledBackupStore) string {
	if _, rooted := store.(backupRootFolderStore); rooted {
		return ""
	}
	return place.Folder
}
