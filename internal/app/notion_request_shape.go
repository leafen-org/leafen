package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Notion checks a request's Notion-Version header, its query and its body
// against one shape for the endpoint before it looks anything up, and says
// what failed in one form (seen 2026-11-07 to 11-10), in Leafen's words
// (notion_request_fault_words.go):
//
//	body isn't valid: body.title takes an array or `undefined`, not `"x"`.
//	body isn't valid; any one of these fixes it:
//	body.filter.value takes `"page"` or `"data_source"`, not `"x"`.
//	body.filter.in_trash is missing.
//
// A value in the message is JSON as JavaScript writes it, cut to 55
// characters and "..." when longer. This file holds the value, the shape and
// the message; notion_request_shape_routes.go holds each endpoint's shape.

// notionValue is a request value with its object keys in the order sent, as
// Notion lists unknown keys in that order.
type notionValue struct {
	kind   byte // 'o' object, 'a' array, 's' string, 'n' number, 'b' boolean, 'z' null
	keys   []string
	fields map[string]*notionValue
	items  []*notionValue
	text   string // a string's value or a number's digits
	truth  bool
}

var errNotionValueNotJSON = errors.New("not one JSON value")

// parseNotionValue reads one JSON value, keys in their order.
func parseNotionValue(raw []byte) (*notionValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := readNotionValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errNotionValueNotJSON
	}
	return value, nil
}

func readNotionValue(decoder *json.Decoder) (*notionValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch typed := token.(type) {
	case json.Delim:
		if typed == '{' {
			object := &notionValue{kind: 'o', fields: map[string]*notionValue{}}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyToken.(string)
				field, err := readNotionValue(decoder)
				if err != nil {
					return nil, err
				}
				if _, seen := object.fields[key]; !seen {
					object.keys = append(object.keys, key)
				}
				object.fields[key] = field
			}
			_, err := decoder.Token()
			return object, err
		}
		if typed == '[' {
			array := &notionValue{kind: 'a'}
			for decoder.More() {
				item, err := readNotionValue(decoder)
				if err != nil {
					return nil, err
				}
				array.items = append(array.items, item)
			}
			_, err := decoder.Token()
			return array, err
		}
		return nil, errNotionValueNotJSON
	case string:
		return &notionValue{kind: 's', text: typed}, nil
	case json.Number:
		return &notionValue{kind: 'n', text: typed.String()}, nil
	case bool:
		return &notionValue{kind: 'b', truth: typed}, nil
	case nil:
		return &notionValue{kind: 'z'}, nil
	}
	return nil, errNotionValueNotJSON
}

func (value *notionValue) field(key string) *notionValue {
	if value == nil || value.kind != 'o' {
		return nil
	}
	return value.fields[key]
}

func (value *notionValue) number() (float64, bool) {
	if value == nil || value.kind != 'n' {
		return 0, false
	}
	number, err := strconv.ParseFloat(value.text, 64)
	return number, err == nil
}

// notionJSText writes a value as JavaScript's JSON.stringify does.
func notionJSText(value *notionValue, out *strings.Builder) {
	switch value.kind {
	case 'o':
		out.WriteByte('{')
		for index, key := range value.keys {
			if index > 0 {
				out.WriteByte(',')
			}
			notionJSString(key, out)
			out.WriteByte(':')
			notionJSText(value.fields[key], out)
		}
		out.WriteByte('}')
	case 'a':
		out.WriteByte('[')
		for index, item := range value.items {
			if index > 0 {
				out.WriteByte(',')
			}
			notionJSText(item, out)
		}
		out.WriteByte(']')
	case 's':
		notionJSString(value.text, out)
	case 'n':
		number, _ := strconv.ParseFloat(value.text, 64)
		out.WriteString(notionJSNumber(number))
	case 'b':
		out.WriteString(strconv.FormatBool(value.truth))
	default:
		out.WriteString("null")
	}
}

func notionJSString(text string, out *strings.Builder) {
	out.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteString(strconv.FormatInt(int64(r)>>4, 16))
				out.WriteString(strconv.FormatInt(int64(r)&15, 16))
				continue
			}
			out.WriteRune(r)
		}
	}
	out.WriteByte('"')
}

func notionJSNumber(number float64) string {
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return "null"
	}
	if number == 0 {
		return "0"
	}
	if magnitude := math.Abs(number); magnitude >= 1e21 || magnitude < 1e-6 {
		text := strconv.FormatFloat(number, 'e', -1, 64)
		mantissa, exponent, _ := strings.Cut(text, "e")
		sign := exponent[:1]
		digits := strings.TrimLeft(exponent[1:], "0")
		return mantissa + "e" + sign + digits
	}
	return strconv.FormatFloat(number, 'f', -1, 64)
}

const notionShownValueLength = 55

// notionShownValue is how a fault shows the value it found: undefined when
// there was none, and JSON longer than 55 characters (counted as JavaScript
// counts them) cut there with "...".
func notionShownValue(value *notionValue) string {
	if value == nil {
		return "undefined"
	}
	var out strings.Builder
	notionJSText(value, &out)
	text := out.String()
	units := 0
	for index, r := range text {
		units += len(utf16.Encode([]rune{r}))
		if units > notionShownValueLength {
			return text[:index] + "..."
		}
	}
	return text
}

// notionFault is one thing a place in a request should be and isn't.
type notionFault struct {
	path   []string // "body", "title", "[0]", …
	expect []string // "an array", "`undefined`", "defined", "not present", …
	shown  string
	// picked is a fault at a field that tells a union's alternatives apart
	// (notionShape.picks).
	picked bool
	// hushed is a fault at a field that leaves its union's faults unsaid
	// (notionShape.hushes).
	hushed bool
}

func notionPathText(path []string) string {
	var out strings.Builder
	for index, part := range path {
		if index > 0 && !strings.HasPrefix(part, "[") {
			out.WriteByte('.')
		}
		out.WriteString(part)
	}
	return out.String()
}

func notionOrList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
}

func (fault notionFault) line() string {
	return notionFaultLine(notionPathText(fault.path), fault.expect, fault.shown)
}

// notionFaultsMessage says what failed where ("body", "query" or
// "Notion-Version header"): one fault plainly, several as choices.
func notionFaultsMessage(where string, faults []notionFault) string {
	lines := make([]string, len(faults))
	for index, fault := range faults {
		lines[index] = fault.line()
	}
	return notionFaultSaid(where, lines...)
}

func notionChildPath(path []string, part string) []string {
	child := make([]string, len(path), len(path)+1)
	copy(child, path)
	return append(child, part)
}

// notionMergeFaults keeps the faults of the alternatives that got furthest
// and says each place once, what it may be joined in order: a value that is
// "an object" in one alternative and "not present" in another "should be an
// object or not present". An alternative that failed only at a field that
// tells them apart is left out while another failed elsewhere. The places
// are said as a tree, each in the order its first branch was named (seen
// 2026-11-10: a list's item [1]'s faults before item [0]'s when the first
// alternative failed at [1]).
func notionMergeFaults(alternatives [][]notionFault) []notionFault {
	picked := 0
	for _, faults := range alternatives {
		if len(faults) > 0 && faults[0].picked {
			picked++
		}
	}
	if picked > 0 && picked < len(alternatives) {
		kept := make([][]notionFault, 0, len(alternatives)-picked)
		for _, faults := range alternatives {
			if len(faults) > 0 && !faults[0].picked {
				kept = append(kept, faults)
			}
		}
		alternatives = kept
	}
	deepest := 0
	for _, faults := range alternatives {
		if len(faults) > 0 && len(faults[0].path) > deepest {
			deepest = len(faults[0].path)
		}
	}
	merged := []notionFault{}
	at := map[string]int{}
	branchOrder := map[string]int{}
	for _, faults := range alternatives {
		if len(faults) == 0 || len(faults[0].path) < deepest {
			continue
		}
		for _, fault := range faults {
			for depth := 1; depth <= len(fault.path); depth++ {
				prefix := strings.Join(fault.path[:depth], "\x00")
				if _, seen := branchOrder[prefix]; !seen {
					branchOrder[prefix] = len(branchOrder)
				}
			}
			key := notionPathText(fault.path)
			index, seen := at[key]
			if !seen {
				at[key] = len(merged)
				merged = append(merged, notionFault{path: fault.path, expect: append([]string{}, fault.expect...), shown: fault.shown})
				continue
			}
			for _, expect := range fault.expect {
				if !containsString(merged[index].expect, expect) {
					merged[index].expect = append(merged[index].expect, expect)
				}
			}
		}
	}
	order := func(fault notionFault) []int {
		ranks := make([]int, len(fault.path))
		for depth := range fault.path {
			ranks[depth] = branchOrder[strings.Join(fault.path[:depth+1], "\x00")]
		}
		return ranks
	}
	sort.SliceStable(merged, func(left, right int) bool {
		a, b := order(merged[left]), order(merged[right])
		for index := 0; index < len(a) && index < len(b); index++ {
			if a[index] != b[index] {
				return a[index] < b[index]
			}
		}
		return len(a) < len(b)
	})
	return merged
}
