//go:build !linux

package app

import "errors"

// renameNoReplace has no form here; links work on these systems.
func renameNoReplace(string, string) error { return errors.ErrUnsupported }
