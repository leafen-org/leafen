package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	backupDestinationLockName       = ".leafen-backup.lock"
	backupStagingDirectoryName      = ".leafen-backup-staging"
	backupRawTemporaryPrefix        = ".leafen-backup-raw-"
	backupRawTemporaryPattern       = backupRawTemporaryPrefix + "*.tmp"
	backupVersionedTemporaryPrefix  = ".leafen-backup-versioned-"
	backupVersionedTemporaryPattern = backupVersionedTemporaryPrefix + "*.tmp"
	backupStagingEntryLimit         = 2
)

type backupDestinationLease struct {
	lockFile    *os.File
	stagingRoot string
	unlock      func() error
}

func acquireBackupDestinationLease(destination string) (*backupDestinationLease, error) {
	lockPath := filepath.Join(destination, backupDestinationLockName)
	lockFile, err := openBackupLockFile(lockPath)
	if err != nil {
		return nil, err
	}
	unlock, acquired, err := tryExclusiveBackupFileLock(lockFile)
	if err != nil {
		_ = lockFile.Close()
		return nil, err
	}
	if !acquired {
		_ = lockFile.Close()
		return nil, errors.New("another Leafen backup owns this destination directory")
	}
	lease := &backupDestinationLease{
		lockFile:    lockFile,
		stagingRoot: filepath.Join(destination, backupStagingDirectoryName),
		unlock:      unlock,
	}
	if err := lease.prepareStaging(); err != nil {
		_ = lease.Close()
		return nil, err
	}
	return lease, nil
}

func openBackupLockFile(path string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errors.New("backup destination lock path is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	pathInfo, pathErr := os.Lstat(path)
	fileInfo, fileErr := file.Stat()
	if pathErr != nil || fileErr != nil || pathInfo.Mode()&os.ModeSymlink != 0 ||
		!pathInfo.Mode().IsRegular() || !os.SameFile(pathInfo, fileInfo) {
		_ = file.Close()
		return nil, errors.Join(
			errors.New("backup destination lock identity changed during admission"),
			pathErr,
			fileErr,
		)
	}
	return file, nil
}

func (lease *backupDestinationLease) prepareStaging() error {
	info, err := os.Lstat(lease.stagingRoot)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(lease.stagingRoot, 0o700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("backup staging path is not a private directory")
	}
	entries, err := os.ReadDir(lease.stagingRoot)
	if err != nil {
		return err
	}
	if len(entries) > backupStagingEntryLimit {
		return fmt.Errorf("backup staging contains %d entries; expected at most %d", len(entries), backupStagingEntryLimit)
	}
	for _, entry := range entries {
		if !isOwnedBackupStagingName(entry.Name()) {
			return fmt.Errorf("backup staging contains an unowned entry: %s", entry.Name())
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("backup staging entry is not a regular file: %s", entry.Name())
		}
		if err := os.Remove(filepath.Join(lease.stagingRoot, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func isOwnedBackupStagingName(name string) bool {
	if !strings.HasSuffix(name, ".tmp") {
		return false
	}
	return strings.HasPrefix(name, backupRawTemporaryPrefix) ||
		strings.HasPrefix(name, backupVersionedTemporaryPrefix)
}

func (lease *backupDestinationLease) Close() error {
	if lease == nil || lease.lockFile == nil {
		return nil
	}
	unlockErr := lease.unlock()
	closeErr := lease.lockFile.Close()
	lease.lockFile = nil
	return errors.Join(unlockErr, closeErr)
}
