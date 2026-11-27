package app

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"regexp"
	"sync"
	"time"

	"leafen.org/leafen/web"
)

// The app's page runs one script of its own before any file comes: the
// person's light or dark look, and the loading screen's word when the app's
// code doesn't come (web/public/theme-init.js, put in the page by the build,
// web/scripts/page-own-parts.mjs). Asked for as a file, it failed with the
// rest when the network went right after the page, and the screen kept the
// mark alone with no word (2026-11-25). The policy lets it run by its hash,
// read from the page this server serves, so nothing else written into a
// page runs.

var pageOwnScript = regexp.MustCompile(`<script>([\s\S]*?)</script>`)

type pageScriptHashes struct {
	size     int64
	modified time.Time
	sources  []string
}

var (
	pageScriptHashesMu   sync.Mutex
	pageScriptHashesKept *pageScriptHashes
)

// appPageScriptSources are the policy's sources for the page's own scripts,
// 'sha256-…' each; read again when a development build's page changes.
func appPageScriptSources() []string {
	info, err := fs.Stat(web.Dist(), "index.html")
	if err != nil {
		return nil
	}
	pageScriptHashesMu.Lock()
	defer pageScriptHashesMu.Unlock()
	if kept := pageScriptHashesKept; kept != nil && kept.size == info.Size() && kept.modified.Equal(info.ModTime()) {
		return kept.sources
	}
	page, err := fs.ReadFile(web.Dist(), "index.html")
	if err != nil {
		return nil
	}
	sources := []string{}
	for _, script := range pageOwnScript.FindAllSubmatch(page, -1) {
		sum := sha256.Sum256(script[1])
		sources = append(sources, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	pageScriptHashesKept = &pageScriptHashes{size: info.Size(), modified: info.ModTime(), sources: sources}
	return sources
}
