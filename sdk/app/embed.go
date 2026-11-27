// Package appsdk carries the app SDK files `leafen app init` writes into a
// new app: the screen module, a copy of the Go worker helper and the notes
// for agents (Apache-2.0, see LICENSE).
package appsdk

import "embed"

//go:embed LICENSE AGENTS.md dist/index.js dist/go-helper.json
var Files embed.FS
