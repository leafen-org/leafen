package app

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A connection's icon, as Notion's "편집" on the square of an MCP token's
// and an internal connection's 정보 표시 (seen 2026-10-30 and 2026-11-09):
// an emoji or a picture's link, as a workspace's icon takes them, kept with
// the connection and shown in place of its first letter, there and in its
// list. Empty, the first letter shows.

const (
	connectionIconLinkBytes  = 2048
	connectionIconEmojiRunes = 16
)

// normalizeConnectionIcon is the icon a connection keeps for what its person
// picked: "" for none, a picture's http(s) link that names no file Leafen
// keeps (a connection owns no file), or one emoji. ok is false otherwise.
func normalizeConnectionIcon(raw string, request *http.Request) (string, bool) {
	icon := strings.TrimSpace(raw)
	if icon == "" {
		return "", true
	}
	if strings.HasPrefix(strings.ToLower(icon), "http://") || strings.HasPrefix(strings.ToLower(icon), "https://") {
		link, err := url.Parse(icon)
		if err != nil || link.Host == "" || len(icon) > connectionIconLinkBytes || isManagedFileMetadataLocator(icon, request) {
			return "", false
		}
		return icon, true
	}
	if utf8.RuneCountInString(icon) > connectionIconEmojiRunes {
		return "", false
	}
	for _, char := range icon {
		if char < 0x80 && char != '#' && char != '*' && !unicode.IsDigit(char) || unicode.IsSpace(char) {
			return "", false
		}
	}
	return icon, true
}
