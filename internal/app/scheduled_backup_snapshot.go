package app

import (
	"bytes"
	"compress/gzip"
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
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A backup folder holds "Leafen backups/snapshots/<time>/", one folder per
// snapshot: the database as it was at one moment (data.db.gz) and
// snapshot.json, which names every other kept file of the data folder by its
// content. Contents sit once in "Leafen backups/blobs/sha256/<ab>/<hash>",
// shared by every snapshot that has them, so a run adds only what is new. A
// snapshot is written as ".incomplete-<time>" and takes its name only when
// whole, and a file's content takes its name only once copied, so a folder
// another program syncs never shows a part as the whole.
const (
	scheduledBackupRootName         = "Leafen backups"
	scheduledBackupSnapshotsName    = "snapshots"
	scheduledBackupBlobsName        = "blobs"
	scheduledBackupManifestName     = "snapshot.json"
	scheduledBackupDatabaseName     = "data.db.gz"
	scheduledBackupFormat           = "leafen.folder-snapshot"
	scheduledBackupFormatVersion    = 1
	scheduledBackupIncompletePrefix = ".incomplete-"
	scheduledBackupIncomingPrefix   = ".incoming-"
	// A part a run left behind when it stopped is cleared once this old.
	scheduledBackupAbandonedAge = 24 * time.Hour
)

// What of the data folder a snapshot leaves out: the database, written apart
// at one moment; the log database; what PocketBase leaves out of its own
// backups; and the work folders of exports and imports, which make again.
var scheduledBackupLeftOut = map[string]bool{
	"data.db": true, "data.db-wal": true, "data.db-shm": true, "data.db-journal": true,
	"auxiliary.db": true, "auxiliary.db-wal": true, "auxiliary.db-shm": true, "auxiliary.db-journal": true,
	core.LocalBackupsDirName: true, core.LocalTempDirName: true, leafenTempDirName: true, core.LocalNotifyDirName: true,
	core.LocalAutocertCacheDirName: true, "lost+found": true,
	workspaceExportDirectory: true, instanceProductExportDirectory: true, workspaceImportDirectory: true, "tmp": true,
}

type scheduledBackupManifest struct {
	Format        string                 `json:"format"`
	FormatVersion int                    `json:"formatVersion"`
	CreatedAt     string                 `json:"createdAt"`
	SchemaVersion int                    `json:"schemaVersion"`
	Release       string                 `json:"release"`
	Database      scheduledBackupEntry   `json:"database"`
	Files         []scheduledBackupEntry `json:"files"`
}

// scheduledBackupEntry is one file of the data folder: where it goes, and its
// content's SHA-256 and size. The database's is that of data.db unpacked. In
// a sealed folder, blob names the content.
type scheduledBackupEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Blob   string `json:"blob,omitempty"`
}

type scheduledBackupOutcome struct {
	Snapshot       string
	ManifestSHA256 string
	AddedBytes     int64
	Snapshots      int
}

func scheduledBackupBlobPath(root, digest string) string {
	return filepath.Join(root, scheduledBackupBlobsName, "sha256", digest[:2], digest)
}

// scheduledBackupBlobStorePath is a content's place in a store, under root.
func scheduledBackupBlobStorePath(root, digest string) string {
	return scheduledBackupStoreJoin(root, scheduledBackupBlobsName, "sha256", digest[:2], digest)
}

// writeScheduledBackupSnapshot writes one snapshot of the data folder into
// a backup folder, then keeps the newest `keep` snapshots and the contents
// they name.
func writeScheduledBackupSnapshot(ctx context.Context, app core.App, folder string, keep int, password string, now time.Time) (scheduledBackupOutcome, error) {
	return writeScheduledBackupSnapshotWith(ctx, app, newFolderBackupStore(folder), keep, passwordBackupSecret(password), now)
}

// writeScheduledBackupSnapshotTo writes a snapshot sealed by a password
// alone, or unsealed with none.
func writeScheduledBackupSnapshotTo(ctx context.Context, app core.App, store scheduledBackupStore, keep int, password string, now time.Time) (scheduledBackupOutcome, error) {
	return writeScheduledBackupSnapshotWith(ctx, app, store, keep, passwordBackupSecret(password), now)
}

// writeScheduledBackupSnapshotTo writes one snapshot into a store, then
// keeps the newest `keep` snapshots and the contents they name.
func writeScheduledBackupSnapshotWith(ctx context.Context, app core.App, store scheduledBackupStore, keep int, secret scheduledBackupSecret, now time.Time) (scheduledBackupOutcome, error) {
	root := scheduledBackupRootName
	snapshots := scheduledBackupStoreJoin(root, scheduledBackupSnapshotsName)
	if err := store.MakeDir(snapshots); err != nil {
		return scheduledBackupOutcome{}, err
	}
	if secret.Opened {
		return scheduledBackupOutcome{}, errScheduledBackupPlaceOpened
	}
	if foreign, err := scheduledBackupPlaceForeign(store, root, secret); err != nil {
		return scheduledBackupOutcome{}, err
	} else if foreign {
		return scheduledBackupOutcome{}, errScheduledBackupPlaceForeign
	}
	// With a secret, everything the snapshot writes is sealed.
	var key *scheduledBackupKey
	if !secret.empty() {
		found, err := scheduledBackupKeyIn(store, root, secret, true)
		if err != nil {
			return scheduledBackupOutcome{}, err
		}
		key = found
	}
	name := now.UTC().Format("20060102T150405Z")
	for suffix := 2; scheduledBackupStoreExists(store, scheduledBackupStoreJoin(snapshots, name)); suffix++ {
		name = fmt.Sprintf("%s-%d", now.UTC().Format("20060102T150405Z"), suffix)
	}
	incomplete := scheduledBackupStoreJoin(snapshots, scheduledBackupIncompletePrefix+name)
	if err := store.MakeDir(incomplete); err != nil {
		return scheduledBackupOutcome{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = store.RemoveAll(incomplete)
		}
	}()

	// What it goes through in all, for the run's progress: the files now,
	// the database once it is copied.
	if progress := backupProgressFrom(ctx); progress != nil {
		progress.total(countScheduledBackupFiles(ctx, app.DataDir()))
	}
	database, err := writeScheduledBackupDatabase(ctx, app, store, scheduledBackupStoreJoin(incomplete, key.fileName(scheduledBackupDatabaseName)), key)
	if err != nil {
		return scheduledBackupOutcome{}, err
	}
	files, added, err := keepScheduledBackupFiles(ctx, app, store, root, key, now)
	if err != nil {
		return scheduledBackupOutcome{}, err
	}
	backupProgressFrom(ctx).step(backupProgressStepFinishing, 0, 0)
	schemaVersion := 0
	_ = app.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&schemaVersion)
	manifest, err := json.MarshalIndent(scheduledBackupManifest{
		Format: scheduledBackupFormat, FormatVersion: scheduledBackupFormatVersion,
		CreatedAt: now.UTC().Format(time.RFC3339), SchemaVersion: schemaVersion, Release: leafenReleaseVersion,
		Database: database, Files: files,
	}, "", " ")
	if err != nil {
		return scheduledBackupOutcome{}, err
	}
	// The list is sealed as it is written; its SHA-256 is of what is kept.
	written := new(bytes.Buffer)
	sealed, err := key.writer(written)
	if err != nil {
		return scheduledBackupOutcome{}, err
	}
	_, writeErr := sealed.Write(manifest)
	if err := errors.Join(writeErr, sealed.Close()); err != nil {
		return scheduledBackupOutcome{}, err
	}
	digest := sha256.Sum256(written.Bytes())
	if err := scheduledBackupStoreWriteAll(store, scheduledBackupStoreJoin(incomplete, key.fileName(scheduledBackupManifestName)), written.Bytes()); err != nil {
		return scheduledBackupOutcome{}, err
	}
	if err := store.Sync(incomplete); err != nil {
		return scheduledBackupOutcome{}, err
	}
	if err := store.Rename(incomplete, scheduledBackupStoreJoin(snapshots, name)); err != nil {
		return scheduledBackupOutcome{}, err
	}
	published = true
	if err := store.Sync(snapshots); err != nil {
		return scheduledBackupOutcome{}, err
	}
	kept, err := pruneScheduledBackups(store, root, keep, key, now)
	if err != nil {
		return scheduledBackupOutcome{}, err
	}
	packed, err := store.Stat(scheduledBackupStoreJoin(snapshots, name, key.fileName(scheduledBackupDatabaseName)))
	if err != nil {
		return scheduledBackupOutcome{}, err
	}
	return scheduledBackupOutcome{
		Snapshot: name, ManifestSHA256: hex.EncodeToString(digest[:]), AddedBytes: added + packed.Size, Snapshots: kept,
	}, nil
}

// writeScheduledBackupDatabase copies the database as it is at one moment
// (VACUUM INTO, which leaves writers working) and packs it into target,
// sealed with a key.
func writeScheduledBackupDatabase(ctx context.Context, app core.App, store scheduledBackupStore, target string, key *scheduledBackupKey) (scheduledBackupEntry, error) {
	temporaryRoot := leafenTempDir(app.DataDir())
	if err := os.MkdirAll(temporaryRoot, 0o700); err != nil {
		return scheduledBackupEntry{}, err
	}
	copied := filepath.Join(temporaryRoot, "leafen-scheduled-backup-"+security.RandomString(12)+".db")
	defer os.Remove(copied)
	progress := backupProgressFrom(ctx)
	progress.step(backupProgressStepDatabase, 0, 1)
	// The read log's gathered records go in first (read_log_buffer.go).
	flushReadLog(app)
	if _, err := app.ConcurrentDB().NewQuery("VACUUM INTO {:path}").WithContext(ctx).Bind(dbx.Params{"path": copied}).Execute(); err != nil {
		return scheduledBackupEntry{}, err
	}
	source, err := os.Open(copied)
	if err != nil {
		return scheduledBackupEntry{}, err
	}
	defer source.Close()
	if info, err := source.Stat(); err == nil {
		progress.total(info.Size(), 0)
	}
	output, err := store.Create(target)
	if err != nil {
		return scheduledBackupEntry{}, err
	}
	sealed, err := key.writer(output)
	if err != nil {
		return scheduledBackupEntry{}, errors.Join(err, output.Close())
	}
	hash := sha256.New()
	packed := gzip.NewWriter(sealed)
	bytes, copyErr := io.Copy(io.MultiWriter(packed, hash), progress.reader(source))
	err = errors.Join(copyErr, packed.Close(), sealed.Close(), output.Close())
	if err != nil {
		return scheduledBackupEntry{}, err
	}
	progress.done(0, 1)
	return scheduledBackupEntry{Path: "data.db", SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: bytes}, nil
}

// readScheduledBackupManifest reads the list of a snapshot folder on the
// server's own file system.
func readScheduledBackupManifest(snapshot string, key *scheduledBackupKey) (scheduledBackupManifest, error) {
	return readScheduledBackupManifestIn(newFolderBackupStore(snapshot), "", key)
}

// readScheduledBackupManifestIn reads a snapshot's list, opening a sealed
// one with the key; a key that isn't its own refuses it.
func readScheduledBackupManifestIn(store scheduledBackupStore, snapshot string, key *scheduledBackupKey) (scheduledBackupManifest, error) {
	raw, err := scheduledBackupStoreReadAll(store, scheduledBackupStoreJoin(snapshot, scheduledBackupManifestName))
	if !errors.Is(err, fs.ErrNotExist) {
		if err != nil {
			return scheduledBackupManifest{}, err
		}
		return decodeScheduledBackupManifest(raw, nil)
	}
	if key == nil {
		return scheduledBackupManifest{}, errScheduledBackupPassword
	}
	if raw, err = scheduledBackupStoreReadAll(store, scheduledBackupStoreJoin(snapshot, key.fileName(scheduledBackupManifestName))); err != nil {
		return scheduledBackupManifest{}, err
	}
	return decodeScheduledBackupManifest(raw, key)
}

// decodeScheduledBackupManifest checks a snapshot's list as it is kept,
// opening a sealed one with the key.
func decodeScheduledBackupManifest(raw []byte, key *scheduledBackupKey) (scheduledBackupManifest, error) {
	if key != nil {
		opened := new(bytes.Buffer)
		if err := key.openSealed(opened, bytes.NewReader(raw)); err != nil {
			return scheduledBackupManifest{}, err
		}
		raw = opened.Bytes()
	}
	manifest := scheduledBackupManifest{}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return scheduledBackupManifest{}, err
	}
	if isRetiredProductFormat(manifest.Format) {
		return scheduledBackupManifest{}, errRetiredProductData
	}
	if manifest.Format != scheduledBackupFormat || manifest.FormatVersion != scheduledBackupFormatVersion ||
		manifest.Database.Path != "data.db" || !sha256Pattern.MatchString(manifest.Database.SHA256) {
		return scheduledBackupManifest{}, errors.New("not a Leafen folder snapshot")
	}
	for _, file := range manifest.Files {
		if !sha256Pattern.MatchString(file.SHA256) || !scheduledBackupEntryPathValid(file.Path) || file.Bytes < 0 ||
			(file.Blob != "" && !sha256Pattern.MatchString(file.Blob)) {
			return scheduledBackupManifest{}, errors.New("the snapshot names a file it can't hold")
		}
	}
	return manifest, nil
}

// A file's place is inside the data folder: relative, with no step out and
// none of the parts a snapshot leaves out.
func scheduledBackupEntryPathValid(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || filepath.IsAbs(path) {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return !scheduledBackupLeftOut[strings.SplitN(path, "/", 2)[0]]
}
