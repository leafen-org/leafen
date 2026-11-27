//go:build leafen_release

package app

const releaseBuild = true

// What a release trusts and reads by itself, written here once the
// maintainers made the production keys (their private halves stay encrypted,
// off this repository): the keys that sign the release notice and desktop
// update list and the sponsor list without a person (the revenue decision of
// 2026-10-17, 4 and 8), the address those lists are published under, and the
// official app catalog. Until the keys are here a release build trusts none
// and won't start (release_keys.go).
var (
	releaseNoticeKeys = map[string]string{
		"leafen-release-2026-11": "dPibCp2RRJHo-bXBgFwDgUMHsLlGzVJH_pjMJtmr62Q",
	}
	sponsorListKeys = map[string]string{
		"leafen-sponsors-2026-11": "aeT2ds5I6M3QRVFETMRZc_pmh93Vf7iIq73wYelzuTY",
	}
)

// officialReleaseListURL is the address the release's lists are under
// (release-v1.json, sponsors-v1.json, desktop-v1.json; docs/release-lists.md);
// LEAFEN_RELEASE_LIST_URL names another.
const officialReleaseListURL = "https://leafen.org/lists"

// The official app catalog: the catalog.json of Leafen's latest release and
// the list key `leafen app catalog` printed (docs/apps.md; decision of
// 2026-11-06). While either is empty no catalog is trusted by itself. They
// are variables so a trial build can name a local catalog with -ldflags -X.
var (
	appOfficialCatalogURL = "https://github.com/leafen-org/leafen/releases/latest/download/catalog.json"
	appOfficialCatalogKey = "PQ+krRjOgT4Cxa2Sg/AP4qyA+qNzCnqRkanfs0WduNU="
)
