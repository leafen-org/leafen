package app

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// Property type conversion keeps one value rule per type pair so the editor,
// the strict API and MCP convert cells the same way, but for the API's change
// to a select or multi-select, which reads cells by the options it lists
// (database_property_conversion_listed.go). Option types move by
// option ID, and every other pair goes through the source's display text and
// the target's reading of that text, as Notion's reads the value a cell keeps
// (seen 2026-11-09 on the screen and through the API: a number made a
// checkbox is unchecked, 1 and 3 too, and a checkbox made a number empty).
// A value the target cannot read becomes empty; the conversion owner keeps
// the original cells recoverable.

var errDatabasePropertyConversionUnsupported = errors.New("database property conversion is not supported")

var databasePropertyConversionTextTypes = map[string]struct{}{
	graphValueTypeRichText: {}, graphValueTypeURL: {}, graphValueTypeEmail: {}, graphValueTypePhone: {},
}

var databasePropertyConversionOptionTypes = map[string]struct{}{
	graphValueTypeSelect: {}, graphValueTypeMultiSelect: {}, graphValueTypeStatus: {},
}

// Stored types whose cells the user writes; computed and system types keep no
// convertible cells.
var databasePropertyConversionStoredTypes = map[string]struct{}{
	graphValueTypeRichText: {}, graphValueTypeURL: {}, graphValueTypeEmail: {}, graphValueTypePhone: {},
	graphValueTypeNumber: {}, graphValueTypeSelect: {}, graphValueTypeMultiSelect: {}, graphValueTypeStatus: {},
	graphValueTypeDate: {}, graphValueTypePerson: {}, graphValueTypeCheckbox: {}, graphValueTypeFiles: {},
}

// databasePropertyConversionNumberPattern reads a number where the kept text
// starts: a sign, digits grouped by commas of three, a decimal point or comma,
// an exponent.
var databasePropertyConversionNumberPattern = regexp.MustCompile(`^([+-]?)(?:(\d+(?:,\d{3})*)([.,]\d+)?|([.,]\d+))([eE][+-]?\d+)?`)

// databasePropertyConversionFraction is a text that is one number over another.
var databasePropertyConversionFraction = regexp.MustCompile(`^\s*([+-]?\d+(?:\.\d+)?)\s*/\s*(\d+(?:\.\d+)?)\s*$`)

// databasePropertyConversionDigitSpaces joins digits a space groups ("1 000").
var databasePropertyConversionDigitSpaces = regexp.MustCompile(`(\d) +(\d)`)

// databasePropertyConversionNumber reads a text as Notion's change to a number
// does, and as its automations read the words a number line holds: it keeps
// only digits, points, commas, minus signs and the letter e, and reads the
// number those start with — "12 apples" 12, "abc 12" 12, "-0.5kg" -0.5, ".5"
// 0.5, "+4" 4, "1,234" 1234 but "12,5" 12.5, "1 000" 1000, "１２" 12, "1e3"
// 1000, "1.2.3" 1.2, "50%" 50, "₩1000" 1000 (seen 2026-11-03), "v1.2" 1.2,
// "x:1" 1, "a@b.c 4" 0.4, while "page 1", a web address or a file name keep
// an e or a point before their digits and read as nothing; one number over
// another, "1/2", is their quotient (seen 2026-11-09). A plus sign goes with
// the words around it, so "5+5+1" is 551 and "1-2+3" 1 (seen 2026-11-10, a
// text property made a number and an automation's number line). A text
// without a digit is empty.
func databasePropertyConversionNumber(text string) (float64, bool) {
	text = strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return '0' + (r - '０')
		case r == '．':
			return '.'
		case r == '，':
			return ','
		case r == '－':
			return '-'
		case r == '＋':
			return '+'
		}
		return r
	}, text)
	for joined := databasePropertyConversionDigitSpaces.ReplaceAllString(text, "$1$2"); joined != text; joined = databasePropertyConversionDigitSpaces.ReplaceAllString(text, "$1$2") {
		text = joined
	}
	if parts := databasePropertyConversionFraction.FindStringSubmatch(text); parts != nil {
		over, overErr := strconv.ParseFloat(parts[1], 64)
		under, underErr := strconv.ParseFloat(parts[2], 64)
		if overErr != nil || underErr != nil || under == 0 {
			return 0, false
		}
		return over / under, true
	}
	text = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || strings.ContainsRune(".,-eE", r) {
			return r
		}
		return -1
	}, text)
	match := databasePropertyConversionNumberPattern.FindStringSubmatch(text)
	if match == nil {
		return 0, false
	}
	fraction := match[3]
	if fraction == "" {
		fraction = match[4]
	}
	if fraction != "" {
		fraction = "." + fraction[1:]
	}
	number, err := strconv.ParseFloat(match[1]+strings.ReplaceAll(match[2], ",", "")+fraction+match[5], 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return 0, false
	}
	return number, true
}

// databasePropertyConversionChecked is the text a checkbox reads as checked:
// only "Yes", as Notion's (seen 2026-11-03: "true", "1", "checked" and "x" stay
// empty), and what a checked box writes when it turns into text.
const databasePropertyConversionChecked = "yes"

// databasePropertyTypeChangeAllowed reports whether a stored property may
// change between these types: either the cells read unchanged or the
// conversion owner converts them.
func databasePropertyTypeChangeAllowed(from, to string) bool {
	if graphValueTypesShareRepresentation(from, to) {
		return true
	}
	_, err := newDatabasePropertyConversion(from, to, nil, nil, databasePropertyConversionNames{})
	return err == nil
}

type databasePropertyConversionNames struct {
	Person func(id string) string
	Page   func(id string) string
	// Rows gives a data source's rows out of trash with their titles, in their order.
	Rows func(dataSourceID string) []databasePropertyConversionRow
}

// databasePropertyConversionRow is a row a relation may take, by its title.
type databasePropertyConversionRow struct {
	ID    string `db:"id"`
	Title string `db:"title"`
}

type databasePropertyConversion struct {
	from, to      string
	sourceOptions map[string]string
	options       []map[string]any
	optionByName  map[string]string
	names         databasePropertyConversionNames
	newOptionID   func() string
	// The data source a relation target points at, and its rows once read.
	relationSource string
	relationRows   map[string]struct{}
	relationTitles map[string]string
	// listed reads cells by the options the target lists alone
	// (newDatabasePropertyListedConversion).
	listed bool
}

// newDatabasePropertyConversion prepares a conversion. targetConfig is the
// requested target settings; for option targets converted from option types
// the source options carry over.
func newDatabasePropertyConversion(
	from, to string, sourceConfig, targetConfig map[string]any, names databasePropertyConversionNames,
) (*databasePropertyConversion, error) {
	for _, excluded := range []string{graphValueTypeTitle, graphValueTypePlace} {
		if from == excluded || to == excluded {
			return nil, errDatabasePropertyConversionUnsupported
		}
	}
	if _, known := graphValueTypes[from]; !known {
		return nil, errDatabasePropertyConversionUnsupported
	}
	if _, known := graphValueTypes[to]; !known {
		return nil, errDatabasePropertyConversionUnsupported
	}
	conversion := &databasePropertyConversion{
		from: from, to: to, sourceOptions: map[string]string{}, optionByName: map[string]string{},
		names: names, newOptionID: core.GenerateDefaultRandomId,
	}
	if to == graphValueTypeRelation {
		conversion.relationSource, _ = targetConfig["relationDatabaseId"].(string)
	}
	for _, option := range databasePropertyConversionOptions(sourceConfig) {
		id, _ := option["id"].(string)
		name, _ := option["name"].(string)
		conversion.sourceOptions[id] = name
	}
	targetOptions := databasePropertyConversionOptions(targetConfig)
	_, sourceHasOptions := databasePropertyConversionOptionTypes[from]
	if _, targetHasOptions := databasePropertyConversionOptionTypes[to]; targetHasOptions && sourceHasOptions && len(targetOptions) == 0 {
		targetOptions = databasePropertyConversionOptions(sourceConfig)
		if to != graphValueTypeStatus {
			for _, option := range targetOptions {
				delete(option, "group")
			}
		}
	}
	for _, option := range targetOptions {
		id, _ := option["id"].(string)
		name, _ := option["name"].(string)
		if id != "" && name != "" {
			conversion.options = append(conversion.options, option)
			conversion.optionByName[name] = id
		}
	}
	return conversion, nil
}

func databasePropertyConversionOptions(config map[string]any) []map[string]any {
	items, _ := config["options"].([]any)
	options := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if option, ok := item.(map[string]any); ok {
			copied := make(map[string]any, len(option))
			for key, value := range option {
				copied[key] = value
			}
			options = append(options, copied)
		}
	}
	return options
}

// Options returns the target options including any the conversion created.
func (conversion *databasePropertyConversion) Options() []map[string]any {
	return conversion.options
}

// Convert returns the target cell for one stored source cell, or nil when the
// target keeps no value.
func (conversion *databasePropertyConversion) Convert(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if _, stored := databasePropertyConversionStoredTypes[conversion.to]; !stored {
		return nil, nil
	}
	if conversion.listed {
		return conversion.listedValue(raw)
	}
	if value, direct, err := conversion.convertDirect(raw); direct || err != nil {
		return value, err
	}
	text, err := conversion.sourceText(raw)
	if err != nil || strings.TrimSpace(text) == "" {
		return nil, err
	}
	return conversion.fromText(strings.TrimSpace(text))
}

func (conversion *databasePropertyConversion) convertDirect(raw json.RawMessage) (json.RawMessage, bool, error) {
	_, fromOptions := databasePropertyConversionOptionTypes[conversion.from]
	_, toOptions := databasePropertyConversionOptionTypes[conversion.to]
	_, fromText := databasePropertyConversionTextTypes[conversion.from]
	_, toText := databasePropertyConversionTextTypes[conversion.to]
	switch {
	case conversion.from == conversion.to || fromText && toText:
		return raw, true, nil
	case fromOptions && toOptions:
		ids := []string{}
		if conversion.from == graphValueTypeMultiSelect {
			if json.Unmarshal(raw, &ids) != nil {
				return nil, true, errNotionDatabaseRowPageProjectionInvalid
			}
		} else {
			var id string
			if json.Unmarshal(raw, &id) != nil {
				return nil, true, errNotionDatabaseRowPageProjectionInvalid
			}
			if id != "" {
				ids = []string{id}
			}
		}
		known := make([]string, 0, len(ids))
		for _, id := range ids {
			if _, exists := conversion.optionIDs()[id]; exists {
				known = append(known, id)
			}
		}
		if conversion.to == graphValueTypeMultiSelect {
			value, err := json.Marshal(known)
			return value, true, err
		}
		if len(known) == 0 {
			return nil, true, nil
		}
		value, err := json.Marshal(known[0])
		return value, true, err
	case conversion.from == graphValueTypeDate && conversion.to == graphValueTypeNumber:
		// A date reads as no number, as Notion's (seen 2026-11-09 on the
		// screen and through the API: 2026-11-03 left the number empty).
		return nil, true, nil
	}
	return nil, false, nil
}

func (conversion *databasePropertyConversion) optionIDs() map[string]struct{} {
	ids := make(map[string]struct{}, len(conversion.options))
	for _, option := range conversion.options {
		if id, _ := option["id"].(string); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

// sourceText is the display text of a stored source cell.
func (conversion *databasePropertyConversion) sourceText(raw json.RawMessage) (string, error) {
	invalid := errNotionDatabaseRowPageProjectionInvalid
	switch conversion.from {
	case graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return "", invalid
		}
		return text, nil
	case graphValueTypeNumber, graphValueTypeUniqueID:
		var number float64
		if json.Unmarshal(raw, &number) != nil {
			return "", invalid
		}
		return strconv.FormatFloat(number, 'f', -1, 64), nil
	case graphValueTypeSelect, graphValueTypeStatus:
		var id string
		if json.Unmarshal(raw, &id) != nil {
			return "", invalid
		}
		return conversion.sourceOptions[id], nil
	case graphValueTypeMultiSelect:
		ids := []string{}
		if json.Unmarshal(raw, &ids) != nil {
			return "", invalid
		}
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			if name := conversion.sourceOptions[id]; name != "" {
				names = append(names, name)
			}
		}
		return strings.Join(names, ","), nil
	case graphValueTypeDate:
		date := struct {
			Start string  `json:"start"`
			End   *string `json:"end"`
		}{}
		if json.Unmarshal(raw, &date) != nil {
			return "", invalid
		}
		if date.End != nil && *date.End != "" {
			return date.Start + " → " + *date.End, nil
		}
		return date.Start, nil
	case graphValueTypeCheckbox:
		var checked bool
		if json.Unmarshal(raw, &checked) != nil {
			return "", invalid
		}
		if checked {
			return "Yes", nil
		}
		return "No", nil
	case graphValueTypePerson:
		ids := []string{}
		if json.Unmarshal(raw, &ids) != nil {
			return "", invalid
		}
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			if conversion.names.Person != nil {
				if name := conversion.names.Person(id); name != "" {
					names = append(names, name)
				}
			}
		}
		return strings.Join(names, ", "), nil
	case graphValueTypeRelation:
		return strings.Join(conversion.relatedPages(raw), ", "), nil
	case graphValueTypeFiles:
		items, ok := databasePropertyConversionFiles(raw)
		if !ok {
			return "", invalid
		}
		texts := make([]string, 0, len(items))
		for _, item := range items {
			if text := item.text(); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, ","), nil
	}
	return "", nil
}

// Spans gives what Notion's text keeps of a value (seen 2026-11-04): a date is
// a date mention, its range in one, shown in full; a relation its pages'
// mentions.
func (conversion *databasePropertyConversion) Spans(raw, value json.RawMessage) []graphTextSpan {
	if conversion.to != graphValueTypeRichText {
		return nil
	}
	if conversion.from == graphValueTypeRelation {
		return conversion.relationSpans(raw)
	}
	if conversion.from == graphValueTypeFiles {
		return databasePropertyConversionFileSpans(raw)
	}
	if conversion.from != graphValueTypeDate {
		return nil
	}
	date := struct {
		Start string  `json:"start"`
		End   *string `json:"end"`
	}{}
	text := ""
	if json.Unmarshal(raw, &date) != nil || json.Unmarshal(value, &text) != nil || text == "" {
		return nil
	}
	span := graphTextSpan{Text: text, Mention: "date", Date: date.Start, DateFormat: "full"}
	if date.End != nil {
		span.DateEnd = *date.End
	}
	return []graphTextSpan{span}
}

// fromText reads display text as the target type.
func (conversion *databasePropertyConversion) fromText(text string) (json.RawMessage, error) {
	switch conversion.to {
	case graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		return json.Marshal(text)
	case graphValueTypeNumber:
		number, ok := databasePropertyConversionNumber(text)
		if !ok {
			return nil, nil
		}
		return json.Marshal(number)
	case graphValueTypeCheckbox:
		return json.Marshal(strings.ToLower(text) == databasePropertyConversionChecked)
	case graphValueTypeSelect:
		// A select takes the text's first comma piece and makes options of
		// the others too, as Notion's (seen 2026-11-09: "hello, A" became
		// hello, "a,b,c" a, with b and c among the options).
		first := ""
		for index, part := range strings.Split(text, ",") {
			if id := conversion.optionFor(strings.TrimSpace(part)); index == 0 {
				first = id
			}
		}
		if first == "" {
			return nil, nil
		}
		return json.Marshal(first)
	case graphValueTypeStatus:
		id := conversion.optionFor(text)
		if id == "" {
			return nil, nil
		}
		return json.Marshal(id)
	case graphValueTypeMultiSelect:
		ids := []string{}
		seen := map[string]struct{}{}
		for _, part := range strings.Split(text, ",") {
			id := conversion.optionFor(strings.TrimSpace(part))
			if _, repeated := seen[id]; id != "" && !repeated {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return nil, nil
		}
		return json.Marshal(ids)
	case graphValueTypeDate:
		start, end, _ := strings.Cut(text, " → ")
		startDate, startRead := databasePropertyConversionDate(start)
		endDate, endRead := databasePropertyConversionDate(end)
		if !startRead || end != "" && !endRead {
			return nil, nil
		}
		date := map[string]any{"start": startDate}
		if end != "" {
			date["end"] = endDate
		}
		return json.Marshal(date)
	}
	// People and files need references Leafen cannot read from text.
	return nil, nil
}

// databasePropertyConversionDate reads text as a date, keeping the ISO forms as
// written and reading a month/day/year as its day, as Notion's (seen
// 2026-11-03: "11/3/2026"; "Nov 3, 2026" and "2026년 11월 3일" stay empty).
func databasePropertyConversionDate(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02", "2006-01-02T15:04", "2006-01-02T15:04:05", time.RFC3339, time.RFC3339Nano} {
		if _, err := time.Parse(layout, value); err == nil {
			return value, true
		}
	}
	if day, err := time.Parse("1/2/2006", value); err == nil {
		return day.Format("2006-01-02"), true
	}
	return "", false
}

// optionFor finds the target option with this name, creating it however
// many the target has, as Notion's conversion makes one of each value.
func (conversion *databasePropertyConversion) optionFor(name string) string {
	if name == "" || utf8.RuneCountInString(name) > maxGraphIdentifierLength {
		return ""
	}
	if id := conversion.optionByName[name]; id != "" {
		return id
	}
	// A name in another case is the same option, the first form kept, as
	// Notion's (seen 2026-11-09: "B" after "b" became b, "A" after "a" a).
	for known, id := range conversion.optionByName {
		if strings.EqualFold(known, name) {
			return id
		}
	}
	// A status reads a value by its options' names alone, as Notion's (seen
	// 2026-11-08: text "완료" became its 완료, "빨강" none, read as its default).
	if conversion.to == graphValueTypeStatus {
		return ""
	}
	id := conversion.newOptionID()
	conversion.options = append(conversion.options, map[string]any{"id": id, "name": name, "color": "default"})
	conversion.optionByName[name] = id
	return id
}

// A file in a files cell, as converting the cell reads it: a file added by
// its link is that link, as Notion's (seen 2026-11-07: a cell made text kept
// "https://example.com/sample-image.png" as an underlined link, not the
// file's name); an uploaded one is its name.
type databasePropertyConversionFile struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Source string `json:"source"`
}

// link is a linked file's web address; one that is none (a linked file
// keeps any text, "not a url", "mailto:…") stays words.
func (file databasePropertyConversionFile) link() string {
	if address := file.address(); validGraphExternalFileURL(address) && validGraphTextLink(address) {
		return address
	}
	return ""
}

func (file databasePropertyConversionFile) address() string {
	if file.Source != "external" {
		return ""
	}
	return strings.TrimSpace(file.URL)
}

func (file databasePropertyConversionFile) text() string {
	if address := file.address(); address != "" {
		return address
	}
	return file.Name
}

func databasePropertyConversionFiles(raw json.RawMessage) ([]databasePropertyConversionFile, bool) {
	items := []databasePropertyConversionFile{}
	return items, json.Unmarshal(raw, &items) == nil
}

// databasePropertyConversionFileSpans writes a files cell made text as its
// files' texts, a linked file's as a link to it, apart by commas.
func databasePropertyConversionFileSpans(raw json.RawMessage) []graphTextSpan {
	items, ok := databasePropertyConversionFiles(raw)
	if !ok {
		return nil
	}
	spans := []graphTextSpan{}
	for _, item := range items {
		text := item.text()
		if text == "" {
			continue
		}
		if len(spans) > 0 {
			spans = append(spans, graphTextSpan{Text: ","})
		}
		spans = append(spans, graphTextSpan{Text: text, Link: item.link()})
	}
	if len(spans) == 0 {
		return nil
	}
	return spans
}
