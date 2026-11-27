package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// thirdPartyNoticesPath is where the screens link the notices of what this
// build carries (src/lib/legalLinks.ts).
const thirdPartyNoticesPath = "THIRD-PARTY-NOTICES.txt"

// diskDist is nil in every build but a development one: a server built with
// the tag leafen_devweb (dist_devweb.go) sets it to the web build a
// developer's tree has on disk. Release builds carry no such tag, so they
// always serve the build they embed.
var diskDist fs.FS

// Dist is the web build, with the third-party notices of this edition at
// /THIRD-PARTY-NOTICES.txt: each edition embeds only its own notices
// (notices.go, notices_noapps.go), so a build without the app platform
// carries no word of the app runtime it leaves out.
func Dist() fs.FS {
	if diskDist != nil {
		return withNotices{diskDist}
	}
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return withNotices{dist}
}

type withNotices struct{ fs.FS }

func (files withNotices) Open(name string) (fs.File, error) {
	if name == thirdPartyNoticesPath {
		return thirdPartyNotices.Open(thirdPartyNoticesFile)
	}
	return files.FS.Open(name)
}
