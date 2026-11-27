package app

import (
	"net/http"
	"net/url"
	"strings"
)

// isManagedFileMetadataLocator is the single policy owner for product metadata
// that may describe an icon/avatar but must never become a second file owner.
func isManagedFileMetadataLocator(raw string, request *http.Request) bool {
	if raw == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(raw), "workspaces/") {
		return true
	}
	candidate := raw
	if strings.HasPrefix(raw, "//") {
		candidate = "https:" + raw
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return false
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return false
	}
	path = "/" + strings.TrimLeft(strings.ToLower(path), "/")
	managedPath := strings.HasPrefix(path, "/api/storage/") ||
		strings.HasPrefix(path, "/api/leafen/v1/files/") ||
		strings.HasPrefix(path, "/api/leafen/v1/file-capabilities/")
	if !managedPath {
		return false
	}
	if parsed.Host == "" || strings.HasPrefix(raw, "//") {
		return true
	}
	hostname := strings.Trim(parsed.Hostname(), "[]")
	return hostname == "localhost" || hostname == "::1" || strings.HasPrefix(hostname, "127.") ||
		(request != nil && strings.EqualFold(parsed.Host, request.Host))
}
