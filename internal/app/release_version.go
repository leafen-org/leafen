package app

import (
	"regexp"
	"strconv"
	"strings"
)

// leafenReleaseVersion is this build's release version. Release builds set it
// with -ldflags "-X leafen.org/leafen/internal/app.leafenReleaseVersion=<version>".
var leafenReleaseVersion = "0.1.0-dev"

var releaseVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

type releaseVersion struct {
	core       [3]int
	prerelease []string
}

func parseReleaseVersion(value string) (releaseVersion, bool) {
	match := releaseVersionPattern.FindStringSubmatch(value)
	if match == nil || len(value) > 64 {
		return releaseVersion{}, false
	}
	version := releaseVersion{}
	for index := range version.core {
		version.core[index], _ = strconv.Atoi(match[index+1])
	}
	if match[4] != "" {
		version.prerelease = strings.Split(match[4], ".")
	}
	return version, true
}

// compareReleaseVersions orders versions as semantic versioning does: a
// prerelease comes before its release.
func compareReleaseVersions(left, right releaseVersion) int {
	for index := range left.core {
		if left.core[index] != right.core[index] {
			if left.core[index] < right.core[index] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(left.prerelease) == 0 && len(right.prerelease) == 0:
		return 0
	case len(left.prerelease) == 0:
		return 1
	case len(right.prerelease) == 0:
		return -1
	}
	for index := 0; index < len(left.prerelease) && index < len(right.prerelease); index++ {
		a, b := left.prerelease[index], right.prerelease[index]
		if a == b {
			continue
		}
		aNumber, aErr := strconv.Atoi(a)
		bNumber, bErr := strconv.Atoi(b)
		switch {
		case aErr == nil && bErr == nil:
			if aNumber < bNumber {
				return -1
			}
			return 1
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		case a < b:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(left.prerelease) < len(right.prerelease):
		return -1
	case len(left.prerelease) > len(right.prerelease):
		return 1
	}
	return 0
}
