package commandline

import (
	"strings"
	"unicode"
)

// powerShellSingleQuotes are the characters PowerShell reads as a single
// quote: the ASCII one and the typographic ‘ ’ ‚ ‛, which end a
// single-quoted string as it does.
const powerShellSingleQuotes = "'‘’‚‛"

// PowerShellQuoted is value as PowerShell's single-quoted string, which
// takes it as it is, in Windows PowerShell 5.1 and PowerShell 7 alike: each
// character PowerShell reads as a single quote is doubled, as PowerShell's
// own escaping does, so a name or folder with ’ in it ("June’s
// workspace") stays inside the string instead of ending it and leaving the
// rest of the line unparsed (2026-11-25).
func PowerShellQuoted(value string) string {
	var quoted strings.Builder
	quoted.WriteByte('\'')
	for _, char := range value {
		if strings.ContainsRune(powerShellSingleQuotes, char) {
			quoted.WriteRune(char)
		}
		quoted.WriteRune(char)
	}
	quoted.WriteByte('\'')
	return quoted.String()
}

// PowerShellPlainPath says whether a Windows path reads as itself typed bare
// in PowerShell and in cmd: letters, digits and \ : . _ - alone. Anything
// else — a space, a quote of either kind, $ ` { } @ # , ; & ( ) — PowerShell
// reads as its own syntax, and the path goes quoted after "&".
func PowerShellPlainPath(path string) bool {
	for _, char := range path {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && !strings.ContainsRune(`\:._-`, char) {
			return false
		}
	}
	return path != ""
}
