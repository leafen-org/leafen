//go:build !leafen_noapps

package web

import "embed"

// The notices of the edition with the app platform: its runtime's modules
// and every built-in app's libraries (scripts/generate-third-party-notices.mjs).
//
//go:embed notices/THIRD-PARTY-NOTICES.txt
var thirdPartyNotices embed.FS

const thirdPartyNoticesFile = "notices/THIRD-PARTY-NOTICES.txt"
