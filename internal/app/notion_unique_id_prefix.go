package app

import (
	"net/url"
	"regexp"
	"strings"
)

// A unique ID's prefix is 2 to 10 characters, a letter and then letters,
// digits or hyphens, and is kept in capitals, as Notion's API and app hold
// it (seen 2026-11-11: "W", "1A", "A_B", "A B", " AB", "가나" and eleven
// characters are refused; "ww" is kept as "WW"). The app's property editor
// checks the same as its person types (IdPrefixConfig); a person's change to
// a prefix, from the app, the public API or MCP, is held to it where every
// such change meets the ID property rules (guardDatabaseUniqueIDProperties),
// and the public API and MCP keep it in capitals.
var uniqueIDPrefixPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{1,9}$`)

// uniqueIDPrefixRefusedMessage is Notion's API's words for a prefix it
// refuses, which say what to fix.
const uniqueIDPrefixRefusedMessage = "Unique ID prefix %q can't be used: " +
	"a prefix is 2 to 10 characters, a letter and then letters, digits or hyphens (no underscores or spaces)."

// uniqueIDPrefix is a prefix as it is kept, or false for one Notion refuses.
func uniqueIDPrefix(prefix string) (string, bool) {
	if !uniqueIDPrefixPattern.MatchString(prefix) {
		return "", false
	}
	return strings.ToUpper(prefix), true
}

// uniqueIDPrefixRefused is the API's answer to a prefix it refuses, as
// Notion's API refuses it, in Leafen's words.
func uniqueIDPrefixRefused(prefix string) error {
	return notionDatabaseRowPageRequestInvalid(uniqueIDPrefixRefusedMessage, prefix)
}

// notionUniqueIDConversionRefused is the API's answer to turning a property
// into an ID, or an ID into another type, which Notion's API never does (seen
// 2026-11-11: "Property ETQ: can not be converted …", its ID as it reads);
// its app dims "ID" among the types a property changes to and has no type
// change for an ID.
func notionUniqueIDConversionRefused(current storedNotionDataSourceProperty, toID bool) error {
	id := current.rowPageProperty().publicID()
	if unescaped, err := url.PathUnescape(id); err == nil {
		id = unescaped
	}
	if toID {
		return notionDatabaseRowPageRequestInvalid(
			"Property %s can't become a unique ID: remove it and add a new unique ID property instead.", id,
		)
	}
	return notionDatabaseRowPageRequestInvalid(
		"Property %s is a unique ID and keeps that type: remove it and add a new property instead.", id,
	)
}
