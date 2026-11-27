package app

import (
	"errors"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// cloneMigrationRecoveryFile makes destination a clone of source (APFS).
func cloneMigrationRecoveryFile(source string, destination string, _ fs.FileInfo) error {
	return unix.Clonefile(source, destination, unix.CLONE_NOFOLLOW)
}

// syncMigrationRecoveryFolder makes a recovery point durable: each folder
// and file it made with a plain fsync, then one full sync, which flushes
// the disk's cache for all of them (a full sync each, one per attachment,
// took 11 s for 2,400 folders here, the plain ones 40 ms). A link's content
// is the data folder's own.
func syncMigrationRecoveryFolder(root string, folders []string, made []string) error {
	for _, path := range append(folders, made...) {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		if err := errors.Join(syscall.Fsync(int(file.Fd())), file.Close()); err != nil {
			return err
		}
	}
	return syncPath(root)
}
