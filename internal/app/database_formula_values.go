package app

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Formula values are nil, text, numbers, booleans, lists, dates and page or
// person references. Text is measured and cut in UTF-16 units and numbers
// read and write as JavaScript does, so the server and the editor agree on
// every value. A date is its ISO text: a day ("2026-01-02"), a UTC instant
// ("2026-01-02T03:04:05Z"), a wall-clock time, a time at its own offset
// ("2026-01-02T12:04:05+09:00"), or a range of them joined by "/". Zone
// names the time zone its times keep, as a date property's time zone does.

type databaseFormulaDate struct {
	Text string
	Zone string
}

type databaseFormulaReference struct {
	Kind  string
	ID    string
	Name  string
	Email string
	// DataSourceID is the database a related page is a row of, so .prop()
	// can read it.
	DataSourceID string
}

func databaseFormulaUnits(text string) []uint16 { return utf16.Encode([]rune(text)) }

func databaseFormulaFromUnits(units []uint16) string { return string(utf16.Decode(units)) }

func databaseFormulaTextLength(text string) int {
	length := 0
	for _, value := range text {
		length++
		if value > 0xFFFF {
			length++
		}
	}
	return length
}

func databaseFormulaText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case databaseFormulaDate:
		return typed.Text
	case databaseFormulaRichText:
		return typed.plain()
	case float64:
		// A number that is no number reads as no text, as Notion's.
		if math.IsNaN(typed) {
			return ""
		}
		return databaseFormulaNumberText(typed)
	case int:
		return databaseFormulaNumberText(float64(typed))
	case bool:
		return strconv.FormatBool(typed)
	case []any:
		parts := make([]string, len(typed))
		for index, item := range typed {
			parts[index] = databaseFormulaText(item)
		}
		return strings.Join(parts, ",")
	case databaseFormulaReference:
		return typed.text()
	}
	return fmt.Sprint(value)
}

// text is a reference's name; an untitled page has none, never its ID.
func (reference databaseFormulaReference) text() string {
	if reference.Name != "" || reference.Kind == "page" {
		return reference.Name
	}
	return reference.ID
}

// databaseFormulaNumber is a value as arithmetic and the number functions
// read it: a number as it is, an infinite one or one that is no number too,
// as Notion's (seen 2026-11-14: round(1e308 * 10) and abs(-1e308 * 10) are
// Infinity, round(sqrt(-1)) no number); true and false as 1 and 0; anything
// else as the number its text spells, or 0. The editor reads them the same
// way (formulaValues.ts toNumber).
func databaseFormulaNumber(value any) float64 {
	var number float64
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		number = float64(typed)
	case bool:
		if typed {
			return 1
		}
		return 0
	default:
		number = databaseFormulaParseNumber(databaseFormulaText(value))
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0
	}
	return number
}

// databaseFormulaFiniteNumber is a count or a place read as a number, one
// that is not finite as 0.
func databaseFormulaFiniteNumber(value any) float64 {
	number := databaseFormulaNumber(value)
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0
	}
	return number
}

func databaseFormulaTruthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case []any:
		return len(typed) > 0
	case databaseFormulaReference:
		return true
	}
	return databaseFormulaText(value) != ""
}

func databaseFormulaSame(left, right any) bool {
	// An empty property is the same as another, not as empty text.
	if (left == nil) != (right == nil) {
		return false
	}
	leftReference, leftOK := left.(databaseFormulaReference)
	rightReference, rightOK := right.(databaseFormulaReference)
	if leftOK && rightOK {
		return leftReference.Kind == rightReference.Kind && leftReference.ID == rightReference.ID
	}
	// Two numbers are the same as JavaScript's ===: no number is the same as
	// none, not even itself, as Notion's (seen 2026-11-14: sqrt(-1) ==
	// sqrt(-1), equal() and includes() of it false, unique() keeps both);
	// Infinity is itself.
	leftNumber, leftIsNumber := left.(float64)
	rightNumber, rightIsNumber := right.(float64)
	if leftIsNumber && rightIsNumber {
		return leftNumber == rightNumber
	}
	// A number is compared with anything else by its own text, NaN's "NaN" too.
	text := func(value any) string {
		if number, ok := value.(float64); ok {
			return databaseFormulaNumberText(number)
		}
		return databaseFormulaText(value)
	}
	return text(left) == text(right)
}

// databaseFormulaBlank is an empty property's value (prop() reads one as
// nil). Text that is empty, written "" or made, is text like any other:
// "abc" > "" and "" == "" are true, while an empty property > "" and
// == "" are false and != "" is true (Notion's, seen 2026-11-09 through its
// API under 한지테스트페이지).
func databaseFormulaBlank(value any) bool {
	return value == nil
}

// databaseFormulaKeptResult is another formula's result as prop() reads it:
// as it is, empty text it made too, as Notion's (seen 2026-11-14:
// prop("format(1 / 0)") == "" is true). The editor reads it the same way
// (formulaValues.ts FormulaPropertyResult).
type databaseFormulaKeptResult struct{ value any }

// databaseFormulaPropertyValue is what prop() reads: a property with nothing in
// it is empty whatever its type, as Notion's formulas read it, so adding to it
// counts it as 0 and most functions of it are empty too; a formula's result is
// as it is.
func databaseFormulaPropertyValue(value any) any {
	if kept, ok := value.(databaseFormulaKeptResult); ok {
		return kept.value
	}
	input := databaseFormulaInput(value)
	if input == "" {
		return nil
	}
	return input
}

func databaseFormulaIsText(value any) bool {
	switch value.(type) {
	case string, databaseFormulaDate, databaseFormulaRichText:
		return true
	}
	return false
}

// databaseFormulaFormat is the value as a cell shows it, as the editor
// writes it (formulaValues.ts formatFormulaValue).
func databaseFormulaFormat(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case databaseFormulaRichText:
		return typed.shown()
	case []any:
		// A number that is no number is left out of a list, as Notion's cell
		// leaves it (seen 2026-11-14: [1e308 * 10, sqrt(-1), 1] shows
		// "Infinity, 1").
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if number, ok := item.(float64); ok && math.IsNaN(number) {
				continue
			}
			parts = append(parts, databaseFormulaFormat(item))
		}
		return strings.Join(parts, ", ")
	case float64:
		// An infinite number shows as format() writes it, "Infinity" and
		// "-Infinity", and one that is no number as nothing, as Notion's
		// cell (seen 2026-11-14).
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return databaseFormulaText(typed)
		}
		return databaseFormulaShownNumber(typed)
	}
	return databaseFormulaText(value)
}

// databaseFormulaShownDecimals is the decimal places a number shows when
// nothing chooses them.
const databaseFormulaShownDecimals = 12

// databaseFormulaShownNumber writes a number as a cell without a number
// format shows it, as Notion's cells (seen 2026-11-14 under 한지테스트페이지)
// and the editor's (numberText.ts shownNumberText): a small number written
// with an exponent below e-20 with as many decimal places as its exponent
// says (3.33333e-7 is 0.0000003, 9.9e-11 0.00000000010); any other its own
// text with the decimals past 12 dropped and the kept ones raised by one
// in their last place when the first dropped is a digit of 5 or more, a
// carry going into the whole part (0.9999999999999999 is 1.000000000000).
// The text is read as written, so a large number's exponent counts among
// its decimals: 3.3333333333333335e+21 is 3.333333333333, and
// 1.234567891e+25, its kept "234567891e+2" raised as a number,
// 1.23456789101, as Notion's.
func databaseFormulaShownNumber(value float64) string {
	text := databaseFormulaNumberText(value)
	if _, exponent, ok := strings.Cut(text, "e-"); ok {
		if places, err := strconv.Atoi(exponent); err == nil && places < 20 {
			return databaseFormulaFixed(value, places)
		}
	}
	sign := ""
	if rest, ok := strings.CutPrefix(text, "-"); ok {
		sign, text = "-", rest
	}
	whole, decimals, _ := strings.Cut(text, ".")
	if len(decimals) > databaseFormulaShownDecimals {
		kept := decimals[:databaseFormulaShownDecimals]
		if next := decimals[databaseFormulaShownDecimals]; next >= '5' && next <= '9' {
			digits := strings.TrimLeft(kept, "0")
			kept = kept[:len(kept)-len(digits)] + databaseFormulaNumberText(databaseFormulaParseNumber(digits)+1)
			if len(kept) > databaseFormulaShownDecimals {
				whole = databaseFormulaNumberText(databaseFormulaParseNumber(whole) + databaseFormulaParseNumber(kept[:1]))
				kept = kept[1:]
			}
		}
		decimals = kept
	}
	if decimals == "" {
		return sign + whole
	}
	return sign + whole + "." + decimals
}

// databaseFormulaNumberText writes a number as JavaScript's String() does.
func databaseFormulaNumberText(value float64) string {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	case value == 0:
		return "0"
	}
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(scientific, "e")
	exponent, _ := strconv.Atoi(exponentText)
	digits := strings.Replace(mantissa, ".", "", 1)
	point := exponent + 1
	switch {
	case len(digits) <= point && point <= 21:
		return sign + digits + strings.Repeat("0", point-len(digits))
	case 0 < point && point <= 21:
		return sign + digits[:point] + "." + digits[point:]
	case -6 < point && point <= 0:
		return sign + "0." + strings.Repeat("0", -point) + digits
	}
	exponentSign := "+"
	if point-1 < 0 {
		exponentSign = "-"
	}
	result := digits[:1]
	if len(digits) > 1 {
		result += "." + digits[1:]
	}
	return sign + result + "e" + exponentSign + strconv.Itoa(databaseFormulaAbsInt(point-1))
}

func databaseFormulaAbsInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

var databaseFormulaDecimalPattern = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

// databaseFormulaParseNumber reads text as JavaScript's Number() does.
func databaseFormulaParseNumber(text string) float64 {
	text = databaseFormulaTrim(text)
	switch text {
	case "":
		return 0
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(text) > 2 && text[0] == '0' {
		base := 0
		switch text[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			if strings.Contains(text[2:], "_") {
				return math.NaN()
			}
			if parsed, err := strconv.ParseUint(text[2:], base, 64); err == nil {
				return float64(parsed)
			}
			return math.NaN()
		}
	}
	if !databaseFormulaDecimalPattern.MatchString(text) {
		return math.NaN()
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil && !math.IsInf(parsed, 0) {
		return math.NaN()
	}
	return parsed
}

// databaseFormulaTrim removes what JavaScript's trim() removes.
func databaseFormulaTrim(text string) string {
	return strings.TrimFunc(text, databaseFormulaTrimmed)
}

// databaseFormulaTrimmed is whether JavaScript's trim() removes a character.
func databaseFormulaTrimmed(value rune) bool {
	switch value {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00A0', '\u1680', '\u2028', '\u2029',
		'\u202F', '\u205F', '\u3000', '\uFEFF':
		return true
	}
	return value >= '\u2000' && value <= '\u200A'
}

// databaseFormulaBudget fails a value with more list items, deeper lists or
// more text than a formula may hold.
func databaseFormulaBudget(value any) error {
	items, text := 0, 0
	var visit func(any, int) error
	visit = func(current any, depth int) error {
		if depth > maxDatabaseFormulaListDepth {
			return errDatabaseFormulaInvalid
		}
		switch typed := current.(type) {
		case []any:
			items += len(typed)
			if items > maxDatabaseFormulaListItems {
				return errDatabaseFormulaInvalid
			}
			for _, item := range typed {
				if err := visit(item, depth+1); err != nil {
					return err
				}
			}
			return nil
		case string:
			text += databaseFormulaTextLength(typed)
		case databaseFormulaDate:
			text += databaseFormulaTextLength(typed.Text)
		case databaseFormulaRichText:
			for _, span := range typed {
				text += databaseFormulaTextLength(span.Text) + databaseFormulaTextLength(span.Link)
			}
		case databaseFormulaReference:
			text += databaseFormulaTextLength(typed.ID) + databaseFormulaTextLength(typed.Name)
		}
		if text > maxDatabaseFormulaValueText {
			return errDatabaseFormulaInvalid
		}
		return nil
	}
	return visit(value, 0)
}

// databaseFormulaOutput is a value as the database stores and serves it: a
// date is a date, in a list too, a reference names its page or person, and a
// number that is not finite, which JSON has no number for, is empty alone
// (its cell's text is databaseFormulaFormat's) and its text in a list, as
// Notion's API writes such a list (seen 2026-11-14: [1e308 * 10, sqrt(-1),
// 1] is "Infinity,,1").
func databaseFormulaOutput(value any, top bool) any {
	switch typed := value.(type) {
	case databaseFormulaRichText:
		return map[string]any{"rich_text": []graphTextSpan(typed)}
	case databaseFormulaDate:
		start, end, ranged := strings.Cut(typed.Text, "/")
		output := map[string]any{"start": start, "end": nil}
		if ranged {
			output["end"] = end
		}
		if typed.Zone != "" {
			output["timeZone"] = typed.Zone
		}
		return output
	case databaseFormulaReference:
		output := map[string]any{"kind": typed.Kind, "id": typed.ID, "name": typed.Name}
		if typed.Email != "" {
			output["email"] = typed.Email
		}
		return output
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			if top {
				return ""
			}
			return databaseFormulaText(typed)
		}
		return typed + 0
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = databaseFormulaOutput(item, false)
		}
		return items
	}
	return value
}

// databaseFormulaKeptDecimals are the decimal places a formula property's
// number keeps.
const databaseFormulaKeptDecimals = 12

// databaseFormulaKept is a formula property's result as Notion keeps it
// (seen 2026-11-14 under 한지테스트페이지): a number, and each number a list
// holds itself (not one in a list within it), rounded to 12 decimal places —
// 1/3 is 0.333333333333, 2/3 0.666666666667, 5e-13 1e-12, 4e-13 0, [1/3,
// 2/3] "0.333333333333,0.666666666667", [[1/3]] "0.3333333333333333". Its
// API, its filters, prop() of it and format(prop()) of it all read the
// rounded number; format(1/3) within one formula doesn't round. A number
// the rounding can't write back (1e297 or more) is no number: empty alone,
// an empty item in a list ([1e297, 1/3] ",0.333333333333"). Rollups keep
// their numbers.
func databaseFormulaKept(value any) any {
	switch typed := value.(type) {
	case float64:
		return databaseFormulaKeptNumber(typed)
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = item
			if number, ok := item.(float64); ok && !math.IsNaN(number) && !math.IsInf(number, 0) {
				if kept := databaseFormulaKeptNumber(number); math.IsNaN(kept) {
					items[index] = ""
				} else {
					items[index] = kept
				}
			}
		}
		return items
	}
	return value
}

// databaseFormulaKeptNumber rounds a number to the decimal places a formula
// keeps as Notion's does, by moving the decimal point of the number's
// shortest decimal text — as lodash's round() does, which matches every
// number Notion's API served (2026-11-14: 999999999999.9999 is
// 999999999999.9998, 123456789.12345679 is 123456789.1234568), where
// multiplying by 10^12 and dividing back keeps them. Past 1e296 the moved
// number is infinite, and the result no number.
func databaseFormulaKeptNumber(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	moved := databaseFormulaRound(databaseFormulaMovedPoint(value, databaseFormulaKeptDecimals))
	if math.IsInf(moved, 0) {
		return math.NaN()
	}
	return databaseFormulaMovedPoint(moved, -databaseFormulaKeptDecimals) + 0
}

// databaseFormulaMovedPoint reads a number's shortest decimal text back
// with its decimal point moved by places.
func databaseFormulaMovedPoint(value float64, places int) float64 {
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(value, 'e', -1, 64), "e")
	power, _ := strconv.Atoi(exponent)
	moved, _ := strconv.ParseFloat(mantissa+"e"+strconv.Itoa(power+places), 64)
	return moved
}

// databaseFormulaUTCTime is a time written in UTC, with "Z" or "+00:00".
var databaseFormulaUTCTime = regexp.MustCompile(`(?i)[T ]\d{2}:\d{2}\S*(?:z|[+-]00:?00)$`)

// databaseFormulaPropertyDate is a date property's value as a formula reads
// it: a time written in UTC keeps UTC's clock, as Notion keeps a date set in
// UTC (seen 2026-11-12: format() writes "… 10:30 AM (UTC)" and the formula's
// cell shows the viewer's clock naming it), as the editor reads it
// (formula.ts). A time a formula works out on the viewer's clock is written
// with "Z" too, so this is a property's reading only.
func databaseFormulaPropertyDate(value any) any {
	input := databaseFormulaInput(value)
	date, ok := input.(databaseFormulaDate)
	if text, isText := input.(string); isText {
		date, ok = databaseFormulaDate{Text: text}, true
	}
	start, _, _ := strings.Cut(date.Text, "/")
	if !ok || date.Zone != "" || !databaseFormulaUTCTime.MatchString(databaseFormulaTrim(start)) {
		return input
	}
	date.Zone = "UTC"
	return date
}

// databaseFormulaInput reads a stored or computed value back as a formula
// value.
func databaseFormulaInput(value any) any {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = databaseFormulaInput(item)
		}
		return items
	case map[string]any:
		if text, ok := databaseFormulaStoredRichText(typed); ok {
			return text
		}
		if start, ok := typed["start"].(string); ok {
			date := databaseFormulaDate{Text: start}
			if end, _ := typed["end"].(string); end != "" {
				date.Text += "/" + end
			}
			// A date's time zone keeps its times on that zone's clock.
			if zone, _ := typed["timeZone"].(string); zone != "" {
				if _, ok := databaseFormulaLocation(zone); ok {
					date.Zone = zone
				}
			}
			return date
		}
		kind, _ := typed["kind"].(string)
		if id, ok := typed["id"].(string); ok && (kind == "page" || kind == "person") {
			name, _ := typed["name"].(string)
			email, _ := typed["email"].(string)
			return databaseFormulaReference{Kind: kind, ID: id, Name: name, Email: email}
		}
		return databaseQueryValueText(typed)
	}
	return value
}

// databaseFormulaHoldsNonFinite is whether a value is, or a list holds, a
// number that is not finite.
func databaseFormulaHoldsNonFinite(value any) bool {
	switch typed := value.(type) {
	case float64:
		return math.IsNaN(typed) || math.IsInf(typed, 0)
	case []any:
		return slices.ContainsFunc(typed, databaseFormulaHoldsNonFinite)
	}
	return false
}
