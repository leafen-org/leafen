package app

import (
	"strings"
	"unicode"
)

// normalizeEmailIdentity owns the canonical account-principal grammar used by
// every native authentication and administrator admission path. It deliberately
// stays pure so malformed input is rejected before storage, provider or mail I/O.
func normalizeEmailIdentity(value string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" || len(email) > maxAuthEmailLength {
		return "", false
	}

	at := -1
	for index, character := range email {
		if unicode.IsSpace(character) {
			return "", false
		}
		if character == '@' {
			if at >= 0 {
				return "", false
			}
			at = index
		}
	}
	if at <= 0 || at >= len(email)-1 {
		return "", false
	}
	domain := email[at+1:]
	dot := strings.IndexByte(domain, '.')
	if dot <= 0 || dot >= len(domain)-1 {
		return "", false
	}
	return email, true
}
