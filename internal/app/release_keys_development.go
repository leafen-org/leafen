//go:build !leafen_release

package app

// releaseBuild is whether this is a release build, made with the
// leafen_release tag.
const releaseBuild = false

// The development keys sign test lists; their private halves are
// in the private development repository only.
var (
	releaseNoticeKeys = map[string]string{
		"leafen-dev-2026-09": "-xHs8Vn5YTabTkMU-rWililXbIa3cL-ZFdiSPZmnTCQ",
	}
	sponsorListKeys = map[string]string{
		"leafen-sponsors-dev-2026-10": "LybQYQqr76o0SqpGp5iJh_RsxN_48ThW1D2dYiOlelA",
	}
)

// A development build reads lists only from LEAFEN_RELEASE_LIST_URL: the
// official lists are signed with the production keys, which it doesn't trust.
const officialReleaseListURL = ""

// Nor does it trust the official app catalog by itself; a trial build names
// a local one with -ldflags -X (an administrator can still add any catalog by
// its address and list key).
var (
	appOfficialCatalogURL = ""
	appOfficialCatalogKey = ""
)
