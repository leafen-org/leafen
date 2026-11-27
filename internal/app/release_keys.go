package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
)

// A release must trust only the production keys: one that trusted the
// development keys would take an update notice, a desktop update list or a
// sponsor list signed with them, whose private halves sit in a development
// repository (2026-10-18: nothing stopped a release keeping them). So a
// release build (the leafen_release tag) won't start with a development key
// or with none, and a build stamped with any version but a development one
// (a pre-release naming "dev") won't start unless it is a release build.

// developmentReleaseKeys are the development keys' public halves, whatever
// name a list gives them.
var developmentReleaseKeys = []string{
	"-xHs8Vn5YTabTkMU-rWililXbIa3cL-ZFdiSPZmnTCQ",
	"LybQYQqr76o0SqpGp5iJh_RsxN_48ThW1D2dYiOlelA",
}

func checkReleaseKeys(release bool, version string, lists ...map[string]string) error {
	if !release {
		parsed, ok := parseReleaseVersion(version)
		if ok && !slices.ContainsFunc(parsed.prerelease, func(part string) bool { return strings.Contains(strings.ToLower(part), "dev") }) {
			return errors.New("release version " + version + " built without the leafen_release tag would trust the development keys")
		}
		return nil
	}
	for _, keys := range lists {
		if len(keys) == 0 {
			return errors.New("this release build trusts no production key for a signed list")
		}
		for id, key := range keys {
			for _, development := range developmentReleaseKeys {
				if key == development || strings.Contains(id, "-dev-") {
					return errors.New("this release build trusts the development key " + id)
				}
			}
		}
	}
	return nil
}

// registerReleaseKeyCheck stops a misbuilt release before any command runs.
func registerReleaseKeyCheck(pb *pocketbase.PocketBase) {
	pb.OnBootstrap().BindFunc(func(event *core.BootstrapEvent) error {
		if err := checkReleaseKeys(releaseBuild, leafenReleaseVersion, releaseNoticeKeys, sponsorListKeys); err != nil {
			return err
		}
		return event.Next()
	})
}
