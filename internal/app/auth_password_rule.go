package app

import (
	"unicode"
	"unicode/utf8"
)

// authPasswordLongEnough is how many characters a password may have with
// letters alone; one with letters and digits needs minimumAuthPasswordLength.
const authPasswordLongEnough = 15

// authPasswordAcceptable is the one rule every new password meets, Notion's
// (seen 2026-10-27): 15 characters, or 8 with both letters and digits. A password set before keeps
// signing in.
func authPasswordAcceptable(password string) bool {
	count := utf8.RuneCountInString(password)
	if count >= authPasswordLongEnough {
		return true
	}
	if count < minimumAuthPasswordLength {
		return false
	}
	letter, digit := false, false
	for _, r := range password {
		if unicode.IsLetter(r) {
			letter = true
		} else if unicode.IsDigit(r) {
			digit = true
		}
	}
	return letter && digit
}
