package app

import (
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The form's "Operating system and install" for a report a connected agent
// drafts: the system the server runs on, with its version where the system
// says it, and how Leafen runs there — the desktop app, Docker or from
// source (as docs/install.md names them).

var (
	bugReportPlistVersion = regexp.MustCompile(`<key>ProductVersion</key>\s*<string>([^<]+)</string>`)
	bugReportOSRelease    = regexp.MustCompile(`(?m)^PRETTY_NAME="?([^"\n]+)"?`)
)

// bugReportSystem is the server's system and install, with where the person
// saw the problem when the agent names it.
func bugReportSystem(app core.App, client string) string {
	system := bugReportOperatingSystem() + " (" + runtime.GOARCH + ") · " + bugReportInstall(app)
	if client = strings.TrimSpace(client); client != "" {
		system += " · seen in " + client
	}
	return system
}

func bugReportInstall(app core.App) string {
	switch {
	case runtime.GOOS == "ios" || runtime.GOOS == "android":
		return "phone app"
	case localOwnerOf(app) != nil:
		return "desktop app"
	case runningInContainer():
		return "Docker"
	}
	return "from source"
}

func bugReportOperatingSystem() string {
	switch runtime.GOOS {
	case "darwin":
		if plist, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist"); err == nil {
			if match := bugReportPlistVersion.FindSubmatch(plist); match != nil {
				return "macOS " + strings.TrimSpace(string(match[1]))
			}
		}
		return "macOS"
	case "windows":
		return bugReportWindowsVersion()
	case "linux":
		// In a container the release file is the image's; its kernel is the
		// host's.
		if !runningInContainer() {
			if release, err := os.ReadFile("/etc/os-release"); err == nil {
				if match := bugReportOSRelease.FindSubmatch(release); match != nil {
					return strings.TrimSpace(string(match[1]))
				}
			}
		}
		if kernel, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
			return "Linux, kernel " + strings.TrimSpace(string(kernel))
		}
		return "Linux"
	case "android":
		return "Android"
	case "ios":
		return "iOS"
	}
	return runtime.GOOS
}
