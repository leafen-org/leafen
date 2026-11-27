//go:build windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryExclusiveBackupFileLock(file *os.File) (func() error, bool, error) {
	return tryBackupFileLock(file, windows.LOCKFILE_EXCLUSIVE_LOCK)
}

// trySharedBackupFileLock takes the lock beside others holding it shared.
func trySharedBackupFileLock(file *os.File) (func() error, bool, error) {
	return tryBackupFileLock(file, 0)
}

func tryBackupFileLock(file *os.File, kind uint32) (func() error, bool, error) {
	overlapped := &windows.Overlapped{}
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		kind|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() error {
		return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
	}, true, nil
}
