//go:build !linux && !darwin

package app

import (
	"errors"
	"io/fs"
)

// cloneMigrationRecoveryFile: no clones here.
func cloneMigrationRecoveryFile(string, string, fs.FileInfo) error {
	return errors.ErrUnsupported
}

// syncMigrationRecoveryFolder makes a recovery point durable: each folder
// and file it made (on Windows a folder only opens: NTFS journals its
// entries, syncPath). A link's content is the data folder's own.
func syncMigrationRecoveryFolder(_ string, folders []string, made []string) error {
	for _, path := range append(folders, made...) {
		if err := syncPath(path); err != nil {
			return err
		}
	}
	return nil
}
