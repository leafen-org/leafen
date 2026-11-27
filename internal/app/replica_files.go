package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The replica's files (decided 2026-10-17): everything of the data folder a
// snapshot keeps — the files people put in, the secrets, the keys — goes
// into the backups' contents as it changes, every few seconds, beside the
// snapshots' own, and a piece, a short list of what changed, says which
// content each path now holds. Once enough pieces gather, a whole one says
// all of it and the earlier ones go. Restoring reads the last whole piece
// and the ones after it. What this server has sent is kept in its log
// database, which nothing copies, so a server restored elsewhere starts by
// comparing its files with the replica's list rather than sending them
// again.

const (
	replicaFilesName       = "files"
	replicaFilesFormat     = "leafen.replica-files"
	replicaFilesVersion    = 1
	replicaFilesWholeEvery = 200
	replicaFilesWholeTag   = "-whole"
)

// What belongs to this machine rather than to the data — a downloaded
// program, the desktop app's updates and window, diagnostics, scratch
// bodies, the replica's own working state — is left out of new snapshots,
// the replica and one-off backups; an older snapshot that holds some of it
// still restores.
var scheduledBackupMachineOnly = map[string]bool{
	"bin": true, "updates": true, ".diagnostics": true, ".app-endpoint-bodies": true,
	"desktop.json": true, "window-background": true, leafenTempDirName: true,
	".data.db-litestream": true, ".auxiliary.db-litestream": true,
	replicaHolderName: true, replicaFencedName: true, serverMovedInName: true, dataDiskLowName: true,
	// The serving process's lock (server_data_lock.go), which Windows keeps
	// from being read while it is held.
	serverDataLockName: true,
}

// replicaFilesPiece is one piece of the list.
// errReplicaFileChanged is a file that changed while it was being sent; the
// next tick sends it as it is then.
var errReplicaFileChanged = errors.New("the file changed while it was sent")

type replicaFilesPiece struct {
	Format  string                 `json:"format"`
	Version int                    `json:"version"`
	Whole   bool                   `json:"whole"`
	Set     []scheduledBackupEntry `json:"set"`
	Removed []string               `json:"removed,omitempty"`
}

// replicaFileState is what the replica holds for a path, and the file's
// size and time when it was sent.
type replicaFileState struct {
	Bytes      int64
	ModifiedNS int64
	SHA256     string
}

type replicaFileSync struct {
	app   core.App
	store scheduledBackupStore
	// root is the backup root, where the contents are; the pieces are in
	// its replica folder.
	root       string
	key        *scheduledBackupKey
	known      map[string]replicaFileState
	next       int64
	sinceWhole int
}

func replicaFilesFolder(root string) string {
	return scheduledBackupStoreJoin(root, replicaFolderName, replicaFilesName)
}

// ensureReplicaFileTable makes the log database's table of what was sent.
func ensureReplicaFileTable(app core.App) error {
	_, err := app.AuxDB().NewQuery(`CREATE TABLE IF NOT EXISTS {{leafen_replica_files}} (
		[[path]] TEXT PRIMARY KEY NOT NULL,
		[[bytes]] INTEGER NOT NULL,
		[[modified_ns]] INTEGER NOT NULL,
		[[sha256]] TEXT NOT NULL
	)`).Execute()
	return err
}

// openReplicaFileSync reads what was sent: the log database's table where
// the replica's list goes on from it, else the replica's own list with no
// file times, so each file is compared by its content.
func openReplicaFileSync(app core.App, store scheduledBackupStore, root string, key *scheduledBackupKey) (*replicaFileSync, error) {
	if err := ensureReplicaFileTable(app); err != nil {
		return nil, err
	}
	sync := &replicaFileSync{app: app, store: store, root: root, key: key, known: map[string]replicaFileState{}}
	pieces, err := listReplicaFilePieces(store, root, key)
	if err != nil {
		return nil, err
	}
	if len(pieces) == 0 {
		// A replica starting afresh gets everything.
		_, err := app.AuxDB().NewQuery(`DELETE FROM {{leafen_replica_files}}`).Execute()
		return sync, err
	}
	sync.next = pieces[len(pieces)-1].number + 1
	for _, piece := range pieces {
		if !piece.whole {
			sync.sinceWhole++
		}
	}
	rows := []struct {
		Path       string `db:"path"`
		Bytes      int64  `db:"bytes"`
		ModifiedNS int64  `db:"modified_ns"`
		SHA256     string `db:"sha256"`
	}{}
	if err := app.AuxDB().NewQuery(`SELECT [[path]], [[bytes]], [[modified_ns]], [[sha256]] FROM {{leafen_replica_files}}`).All(&rows); err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		for _, row := range rows {
			sync.known[row.Path] = replicaFileState{Bytes: row.Bytes, ModifiedNS: row.ModifiedNS, SHA256: row.SHA256}
		}
		return sync, nil
	}
	held, err := readReplicaFileList(store, root, key)
	if err != nil {
		return nil, err
	}
	for path, entry := range held {
		sync.known[path] = replicaFileState{Bytes: entry.Bytes, SHA256: entry.SHA256}
	}
	return sync, nil
}

type replicaFilePieceName struct {
	name   string
	number int64
	whole  bool
}

// listReplicaFilePieces lists the pieces from the last whole one on, in
// order.
func listReplicaFilePieces(store scheduledBackupStore, root string, key *scheduledBackupKey) ([]replicaFilePieceName, error) {
	entries, err := store.List(replicaFilesFolder(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	pieces := []replicaFilePieceName{}
	for _, entry := range entries {
		name := entry.Name
		if key != nil {
			if !strings.HasSuffix(name, ".sealed") {
				continue
			}
			name = strings.TrimSuffix(name, ".sealed")
		}
		stem, ok := strings.CutSuffix(name, ".json")
		if entry.Dir || !ok {
			continue
		}
		whole := strings.HasSuffix(stem, replicaFilesWholeTag)
		number, err := strconv.ParseInt(strings.TrimSuffix(stem, replicaFilesWholeTag), 10, 64)
		if err != nil || number < 0 {
			continue
		}
		pieces = append(pieces, replicaFilePieceName{name: entry.Name, number: number, whole: whole})
	}
	sort.Slice(pieces, func(left, right int) bool { return pieces[left].number < pieces[right].number })
	for index := len(pieces) - 1; index >= 0; index-- {
		if pieces[index].whole {
			return pieces[index:], nil
		}
	}
	return pieces, nil
}

// readReplicaFileList is what the replica holds now, path by path.
func readReplicaFileList(store scheduledBackupStore, root string, key *scheduledBackupKey) (map[string]scheduledBackupEntry, error) {
	pieces, err := listReplicaFilePieces(store, root, key)
	if err != nil {
		return nil, err
	}
	held := map[string]scheduledBackupEntry{}
	for _, piece := range pieces {
		reader, err := key.reader(store, scheduledBackupStoreJoin(replicaFilesFolder(root), piece.name))
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(reader, 1<<30))
		reader.Close()
		if err != nil {
			return nil, err
		}
		decoded := replicaFilesPiece{}
		if err := json.Unmarshal(raw, &decoded); err != nil || decoded.Format != replicaFilesFormat || decoded.Version != replicaFilesVersion {
			return nil, errors.New("the replica's file list is not valid")
		}
		if decoded.Whole {
			clear(held)
		}
		for _, path := range decoded.Removed {
			delete(held, path)
		}
		for _, entry := range decoded.Set {
			if !sha256Pattern.MatchString(entry.SHA256) || !scheduledBackupEntryPathValid(entry.Path) || entry.Bytes < 0 {
				return nil, errors.New("the replica's file list names a file it can't hold")
			}
			held[entry.Path] = entry
		}
	}
	return held, nil
}

// replicaFileLeftOut is whether a top-level entry of the data folder stays
// out of the replica.
func replicaFileLeftOut(top string) bool {
	return scheduledBackupLeftOut[top] || scheduledBackupMachineOnly[top]
}

// tick sends what changed since the last one and answers how many files it
// sent. Once ctx ends it sends no more and lists those that went: the rest
// go at the next tick.
func (sync *replicaFileSync) tick(ctx context.Context) (int, error) {
	dataRoot, err := filepath.Abs(sync.app.DataDir())
	if err != nil {
		return 0, err
	}
	present := map[string]bool{}
	changed := []string{}
	stats := map[string]fs.FileInfo{}
	walkErr := filepath.WalkDir(dataRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A file gone while the folder is read is gone.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(dataRoot, path)
		if err != nil || relative == "." {
			return err
		}
		if top := strings.SplitN(filepath.ToSlash(relative), "/", 2)[0]; replicaFileLeftOut(top) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), productFilePreviewFolderPrefix) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		present[key] = true
		if known, ok := sync.known[key]; !ok || known.Bytes != info.Size() || known.ModifiedNS != info.ModTime().UnixNano() {
			changed = append(changed, key)
			stats[key] = info
		}
		return nil
	})
	if walkErr != nil {
		return 0, walkErr
	}
	removed := []string{}
	for path := range sync.known {
		if !present[path] {
			removed = append(removed, path)
		}
	}
	if len(changed) == 0 && len(removed) == 0 {
		return 0, nil
	}
	sort.Strings(changed)
	sort.Strings(removed)
	// At a stop (a context with a deadline) the smallest go first and each is
	// listed once it is there, so one slow to send leaves the others listed.
	_, stopping := ctx.Deadline()
	if stopping {
		sort.SliceStable(changed, func(left, right int) bool { return stats[changed[left]].Size() < stats[changed[right]].Size() })
	}
	scheduledBackupContents.Lock()
	defer scheduledBackupContents.Unlock()
	piece := replicaFilesPiece{Format: replicaFilesFormat, Version: replicaFilesVersion, Removed: removed}
	updates := map[string]replicaFileState{}
	listed := 0
	for index, path := range changed {
		info := stats[path]
		state, sent, err := sync.send(ctx, filepath.Join(dataRoot, filepath.FromSlash(path)), info)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errReplicaFileChanged) {
			continue
		}
		if err != nil && ctx.Err() != nil {
			cut := fmt.Errorf("%d of %d changed files not sent: %w", len(changed)-index, len(changed), ctx.Err())
			return listed, errors.Join(cut, sync.list(piece, updates))
		}
		if err != nil {
			return 0, fmt.Errorf("sending %s: %w", path, err)
		}
		updates[path] = state
		if sent || sync.known[path].SHA256 != state.SHA256 {
			entry := scheduledBackupEntry{Path: path, SHA256: state.SHA256, Bytes: state.Bytes}
			if sync.key != nil {
				entry.Blob = sync.key.blobName(state.SHA256)
			}
			piece.Set = append(piece.Set, entry)
		}
		if stopping {
			if err := sync.list(piece, updates); err != nil {
				return listed, err
			}
			listed += len(piece.Set)
			piece.Set, piece.Removed, updates = nil, nil, map[string]replicaFileState{}
		}
	}
	if err := sync.list(piece, updates); err != nil {
		return listed, err
	}
	listed += len(piece.Set)
	if sync.sinceWhole >= replicaFilesWholeEvery {
		if err := sync.writeWhole(); err != nil {
			return listed, err
		}
	}
	return listed, nil
}

// list writes a piece of the list with what went and keeps it in the log
// database.
func (sync *replicaFileSync) list(piece replicaFilesPiece, updates map[string]replicaFileState) error {
	if len(piece.Set) > 0 || len(piece.Removed) > 0 {
		if err := sync.writePiece(piece, false); err != nil {
			return err
		}
		sync.sinceWhole++
	}
	return sync.remember(updates, piece.Removed)
}

// send puts a file's content among the backups' contents where it isn't
// yet, reading the file for its SHA-256 first; both readings stop once ctx
// ends.
func (sync *replicaFileSync) send(ctx context.Context, path string, info fs.FileInfo) (replicaFileState, bool, error) {
	source, err := os.Open(path)
	if err != nil {
		return replicaFileState{}, false, err
	}
	hash := sha256.New()
	size, err := io.Copy(hash, readerUntil(ctx)(source))
	source.Close()
	if err != nil {
		return replicaFileState{}, false, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	state := replicaFileState{Bytes: size, ModifiedNS: info.ModTime().UnixNano(), SHA256: digest}
	if scheduledBackupBlobExists(sync.store, sync.root, digest, size, sync.key) {
		return state, false, nil
	}
	copied, _, err := copyScheduledBackupBlob(sync.store, sync.root, path, sync.key, readerUntil(ctx))
	if err != nil {
		return replicaFileState{}, false, err
	}
	if copied != digest {
		return replicaFileState{}, false, errReplicaFileChanged
	}
	return state, true, nil
}

// writePiece writes the next piece of the list.
func (sync *replicaFileSync) writePiece(piece replicaFilesPiece, whole bool) error {
	piece.Whole = whole
	raw, err := json.Marshal(piece)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%016d", sync.next)
	if whole {
		name += replicaFilesWholeTag
	}
	folder := replicaFilesFolder(sync.root)
	if err := sync.store.MakeDir(folder); err != nil {
		return err
	}
	target := scheduledBackupStoreJoin(folder, sync.key.fileName(name+".json"))
	output, err := sync.store.Create(target)
	if errors.Is(err, fs.ErrExist) {
		if err = sync.store.RemoveAll(target); err == nil {
			output, err = sync.store.Create(target)
		}
	}
	if err != nil {
		return err
	}
	sealed, err := sync.key.writer(output)
	if err != nil {
		return errors.Join(err, output.Close(), sync.store.RemoveAll(target))
	}
	_, writeErr := sealed.Write(raw)
	if err := errors.Join(writeErr, sealed.Close(), output.Close()); err != nil {
		return errors.Join(err, sync.store.RemoveAll(target))
	}
	sync.next++
	return nil
}

// writeWhole writes a piece with all of the list and lets the earlier ones
// go.
func (sync *replicaFileSync) writeWhole() error {
	earlier, err := listReplicaFilePieces(sync.store, sync.root, sync.key)
	if err != nil {
		return err
	}
	piece := replicaFilesPiece{Format: replicaFilesFormat, Version: replicaFilesVersion, Set: []scheduledBackupEntry{}}
	paths := make([]string, 0, len(sync.known))
	for path := range sync.known {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		state := sync.known[path]
		entry := scheduledBackupEntry{Path: path, SHA256: state.SHA256, Bytes: state.Bytes}
		if sync.key != nil {
			entry.Blob = sync.key.blobName(state.SHA256)
		}
		piece.Set = append(piece.Set, entry)
	}
	if err := sync.writePiece(piece, true); err != nil {
		return err
	}
	sync.sinceWhole = 0
	for _, old := range earlier {
		_ = sync.store.RemoveAll(scheduledBackupStoreJoin(replicaFilesFolder(sync.root), old.name))
	}
	return nil
}

// remember keeps what was sent in the log database.
func (sync *replicaFileSync) remember(updates map[string]replicaFileState, removed []string) error {
	err := sync.app.AuxRunInTransaction(func(txApp core.App) error {
		tx := txApp.AuxDB()
		for _, path := range removed {
			if _, err := tx.NewQuery(`DELETE FROM {{leafen_replica_files}} WHERE [[path]] = {:path}`).
				Bind(dbx.Params{"path": path}).Execute(); err != nil {
				return err
			}
		}
		for path, state := range updates {
			if _, err := tx.NewQuery(`INSERT INTO {{leafen_replica_files}} ([[path]], [[bytes]], [[modified_ns]], [[sha256]])
				VALUES ({:path}, {:bytes}, {:modified}, {:sha256})
				ON CONFLICT ([[path]]) DO UPDATE SET [[bytes]] = excluded.[[bytes]],
					[[modified_ns]] = excluded.[[modified_ns]], [[sha256]] = excluded.[[sha256]]`).
				Bind(dbx.Params{"path": path, "bytes": state.Bytes, "modified": state.ModifiedNS, "sha256": state.SHA256}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, path := range removed {
		delete(sync.known, path)
	}
	for path, state := range updates {
		sync.known[path] = state
	}
	return nil
}
