package app

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// bugReportWindowsVersion is Windows's version as the system reports it;
// builds from 22000 are Windows 11.
func bugReportWindowsVersion() string {
	version := windows.RtlGetVersion()
	name := "Windows"
	switch {
	case version.MajorVersion == 10 && version.BuildNumber >= 22000:
		name = "Windows 11"
	case version.MajorVersion == 10:
		name = "Windows 10"
	}
	return fmt.Sprintf("%s (%d.%d.%d)", name, version.MajorVersion, version.MinorVersion, version.BuildNumber)
}
