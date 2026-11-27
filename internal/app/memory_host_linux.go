//go:build linux

package app

import "os"

// hostMemoryBytes is this machine's memory.
func hostMemoryBytes() (int64, bool) {
	source, err := os.ReadFile(defaultHostMemoryInformation)
	if err != nil {
		return 0, false
	}
	return totalMemoryBytesFromMeminfo(string(source))
}
