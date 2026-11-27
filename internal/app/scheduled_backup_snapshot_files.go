package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	gopath "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// walkScheduledBackupFiles calls keep for every file of the data folder a
// snapshot keeps, by its place in the folder.
func walkScheduledBackupFiles(ctx context.Context, dataRoot string, keep func(path, relative string, entry fs.DirEntry) error) error {
	return filepath.WalkDir(dataRoot, func(path string, entry fs.DirEntry, err error) error {
		// A folder gone while the data folder is read is gone, as the
		// replica's walk takes it (replica_files.go).
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		relative, err := filepath.Rel(dataRoot, path)
		if err != nil || relative == "." {
			return err
		}
		if top := strings.SplitN(filepath.ToSlash(relative), "/", 2)[0]; scheduledBackupLeftOut[top] || scheduledBackupMachineOnly[top] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// A photo's smaller previews make again from the photo when next shown.
		if entry.IsDir() && strings.HasPrefix(entry.Name(), productFilePreviewFolderPrefix) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		return keep(path, filepath.ToSlash(relative), entry)
	})
}

// countScheduledBackupFiles is how many files, and bytes, a snapshot of the
// data folder goes through, for its progress; a file it can't read counts
// as the run finds it.
func countScheduledBackupFiles(ctx context.Context, dataRoot string) (int64, int) {
	var bytes int64
	files := 0
	_ = walkScheduledBackupFiles(ctx, dataRoot, func(_, _ string, entry fs.DirEntry) error {
		if info, err := entry.Info(); err == nil {
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files
}

// keepScheduledBackupFiles names every kept file of the data folder by its
// content and copies the contents the store lacks. A file whose size and
// time are as last seen keeps the hash read then. The run's progress counts
// each file as it is found already there or copied.
func keepScheduledBackupFiles(ctx context.Context, app core.App, store scheduledBackupStore, root string, key *scheduledBackupKey, now time.Time) ([]scheduledBackupEntry, int64, error) {
	dataRoot, err := filepath.Abs(app.DataDir())
	if err != nil {
		return nil, 0, err
	}
	progress := backupProgressFrom(ctx)
	progress.step(backupProgressStepFiles, 0, 0)
	seen := now.UnixMilli()
	entries := []scheduledBackupEntry{}
	var added int64
	walkErr := walkScheduledBackupFiles(ctx, dataRoot, func(path, relativeKey string, entry fs.DirEntry) error {
		// A file deleted while the run goes through the folder (2026-11-22:
		// one deleted midway failed the whole backup) is left out, and the
		// run counts it (lastRun.filesSkipped).
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			progress.skipped(0)
			return nil
		}
		if err != nil {
			return err
		}
		digest, copied, err := keepScheduledBackupFile(app, store, root, path, relativeKey, info, key, seen, progress)
		if errors.Is(err, errScheduledBackupFileGone) {
			progress.skipped(0)
			return nil
		}
		if err != nil {
			return err
		}
		added += copied
		progress.sent(copied)
		kept := scheduledBackupEntry{Path: relativeKey, SHA256: digest, Bytes: info.Size()}
		if key != nil {
			kept.Blob = key.blobName(digest)
		}
		entries = append(entries, kept)
		return nil
	})
	if walkErr != nil {
		return nil, 0, walkErr
	}
	// Files gone from the data folder leave the hash list too.
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_scheduled_backup_file_hashes}} WHERE [[seen_at]] < {:seen}`).
		Bind(dbx.Params{"seen": seen}).Execute(); err != nil {
		return nil, 0, err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Path < entries[right].Path })
	return entries, added, nil
}

func keepScheduledBackupFile(app core.App, store scheduledBackupStore, root, path, relativeKey string, info fs.FileInfo,
	key *scheduledBackupKey, seen int64, progress *backupProgress) (string, int64, error) {
	known := struct {
		Bytes      int64  `db:"bytes"`
		ModifiedNS int64  `db:"modified_ns"`
		SHA256     string `db:"sha256"`
	}{}
	err := app.DB().NewQuery(`SELECT [[bytes]], [[modified_ns]], [[sha256]] FROM {{leafen_scheduled_backup_file_hashes}}
		WHERE [[path]] = {:path}`).Bind(dbx.Params{"path": relativeKey}).One(&known)
	digest := ""
	if err == nil && known.Bytes == info.Size() && known.ModifiedNS == info.ModTime().UnixNano() && sha256Pattern.MatchString(known.SHA256) {
		digest = known.SHA256
	}
	var copied int64
	if digest != "" && scheduledBackupBlobExists(store, root, digest, info.Size(), key) {
		// Already there: the whole file is done at once.
		progress.done(info.Size(), 1)
	} else {
		// Reading the file for its hash also copies it, so a file that
		// changes meanwhile is named by what was copied; its bytes count as
		// they are read.
		digest, copied, err = copyScheduledBackupBlob(store, root, path, key, progress.reader)
		if err != nil {
			return "", 0, err
		}
		progress.done(0, 1)
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_scheduled_backup_file_hashes}} ([[path]], [[bytes]], [[modified_ns]], [[sha256]], [[seen_at]])
		VALUES ({:path}, {:bytes}, {:modified}, {:sha256}, {:seen})
		ON CONFLICT ([[path]]) DO UPDATE SET [[bytes]] = excluded.[[bytes]], [[modified_ns]] = excluded.[[modified_ns]],
			[[sha256]] = excluded.[[sha256]], [[seen_at]] = excluded.[[seen_at]]
	`).Bind(dbx.Params{"path": relativeKey, "bytes": info.Size(), "modified": info.ModTime().UnixNano(), "sha256": digest, "seen": seen}).Execute()
	return digest, copied, err
}

// scheduledBackupBlobExists is whether the store holds a content: its size
// as well where it isn't sealed.
func scheduledBackupBlobExists(store scheduledBackupStore, root, digest string, size int64, key *scheduledBackupKey) bool {
	entry, err := store.Stat(scheduledBackupBlobStorePath(root, key.blobName(digest)))
	return err == nil && !entry.Dir && (key != nil || entry.Size == size)
}

// errScheduledBackupFileGone is a file of the data folder deleted before it
// could be read.
var errScheduledBackupFileGone = errors.New("the file was deleted before it was read")

// copyScheduledBackupBlob copies a file into the contents, named by its
// SHA-256 once whole; a content already there stays as it is. The file is
// read through what through makes of it: a backup's count of what was read,
// the replica's stop once its time is up.
func copyScheduledBackupBlob(store scheduledBackupStore, root, path string, key *scheduledBackupKey, through func(io.Reader) io.Reader) (string, int64, error) {
	source, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", 0, errors.Join(errScheduledBackupFileGone, err)
	}
	if err != nil {
		return "", 0, err
	}
	defer source.Close()
	incoming := scheduledBackupStoreJoin(root, scheduledBackupBlobsName, "sha256")
	if err := store.MakeDir(incoming); err != nil {
		return "", 0, err
	}
	temporary := scheduledBackupStoreJoin(incoming, scheduledBackupIncomingPrefix+security.RandomString(16))
	output, err := store.Create(temporary)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = store.RemoveAll(temporary) }()
	sealed, err := key.writer(output)
	if err != nil {
		return "", 0, errors.Join(err, output.Close())
	}
	hash := sha256.New()
	bytes, copyErr := io.Copy(io.MultiWriter(sealed, hash), through(source))
	if err := errors.Join(copyErr, sealed.Close(), output.Close()); err != nil {
		return "", 0, err
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if scheduledBackupBlobExists(store, root, digest, bytes, key) {
		return digest, 0, nil
	}
	target := scheduledBackupBlobStorePath(root, key.blobName(digest))
	// A content there that doesn't match, as one cut short, gives way.
	if err := errors.Join(store.MakeDir(gopath.Dir(target)), store.RemoveAll(target)); err != nil {
		return "", 0, err
	}
	if err := store.Rename(temporary, target); err != nil {
		return "", 0, err
	}
	return digest, bytes, store.Sync(gopath.Dir(target))
}
