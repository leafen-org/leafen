package app

import "strings"

// Notion's formulas count text by characters as lodash does: a character
// is a character of the Basic Multilingual Plane with one combining mark, an
// astral character, or a pair of regional indicators (a flag), then an
// optional variation selector and one more combining mark or skin tone, and
// any characters a zero-width joiner joins to it, each with its own; a skin
// tone before another stands alone. length(), padStart() and padEnd() count
// so, and other text functions count UTF-16 units. The editor counts the
// same way (formulaCharacters.ts).

func databaseFormulaCombining(value rune) bool {
	return value >= 0x0300 && value <= 0x036F || value >= 0xFE20 && value <= 0xFE2F || value >= 0x20D0 && value <= 0x20FF
}

func databaseFormulaSkinTone(value rune) bool { return value >= 0x1F3FB && value <= 0x1F3FF }

func databaseFormulaRegional(value rune) bool { return value >= 0x1F1E6 && value <= 0x1F1FF }

// databaseFormulaCharacters splits text into the characters formulas count.
func databaseFormulaCharacters(text string) []string {
	characters := []string{}
	runes := []rune(text)
	databaseFormulaEachCharacter(runes, func(start, end int) {
		characters = append(characters, string(runes[start:end]))
	})
	return characters
}

// databaseFormulaCharacterCount counts the characters formulas count without
// holding each (a value's text may be 10,000,000 units long).
func databaseFormulaCharacterCount(text string) int {
	count := 0
	databaseFormulaEachCharacter([]rune(text), func(int, int) { count++ })
	return count
}

// databaseFormulaEachCharacter gives where each character formulas count
// starts and ends among the runes.
func databaseFormulaEachCharacter(runes []rune, each func(start, end int)) {
	// base takes a character's base at index: a flag, or one character.
	base := func(index int) int {
		if databaseFormulaRegional(runes[index]) && index+1 < len(runes) && databaseFormulaRegional(runes[index+1]) {
			return index + 2
		}
		return index + 1
	}
	// modifiers takes an optional variation selector and one combining mark
	// or skin tone at index.
	modifiers := func(index int) int {
		if index < len(runes) && (runes[index] == 0xFE0E || runes[index] == 0xFE0F) {
			index++
		}
		if index < len(runes) && (databaseFormulaCombining(runes[index]) || databaseFormulaSkinTone(runes[index])) {
			index++
		}
		return index
	}
	for start := 0; start < len(runes); {
		end := start + 1
		if !databaseFormulaSkinTone(runes[start]) || end >= len(runes) || !databaseFormulaSkinTone(runes[end]) {
			end = base(start)
			if runes[start] <= 0xFFFF && end < len(runes) && databaseFormulaCombining(runes[end]) {
				end++
			}
			end = modifiers(end)
			for end+1 < len(runes) && runes[end] == 0x200D {
				end = modifiers(base(end + 1))
			}
		}
		each(start, end)
		start = end
	}
}

// databaseFormulaPadding is what padStart() and padEnd() add to reach a
// count of characters: the fill repeated and cut to whole characters of the
// repeated text, as Notion pads.
func databaseFormulaPadding(missing int, fill string) (string, error) {
	count := databaseFormulaCharacterCount(fill)
	copies := (missing + count - 1) / count
	// All but the last copy are padding, so a longer repeat is too long text.
	if databaseFormulaTextLength(fill)*(copies-1) > maxDatabaseFormulaValueText {
		return "", errDatabaseFormulaInvalid
	}
	characters := databaseFormulaCharacters(strings.Repeat(fill, copies))
	return strings.Join(characters[:min(missing, len(characters))], ""), nil
}
