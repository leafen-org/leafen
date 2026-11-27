package app

import "unicode"

// readDatabaseFormulaTokens reads a formula into tokens. A point with no
// digit after it ("1.") reads as a point waiting for a property or function,
// not as part of the number (databaseFormulaNumberEnd).
func readDatabaseFormulaTokens(runes []rune) []databaseFormulaToken {
	tokens, _, _ := scanDatabaseFormulaTokens(runes)
	return tokens
}

// scanDatabaseFormulaTokens reads a formula into tokens and names the first
// character no token starts with, which keeps the formula from being read as
// Notion's reading refuses it ("Invalid character “#”.", seen 2026-11-12
// through its API and 2026-11-27 in its editor) — the web editor names each
// the same (formulaSyntax.ts formulaInvalidCharacters).
func scanDatabaseFormulaTokens(runes []rune) ([]databaseFormulaToken, rune, bool) {
	tokens := []databaseFormulaToken{}
	invalid, found := rune(0), false
	// Whether a space or a comment came since the last token, for the token
	// the last turn read (databaseFormulaToken.spaced).
	// The last turn starts at from, where a token it read is written.
	gap, marked, from, index := false, 0, 0, 0
	for index < len(runes) {
		if marked < len(tokens) {
			tokens[marked].spaced, tokens[marked].start, tokens[marked].end = gap, from, index
			marked, gap = len(tokens), false
		}
		from = index
		current := runes[index]
		switch {
		case unicode.IsSpace(current) || current == '\uFEFF':
			index++
			gap = true
		case databaseFormulaCommentEnd(runes, index) >= 0:
			index = databaseFormulaCommentEnd(runes, index)
			gap = true
		// Only a double quote starts text, as Notion's editor reads a formula:
		// a single one is a character no token starts with (formulaSyntax.ts).
		case current == '"':
			quote := current
			value := []rune{}
			index++
			for index < len(runes) && runes[index] != quote {
				if runes[index] == '\\' && index+1 < len(runes) {
					value = append(value, databaseFormulaStringEscape(runes[index+1], quote)...)
					index += 2
					continue
				}
				value = append(value, runes[index])
				index++
			}
			index++
			tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenString, value: string(value)})
		case databaseFormulaASCIIDigit(current):
			start := index
			index = databaseFormulaNumberEnd(runes, index, false)
			tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenNumber, value: string(runes[start:index])})
		case databaseFormulaNameStart(current):
			start := index
			index++
			for index < len(runes) && (databaseFormulaNameStart(runes[index]) || databaseFormulaASCIIDigit(runes[index])) {
				index++
			}
			tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenIdentifier, value: string(runes[start:index])})
		default:
			if index+1 < len(runes) {
				switch two := string(runes[index : index+2]); two {
				case ">=", "<=", "==", "!=", "&&", "||":
					tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenOperator, value: two})
					index += 2
					continue
				}
			}
			index++
			switch current {
			case '+', '-', '*', '/', '%', '^', '>', '<', '!':
				tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenOperator, value: string(current)})
			case '(', ')':
				tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenParen, value: string(current)})
			case '[', ']':
				tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenBracket, value: string(current)})
			case '.':
				tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenDot, value: "."})
			case ',':
				tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenComma, value: ","})
			case '?':
				tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenQuestion, value: "?"})
			case ':':
				tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenColon, value: ":"})
			default:
				if !found {
					invalid, found = current, true
				}
			}
		}
	}
	if marked < len(tokens) {
		tokens[marked].spaced, tokens[marked].start, tokens[marked].end = gap, from, min(index, len(runes))
	}
	return tokens, invalid, found
}

// tokenizeDatabaseFormulaChecked is a formula's tokens, or the error of the
// first character no token starts with.
func tokenizeDatabaseFormulaChecked(input string) ([]databaseFormulaToken, error) {
	tokens, invalid, found := scanDatabaseFormulaTokens([]rune(input))
	if found {
		return nil, databaseFormulaSyntaxError{notionFormulaInvalidCharacter(invalid)}
	}
	return tokens, nil
}

// databaseFormulaNameStart is whether a name may start with value: any
// letter or "_", digits 0-9 following, as Notion's parser reads a name (seen
// 2026-11-12 through its API, and 2026-11-27 in its editor: a Korean name
// typed bare is a name not defined). The web editor's tokenizer reads names
// the same (FORMULA_NAME_START).
func databaseFormulaNameStart(value rune) bool { return value == '_' || unicode.IsLetter(value) }

// databaseFormulaIdentifierStart is the narrower a-z, A-Z and "_" an
// automation variable's name keeps to as it is bound
// (automationVariableFormulaName), which saved formulas hold.
func databaseFormulaIdentifierStart(value rune) bool {
	return value == '_' || (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z')
}
