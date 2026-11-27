package app

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Text functions measure and cut text in UTF-16 units, change case by
// Unicode's full case mapping ("ß" upper-cases to "SS") and read patterns
// as regular expressions; as in Notion, a pattern that is not one fails
// the formula. Changing case, trimming, repeating and padding keep rich
// text's formatting and mentions; other functions read its plain text. The
// editor's evaluator does the same.

// databaseFormulaLeadingNumber is the number text begins with, as
// JavaScript's parseFloat reads it.
var databaseFormulaLeadingNumber = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?`)

// toNumber reads a value as Notion's toNumber does (seen 2026-10-10): a date
// as its instant's milliseconds, true and false as 1 and 0, and text as the
// number it spells or begins with — "1e3" is 1000, "0x10" 16, "12abc" 12 and
// "1,234" 1 — and blank text, other text and an empty list as empty.
// Arithmetic still counts an empty value as 0. The editor's evaluator does
// the same.
func (environment *databaseFormulaEnvironment) toNumber(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case float64, int, bool:
		return databaseFormulaNumber(typed)
	case databaseFormulaDate:
		if moment, ok := environment.momentOf(typed); ok {
			return float64(moment.at.UnixMilli())
		}
		return nil
	case []any:
		if len(typed) == 0 {
			return nil
		}
	}
	text := databaseFormulaTrim(databaseFormulaText(value))
	if text == "" {
		return nil
	}
	if whole := databaseFormulaParseNumber(text); !math.IsNaN(whole) && !math.IsInf(whole, 0) {
		return whole
	}
	if leading, err := strconv.ParseFloat(databaseFormulaLeadingNumber.FindString(text), 64); err == nil && !math.IsInf(leading, 0) {
		return leading
	}
	return nil
}

func (environment *databaseFormulaEnvironment) textFunction(name string, values []any) (any, bool, error) {
	text := func(index int) string { return databaseFormulaText(databaseFormulaArgument(values, index)) }
	switch name {
	case "format":
		return environment.formatText(databaseFormulaArgument(values, 0)), true, nil
	case "formatNumber":
		// It takes numbers only: an empty one fails it, as Notion's (seen
		// 2026-11-14: empty(formatNumber(prop("n"), "number")) of an empty n
		// is no value), as the functions of numbers (databaseFormulaNumbersOnly).
		if slices.Contains(values, nil) {
			return nil, true, errDatabaseFormulaInvalid
		}
		result, err := databaseFormulaFormatNumber(values)
		return result, true, err
	case "toNumber":
		// Of a list, an empty one too, it fails the formula, as Notion's
		// (seen 2026-11-14: toNumber([1, 2]) and toNumber([]) are no value).
		if _, list := databaseFormulaArgument(values, 0).([]any); list {
			return nil, true, errDatabaseFormulaInvalid
		}
		if databaseFormulaArgument(values, 0) == nil {
			return nil, true, nil
		}
		return environment.toNumber(databaseFormulaArgument(values, 0)), true, nil
	case "lower", "upper":
		caser := cases.Lower(language.Und)
		if name == "upper" {
			caser = cases.Upper(language.Und)
		}
		if rich, ok := databaseFormulaArgument(values, 0).(databaseFormulaRichText); ok {
			return rich.mapText(caser.String), true, nil
		}
		return caser.String(text(0)), true, nil
	case "trim":
		if rich, ok := databaseFormulaArgument(values, 0).(databaseFormulaRichText); ok {
			return rich.trim(), true, nil
		}
		return databaseFormulaTrim(text(0)), true, nil
	case "startsWith":
		return strings.HasPrefix(text(0), text(1)), true, nil
	case "endsWith":
		return strings.HasSuffix(text(0), text(1)), true, nil
	case "contains":
		return strings.Contains(text(0), text(1)), true, nil
	case "substring":
		return databaseFormulaSubstring(text(0), values), true, nil
	case "repeat":
		count := databaseFormulaRepeatCount(databaseFormulaFiniteNumber(databaseFormulaArgument(values, 1)))
		if rich, ok := databaseFormulaArgument(values, 0).(databaseFormulaRichText); ok {
			value, err := rich.repeat(count)
			return value, true, err
		}
		// Measured before it is written: past a value's text the formula fails.
		if databaseFormulaTextLength(text(0))*count > maxDatabaseFormulaValueText {
			return nil, true, errDatabaseFormulaInvalid
		}
		return strings.Repeat(text(0), count), true, nil
	case "padStart", "padEnd":
		return databaseFormulaPadText(databaseFormulaArgument(values, 0), values, name == "padStart")
	case "replace", "replaceAll":
		value, err := databaseFormulaReplace(text(0), text(1), text(2), name == "replaceAll")
		return value, true, err
	case "test":
		matched, err := databaseFormulaTest(text(0), text(1))
		return matched, true, err
	case "match":
		value, err := databaseFormulaMatch(text(0), text(1))
		return value, true, err
	case "split":
		return databaseFormulaSplit(text(0), text(1)), true, nil
	case "length":
		if list, ok := databaseFormulaArgument(values, 0).([]any); ok {
			return float64(len(list)), true, nil
		}
		if rich, ok := databaseFormulaArgument(values, 0).(databaseFormulaRichText); ok {
			return float64(rich.length()), true, nil
		}
		return float64(databaseFormulaCharacterCount(text(0))), true, nil
	case "link":
		return environment.link(databaseFormulaArgument(values, 0), databaseFormulaArgument(values, 1)), true, nil
	case "style":
		return environment.style(values), true, nil
	case "unstyle":
		return environment.unstyle(values), true, nil
	case "name":
		if reference, ok := databaseFormulaArgument(values, 0).(databaseFormulaReference); ok {
			return reference.text(), true, nil
		}
		return text(0), true, nil
	case "email":
		if reference, ok := databaseFormulaArgument(values, 0).(databaseFormulaReference); ok {
			return reference.Email, true, nil
		}
		return "", true, nil
	case "equal":
		return databaseFormulaSame(databaseFormulaArgument(values, 0), databaseFormulaArgument(values, 1)), true, nil
	case "unequal":
		return !databaseFormulaSame(databaseFormulaArgument(values, 0), databaseFormulaArgument(values, 1)), true, nil
	case "empty":
		// With nothing to test it comes to nothing, as Notion's (seen 2026-11-04);
		// false is empty, as 0 and "" are (seen 2026-11-15 through its API and
		// on screen: empty(false), empty(1 == 2) and an unchecked checkbox's
		// empty(prop("c")) are true, empty([false]) false), as the editor's
		// (formulaFunctions.ts).
		if len(values) == 0 {
			return nil, true, nil
		}
		switch typed := values[0].(type) {
		case nil:
			return true, true, nil
		case bool:
			return !typed, true, nil
		case []any:
			return len(typed) == 0, true, nil
		case string:
			return typed == "", true, nil
		case float64:
			return typed == 0, true, nil
		}
		return false, true, nil
	case "not":
		return !databaseFormulaTruthy(databaseFormulaArgument(values, 0)), true, nil
	}
	return nil, false, nil
}

func databaseFormulaIndex(value any) int {
	return int(math.Max(0, math.Min(math.MaxInt32, math.Trunc(databaseFormulaFiniteNumber(value)))))
}

func databaseFormulaSubstring(text string, values []any) string {
	units := databaseFormulaUnits(text)
	from := min(databaseFormulaIndex(databaseFormulaArgument(values, 1)), len(units))
	end := databaseFormulaArgument(values, 2)
	if end == nil || end == "" {
		return databaseFormulaFromUnits(units[from:])
	}
	to := min(max(from, databaseFormulaIndex(end)), len(units))
	return databaseFormulaFromUnits(units[from:to])
}

// databaseFormulaPadText pads a value's text; rich text is padded to the
// length of the text it shows and keeps its spans.
func databaseFormulaPadText(value any, values []any, start bool) (any, bool, error) {
	rich, isRich := value.(databaseFormulaRichText)
	text := databaseFormulaText(value)
	if isRich {
		text = rich.shown()
	}
	target := min(math.Trunc(databaseFormulaFiniteNumber(databaseFormulaArgument(values, 1))), maxDatabaseFormulaPadLength)
	fill := " "
	if len(values) > 2 {
		fill = databaseFormulaText(values[2])
	}
	missing := int(target) - databaseFormulaCharacterCount(text)
	if missing <= 0 || fill == "" {
		if isRich {
			return rich, true, nil
		}
		return text, true, nil
	}
	padding, err := databaseFormulaPadding(missing, fill)
	if err != nil {
		return nil, true, err
	}
	if isRich {
		spans := append([]graphTextSpan(nil), rich...)
		if start {
			spans = append([]graphTextSpan{{Text: padding}}, spans...)
		} else {
			spans = append(spans, graphTextSpan{Text: padding})
		}
		return databaseFormulaRich(spans), true, nil
	}
	if start {
		return padding + text, true, nil
	}
	return text + padding, true, nil
}

func databaseFormulaSplit(text, separator string) []any {
	result := []any{}
	if separator == "" {
		for _, unit := range databaseFormulaUnits(text) {
			result = append(result, databaseFormulaFromUnits([]uint16{unit}))
		}
		return result
	}
	for _, part := range strings.Split(text, separator) {
		result = append(result, part)
	}
	return result
}

// databaseFormulaRepeatCount is how many copies repeat() writes, as Notion
// counts them (seen 2026-10-13): a fraction counts up — 2.4 copies are 3 —
// nothing below one, and at most 10,000; the text is never cut.
func databaseFormulaRepeatCount(number float64) int {
	if math.IsNaN(number) || number <= 0 {
		return 0
	}
	return int(math.Min(maxDatabaseFormulaRepeat, math.Ceil(number)))
}
