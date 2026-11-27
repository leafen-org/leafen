package app

import "strings"

// The check knows a list's items where they share a type, and the pages and
// people a formula holds, as Notion's does (seen 2026-11-21 through its API
// and editor, a synthetic database under 한지테스트페이지): [1, 2].first() > "a"
// is a comparison of number and text, ["a"].map(current * 2) math on text
// and number, a relation's page is "block" (math on block and number), a
// person "person"; a list is "array" whatever its items (a comparison of
// array and text), [1, "a"] has none known. A list function refuses what
// isn't a list, as its target or as a value (first() on text), join() only
// as a method's; sum() and its kin a list of what isn't numbers (sum() on an
// array of texts); id() takes a page or a person, name() and email() a
// person. Pages, people and lists don't compare (block with block). A list's
// type is "array:" and its items'.

const notionFormulaListPrefix = "array:"

// notionFormulaListOf is the type of a list whose items are of item ("" for
// none known).
func notionFormulaListOf(item string) string {
	if item == "" {
		return "array"
	}
	return notionFormulaListPrefix + item
}

// notionFormulaAlike is the type a list's items all have, "" where they
// differ — an undefined one among numbers too ([1, empty()].first() > "a" is
// taken, 2026-11-21).
func notionFormulaAlike(items []string) string {
	for _, item := range items {
		if item != items[0] {
			return ""
		}
	}
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

// notionFormulaIsList is whether a type is a list's.
func notionFormulaIsList(kind string) bool {
	return kind == "array" || strings.HasPrefix(kind, notionFormulaListPrefix)
}

// notionFormulaItem is the type of a list's items, "" where not known.
func notionFormulaItem(kind string) string {
	if strings.HasPrefix(kind, notionFormulaListPrefix) {
		return kind[len(notionFormulaListPrefix):]
	}
	return ""
}

// notionFormulaSaid is a type as Notion's messages name it.
func notionFormulaSaid(kind string) string {
	if notionFormulaIsList(kind) {
		return "array"
	}
	if _, page := notionFormulaPageSource(kind); page {
		return "block"
	}
	return kind
}

// notionFormulaUncomparable is whether values of a type don't compare even
// with their own.
func notionFormulaUncomparable(kind string) bool {
	_, page := notionFormulaPageSource(kind)
	return notionFormulaIsList(kind) || page || kind == "person"
}

// notionFormulaListResult is what a list function gives, from its values'
// types (a method's own value its first), where the list it reads tells it.
func notionFormulaListResult(name string, types []string) (string, bool) {
	first := ""
	if len(types) > 0 {
		first = types[0]
	}
	switch name {
	case "first", "last", "at", "find":
		return notionFormulaItem(first), true
	case "filter", "sort", "reverse", "slice", "unique", "splice":
		if notionFormulaIsList(first) {
			return first, true
		}
	case "map":
		if len(types) > 1 {
			return notionFormulaListOf(types[len(types)-1]), true
		}
	case "flat":
		// A list of lists gives their items; of anything else, itself.
		if item := notionFormulaItem(first); notionFormulaIsList(item) {
			return item, true
		} else if notionFormulaIsList(first) && item == "" {
			return "array", true
		} else if notionFormulaIsList(first) {
			return first, true
		}
	case "concat":
		shared := notionFormulaShared(types...)
		if notionFormulaIsList(shared) {
			return shared, true
		}
	case "split", "match":
		return notionFormulaListOf("text"), true
	}
	return "", false
}

// What each function's values must be, by place: a number ("n"), a date
// ("d"), a list ("l"; "m" only as a method's own value), a page or a person
// ("p"), a person ("P"); the rest take anything. The functions over numbers
// take lists of them too.
var notionFormulaParameters = map[string]string{
	"abs": "n", "round": "nn", "ceil": "n", "floor": "n", "sqrt": "n", "cbrt": "n", "exp": "n", "ln": "n",
	"log10": "n", "log2": "n", "sign": "n", "pow": "nn", "mod": "nn", "subtract": "nn", "multiply": "nn",
	"divide": "nn", "dateAdd": "dn", "dateSubtract": "dn", "dateBetween": "dd", "dateRange": "dd",
	"dateStart": "d", "dateEnd": "d", "formatDate": "d", "timestamp": "d", "year": "d", "month": "d",
	"week": "d", "date": "d", "day": "d", "hour": "d", "minute": "d", "nextWorkday": "d", "addWorkdays": "dn",
	"id": "p", "name": "P", "email": "P", "add": "nn", "slice": "l", "first": "l", "last": "l", "at": "l",
	"find": "l", "findIndex": "l", "filter": "l", "some": "l", "every": "l", "map": "l", "sort": "l",
	"reverse": "l", "unique": "l", "includes": "l", "flat": "l", "count": "l", "splice": "l", "join": "m",
}

// notionFormulaArgumentProblem is what Notion names a function's value at
// index of a type it can't take, or "" where it fits.
func notionFormulaArgumentProblem(name string, index int, kind string, method bool) string {
	parameters := notionFormulaParameters[name]
	wanted := byte(0)
	switch {
	case notionFormulaOverNumbers[name]:
		if item := notionFormulaItem(kind); notionFormulaIsList(kind) && (item == "" || item == "number") {
			return ""
		}
		wanted = 'n'
	case notionFormulaOverLists[name]:
		wanted = 'l'
	case index < len(parameters):
		wanted = parameters[index]
	}
	if wanted == 'm' {
		wanted = 0
		if method && index == 0 {
			wanted = 'l'
		}
	}
	fits := wanted == 'n' && kind == "number" || wanted == 'd' && kind == "date" || wanted == 'l' && notionFormulaIsList(kind) ||
		wanted == 'p' && (notionFormulaSaid(kind) == "block" || kind == "person") || wanted == 'P' && kind == "person"
	if wanted == 0 || kind == "" || fits {
		return ""
	}
	if method && index == 0 {
		return name + "() doesn't work on " + notionFormulaSaid(kind) + " values."
	}
	return name + "() can't take " + notionFormulaSaid(kind) + " values here."
}

// notionFormulaPropertyType is the type a property's values have in a
// formula: a relation's a list of pages, a people property's of people, a
// multi-select's or files' of texts, a page's author a person.
func notionFormulaPropertyType(nativeType string) string {
	switch nativeType {
	case graphValueTypeNumber:
		return "number"
	case graphValueTypeCheckbox:
		return "boolean"
	case graphValueTypeDate, graphValueTypeCreatedTime, graphValueTypeLastEditedTime:
		return "date"
	case graphValueTypeRelation:
		return notionFormulaListOf("block")
	case graphValueTypePerson:
		return notionFormulaListOf("person")
	case graphValueTypeMultiSelect, graphValueTypeFiles:
		return notionFormulaListOf("text")
	case graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		return "person"
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone,
		graphValueTypeSelect, graphValueTypeStatus, graphValueTypeUniqueID:
		return "text"
	}
	return ""
}
