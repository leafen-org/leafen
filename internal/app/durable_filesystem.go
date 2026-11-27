package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// syncPath makes a file's contents, or a folder's entries, durable. Windows
// flushes a file only through a handle that may write it: one opened to read
// is refused with "Access is denied" (2026-11-19: every restore there failed
// at its first file, "sync …\auxiliary.db: Access is denied."), so there a
// file opens to write, changing nothing. Windows can't flush a folder at all
// (its handle refuses the same way) and NTFS journals a folder's entries
// itself, so there a folder's sync is done once it opens.
func syncPath(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		info, err := file.Stat()
		if err != nil || info.IsDir() {
			return errors.Join(err, file.Close())
		}
		if err := file.Close(); err != nil {
			return err
		}
		if file, err = os.OpenFile(path, os.O_WRONLY, 0); err != nil {
			return err
		}
	}
	return errors.Join(file.Sync(), file.Close())
}

func syncDirectory(path string) error {
	return syncPath(path)
}

func syncRestoredTree(root string, synchronize func(string) error) error {
	if err := visitDirectoryEntries(root, func(path string, info os.FileInfo) error {
		if info.Mode().IsRegular() {
			return synchronize(path)
		}
		if !info.IsDir() {
			return fmt.Errorf("restore contains an unsupported entry: %s", filepath.Base(path))
		}
		return nil
	}); err != nil {
		return err
	}
	if err := visitDirectoryEntries(root, func(path string, info os.FileInfo) error {
		if info.IsDir() {
			return syncRestoredTree(path, synchronize)
		}
		return nil
	}); err != nil {
		return err
	}
	return synchronize(root)
}

func visitDirectoryEntries(root string, visit func(string, os.FileInfo) error) error {
	directory, err := os.Open(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	for {
		entries, err := directory.ReadDir(1)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		entry := entries[0]
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if err := visit(filepath.Join(root, entry.Name()), info); err != nil {
			return err
		}
	}
}
