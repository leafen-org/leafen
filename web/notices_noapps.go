//go:build leafen_noapps

package web

import "embed"

// The notices of the edition without the app platform: the Go modules that
// build links and only the built-in app libraries it still carries
// (scripts/generate-third-party-notices.mjs).
//
//go:embed notices/THIRD-PARTY-NOTICES-noapps.txt
var thirdPartyNotices embed.FS

const thirdPartyNoticesFile = "notices/THIRD-PARTY-NOTICES-noapps.txt"
