package app

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
)

// notionFormulaReading is a formula a request writes as Notion's parser
// reads it (seen 2026-11-12, API 2026-03-11): as a formula of today first
// (strict), and as a formula of Notion's first kind (legacy), which Notion
// takes where today's reading doesn't read or doesn't check, keeping it as
// today's (notionFormulaKept, notion_formula_dialect.go). fault is what
// today's reading says when it doesn't read; a formula that reads neither
// way is refused with it as `Parse error in formula "<name>": <fault>`.
// Notion reads the whole formula into tokens first, so a character it has
// no token for, or a string left open, is the fault wherever it stands; then
// its grammar's. A formula quoted in single quotes Notion reads when it
// reads as a whole, its fault otherwise the one its first reading met
// ("'abc' 1": the quote). Each block_property reference stands where a
// property would.
type notionFormulaReading struct {
	strict, legacy             *databaseFormulaNode
	strictTokens, legacyTokens []databaseFormulaToken
	fault                      string
}

func notionFormulaRead(expression string) notionFormulaReading {
	reading := notionFormulaReading{}
	if expression == "" {
		return reading
	}
	reading.strict, reading.strictTokens, reading.fault = notionFormulaReadAs(expression, false)
	reading.legacy, reading.legacyTokens, _ = notionFormulaReadAs(expression, true)
	return reading
}

// reads tells whether the formula reads either way.
func (reading notionFormulaReading) reads() bool {
	return reading.strict != nil || reading.legacy != nil
}

func notionFormulaReadAs(expression string, legacy bool) (*databaseFormulaNode, []databaseFormulaToken, string) {
	root, tokens, fault := notionFormulaReadWith(expression, false, legacy)
	if fault != "" && strings.ContainsRune(expression, '\'') {
		if quoted, quotedTokens, quotedFault := notionFormulaReadWith(expression, true, legacy); quotedFault == "" {
			return quoted, quotedTokens, ""
		}
	}
	return root, tokens, fault
}

func notionFormulaReadWith(expression string, singleQuotes, legacy bool) (*databaseFormulaNode, []databaseFormulaToken, string) {
	if len([]rune(expression)) > maxDatabaseFormulaExpressionRunes {
		return nil, nil, errDatabaseFormulaLong.description
	}
	tokens, fault := notionFormulaTokens(expression, singleQuotes, legacy)
	if fault != "" {
		return nil, nil, fault
	}
	if len(tokens) == 0 {
		return nil, nil, errDatabaseFormulaExpression.description
	}
	root, err := parseNotionFormulaTokens(tokens, legacy)
	if err != nil {
		return nil, nil, notionFormulaFaultDescription(err)
	}
	return root, tokens, ""
}

// notionFormulaParseFault is what Notion's parser says of a formula a
// request writes that reads neither way, or "".
func notionFormulaParseFault(expression string) string {
	if reading := notionFormulaRead(expression); expression != "" && !reading.reads() {
		return reading.fault
	}
	return ""
}

// notionFormulaParses reports whether a formula reads as a formula of today
// that Leafen keeps as written.
func notionFormulaParses(expression string) bool {
	return expression == "" || notionFormulaRead(expression).strict != nil && notionFormulaLeafenFault(expression) == ""
}

// notionFormulaLeafenFault is Leafen's own fault for a formula it would keep
// that Notion's reading reads but Leafen's doesn't — a name in letters
// Leafen's formulas don't have — or "".
func notionFormulaLeafenFault(written string) string {
	masked, ok := notionFormulaMaskedReferences(written)
	if !ok {
		return notionFormulaInvalidCharacter('{')
	}
	if _, err := parseDatabaseFormula(masked); err != nil {
		return notionFormulaFaultDescription(err)
	}
	return ""
}

func notionFormulaFaultDescription(err error) string {
	var syntax databaseFormulaSyntaxError
	if errors.As(err, &syntax) {
		return syntax.description
	}
	return errDatabaseFormulaExpression.description
}

func notionFormulaInvalidCharacter(character rune) string {
	return "Invalid character “" + string(character) + "”."
}

// notionFormulaMaskedReferences puts prop(0) for each block_property
// reference, which then reads as a property would.
func notionFormulaMaskedReferences(expression string) (string, bool) {
	masked := expression
	for {
		start := strings.Index(masked, notionFormulaBlockPropertyPrefix)
		if start < 0 {
			return masked, true
		}
		end := strings.Index(masked[start:], "}}")
		if end < 0 {
			return "", false
		}
		masked = masked[:start] + "prop(0)" + masked[start+end+len("}}"):]
	}
}

// notionFormulaReference is the length of the block_property reference at
// index, {{notion:block_property:<property>:<data source>:<workspace>}}, or
// 0 when none is written whole there.
func notionFormulaReference(runes []rune, index int) int {
	rest := string(runes[index:])
	if !strings.HasPrefix(rest, notionFormulaBlockPropertyPrefix) {
		return 0
	}
	end := strings.Index(rest, "}}")
	if end < 0 {
		return 0
	}
	for _, part := range strings.Split(rest[len(notionFormulaBlockPropertyPrefix):end], ":") {
		if part == "" {
			return 0
		}
	}
	if strings.Count(rest[len(notionFormulaBlockPropertyPrefix):end], ":") != 2 {
		return 0
	}
	return len([]rune(rest[:end+len("}}")]))
}

// notionFormulaTokens reads a formula into tokens as Notion's parser does
// (seen 2026-11-12): a name of any letters, "_" and digits after the first;
// a number's leading point only where no value ends before it ("1 .5" is a
// member of 1), and in the legacy reading a number in hexadecimal, binary or
// octal ("0x1F", "0b1", "0o7"); strings in double quotes, in single quotes
// only when asked, a backslash ending the formula inside one being the
// fault; a block_property reference as one name; comments left out
// (databaseFormulaCommentEnd); and the fault of the first character it has
// no token for.
func notionFormulaTokens(expression string, singleQuotes, legacy bool) ([]databaseFormulaToken, string) {
	runes := []rune(expression)
	tokens := []databaseFormulaToken{}
	valueEnds := func() bool {
		if len(tokens) == 0 {
			return false
		}
		last := tokens[len(tokens)-1]
		switch last.kind {
		case databaseFormulaTokenNumber, databaseFormulaTokenString, databaseFormulaTokenIdentifier:
			return true
		case databaseFormulaTokenParen, databaseFormulaTokenBracket:
			return last.value == ")" || last.value == "]"
		}
		return false
	}
	for index := 0; index < len(runes); {
		current := runes[index]
		switch {
		case unicode.IsSpace(current) || current == '\uFEFF':
			index++
		case databaseFormulaCommentEnd(runes, index) >= 0:
			index = databaseFormulaCommentEnd(runes, index)
		case current == '"' || current == '\'' && singleQuotes:
			value := []rune{}
			index++
			for {
				if index >= len(runes) {
					return nil, "Unclosed string literal."
				}
				if runes[index] == current {
					index++
					break
				}
				if runes[index] == '\\' {
					if index+1 >= len(runes) {
						return nil, notionFormulaInvalidCharacter('\\')
					}
					value = append(value, databaseFormulaStringEscape(runes[index+1], current)...)
					index += 2
					continue
				}
				value = append(value, runes[index])
				index++
			}
			tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenString, value: string(value)})
		case current == '{':
			length := notionFormulaReference(runes, index)
			if length == 0 {
				return nil, notionFormulaInvalidCharacter(current)
			}
			tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenIdentifier, value: string(runes[index : index+length])})
			index += length
		case legacy && current == '0' && index+2 < len(runes) && notionFormulaRadix(runes[index+1], runes[index+2]) > 0:
			radix := notionFormulaRadix(runes[index+1], runes[index+2])
			start := index + 2
			for index = start; index < len(runes) && notionFormulaRadix(runes[start-1], runes[index]) > 0; index++ {
			}
			value, err := strconv.ParseUint(string(runes[start:index]), radix, 64)
			if err != nil {
				return nil, errDatabaseFormulaExpression.description
			}
			tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenNumber, value: strconv.FormatUint(value, 10)})
		case databaseFormulaASCIIDigit(current) ||
			legacy && current == '.' && !valueEnds() && index+1 < len(runes) && databaseFormulaASCIIDigit(runes[index+1]):
			start := index
			index = databaseFormulaNumberEnd(runes, index, legacy)
			tokens = append(tokens, databaseFormulaToken{kind: databaseFormulaTokenNumber, value: string(runes[start:index])})
		case unicode.IsLetter(current) || current == '_':
			start := index
			for index++; index < len(runes) && (unicode.IsLetter(runes[index]) || runes[index] == '_' || databaseFormulaASCIIDigit(runes[index])); index++ {
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
			kind, known := notionFormulaPunctuation[current]
			if !known {
				return nil, notionFormulaInvalidCharacter(current)
			}
			tokens = append(tokens, databaseFormulaToken{kind: kind, value: string(current)})
			index++
		}
	}
	return tokens, ""
}

// notionFormulaRadix is the base a number written "0<letter><digit>…" is
// in, its letter lower case, or 0 when digit isn't one of it.
func notionFormulaRadix(letter, digit rune) int {
	switch {
	case letter == 'x' && (databaseFormulaASCIIDigit(digit) || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F'):
		return 16
	case letter == 'b' && (digit == '0' || digit == '1'):
		return 2
	case letter == 'o' && digit >= '0' && digit <= '7':
		return 8
	}
	return 0
}

var notionFormulaPunctuation = map[rune]databaseFormulaTokenKind{
	'+': databaseFormulaTokenOperator, '-': databaseFormulaTokenOperator, '*': databaseFormulaTokenOperator,
	'/': databaseFormulaTokenOperator, '%': databaseFormulaTokenOperator, '^': databaseFormulaTokenOperator,
	'>': databaseFormulaTokenOperator, '<': databaseFormulaTokenOperator, '!': databaseFormulaTokenOperator,
	'(': databaseFormulaTokenParen, ')': databaseFormulaTokenParen,
	'[': databaseFormulaTokenBracket, ']': databaseFormulaTokenBracket,
	'.': databaseFormulaTokenDot, ',': databaseFormulaTokenComma,
	'?': databaseFormulaTokenQuestion, ':': databaseFormulaTokenColon,
}
