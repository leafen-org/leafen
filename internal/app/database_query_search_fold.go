package app

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// foldDatabaseViewSearchText is text as a database view's search compares
// it, as Notion's view search (seen 2026-11-16 in two tabs) and the screen's
// (foldViewSearchText in web/src/lib/viewSearchText.ts): decomposed, lower
// case and without the combining diacritical marks U+0300–U+036F, either
// side — "cafe" finds "café", "nang" "Nẵng", "иод" "йод". A letter that
// doesn't decompose keeps its form ("da" doesn't find "Đà", nor "strasse"
// "Straße"), as do marks outside that block and compatibility forms, and a
// Hangul syllable finds the syllables it begins ("해" finds "행"). Search
// across the workspace folds otherwise (foldSearchText).
func foldDatabaseViewSearchText(value string) string {
	lowered := strings.ToLower(norm.NFD.String(value))
	return strings.Map(func(r rune) rune {
		if r >= 0x0300 && r <= 0x036F {
			return -1
		}
		return r
	}, lowered)
}
