package app

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// scheduledBackupStore is where scheduled backups go: a folder the server
// writes (user decision 2026-10-03, first), or a WebDAV server (second).
// Paths are slash-separated and relative to the store's root; a missing
// path answers fs.ErrNotExist.
//
// A store is used from several goroutines at once: a replica's database
// goes out from Litestream's own, its files from the runner's and its beat
// from another, on one store (replica_session.go). So what a store keeps
// between calls is set when it opens, or is a knownFolders, which takes a
// lock (2026-11-26: the WebDAV store kept its folders in a map without one,
// and two of them making or removing folders at once could stop the
// server).
type scheduledBackupStore interface {
	// MakeDir makes a folder and the folders above it.
	MakeDir(path string) error
	// Create writes a new file, there under its name once Close returns:
	// whole when nothing failed, so a caller writes where what a failure
	// leaves is cleared (an incomplete snapshot, an incoming content).
	Create(path string) (io.WriteCloser, error)
	Open(path string) (io.ReadCloser, error)
	Stat(path string) (scheduledBackupStoreEntry, error)
	// Rename moves a file or folder within the store, failing when the
	// target exists.
	Rename(from, to string) error
	// List lists a folder's entries.
	List(path string) ([]scheduledBackupStoreEntry, error)
	RemoveAll(path string) error
	// Replace writes a file whole over the one there, if any.
	Replace(path string, content []byte) error
	// Sync makes what a folder holds durable where the store can.
	Sync(path string) error
}

type scheduledBackupStoreEntry struct {
	Name     string
	Dir      bool
	Size     int64
	Modified time.Time
}

// closeScheduledBackupStore lets a store's access go, where it holds one.
func closeScheduledBackupStore(store scheduledBackupStore) {
	if closer, ok := store.(io.Closer); ok {
		_ = closer.Close()
	}
}

// knownFolders is the folders a store has seen or made, so each is asked
// about once; safe for several goroutines at once.
type knownFolders struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (folders *knownFolders) has(path string) bool {
	folders.mu.Lock()
	defer folders.mu.Unlock()
	return folders.seen[path]
}

func (folders *knownFolders) add(path string) {
	folders.mu.Lock()
	defer folders.mu.Unlock()
	if folders.seen == nil {
		folders.seen = map[string]bool{}
	}
	folders.seen[path] = true
}

// forget lets a folder go and those under it, as it is moved or removed.
func (folders *knownFolders) forget(path string) {
	path = strings.Trim(path, "/")
	folders.mu.Lock()
	defer folders.mu.Unlock()
	for folder := range folders.seen {
		if folder == path || strings.HasPrefix(folder, path+"/") {
			delete(folders.seen, folder)
		}
	}
}

// scheduledBackupStoreJoin joins slash-separated store path parts.
func scheduledBackupStoreJoin(parts ...string) string {
	return path.Join(parts...)
}

// folderBackupStore is a folder on the server's own file system.
type folderBackupStore struct {
	root string
}

func newFolderBackupStore(root string) folderBackupStore {
	return folderBackupStore{root: root}
}

func (store folderBackupStore) local(relative string) string {
	return filepath.Join(store.root, filepath.FromSlash(relative))
}

// MakeDir makes what Leafen keeps inside the place, never the place itself:
// a place gone (a disk taken out, a share not mounted, a folder moved) fails
// the backup or the replica, said as a place that can't be reached, rather
// than filling this computer's disk at its path, where the folder made
// would also stand in the way of the one coming back (2026-11-06).
func (store folderBackupStore) MakeDir(relative string) error {
	info, err := os.Stat(store.root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return &fs.PathError{Op: "mkdir", Path: store.root, Err: fs.ErrNotExist}
	}
	return os.MkdirAll(store.local(relative), 0o700)
}

// Create writes beside the target and renames it into place once whole and
// synced, so a cut-off write leaves no partial file under the name.
func (store folderBackupStore) Create(relative string) (io.WriteCloser, error) {
	target := store.local(relative)
	if _, err := os.Lstat(target); err == nil {
		return nil, fs.ErrExist
	}
	file, err := os.CreateTemp(filepath.Dir(target), scheduledBackupIncomingPrefix+"*")
	if err != nil {
		return nil, err
	}
	return &folderBackupWriter{file: file, target: target}, nil
}

type folderBackupWriter struct {
	file   *os.File
	target string
}

func (writer *folderBackupWriter) Write(data []byte) (int, error) { return writer.file.Write(data) }

func (writer *folderBackupWriter) Close() error {
	name := writer.file.Name()
	if err := errors.Join(writer.file.Sync(), writer.file.Close()); err != nil {
		_ = os.Remove(name)
		return err
	}
	if _, err := os.Lstat(writer.target); err == nil {
		_ = os.Remove(name)
		return fs.ErrExist
	}
	if err := os.Rename(name, writer.target); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func (store folderBackupStore) Open(relative string) (io.ReadCloser, error) {
	file, err := os.Open(store.local(relative))
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (store folderBackupStore) Stat(relative string) (scheduledBackupStoreEntry, error) {
	info, err := os.Stat(store.local(relative))
	if err != nil {
		return scheduledBackupStoreEntry{}, err
	}
	return scheduledBackupStoreEntry{Name: info.Name(), Dir: info.IsDir(), Size: info.Size(), Modified: info.ModTime()}, nil
}

func (store folderBackupStore) Rename(from, to string) error {
	if _, err := os.Lstat(store.local(to)); err == nil {
		return fs.ErrExist
	}
	return os.Rename(store.local(from), store.local(to))
}

func (store folderBackupStore) List(relative string) ([]scheduledBackupStoreEntry, error) {
	entries, err := os.ReadDir(store.local(relative))
	if err != nil {
		return nil, err
	}
	listed := make([]scheduledBackupStoreEntry, 0, len(entries))
	for _, entry := range entries {
		item := scheduledBackupStoreEntry{Name: entry.Name(), Dir: entry.IsDir()}
		if info, err := entry.Info(); err == nil {
			item.Size, item.Modified = info.Size(), info.ModTime()
		}
		listed = append(listed, item)
	}
	return listed, nil
}

func (store folderBackupStore) RemoveAll(relative string) error {
	return os.RemoveAll(store.local(relative))
}

func (store folderBackupStore) Replace(relative string, content []byte) error {
	target := store.local(relative)
	file, err := os.CreateTemp(filepath.Dir(target), scheduledBackupIncomingPrefix+"*")
	if err != nil {
		return err
	}
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		_ = os.Remove(file.Name())
		return err
	}
	if err := os.Rename(file.Name(), target); err != nil {
		_ = os.Remove(file.Name())
		return err
	}
	return syncDirectory(filepath.Dir(target))
}

func (store folderBackupStore) Sync(relative string) error {
	return syncDirectory(store.local(relative))
}

// scheduledBackupStoreWriteAll writes a whole new file to the store.
func scheduledBackupStoreWriteAll(store scheduledBackupStore, target string, content []byte) error {
	output, err := store.Create(target)
	if err != nil {
		return err
	}
	_, writeErr := output.Write(content)
	return errors.Join(writeErr, output.Close())
}

// scheduledBackupStoreReadAll reads a whole file from the store.
func scheduledBackupStoreReadAll(store scheduledBackupStore, source string) ([]byte, error) {
	input, err := store.Open(source)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	return io.ReadAll(input)
}

// scheduledBackupStoreExists is whether the store holds a path.
func scheduledBackupStoreExists(store scheduledBackupStore, target string) bool {
	_, err := store.Stat(target)
	return err == nil
}

// scheduledBackupStorePathValid is a store path inside its root: relative,
// with no step out.
func scheduledBackupStorePathValid(target string) bool {
	return target != "" && !strings.HasPrefix(target, "/") && !strings.Contains("/"+target+"/", "/../")
}
