package app

import (
	"net/url"
	"path"
	"strings"
)

// notionHostedFileName is the name a file Notion hosts goes by: the last
// part of its address's path, which keeps the name it was uploaded with, as
// Notion's own export names it (an uploaded cover "cover-upload.png", seen
// 2026-11-14 and in its address 2026-11-15); fallback when the address names
// none.
func notionHostedFileName(rawURL, fallback string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fallback
	}
	name := strings.TrimSpace(path.Base(parsed.Path))
	if name == "" || name == "." || name == "/" {
		return fallback
	}
	return name
}
