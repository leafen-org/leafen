package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/spf13/cobra"
)

// A refused restore left the empty databases and folders PocketBase's start
// makes in the target before any command runs (2026-10-16: data.db,
// auxiliary.db, .notify, .secrets and storage in a folder the restore turned
// away). The restore notes what the target held before that start, and a
// refusal takes away only what the start made: in a target that held nothing
// of Leafen's, all of it; in a data folder, only the empty folders, so its
// own files always stay. A restore that began putting the backup in place
// and couldn't take it back out keeps everything, its recovery point
// included; one that could is refused like any other, the folder it made
// going too (2026-11-19, Windows: a failed restore left its new folder, and
// trying again was refused as "not fresh").

// restoreLeafenFiles and restoreLeafenDirs are the top-level entries a data
// folder's start makes; any ".leafen-" entry is Leafen's own as well (a
// served folder holds .leafen-replica-holder, which made a restore into a
// folder only ever started refuse it as the operator's).
var (
	restoreLeafenFiles = map[string]bool{
		"data.db": true, "data.db-wal": true, "data.db-shm": true, "data.db-journal": true,
		"auxiliary.db": true, "auxiliary.db-wal": true, "auxiliary.db-shm": true, "auxiliary.db-journal": true,
	}
	restoreLeafenDirs = map[string]bool{
		"storage": true, core.LocalBackupsDirName: true, core.LocalTempDirName: true, leafenTempDirName: true, core.LocalNotifyDirName: true,
		administratorSecretDirectory: true,
	}
)

func restoreLeafenEntry(name string, dir bool) bool {
	if strings.HasPrefix(name, ".leafen-") {
		return true
	}
	if dir {
		return restoreLeafenDirs[name]
	}
	return restoreLeafenFiles[name]
}

// restoreTargetBefore is what the target held before the start.
type restoreTargetBefore struct {
	noted   bool
	existed bool
	leafen  bool
	entries map[string]bool
}

// noteRestoreTargetBeforeStart notes the target's entries, before anything
// opens it, when the command about to run is the one given: restore, or
// backup, which turns away a data folder the start found without data.db.
func noteRestoreTargetBeforeStart(pb *pocketbase.PocketBase, given *cobra.Command, before *restoreTargetBefore) {
	pb.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{
		Id:       "leafenTargetBefore-" + given.Name(),
		Priority: -1001,
		Func: func(event *core.BootstrapEvent) error {
			if command, _, err := pb.RootCmd.Find(os.Args[1:]); err == nil && command == given && !before.noted {
				entries, err := os.ReadDir(event.App.DataDir())
				if err == nil || errors.Is(err, fs.ErrNotExist) {
					before.noted, before.existed, before.entries = true, err == nil, map[string]bool{}
					for _, entry := range entries {
						before.entries[entry.Name()] = true
						before.leafen = before.leafen || restoreLeafenEntry(entry.Name(), entry.IsDir())
					}
				}
			}
			return event.Next()
		},
	})
}

// restorePublicationError is a restore that failed while putting the backup
// in place and couldn't settle back: what the target holds then is its
// recovery, not a trace.
type restorePublicationError struct{ err error }

func (failure restorePublicationError) Error() string { return failure.err.Error() }
func (failure restorePublicationError) Unwrap() error { return failure.err }

// removeRefusedRestoreTraces takes away what the start made, after a refusal.
func removeRefusedRestoreTraces(pb *pocketbase.PocketBase, before *restoreTargetBefore, refusal error) error {
	if !before.noted || errors.As(refusal, &restorePublicationError{}) {
		return nil
	}
	if err := pb.ClearBootstrap(); err != nil {
		return err
	}
	dataRoot := pb.DataDir()
	entries, err := os.ReadDir(dataRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	removals := []error{}
	for _, entry := range entries {
		if before.entries[entry.Name()] || !restoreLeafenEntry(entry.Name(), entry.IsDir()) {
			continue
		}
		if before.leafen {
			if entry.IsDir() {
				_ = os.Remove(filepath.Join(dataRoot, entry.Name()))
			}
			continue
		}
		removals = append(removals, os.RemoveAll(filepath.Join(dataRoot, entry.Name())))
	}
	if !before.existed {
		// Only an empty folder goes: Remove leaves one holding anything.
		_ = os.Remove(dataRoot)
	}
	return errors.Join(removals...)
}
