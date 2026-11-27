package app

import "unicode"

// readDatabaseFormulaTokens reads a formula into tokens. A point with no
// digit after it ("1.") reads as a point waiting for a property or function,
// not as part of the number (databaseFormulaNumberEnd).
func readDatabaseFormulaTokens(runes []rune) []databaseFormulaToken {
	tokens := []databaseFormulaToken{}
	for index := 0; index < len(runes); {
		current := runes[index]
		switch {
		case unicode.IsSpace(current) || current == '\uFEFF':
			index++
		case databaseFormulaCommentEnd(runes, index) >= 0:
			index = databaseFormulaCommentEnd(runes, index)
		case current == '"' || current == '\'':
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
		case databaseFormulaIdentifierStart(current):
			start := index
			index++
			for index < len(runes) && (databaseFormulaIdentifierStart(runes[index]) || databaseFormulaASCIIDigit(runes[index])) {
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
			}
		}
	}
	return tokens
}
