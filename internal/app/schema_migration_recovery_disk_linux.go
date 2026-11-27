package app

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// cloneMigrationRecoveryFile makes destination a clone of source (FICLONE:
// Btrfs, XFS); a disk that can't leaves nothing behind.
func cloneMigrationRecoveryFile(source string, destination string, info fs.FileInfo) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	err = errors.Join(unix.IoctlFileClone(int(output.Fd()), int(input.Fd())), output.Close())
	if err == nil {
		err = os.Chtimes(destination, info.ModTime(), info.ModTime())
	}
	if err != nil {
		_ = os.Remove(destination)
	}
	return err
}

// syncMigrationRecoveryFolder makes a recovery point durable: on Linux one
// syncfs of its disk takes every folder made, clone, link and copy at once
// (a folder's fsync each would be one per attachment).
func syncMigrationRecoveryFolder(root string, _ []string, _ []string) error {
	folder, err := os.Open(root)
	if err != nil {
		return err
	}
	return errors.Join(unix.Syncfs(int(folder.Fd())), folder.Close())
}
