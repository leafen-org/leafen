package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase/tools/osutils"
)

// publishRestoredDataRoot moves the data folder's content into oldRoot and
// extractedRoot's into the data folder. The data folder's own entry a work
// folder is in (Leafen's own work folder, restoreDataRoot) stays put both
// ways.
func publishRestoredDataRoot(dataRoot, extractedRoot, oldRoot string, synchronize func(string) error) error {
	stay := restoreWorkEntries(dataRoot, extractedRoot, oldRoot)
	if err := syncRestoredTree(extractedRoot, synchronize); err != nil {
		return err
	}
	if err := osutils.MoveDirContent(dataRoot, oldRoot, stay...); err != nil {
		return err
	}
	if err := osutils.MoveDirContent(extractedRoot, dataRoot, stay...); err != nil {
		return errors.Join(
			err,
			rollbackRestoredDataRoot(dataRoot, extractedRoot, oldRoot, synchronize),
		)
	}
	if err := synchronize(dataRoot); err != nil {
		return errors.Join(
			err,
			rollbackRestoredDataRoot(dataRoot, extractedRoot, oldRoot, synchronize),
		)
	}
	if err := synchronize(filepath.Dir(oldRoot)); err != nil {
		return errors.Join(
			err,
			rollbackRestoredDataRoot(dataRoot, extractedRoot, oldRoot, synchronize),
		)
	}
	return nil
}

func rollbackRestoredDataRoot(dataRoot, extractedRoot, oldRoot string, synchronize func(string) error) error {
	if err := osutils.MoveDirContent(dataRoot, extractedRoot, restoreWorkEntries(dataRoot, extractedRoot, oldRoot)...); err != nil {
		return err
	}
	if err := osutils.MoveDirContent(oldRoot, dataRoot); err != nil {
		return err
	}
	return errors.Join(
		synchronize(dataRoot),
		synchronize(filepath.Dir(oldRoot)),
	)
}

// restoreWorkEntries names the data folder's top-level entries that hold one
// of the work folders, for those inside it.
func restoreWorkEntries(dataRoot string, work ...string) []string {
	entries := []string{}
	for _, folder := range work {
		relative, err := filepath.Rel(dataRoot, folder)
		if err != nil || relative == "." || !pathWithin(dataRoot, folder) {
			continue
		}
		entries = append(entries, strings.SplitN(filepath.ToSlash(relative), "/", 2)[0])
	}
	return entries
}

func restorePublicationRecoverySettled(oldRoot string) (bool, error) {
	entries, err := os.ReadDir(oldRoot)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
