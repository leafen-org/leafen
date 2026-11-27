package app

import (
	"errors"
	"io/fs"
	"os"
)

// linkRefused reports a hard link the system won't make, as Android refuses
// them in an app's own data (SELinux), or a file system without them.
func linkRefused(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, errors.ErrUnsupported)
}

// publishNoReplace gives target the file source names without ever
// replacing a target that exists, failing then with an error matching
// os.ErrExist. It links, leaving source in place; where the system refuses
// links it renames without replacing instead, which moves source.
func publishNoReplace(source, target string) error {
	err := os.Link(source, target)
	if err == nil || !linkRefused(err) {
		return err
	}
	if renameErr := renameNoReplace(source, target); !errors.Is(renameErr, errors.ErrUnsupported) {
		return renameErr
	}
	return err
}
