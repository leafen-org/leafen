package app

import (
	"os"
	"runtime/debug"
	"strconv"
)

// serverBuildIdentity names the build this process runs: its release
// version, the source revision it was built from and the executable file
// it started as, read once at start (a binary replaced while it runs does
// not rename the old process). Results kept for reuse — a database query's
// rows, a SQL query's — are named by it too, so a newer build, whose
// filters and formulas may read rows differently, never answers with rows
// an older one worked out, while the same build keeps reusing them across
// restarts.
var serverBuildIdentity = readServerBuildIdentity()

func readServerBuildIdentity() string {
	identity := leafenReleaseVersion
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" || setting.Key == "vcs.modified" {
				identity += "|" + setting.Value
			}
		}
	}
	if path, err := os.Executable(); err == nil {
		if stat, err := os.Stat(path); err == nil {
			identity += "|" + strconv.FormatInt(stat.Size(), 10) + "|" + strconv.FormatInt(stat.ModTime().UnixNano(), 10)
		}
	}
	return identity
}
