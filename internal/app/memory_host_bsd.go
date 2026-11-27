//go:build darwin || freebsd

package app

import (
	"math"
	"runtime"

	"golang.org/x/sys/unix"
)

// hostMemoryBytes is this machine's memory.
func hostMemoryBytes() (int64, bool) {
	name := "hw.physmem"
	if runtime.GOOS == "darwin" {
		name = "hw.memsize"
	}
	total, err := unix.SysctlUint64(name)
	if err != nil || total == 0 || total > math.MaxInt64 {
		return 0, false
	}
	return int64(total), true
}
