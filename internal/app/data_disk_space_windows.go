//go:build windows

package app

import "golang.org/x/sys/windows"

// dataDiskSpace is the bytes free to this process and the size of the disk
// holding dir.
func dataDiskSpace(dir string) (free, total uint64, ok bool) {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0, false
	}
	var totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(path, &free, &total, &totalFree); err != nil {
		return 0, 0, false
	}
	return free, total, true
}
