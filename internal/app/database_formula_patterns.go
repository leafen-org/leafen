package app

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// Formula patterns are JavaScript regular expressions without flags, read
// over UTF-16 units as Notion and the editor's evaluator read them: a pattern
// is rewritten for regexp2's ECMAScript mode where the two differ
// (database_formula_pattern_source.go), and text is matched as its units, a
// surrogate unit standing in as a private-use character, because regexp2
// does not find surrogates in every character set. A match that takes
// longer than databaseFormulaPatternTimeout fails the formula.

var errDatabaseFormulaPattern = errors.New("formula pattern is not a regular expression")

const (
	databaseFormulaPatternTimeout = 100 * time.Millisecond
	// A surrogate unit u stands in as u + databaseFormulaSurrogateShift,
	// U+F0000..U+F07FF.
	databaseFormulaSurrogateShift = 0xF0000 - 0xD800
	maxDatabaseFormulaPatterns    = 256
)

var databaseFormulaPatterns = struct {
	sync.Mutex
	compiled map[string]*regexp2.Regexp
}{compiled: map[string]*regexp2.Regexp{}}

// databaseFormulaPattern compiles a JavaScript pattern, or fails when
// JavaScript would not read it as one.
func databaseFormulaPattern(pattern string) (*regexp2.Regexp, error) {
	databaseFormulaPatterns.Lock()
	expression := databaseFormulaPatterns.compiled[pattern]
	databaseFormulaPatterns.Unlock()
	if expression != nil {
		return expression, nil
	}
	if databaseFormulaUnsafePattern(pattern) {
		return nil, errDatabaseFormulaPattern
	}
	source, err := databaseFormulaPatternSource(pattern)
	if err != nil {
		return nil, err
	}
	expression, err = regexp2.Compile(source, regexp2.ECMAScript)
	if err != nil {
		return nil, errDatabaseFormulaPattern
	}
	expression.MatchTimeout = databaseFormulaPatternTimeout
	databaseFormulaPatterns.Lock()
	if len(databaseFormulaPatterns.compiled) >= maxDatabaseFormulaPatterns {
		clear(databaseFormulaPatterns.compiled)
	}
	databaseFormulaPatterns.compiled[pattern] = expression
	databaseFormulaPatterns.Unlock()
	return expression, nil
}

func databaseFormulaPatternRune(unit uint16) rune {
	if unit >= 0xD800 && unit <= 0xDFFF {
		return rune(unit) + databaseFormulaSurrogateShift
	}
	return rune(unit)
}

// databaseFormulaPatternRunes is text as a pattern matches it: its units.
func databaseFormulaPatternRunes(text string) []rune {
	units := databaseFormulaUnits(text)
	runes := make([]rune, len(units))
	for index, unit := range units {
		runes[index] = databaseFormulaPatternRune(unit)
	}
	return runes
}

// databaseFormulaPatternText is matched units as text; a lone half of a
// surrogate pair is U+FFFD.
func databaseFormulaPatternText(runes []rune) string {
	units := make([]uint16, len(runes))
	for index, value := range runes {
		if value > 0xFFFF {
			value -= databaseFormulaSurrogateShift
		}
		units[index] = uint16(value)
	}
	return databaseFormulaFromUnits(units)
}

func databaseFormulaTest(text, pattern string) (bool, error) {
	if pattern == "" {
		return false, nil
	}
	expression, err := databaseFormulaPattern(pattern)
	if err != nil {
		return false, err
	}
	matched, err := expression.MatchRunes(databaseFormulaPatternRunes(text))
	if err != nil {
		return false, errDatabaseFormulaPattern
	}
	return matched, nil
}

// databaseFormulaNextMatch finds the match at or after start; as in
// JavaScript, the search after an empty match starts one unit later.
func databaseFormulaNextMatch(expression *regexp2.Regexp, runes []rune, start int) (*regexp2.Match, int, error) {
	if start > len(runes) {
		return nil, start, nil
	}
	match, err := expression.FindRunesMatchStartingAt(runes, start)
	if err != nil {
		return nil, start, errDatabaseFormulaPattern
	}
	if match == nil {
		return nil, start, nil
	}
	return match, match.RuneIndex + max(match.RuneLength, 1), nil
}

func databaseFormulaMatch(text, pattern string) ([]any, error) {
	result := []any{}
	if pattern == "" {
		return result, nil
	}
	expression, err := databaseFormulaPattern(pattern)
	if err != nil {
		return nil, err
	}
	runes := databaseFormulaPatternRunes(text)
	for start := 0; len(result) <= maxDatabaseFormulaListItems; {
		var match *regexp2.Match
		if match, start, err = databaseFormulaNextMatch(expression, runes, start); err != nil {
			return nil, err
		} else if match == nil {
			break
		}
		result = append(result, databaseFormulaPatternText(match.Runes()))
	}
	return result, nil
}

func databaseFormulaReplace(text, pattern, replacement string, all bool) (string, error) {
	if pattern == "" {
		return text, nil
	}
	expression, err := databaseFormulaPattern(pattern)
	if err != nil {
		return "", err
	}
	runes, template := databaseFormulaPatternRunes(text), databaseFormulaPatternRunes(replacement)
	named := false
	for _, name := range expression.GetGroupNames() {
		named = named || name != ""
	}
	var result []rune
	last := 0
	for start := 0; ; {
		var match *regexp2.Match
		if match, start, err = databaseFormulaNextMatch(expression, runes, start); err != nil {
			return "", err
		} else if match == nil {
			break
		}
		result = append(result, runes[last:match.RuneIndex]...)
		result = databaseFormulaExpand(result, template, runes, match, named)
		last = match.RuneIndex + match.RuneLength
		if len(result) > maxDatabaseFormulaValueText {
			return "", errDatabaseFormulaInvalid
		}
		if !all {
			break
		}
	}
	return databaseFormulaPatternText(append(result, runes[last:]...)), nil
}

// databaseFormulaExpand writes a replacement as JavaScript does: $$, $&,
// $`, $', $1..$99 and, when the pattern names groups, $<name>.
func databaseFormulaExpand(result, template, text []rune, match *regexp2.Match, named bool) []rune {
	captured := func(group *regexp2.Group) []rune {
		if group == nil || len(group.Captures) == 0 {
			return nil
		}
		return group.Runes()
	}
	count := match.GroupCount() - 1
	for index := 0; index < len(template); index++ {
		if template[index] != '$' || index+1 >= len(template) {
			result = append(result, template[index])
			continue
		}
		switch next := template[index+1]; {
		case next == '$':
			result = append(result, '$')
			index++
		case next == '&':
			result = append(result, text[match.RuneIndex:match.RuneIndex+match.RuneLength]...)
			index++
		case next == '`':
			result = append(result, text[:match.RuneIndex]...)
			index++
		case next == '\'':
			result = append(result, text[match.RuneIndex+match.RuneLength:]...)
			index++
		case next >= '0' && next <= '9':
			digits, number := 1, int(next-'0')
			if index+2 < len(template) && template[index+2] >= '0' && template[index+2] <= '9' {
				if two := number*10 + int(template[index+2]-'0'); two >= 1 && two <= count {
					digits, number = 2, two
				}
			}
			if number < 1 || number > count {
				result = append(result, template[index:index+1+digits]...)
			} else {
				result = append(result, captured(match.GroupByNumber(number))...)
			}
			index += digits
		case next == '<' && named:
			end := index + 2
			for end < len(template) && template[end] != '>' {
				end++
			}
			if end == len(template) {
				result = append(result, '$')
				continue
			}
			result = append(result, captured(match.GroupByName(string(template[index+2:end])))...)
			index = end
		default:
			result = append(result, '$')
		}
	}
	return result
}

// databaseFormulaUnsafePattern is a pattern that repeats a group holding a
// repetition — "(a+)+", "(a+)?", "(ab+)*c", "([a-z]+)*" — which Notion
// refuses (seen 2026-11-14); an escaped sign or one in a class repeats
// nothing ("(a\+)+", "(a|b)+" are kept). The editor reads patterns the same
// way (formulaRunError.ts unsafeFormulaPattern).
func databaseFormulaUnsafePattern(pattern string) bool {
	groups := []bool{}
	inClass := false
	for index := 0; index < len(pattern); index++ {
		char := pattern[index]
		if char == '\\' {
			index++
			continue
		}
		if inClass {
			if char == ']' {
				inClass = false
			}
			continue
		}
		switch {
		case char == '[':
			inClass = true
		case char == '(':
			groups = append(groups, false)
			if index+1 < len(pattern) && pattern[index+1] == '?' {
				index++
			}
		case char == ')':
			repeated := false
			if len(groups) > 0 {
				repeated, groups = groups[len(groups)-1], groups[:len(groups)-1]
			}
			if repeated && index+1 < len(pattern) && strings.ContainsRune("+*?{", rune(pattern[index+1])) {
				return true
			}
			if repeated && len(groups) > 0 {
				groups[len(groups)-1] = true
			}
		case (char == '+' || char == '*' || char == '{') && len(groups) > 0:
			groups[len(groups)-1] = true
		}
	}
	return false
}
