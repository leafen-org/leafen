//go:build !linux && !darwin && !freebsd

package app

// hostMemoryBytes is unknown here; the server plans for the default.
func hostMemoryBytes() (int64, bool) { return 0, false }
