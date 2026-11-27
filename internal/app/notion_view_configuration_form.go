package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// A view configuration's form, as Notion's API checks it before reading
// anything it names (seen 2026-11-12): field by field in the order of its
// type's own list, and into each object or list before the field after it;
// the first field out of form is refused — missing for one left out that
// has to be given, what it takes otherwise (notionFaultLine). A field the list doesn't name is passed over.

// notionViewFormPresence is whether a field has to be given and whether it
// may be null.
type notionViewFormPresence int

const (
	notionViewFormRequired notionViewFormPresence = iota // given, not null
	notionViewFormOptional                               // or left out, not null
	notionViewFormNullable                               // or left out, or null
)

// notionViewForm is what one field may be: an enum's choices as JSON
// (`"week"`, `0`), a boolean, number, integer (none below 0), whole number
// (any sign, as a timeline's center_timestamp), words, a list (its items each
// of one form, at most `most` of them when that is set), an object (its
// fields in order), an object whose fields go by its type (union, keyed by
// the type, its types in the order Notion lists them), or a form checked on
// its own.
type notionViewForm struct {
	kind       string
	presence   notionViewFormPresence
	choices    []string
	fields     []notionViewFormField
	items      *notionViewForm
	most       int
	unionTypes []string
	union      map[string][]notionViewFormField
	check      func(path string, raw json.RawMessage) string
}

type notionViewFormField struct {
	name string
	form notionViewForm
}

func notionViewFormUndefined(path string) string {
	return notionFaultSaid("body", notionFaultMissing("body.configuration."+path))
}

func notionViewFormShouldBe(path, kind string, value json.RawMessage) string {
	return notionBodyFault("body.configuration."+path, kind, notionViewRefusedValue(value))
}

// notionViewFormList joins what a field may be as Notion's refusal does: "a
// or b", "a, b, or c".
func notionViewFormList(items []string) string {
	switch len(items) {
	case 1:
		return items[0]
	case 2:
		return items[0] + " or " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", or " + items[len(items)-1]
}

// described is what the field may be, in the refusal's words.
func (form notionViewForm) described(kind string) string {
	items := []string{}
	if form.kind == "enum" && kind == "" {
		for _, choice := range form.choices {
			items = append(items, "`"+choice+"`")
		}
	} else {
		if kind == "" {
			kind = map[string]string{
				"boolean": "a boolean", "number": "a number", "integer": "a number", "whole": "a number", "string": "a string",
				"array": "an array", "object": "an object", "union": "an object",
			}[form.kind]
		}
		items = append(items, kind)
	}
	switch form.presence {
	case notionViewFormOptional:
		items = append(items, "`undefined`")
	case notionViewFormNullable:
		items = append(items, "`null`", "`undefined`")
	}
	return notionViewFormList(items)
}

// refusal is Notion's refusal of the field at path, raw as given, or "".
func (form notionViewForm) refusal(path string, raw json.RawMessage, given bool) string {
	raw = bytes.TrimSpace(raw)
	if !given {
		if form.presence == notionViewFormRequired {
			return notionViewFormUndefined(path)
		}
		return ""
	}
	if string(raw) == "null" {
		if form.presence == notionViewFormNullable {
			return ""
		}
		return notionViewFormShouldBe(path, form.described(""), raw)
	}
	wrong := func() string { return notionViewFormShouldBe(path, form.described(""), raw) }
	first := byte(0)
	if len(raw) > 0 {
		first = raw[0]
	}
	switch form.kind {
	case "check":
		return form.check(path, raw)
	case "enum":
		if !slices.Contains(form.choices, notionViewRefusedValue(raw)) {
			return wrong()
		}
	case "boolean":
		if string(raw) != "true" && string(raw) != "false" {
			return wrong()
		}
	case "number", "integer", "whole":
		var number float64
		if json.Unmarshal(raw, &number) != nil {
			return wrong()
		}
		if form.kind == "integer" && number != float64(int64(number)) || form.kind == "whole" && number != math.Trunc(number) {
			return notionViewFormShouldBe(path, form.described("an integer"), raw)
		}
		if form.kind == "integer" && number < 0 {
			return notionViewFormShouldBe(path, form.described("≥ `0`"), raw)
		}
	case "string":
		if first != '"' {
			return wrong()
		}
	case "array":
		items := []json.RawMessage{}
		if first != '[' || json.Unmarshal(raw, &items) != nil {
			return wrong()
		}
		for index, item := range items {
			if form.items == nil {
				break
			}
			if refusal := form.items.refusal(fmt.Sprintf("%s[%d]", path, index), item, true); refusal != "" {
				return refusal
			}
		}
		// Its length after its items, as Notion's (seen 2026-11-12).
		if form.most > 0 && len(items) > form.most {
			return notionBodyFault("body.configuration."+path+".length", fmt.Sprintf("≤ `%d`", form.most), fmt.Sprint(len(items)))
		}
	case "object", "union":
		fields := map[string]json.RawMessage{}
		if first != '{' || json.Unmarshal(raw, &fields) != nil {
			return wrong()
		}
		if form.kind == "object" {
			return notionViewFormFields(path, fields, form.fields)
		}
		kind := notionViewForm{kind: "enum", choices: notionViewFormChoices(form.unionTypes...)}
		typeValue, typed := fields["type"]
		if refusal := kind.refusal(path+".type", typeValue, typed); refusal != "" {
			return refusal
		}
		var name string
		_ = json.Unmarshal(typeValue, &name)
		return notionViewFormFields(path, fields, form.union[name])
	}
	return ""
}

// notionViewFormFields is the refusal of the first of an object's fields out
// of form, in the order its form lists them.
func notionViewFormFields(path string, fields map[string]json.RawMessage, forms []notionViewFormField) string {
	for _, field := range forms {
		at := field.name
		if path != "" {
			at = path + "." + field.name
		}
		raw, given := fields[field.name]
		if refusal := field.form.refusal(at, raw, given); refusal != "" {
			return refusal
		}
	}
	return ""
}

// notionViewFormChoices are words as an enum's choices.
func notionViewFormChoices(words ...string) []string {
	choices := make([]string, len(words))
	for index, word := range words {
		choices[index] = `"` + word + `"`
	}
	return choices
}

func notionViewFormEnum(presence notionViewFormPresence, words ...string) notionViewForm {
	return notionViewForm{kind: "enum", presence: presence, choices: notionViewFormChoices(words...)}
}

func notionViewFormOf(kind string, presence notionViewFormPresence) notionViewForm {
	return notionViewForm{kind: kind, presence: presence}
}

func notionViewFormObject(presence notionViewFormPresence, fields ...notionViewFormField) notionViewForm {
	return notionViewForm{kind: "object", presence: presence, fields: fields}
}

// A view group's form (seen 2026-11-12): its type, one of eighteen, then its
// property and what its type groups by — a status by its groups or options,
// words exactly or by their first letter, a date by its unit — its sort, an
// object with its order, whether it hides its empty groups, and a date's week
// start or a number's ranges; a formula's group sorts within what it groups
// its result by, a date, words, a number or a box, as those do.
func notionViewGroupForm(presence notionViewFormPresence) notionViewForm {
	sort := notionViewFormField{"sort", notionViewFormObject(notionViewFormRequired,
		notionViewFormField{"type", notionViewFormEnum(notionViewFormRequired, notionViewGroupSorts...)})}
	byWords := notionViewFormField{"group_by", notionViewFormEnum(notionViewFormRequired, "exact", "alphabet_prefix")}
	byDate := notionViewFormField{"group_by", notionViewFormEnum(notionViewFormRequired, notionViewDateGroupings...)}
	start := notionViewFormField{"start_day_of_week", notionViewForm{kind: "enum", presence: notionViewFormOptional, choices: []string{"0", "1"}}}
	ranges := []notionViewFormField{
		{"range_start", notionViewFormOf("number", notionViewFormOptional)},
		{"range_end", notionViewFormOf("number", notionViewFormOptional)},
		{"range_size", notionViewFormOf("number", notionViewFormOptional)},
	}
	formula := notionViewForm{kind: "union", presence: notionViewFormRequired, unionTypes: []string{"date", "text", "number", "checkbox"},
		union: map[string][]notionViewFormField{
			"date": {byDate, sort, start}, "text": {byWords, sort}, "number": append([]notionViewFormField{sort}, ranges...), "checkbox": {sort},
		}}
	property := notionViewFormField{"property_id", notionViewFormOf("string", notionViewFormRequired)}
	hide := notionViewFormField{"hide_empty_groups", notionViewFormOf("boolean", notionViewFormOptional)}
	union := map[string][]notionViewFormField{}
	for _, groupType := range notionViewGroupTypes {
		switch groupType {
		case "status":
			union[groupType] = []notionViewFormField{property, {"group_by", notionViewFormEnum(notionViewFormRequired, "group", "option")}, sort, hide}
		case "text", "title", "url", "email", "phone_number":
			union[groupType] = []notionViewFormField{property, byWords, sort, hide}
		case "date", "created_time", "last_edited_time":
			union[groupType] = []notionViewFormField{property, byDate, sort, hide, start}
		case "number":
			union[groupType] = append([]notionViewFormField{property, sort, hide}, ranges...)
		case "formula":
			union[groupType] = []notionViewFormField{property, {"group_by", formula}, hide}
		default:
			union[groupType] = []notionViewFormField{property, sort, hide}
		}
	}
	return notionViewForm{kind: "union", presence: presence, unionTypes: notionViewGroupTypes, union: union}
}
