//go:build linux || darwin || freebsd

package app

import "syscall"

// dataDiskSpace is the bytes free to this process and the size of the disk
// holding dir.
func dataDiskSpace(dir string) (free, total uint64, ok bool) {
	stat := syscall.Statfs_t{}
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, 0, false
	}
	blockSize := uint64(stat.Bsize)
	return uint64(stat.Bavail) * blockSize, uint64(stat.Blocks) * blockSize, true
}
