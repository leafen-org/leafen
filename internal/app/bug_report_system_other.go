//go:build !windows

package app

// bugReportWindowsVersion is only asked on Windows.
func bugReportWindowsVersion() string {
	return "Windows"
}
