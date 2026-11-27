package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	sqliteDriver "modernc.org/sqlite"
)

// A migration's recovery point (schema_migration_recovery.go) is a data
// folder of its own, made so the time it takes follows the database, not the
// files people put in:
//
//   - data.db is copied as it is at one moment, page by page in one read
//     (SQLite's online backup; VACUUM INTO, which builds the database again
//     and took twice as long, where the driver has no backup).
//   - Every other file is a clone where the disk makes one (APFS, Btrfs, XFS):
//     a file of its own sharing the original's blocks until either changes,
//     made in a moment whatever its size.
//   - Where it doesn't, the files of storage are hard links to the data
//     folder's own. PocketBase's file store never writes into a file there:
//     a new content is a new file renamed over the name, and a deleted one
//     loses only its name, so a link keeps what the file held. Its .attrs
//     notes it writes in place, but only when a name is written again, which
//     Leafen doesn't do: names are random or a content's hash, and a still or
//     a preview, which is made again under one name, is left out.
//   - The rest — keys, secrets, the server's addresses, which may be written
//     in place — and whatever can't be linked, on another disk or a file
//     system without hard links, is copied: slower, but kept.
//
// It leaves out what a folder snapshot leaves out (scheduledBackupLeftOut,
// scheduledBackupMachineOnly): the log database, the backups and work
// folders, exports and imports under way, and this machine's own parts.
// Its first start as a data folder takes a new data epoch, as a restore's.

type migrationRecoveryKept struct {
	DatabaseBytes  int64
	DatabaseSHA256 string
	Files          int
	FileBytes      int64
	Cloned         int
	Linked         int
	Copied         int
	// DatabaseTook is how long the database's copy and its hash took.
	DatabaseTook time.Duration
}

// stageMigrationRecoveryDatabase begins a recovery point at target, which
// isn't there yet, on the data folder's disk: the database as it is now, and
// its SHA-256, which says whether a point kept before still holds the data
// (schema_migration_recovery.go).
func stageMigrationRecoveryDatabase(app core.App, target string) (migrationRecoveryKept, error) {
	kept := migrationRecoveryKept{}
	if err := os.Mkdir(target, 0o700); err != nil {
		return kept, err
	}
	started := time.Now()
	database := filepath.Join(target, dataDatabaseFile)
	if err := copyMigrationRecoveryDatabase(app, filepath.Join(app.DataDir(), dataDatabaseFile), database); err != nil {
		return kept, err
	}
	var err error
	if kept.DatabaseSHA256, kept.DatabaseBytes, err = fileSHA256(database); err != nil {
		return kept, err
	}
	kept.DatabaseTook = time.Since(started)
	return kept, nil
}

// keepMigrationRecoveryFiles adds the data folder's other files to a point
// stageMigrationRecoveryDatabase began, and makes it durable.
func keepMigrationRecoveryFiles(app core.App, target string, kept *migrationRecoveryKept) error {
	dataRoot, err := filepath.Abs(app.DataDir())
	if err != nil {
		return err
	}
	folders := []string{target}
	made := []string{filepath.Join(target, dataDatabaseFile)}

	// A way the disk refuses once isn't tried again.
	cloning, linking := true, true
	walkErr := filepath.WalkDir(dataRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dataRoot, path)
		if err != nil || relative == "." {
			return err
		}
		top := strings.SplitN(filepath.ToSlash(relative), "/", 2)[0]
		if scheduledBackupLeftOut[top] || scheduledBackupMachineOnly[top] ||
			(entry.IsDir() && strings.HasPrefix(entry.Name(), productFilePreviewFolderPrefix)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(target, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			folders = append(folders, destination)
			return os.Mkdir(destination, info.Mode().Perm()|0o700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		kept.Files++
		kept.FileBytes += info.Size()
		if cloning {
			if cloneMigrationRecoveryFile(path, destination, info) == nil {
				kept.Cloned++
				made = append(made, destination)
				return nil
			}
			cloning = false
		}
		if linking && top == core.LocalStorageDirName {
			if os.Link(path, destination) == nil {
				kept.Linked++
				return nil
			}
			linking = false
		}
		kept.Copied++
		made = append(made, destination)
		return copyMigrationRecoveryFile(path, destination, info)
	})
	if walkErr != nil {
		return walkErr
	}
	if err := markRestoredDataRootForNewEpoch(target); err != nil {
		return err
	}
	work := restoreWorkDir(target)
	folders = append(folders, filepath.Dir(work), work)
	made = append(made, filepath.Join(work, restoreEpochMarkerName))
	return syncMigrationRecoveryFolder(target, folders, made)
}

// copyMigrationRecoveryDatabase copies the database at source to target, a
// file that isn't there yet, as it is at one moment.
func copyMigrationRecoveryDatabase(app core.App, source string, target string) error {
	database, err := sql.Open("sqlite", source+"?_pragma=busy_timeout("+strconv.FormatInt(sqliteBusyTimeout.Milliseconds(), 10)+")")
	if err != nil {
		return err
	}
	defer database.Close()
	connection, err := database.Conn(context.Background())
	if err != nil {
		return err
	}
	defer connection.Close()
	backedUp := false
	err = connection.Raw(func(driverConnection any) error {
		backer, ok := driverConnection.(interface {
			NewBackup(string) (*sqliteDriver.Backup, error)
		})
		if !ok {
			return nil
		}
		backup, err := backer.NewBackup(target)
		if err != nil {
			return err
		}
		_, stepErr := backup.Step(-1)
		backedUp = stepErr == nil
		return errors.Join(stepErr, backup.Finish())
	})
	if err != nil || backedUp {
		return err
	}
	_, err = app.ConcurrentDB().NewQuery("VACUUM INTO {:path}").Bind(dbx.Params{"path": target}).Execute()
	return err
}

// copyMigrationRecoveryFile copies a file with its mode and time.
func copyMigrationRecoveryFile(source string, destination string, info fs.FileInfo) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return err
	}
	return os.Chtimes(destination, info.ModTime(), info.ModTime())
}
