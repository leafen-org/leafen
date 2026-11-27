//go:build darwin || linux

package app

import (
	"errors"
	"os"
	"syscall"
)

func tryExclusiveBackupFileLock(file *os.File) (func() error, bool, error) {
	return tryBackupFileLock(file, syscall.LOCK_EX)
}

// trySharedBackupFileLock takes the lock beside others holding it shared.
func trySharedBackupFileLock(file *os.File) (func() error, bool, error) {
	return tryBackupFileLock(file, syscall.LOCK_SH)
}

func tryBackupFileLock(file *os.File, kind int) (func() error, bool, error) {
	err := syscall.Flock(int(file.Fd()), kind|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() error {
		return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}, true, nil
}
