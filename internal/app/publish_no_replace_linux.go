//go:build linux

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// renameNoReplace renames source to target unless target exists (Linux and
// Android, renameat2 with RENAME_NOREPLACE).
func renameNoReplace(source, target string) error {
	err := unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS):
		return errors.ErrUnsupported
	default:
		return &os.LinkError{Op: "renameat2", Old: source, New: target, Err: err}
	}
}
