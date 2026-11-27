package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A formula's text is written in double quotes (readDatabaseFormulaTokens),
// as Notion's editor reads it: a single quote is a character no token starts
// with there. Notion's API still takes text in single quotes and keeps it in
// double ones ('it"s' + 'y' is kept "it\"s" + "y", seen 2026-11-28), and
// Leafen read single quotes as text before, so a formula a request writes
// (notionFormulaKept) and the formulas Leafen kept so (migration 869) are
// written in double quotes, reading the same text.

// databaseFormulaDoubleQuoted is the formula with each text in single quotes
// written in double ones, the text it reads the same — an escaped single
// quote written bare, a double quote escaped, any other escape as it was —
// and whether it changed. Comments and text in double quotes stay as they
// are, and so does a single quote left open (the formula didn't read before).
func databaseFormulaDoubleQuoted(expression string) (string, bool) {
	if !strings.ContainsRune(expression, '\'') {
		return expression, false
	}
	runes := []rune(expression)
	var written strings.Builder
	changed := false
	for index := 0; index < len(runes); {
		if end := databaseFormulaCommentEnd(runes, index); end >= 0 {
			written.WriteString(string(runes[index:end]))
			index = end
			continue
		}
		current := runes[index]
		if current != '"' && current != '\'' {
			written.WriteRune(current)
			index++
			continue
		}
		end := databaseFormulaQuotedEnd(runes, index)
		if end < 0 {
			written.WriteString(string(runes[index:]))
			break
		}
		if current == '"' {
			written.WriteString(string(runes[index:end]))
			index = end
			continue
		}
		written.WriteRune('"')
		for inner := index + 1; inner < end-1; inner++ {
			switch character := runes[inner]; {
			case character == '\\':
				inner++
				if runes[inner] != '\'' {
					written.WriteRune('\\')
				}
				written.WriteRune(runes[inner])
			case character == '"':
				written.WriteString(`\"`)
			default:
				written.WriteRune(character)
			}
		}
		written.WriteRune('"')
		changed = true
		index = end
	}
	if !changed {
		return expression, false
	}
	return written.String(), true
}

// databaseFormulaQuotedEnd is where the text opened by the quote at start
// ends, after its closing quote, a backslash taking the character after it;
// -1 when it is left open.
func databaseFormulaQuotedEnd(runes []rune, start int) int {
	quote := runes[start]
	for index := start + 1; index < len(runes); index++ {
		switch runes[index] {
		case '\\':
			if index+1 >= len(runes) {
				return -1
			}
			index++
		case quote:
			return index + 1
		}
	}
	return -1
}

// databaseFormulaDoubleQuotedJSON is a stored document with its formulas in
// double quotes, or "" when it holds none in single quotes.
func databaseFormulaDoubleQuotedJSON(raw string, own bool) (string, error) {
	return databaseFormulaRewrittenJSON(raw, own, databaseFormulaDoubleQuoted)
}

// quoteDatabaseFormulaStrings is migration 869's content: the formulas
// Leafen kept with text in single quotes, written in double ones.
func quoteDatabaseFormulaStrings(app core.App) error {
	return rewriteStoredDatabaseFormulas(app, storedDatabaseFormulaRewrite{
		rewrite: databaseFormulaDoubleQuoted, needles: []string{"'"},
		why: "formulas-double-quoted", logged: "formulas written with text in double quotes",
	})
}
