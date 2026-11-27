package app

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// notionShape is what one place in a request may hold, as Notion's request
// check sees it. A shape names what it is (kind), whether it may be left out
// (optional, said as "`undefined`") or null, and what it holds.
type notionShape struct {
	kind     notionShapeKind
	optional bool
	nullable bool
	fields   []notionShapeField // an object's known keys, checked in this order
	closed   bool               // an object takes no other key ("should be not present")
	each     *notionShape       // an array's items, or every value of an object used as a record
	values   []string           // the strings an enum takes
	minimum  *float64
	maximum  *float64
	// above and below are bounds a number may not reach ("> `0`", "< `1`").
	above *float64
	below *float64
	// whole is a number that should be an integer.
	whole    bool
	maxItems int
	minItems int
	// minLength and maxLength are the fewest and most UTF-16 units a string
	// takes; populated is a string that isn't empty, said so.
	minLength int
	maxLength int
	populated bool
	branches  []*notionShape // a union's alternatives, or a tagged union's in tag order
	tag       string         // a tagged union's key
	// picks is a field that tells a union's alternatives apart: an
	// alternative that fails only there isn't named while another fails
	// elsewhere (seen 2026-11-10 of a file's type).
	picks bool
	// hushes is a field whose fault, in one of a union's alternatives,
	// leaves the union's faults unsaid: the union fails as that alternative,
	// picked, so a union around it names its other alternatives (seen
	// 2026-11-12 of a changed property's type).
	hushes bool
	// own is what Leafen takes beside Notion's, refused as Notion's refuses it
	// when it doesn't fit: a union's alternative isn't named, and an object's
	// key has no place there with the keys Notion doesn't take.
	own bool
	// said is what a checked string should be: a valid ISO 8601 date, or
	// what accepts takes.
	said    string
	accepts func(text string) bool
	// truthy is a record that takes no value JavaScript reads as false:
	// null, "", 0 or false (seen 2026-11-10 of a page's properties).
	truthy bool
}

type notionShapeField struct {
	name  string
	shape *notionShape
}

type notionShapeKind int

const (
	notionAny notionShapeKind = iota
	notionObject
	notionArray
	notionString
	notionNumber
	notionBoolean
	notionUUID
	notionEnum
	notionTrue
	notionRecord // any value whose keys are checked, as JavaScript's Object.keys sees them
	notionUnion
	notionTagged
	// notionEmoji is one emoji, as Notion's page and data source icons take
	// it (notionSingleEmoji).
	notionEmoji
	// notionISODate is a string Notion reads as an ISO 8601 date
	// (notionISODateValid).
	notionISODate
	// notionBlockUUID is an ID as a new block's id takes it (notionBlockUUIDText).
	notionBlockUUID
	// notionAbsent is a key a place takes no value under, said "not present".
	notionAbsent
)

func notionFields(fields ...notionShapeField) []notionShapeField { return fields }

func notionField(name string, shape *notionShape) notionShapeField {
	return notionShapeField{name: name, shape: shape}
}

func shapeOf(kind notionShapeKind) *notionShape { return &notionShape{kind: kind} }

func (shape *notionShape) opt() *notionShape {
	copied := *shape
	copied.optional = true
	return &copied
}

func (shape *notionShape) orNull() *notionShape {
	copied := *shape
	copied.nullable = true
	return &copied
}

func (shape *notionShape) atLeast(minimum float64) *notionShape {
	copied := *shape
	copied.minimum = &minimum
	return &copied
}

func (shape *notionShape) atMost(maximum float64) *notionShape {
	copied := *shape
	copied.maximum = &maximum
	return &copied
}

// integer is a number without a fraction.
func (shape *notionShape) integer() *notionShape {
	copied := *shape
	copied.whole = true
	return &copied
}

// between is a number greater than above and less than below.
func (shape *notionShape) between(above, below float64) *notionShape {
	copied := *shape
	copied.above, copied.below = &above, &below
	return &copied
}

// longAtLeast is a string of at least minimum UTF-16 units.
func (shape *notionShape) longAtLeast(minimum int) *notionShape {
	copied := *shape
	copied.minLength = minimum
	return &copied
}

// longAtMost is a string of at most maximum UTF-16 units.
func (shape *notionShape) longAtMost(maximum int) *notionShape {
	copied := *shape
	copied.maxLength = maximum
	return &copied
}

// notEmpty is a string that should be populated.
func (shape *notionShape) notEmpty() *notionShape {
	copied := *shape
	copied.populated = true
	return &copied
}

// atMostItems is an array of at most maximum items, said after its items.
func (shape *notionShape) atMostItems(maximum int) *notionShape {
	copied := *shape
	copied.maxItems = maximum
	return &copied
}

// atLeastItems is an array of at least minimum items, said after its items.
func (shape *notionShape) atLeastItems(minimum int) *notionShape {
	copied := *shape
	copied.minItems = minimum
	return &copied
}

func notionEnumOf(values ...string) *notionShape {
	return &notionShape{kind: notionEnum, values: values}
}

func notionObjectOf(closed bool, fields ...notionShapeField) *notionShape {
	return &notionShape{kind: notionObject, closed: closed, fields: fields}
}

func notionArrayOf(each *notionShape) *notionShape {
	return &notionShape{kind: notionArray, each: each}
}

func notionUnionOf(branches ...*notionShape) *notionShape {
	return &notionShape{kind: notionUnion, branches: branches}
}

// notionTaggedOf picks its alternative by the tag key's value, each
// alternative holding that key as a one-value enum.
func notionTaggedOf(tag string, branches ...*notionShape) *notionShape {
	return &notionShape{kind: notionTagged, tag: tag, branches: branches}
}

func (shape *notionShape) typeName() string {
	switch shape.kind {
	case notionObject, notionUnion, notionTagged, notionRecord:
		return "an object"
	case notionArray:
		return "an array"
	case notionString, notionUUID, notionEmoji, notionISODate, notionBlockUUID:
		return "a string"
	case notionNumber:
		return "a number"
	case notionBoolean:
		return "a boolean"
	case notionTrue:
		return "`true`"
	}
	return ""
}

// with is what a shape may be, followed by null and undefined when it may
// be those.
func (shape *notionShape) with(first ...string) []string {
	expect := append([]string{}, first...)
	if shape.nullable {
		expect = append(expect, "`null`")
	}
	if shape.optional {
		expect = append(expect, "`undefined`")
	}
	return expect
}

func notionQuoted(text string) string {
	var out strings.Builder
	out.WriteByte('`')
	notionJSString(text, &out)
	out.WriteByte('`')
	return out.String()
}

func (shape *notionShape) typeFits(value *notionValue) bool {
	switch shape.kind {
	case notionUnion:
		// A union of objects is an object; one with other alternatives (a
		// property value) leaves each value to them.
		for _, branch := range shape.branches {
			if branch.kind != notionObject && branch.kind != notionUnion && branch.kind != notionTagged {
				return true
			}
		}
		return value.kind == 'o'
	case notionObject, notionTagged:
		return value.kind == 'o'
	case notionArray:
		return value.kind == 'a'
	case notionString, notionUUID, notionEmoji, notionISODate, notionBlockUUID:
		return value.kind == 's'
	case notionNumber:
		return value.kind == 'n'
	case notionBoolean:
		return value.kind == 'b'
	case notionTrue:
		return value.kind == 'b' && value.truth
	}
	return true
}

// check is what is wrong with value at path, nothing when it fits; several
// faults are choices, any of which fixes it.
func (shape *notionShape) check(value *notionValue, path []string) []notionFault {
	if value == nil {
		if shape.optional || shape.kind == notionAny {
			return nil
		}
		return []notionFault{{path: path, expect: []string{"defined"}, shown: "undefined"}}
	}
	if value.kind == 'z' && shape.nullable {
		return nil
	}
	switch shape.kind {
	case notionAny:
		return nil
	case notionAbsent:
		return []notionFault{{path: path, expect: []string{"not present"}, shown: notionShownValue(value)}}
	case notionRecord:
		// A record is any value but null, its keys as Object.keys finds them.
		if value.kind == 'z' || shape.truthy && notionFalsy(value) {
			return []notionFault{{path: path, expect: shape.with(shape.typeName()), shown: notionShownValue(value)}}
		}
		return shape.checkRecord(value, path)
	case notionEnum:
		if value.kind != 's' || !containsString(shape.values, value.text) {
			quoted := make([]string, len(shape.values))
			for index, text := range shape.values {
				quoted[index] = notionQuoted(text)
			}
			return []notionFault{{path: path, expect: shape.with(quoted...), shown: notionShownValue(value)}}
		}
		return nil
	}
	if !shape.typeFits(value) {
		return []notionFault{{path: path, expect: shape.with(shape.typeName()), shown: notionShownValue(value)}}
	}
	switch shape.kind {
	case notionUUID:
		if !notionBodyUUID.MatchString(value.text) && !leafenOwnID.MatchString(value.text) {
			return []notionFault{{path: path, expect: shape.with("a valid uuid"), shown: notionShownValue(value)}}
		}
	case notionEmoji:
		// Notion names every emoji it takes here; Leafen says what it takes.
		if !notionSingleEmoji(value.text) {
			return []notionFault{{path: path, expect: shape.with("one emoji"), shown: notionShownValue(value)}}
		}
	case notionISODate:
		if !notionISODateValid(value.text) {
			return []notionFault{{path: path, expect: shape.with(shape.said), shown: notionShownValue(value)}}
		}
	case notionBlockUUID:
		if !notionBlockUUIDText.MatchString(value.text) {
			return []notionFault{{path: path, expect: shape.with("a valid uuid"), shown: notionShownValue(value)}}
		}
	case notionString:
		length := len(utf16.Encode([]rune(value.text)))
		switch {
		case shape.accepts != nil && !shape.accepts(value.text):
			return []notionFault{{path: path, expect: shape.with(shape.said), shown: notionShownValue(value)}}
		case shape.populated && length == 0:
			return []notionFault{{path: path, expect: shape.with("populated"), shown: notionShownValue(value)}}
		case length < shape.minLength:
			return []notionFault{{
				path: notionChildPath(path, "length"), expect: []string{"≥ `" + strconv.Itoa(shape.minLength) + "`"}, shown: strconv.Itoa(length),
			}}
		case shape.maxLength > 0 && length > shape.maxLength:
			return []notionFault{{
				path: notionChildPath(path, "length"), expect: []string{"≤ `" + strconv.Itoa(shape.maxLength) + "`"}, shown: strconv.Itoa(length),
			}}
		}
	case notionNumber:
		number, _ := value.number()
		if shape.whole && number != math.Trunc(number) {
			return []notionFault{{path: path, expect: shape.with("an integer"), shown: notionShownValue(value)}}
		}
		if shape.minimum != nil && number < *shape.minimum {
			return []notionFault{{path: path, expect: shape.with("≥ `" + notionJSNumber(*shape.minimum) + "`"), shown: notionShownValue(value)}}
		}
		if shape.maximum != nil && number > *shape.maximum {
			return []notionFault{{path: path, expect: shape.with("≤ `" + notionJSNumber(*shape.maximum) + "`"), shown: notionShownValue(value)}}
		}
		if shape.above != nil && number <= *shape.above {
			return []notionFault{{path: path, expect: shape.with("> `" + notionJSNumber(*shape.above) + "`"), shown: notionShownValue(value)}}
		}
		if shape.below != nil && number >= *shape.below {
			return []notionFault{{path: path, expect: shape.with("< `" + notionJSNumber(*shape.below) + "`"), shown: notionShownValue(value)}}
		}
	case notionArray:
		return shape.checkArray(value, path)
	case notionObject:
		return shape.checkObject(value, path)
	case notionUnion:
		alternatives := make([][]notionFault, 0, len(shape.branches))
		var hushed []notionFault
		for _, branch := range shape.branches {
			faults := branch.check(value, path)
			if len(faults) == 0 {
				return nil
			}
			if faults[0].hushed && hushed == nil {
				hushed = faults
			}
			if !branch.own {
				alternatives = append(alternatives, faults)
			}
		}
		if hushed != nil {
			for index := range hushed {
				hushed[index].hushed, hushed[index].picked = false, true
			}
			return hushed
		}
		return notionMergeFaults(alternatives)
	case notionTagged:
		return shape.checkTagged(value, path)
	}
	return nil
}

// checkArray checks the items, then how many there are (seen 2026-11-10:
// a list of 101 whose first item is wrong names that item), too many or too
// few.
func (shape *notionShape) checkArray(value *notionValue, path []string) []notionFault {
	if shape.each != nil {
		for index, item := range value.items {
			if faults := shape.each.check(item, notionChildPath(path, "["+strconv.Itoa(index)+"]")); len(faults) > 0 {
				return faults
			}
		}
	}
	if shape.maxItems > 0 && len(value.items) > shape.maxItems {
		return []notionFault{{
			path:   notionChildPath(path, "length"),
			expect: []string{"≤ `" + strconv.Itoa(shape.maxItems) + "`"},
			shown:  strconv.Itoa(len(value.items)),
		}}
	}
	if len(value.items) < shape.minItems {
		return []notionFault{{
			path:   notionChildPath(path, "length"),
			expect: []string{"≥ `" + strconv.Itoa(shape.minItems) + "`"},
			shown:  strconv.Itoa(len(value.items)),
		}}
	}
	return nil
}

// checkObject checks the known keys in order and stops at the first that
// doesn't fit; only then are keys it doesn't take named, all of them. A key
// it doesn't take that tells a union's alternatives apart (an absent field
// that picks) leaves the alternative unnamed when it is the only one.
func (shape *notionShape) checkObject(value *notionValue, path []string) []notionFault {
	known := map[string]bool{}
	picking := map[string]bool{}
	for _, field := range shape.fields {
		if field.shape.kind == notionAbsent && field.shape.picks {
			picking[field.name] = true
			continue
		}
		if field.shape.own {
			known[field.name] = len(field.shape.check(value.fields[field.name], nil)) == 0
			continue
		}
		known[field.name] = true
		if faults := field.shape.check(value.fields[field.name], notionChildPath(path, field.name)); len(faults) > 0 {
			for index := range faults {
				faults[index].picked = faults[index].picked || field.shape.picks
				faults[index].hushed = field.shape.hushes
			}
			return faults
		}
	}
	if shape.each != nil {
		for _, key := range value.keys {
			if known[key] {
				continue
			}
			if faults := shape.each.check(value.fields[key], notionChildPath(path, key)); len(faults) > 0 {
				return faults
			}
		}
	}
	if !shape.closed {
		return nil
	}
	faults := []notionFault{}
	picked := true
	for _, key := range value.keys {
		if !known[key] {
			picked = picked && picking[key]
			faults = append(faults, notionFault{path: notionChildPath(path, key), expect: []string{"not present"}, shown: notionShownValue(value.fields[key])})
		}
	}
	for index := range faults {
		faults[index].picked = picked
	}
	return faults
}

// notionFalsy reports whether JavaScript reads a value as false: "", 0 or
// false (null aside).
func notionFalsy(value *notionValue) bool {
	number, isNumber := value.number()
	return value.kind == 's' && value.text == "" || isNumber && number == 0 || value.kind == 'b' && !value.truth
}

// checkRecord checks every value under a key, as Object.keys finds keys:
// an object's, an array's or a string's indexes, none in a number.
func (shape *notionShape) checkRecord(value *notionValue, path []string) []notionFault {
	switch value.kind {
	case 'o':
		for _, key := range value.keys {
			if faults := shape.each.check(value.fields[key], notionChildPath(path, key)); len(faults) > 0 {
				return faults
			}
		}
	case 'a':
		for index, item := range value.items {
			if faults := shape.each.check(item, notionChildPath(path, strconv.Itoa(index))); len(faults) > 0 {
				return faults
			}
		}
	case 's':
		index := 0
		for _, r := range value.text {
			letter := &notionValue{kind: 's', text: string(r)}
			if faults := shape.each.check(letter, notionChildPath(path, strconv.Itoa(index))); len(faults) > 0 {
				return faults
			}
			index += utf16.RuneLen(r)
		}
	}
	return nil
}

func (shape *notionShape) checkTagged(value *notionValue, path []string) []notionFault {
	tag := value.fields[shape.tag]
	tags := make([]string, len(shape.branches))
	for index, branch := range shape.branches {
		tags[index] = branch.fields[0].shape.values[0]
		if tag != nil && tag.kind == 's' && tag.text == tags[index] {
			return branch.check(value, path)
		}
	}
	tagPath := notionChildPath(path, shape.tag)
	if tag == nil {
		return []notionFault{{path: tagPath, expect: []string{"defined"}, shown: "undefined"}}
	}
	quoted := make([]string, len(tags))
	for index, text := range tags {
		quoted[index] = notionQuoted(text)
	}
	return []notionFault{{path: tagPath, expect: quoted, shown: notionShownValue(tag)}}
}

// notionSingleEmoji reports whether text is one emoji, as an icon's emoji
// is (seen 2026-11-10: Notion takes "🥬", "☺", "#️⃣", "🇰🇷", "👍🏽" and "👨‍👩‍👧"
// and refuses "1", "😀😀" and words): one pictograph, a keycap or a flag,
// with its skin tone, variation selectors and tags, and the pictographs a
// zero-width joiner joins to it.
func notionSingleEmoji(text string) bool {
	if text == "" || len(text) > 64 || !utf8.ValidString(text) {
		return false
	}
	emoji, regional, keycapBase, joined, pictograph := 0, 0, false, false, false
	for _, r := range text {
		switch {
		case r == 0x200D:
			joined = true
			continue
		case r == 0xFE0E || r == 0xFE0F || r >= 0x1F3FB && r <= 0x1F3FF || r >= 0xE0020 && r <= 0xE007F:
			continue
		case r == 0x20E3:
			if !keycapBase {
				return false
			}
			pictograph = true
			continue
		case r >= 0x1F1E6 && r <= 0x1F1FF:
			regional++
			if regional%2 == 0 {
				continue
			}
		case r == '#' || r == '*' || r >= '0' && r <= '9':
			keycapBase = true
		case notionEmojiPictograph(r):
			pictograph = true
		default:
			return false
		}
		if !joined {
			emoji++
		}
		joined = false
	}
	return emoji == 1 && (pictograph || regional == 2)
}

// notionEmojiPictograph reports whether r is a pictograph an emoji starts
// with.
func notionEmojiPictograph(r rune) bool {
	return r == 0x00A9 || r == 0x00AE || r == 0x203C || r == 0x2049 || r == 0x2122 || r == 0x2139 ||
		r >= 0x2194 && r <= 0x21AA || r == 0x24C2 || r >= 0x231A && r <= 0x23FF || r >= 0x25AA && r <= 0x27BF ||
		r >= 0x2934 && r <= 0x2935 || r >= 0x2B05 && r <= 0x2B55 || r == 0x3030 || r == 0x303D || r == 0x3297 ||
		r == 0x3299 || r >= 0x1F000 && r <= 0x1FAFF
}
