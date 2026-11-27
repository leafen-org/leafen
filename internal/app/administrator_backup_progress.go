package app

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A backup file shows its steps as it is made, as a scheduled backup does
// (backup_progress.go): PocketBase packing the data folder, its file
// written as Leafen's, checked, and kept for download; each step counts its
// own bytes where they can be told.
const administratorBackupArchiveLook = time.Second

// watchAdministratorBackupArchive counts what PocketBase has packed so far,
// by the size of the archive it writes in its temporary folder, until stop
// is called. How much it will pack in all can't be told: it compresses.
func watchAdministratorBackupArchive(ctx context.Context, app core.App, progress *backupProgress) (stop func()) {
	if progress == nil {
		return func() {}
	}
	progress.restart(backupProgressStepArchive, 0, 0)
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	pattern := filepath.Join(app.DataDir(), core.LocalTempDirName, "pb_backup_*")
	go func() {
		defer close(done)
		ticker := time.NewTicker(administratorBackupArchiveLook)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			matches, _ := filepath.Glob(pattern)
			var newest os.FileInfo
			for _, match := range matches {
				if info, err := os.Stat(match); err == nil && (newest == nil || info.ModTime().After(newest.ModTime())) {
					newest = info
				}
			}
			if newest != nil {
				progress.setDone(newest.Size())
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// administratorBackupProgressOf is how far the job's file has got while it
// is made here, nil otherwise.
func administratorBackupProgressOf(app core.App, jobID string) *administratorBackupProgress {
	progress := runningBackupProgress(app, backupFileProgressStoreKey)
	if progress == nil || progress.jobID != jobID {
		return nil
	}
	return progress.snapshot()
}
