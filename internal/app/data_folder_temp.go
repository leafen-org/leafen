package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// What one Leafen process does in the data folder doesn't go from under
// another's (2026-11-06: a server bringing its data up stopped with
// "LEAFEN_SCHEMA_MIGRATION_RECOVERY_FAILED: unable to open database …
// .pb_temp_to_delete/auxiliary.db" as `account device-code` started beside
// it). PocketBase empties its temporary folder at every process's start,
// whatever another process has there. So Leafen keeps its own work — a
// database copied for a snapshot, files on their way to WebDAV, the
// replica's spool — in a folder of its own, which a start empties only of
// what has lain there a day; and what PocketBase itself writes there (a
// backup's archive) holds the folder's lease, which a start waits for before
// PocketBase empties it.

// leafenTempDirName is Leafen's own work folder in the data folder, left out
// of backups and snapshots as PocketBase's is.
const leafenTempDirName = ".leafen_temp"

const (
	tempLeaseLockName = ".leafen-temp.lock"
	// A start waits this long at most for backups under way.
	tempLeaseWait     = 30 * time.Minute
	tempLeaseSaidEach = 15 * time.Second
	// What has lain in Leafen's own folder this long is left from a process
	// that stopped; nothing at work keeps a file there that long.
	leafenTempAbandonedAge = 24 * time.Hour
)

func leafenTempDir(dataDir string) string { return filepath.Join(dataDir, leafenTempDirName) }

// holdTempLease takes the lease on PocketBase's temporary folder: shared while
// a backup writes there, exclusive while a start empties it. It waits for the
// other kind, saying so now and then.
func holdTempLease(dataDir string, exclusive bool) (func(), error) {
	root := filepath.Join(dataDir, core.LocalBackupsDirName)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	file, err := openBackupLockFile(filepath.Join(root, tempLeaseLockName))
	if err != nil {
		return nil, err
	}
	take := trySharedBackupFileLock
	if exclusive {
		take = tryExclusiveBackupFileLock
	}
	started := time.Now()
	said := started
	for {
		unlock, taken, err := take(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if taken {
			return func() {
				_ = unlock()
				_ = file.Close()
			}, nil
		}
		if time.Since(started) > tempLeaseWait {
			_ = file.Close()
			if exclusive {
				return nil, errors.New("another Leafen process is still making a backup in this data folder")
			}
			return nil, errors.New("another Leafen process is still starting on this data folder")
		}
		if exclusive && time.Since(said) >= tempLeaseSaidEach {
			said = time.Now()
			fmt.Fprintln(os.Stderr, "leafen: waiting for another Leafen process to finish a backup in this data folder")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// dropAbandonedLeafenTemp clears what a stopped process left in Leafen's own
// folder.
func dropAbandonedLeafenTemp(dataDir string) {
	root := leafenTempDir(dataDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && time.Since(info.ModTime()) > leafenTempAbandonedAge {
			_ = os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

// createBackupUnderLease makes PocketBase's backup, which writes its archive
// in PocketBase's temporary folder, holding that folder's lease.
func createBackupUnderLease(ctx context.Context, app core.App, name string) error {
	release, err := holdTempLease(app.DataDir(), false)
	if err != nil {
		return err
	}
	defer release()
	return app.CreateBackup(ctx, name)
}
