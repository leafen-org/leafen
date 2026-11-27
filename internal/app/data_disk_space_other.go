//go:build !linux && !darwin && !freebsd && !windows

package app

// dataDiskSpace can't read a disk's free space on this system, so the
// floor isn't checked.
func dataDiskSpace(string) (free, total uint64, ok bool) {
	return 0, 0, false
}
