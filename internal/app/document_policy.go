package app

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

// Leafen's own pages carry a Content-Security-Policy as one more layer
// against injected script (docs/app-platform.md 4, stage supplement ③):
// apps' names, descriptions and notices reach these pages, so scripts load
// only from Leafen itself (the page's own first script by its hash, and, on
// a published site, the analytics its owner chose, by nonce), plugins and
// <base> are off, and frames come only from Leafen, the web and the
// browser's own blobs. Connections, images and styles stay open: pages
// reach other Leafen servers and the web's media.

// googleAnalyticsScriptHosts are the hosts a published site's Google
// Analytics loads its scripts from.
var googleAnalyticsScriptHosts = []string{"https://www.googletagmanager.com", "https://www.google-analytics.com"}

func leafenDocumentPolicy(nonce string, scriptHosts ...string) string {
	scripts := append([]string{"'self'"}, scriptHosts...)
	// The page's own first script, by its hash (document_page_scripts.go).
	scripts = append(scripts, appPageScriptSources()...)
	if nonce != "" {
		scripts = append(scripts, "'nonce-"+nonce+"'")
	}
	return strings.Join([]string{
		"script-src " + strings.Join(scripts, " "),
		"object-src 'none'",
		"base-uri 'none'",
		"frame-src 'self' https: http: blob:",
	}, "; ")
}

func setLeafenDocumentPolicy(header http.Header, nonce string, scriptHosts ...string) {
	header.Set("Content-Security-Policy", leafenDocumentPolicy(nonce, scriptHosts...))
}

func newDocumentNonce() string {
	random := make([]byte, 18)
	_, _ = rand.Read(random)
	return base64.RawStdEncoding.EncodeToString(random)
}
