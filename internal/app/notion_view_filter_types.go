package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A property filter names a kind of filter by its key, which Notion's API
// takes for a property whose values filter as that kind (seen 2026-11-11):
// a title, text, URL, email or phone number as text, by any of their keys or
// a formula's text; a number or an ID as a number; a date or a page
// timestamp as a date; a person or a page's author as a person; a formula
// by the kind of its result, which takes any key but checkbox's for empty or
// not. Any other is refused as Notion's API refuses it, in Leafen's words,
// once the whole request's form is right.

// notionViewFilterKeys are the keys a property filter names its kind by, in
// Notion's order.
var notionViewFilterKeys = []string{
	"title", "rich_text", "number", "checkbox", "select", "multi_select", "status", "date", "people", "files",
	"url", "email", "phone_number", "relation", "created_by", "created_time", "last_edited_by",
	"last_edited_time", "formula", "unique_id", "rollup",
}

// notionViewFirstFilterKey is the first key, in Notion's order, an object
// names a filter's kind by; "" for none.
func notionViewFirstFilterKey(object map[string]json.RawMessage) string {
	for _, name := range notionViewFilterKeys {
		if _, named := object[name]; named {
			return name
		}
	}
	return ""
}

// notionViewKindOfFilter is the kind of filter a key names, as Notion's
// refusals say it; a formula's by the kind inside it.
func notionViewKindOfFilter(key, computedKind string) string {
	switch key {
	case "title", "rich_text", "url", "email", "phone_number":
		return "text"
	case "number", "unique_id":
		return "number"
	case "date", "created_time", "last_edited_time", "last_visited_time":
		return "date"
	case "people", "created_by", "last_edited_by":
		return "person"
	case "files":
		return "file"
	case "formula":
		if computedKind == "string" {
			return "text"
		}
		return computedKind
	}
	return key
}

// notionViewPropertyFilterKind is the kind of filter a property's values
// filter as, a formula's by its result ("" where unknown).
func notionViewPropertyFilterKind(property notionViewProperty) string {
	if property.OfficialType == "formula" {
		return property.FormulaResult
	}
	return notionViewKindOfFilter(property.OfficialType, "")
}

// notionViewDatabasePropertyName is a property's type as Notion's refusal
// names it.
func notionViewDatabasePropertyName(officialType string) string {
	switch officialType {
	case "rich_text":
		return "text"
	case "people":
		return "person"
	case "files":
		return "file"
	case "unique_id":
		return "auto_increment_id"
	}
	return officialType
}

// notionViewFilterKindFits refuses a filter whose kind isn't the kind its property's
// values filter as, naming both kinds as Notion's refusal does (seen
// 2026-11-11). A formula of unknown result takes any.
func notionViewFilterKindFits(property notionViewProperty, key, computedKind, condition string) error {
	filterKind := notionViewKindOfFilter(key, computedKind)
	if property.OfficialType == "formula" {
		result := property.FormulaResult
		emptiness := condition == "is_empty" || condition == "is_not_empty"
		if result == "" || filterKind == result || emptiness && result != "checkbox" && key != "rollup" {
			return nil
		}
		return notionViewFilterInvalid("This filter doesn't fit the formula's result: the formula gives %s values and the filter is for %s values", result, filterKind)
	}
	if filterKind == notionViewPropertyFilterKind(property) {
		return nil
	}
	return notionViewFilterInvalid("This filter doesn't fit the property: the database property is %s and the filter is for %s values",
		notionViewDatabasePropertyName(property.OfficialType), filterKind)
}

// notionViewFormulaResult is the kind of result a formula gives, as Notion's
// filters name it — text, number, checkbox or date — read from its
// expression; "" where the expression doesn't tell, as a let's name or an
// if() whose results differ. One that gives nothing but empty() reads as
// text, as Notion's (seen 2026-11-20: empty(), if(false, empty(), empty())
// and a formula naming such a formula are {"type":"string","string":null}
// and filter as "formula text").
func notionViewFormulaResult(expression string, propertyKind func(name string) string) string {
	return notionViewFormulaKnownKind(notionViewFormulaKind(expression, propertyKind))
}

// notionViewFormulaKind is notionViewFormulaResult's kind before an empty()
// that is all a formula gives reads as text: notionViewFormulaNoKind, which
// a formula naming this one takes as the kind of what it gives besides, as
// Notion's (seen 2026-11-20: if(false, 1, prop("E")) of an E that is
// empty() is {"type":"number","number":null}).
func notionViewFormulaKind(expression string, propertyKind func(name string) string) string {
	node, err := parseDatabaseFormula(expression)
	if err != nil || node == nil {
		return ""
	}
	return notionViewFormulaNodeResult(node, propertyKind, 0)
}

// notionViewFormulaNoKind is the kind of empty() with nothing to test: none
// of its own, Notion's "undefined" (notionFormulaUndefined). Where a formula
// gives it or another value — an if()'s or ifs()'s results, a list's items —
// it gives that value's kind, as Notion's (seen 2026-11-20 through its API:
// if(false, 1, empty()), if(true, empty(), 1), ifs(false, 1, empty()) and
// let(x, empty(), if(false, 1, x)) are {"type":"number","number":null},
// if(false, now(), empty()) a date's, if(false, true, empty()) a boolean's,
// and each filters as its kind).
const notionViewFormulaNoKind = "\x00none"

// notionViewFormulaKnownKind is a kind as a formula's result goes out: text
// for one that gives nothing but empty().
func notionViewFormulaKnownKind(kind string) string {
	if kind == notionViewFormulaNoKind {
		return "text"
	}
	return kind
}

var notionViewFormulaFunctionResults = func() map[string]string {
	results := map[string]string{}
	for result, names := range map[string][]string{
		"number": {"abs", "round", "ceil", "floor", "sqrt", "cbrt", "exp", "ln", "log10", "log2", "sign", "pow", "mod",
			"add", "subtract", "multiply", "divide", "min", "max", "sum", "mean", "median", "toNumber", "length",
			"year", "month", "week", "day", "date", "hour", "minute", "dateBetween", "timestamp", "pi", "e"},
		"text": {"format", "upper", "lower", "replace", "replaceAll", "substring", "join", "repeat", "padStart",
			"padEnd", "trim", "formatDate", "style", "unstyle", "link", "concat"},
		"date": {"now", "today", "dateAdd", "dateSubtract", "dateRange", "parseDate", "fromTimestamp", "dateStart", "dateEnd"},
		"checkbox": {"empty", "contains", "test", "and", "or", "not", "equal", "unequal", "larger", "largerEq",
			"smaller", "smallerEq", "includes", "every", "some", "toBoolean"},
	} {
		for _, name := range names {
			results[name] = result
		}
	}
	return results
}()

func notionViewFormulaNodeResult(node *databaseFormulaNode, propertyKind func(string) string, depth int) string {
	if node == nil || depth > 64 {
		return ""
	}
	child := func(index int) string {
		if index >= len(node.children) {
			return ""
		}
		return notionViewFormulaNodeResult(node.children[index], propertyKind, depth+1)
	}
	same := func(left, right string) string {
		switch {
		case left == right || right == notionViewFormulaNoKind:
			return left
		case left == notionViewFormulaNoKind:
			return right
		}
		return ""
	}
	switch node.kind {
	case databaseFormulaNodeNumber:
		return "number"
	case databaseFormulaNodeString:
		return "text"
	case databaseFormulaNodeIdentifier:
		if node.text == "true" || node.text == "false" {
			return "checkbox"
		}
		// A name let() or lets() binds is of its value's kind (notionViewFormulaLetResult).
		return propertyKind(notionViewFormulaBoundName + node.text)
	case databaseFormulaNodeUnary:
		if node.text == "-" || node.text == "+" {
			return "number"
		}
		return "checkbox"
	case databaseFormulaNodeBinary:
		switch node.text {
		case "-", "*", "/", "%", "^":
			return "number"
		case "+":
			left, right := child(0), child(1)
			if left == "text" || right == "text" {
				return "text"
			}
			return same(left, right)
		}
		return "checkbox"
	case databaseFormulaNodeTernary:
		return same(child(1), child(2))
	case databaseFormulaNodeCall:
		switch node.text {
		case "prop":
			if len(node.children) == 1 && node.children[0].kind == databaseFormulaNodeString {
				return propertyKind(node.children[0].text)
			}
			return ""
		case "if":
			return same(child(1), child(2))
		}
		return notionViewFormulaCallResult(node, propertyKind, depth)
	case databaseFormulaNodeMethod:
		if node.text == "prop" {
			return ""
		}
		return notionViewFormulaCallResult(node, propertyKind, depth)
	}
	return ""
}

// notionViewFormulaCallResult is a function's result kind: an item a list
// gives is of its items' kind and ifs() of its results', even when it gives
// nothing (Notion's API, seen 2026-11-12: at([1, 2], 5), [1, 2].find(current
// > 5) and ifs(false, 1) are each a number with no value).
func notionViewFormulaCallResult(node *databaseFormulaNode, propertyKind func(string) string, depth int) string {
	switch node.text {
	case "at", "first", "last", "find":
		if len(node.children) == 0 {
			return ""
		}
		return notionViewFormulaItemResult(node.children[0], propertyKind, depth+1)
	case "let", "lets":
		return notionViewFormulaLetResult(node, propertyKind, depth)
	case "ifs":
		results := []*databaseFormulaNode{}
		for index := 1; index < len(node.children); index += 2 {
			results = append(results, node.children[index])
		}
		if len(node.children)%2 == 1 {
			results = append(results, node.children[len(node.children)-1])
		}
		return notionViewFormulaSameResult(results, propertyKind, depth+1)
	case "empty":
		// With nothing to test it gives nothing, of no kind; with a value,
		// whether that is empty (a method's own value is its first).
		if len(node.children) == 0 {
			return notionViewFormulaNoKind
		}
	}
	return notionViewFormulaFunctionResults[node.text]
}

// notionViewFormulaBoundName marks a name let() or lets() binds where a
// formula's kinds are looked up, apart from the properties' names.
const notionViewFormulaBoundName = "\x00"

// notionViewFormulaLetResult is the kind of what let() or lets() gives: its
// last value's, each name it binds of its value's kind, as Notion's (seen
// 2026-11-14: let(x, prop("d"), x) and lets(a, 1, b, prop("d"), b) of an
// empty d are {"type":"date","date":null}).
func notionViewFormulaLetResult(node *databaseFormulaNode, propertyKind func(string) string, depth int) string {
	if len(node.children)%2 == 0 {
		return ""
	}
	bound := map[string]string{}
	kind := func(name string) string {
		if result, found := bound[name]; found {
			return result
		}
		return propertyKind(name)
	}
	for index := 0; index+1 < len(node.children); index += 2 {
		name := node.children[index]
		if name.kind != databaseFormulaNodeIdentifier {
			return ""
		}
		bound[notionViewFormulaBoundName+name.text] = notionViewFormulaNodeResult(node.children[index+1], kind, depth+1)
	}
	return notionViewFormulaNodeResult(node.children[len(node.children)-1], kind, depth+1)
}

// notionViewFormulaItemResult is the kind of a list's items: a written
// list's when they are all of one kind, and the items a list function keeps
// are its list's.
func notionViewFormulaItemResult(node *databaseFormulaNode, propertyKind func(string) string, depth int) string {
	if node == nil || depth > 64 {
		return ""
	}
	switch node.kind {
	case databaseFormulaNodeList:
		return notionViewFormulaSameResult(node.children, propertyKind, depth+1)
	case databaseFormulaNodeCall, databaseFormulaNodeMethod:
		switch node.text {
		case "filter", "sort", "reverse", "slice", "unique":
			if len(node.children) > 0 {
				return notionViewFormulaItemResult(node.children[0], propertyKind, depth+1)
			}
		}
	}
	return ""
}

// notionViewFormulaSameResult is the kind the nodes all have, those of no
// kind (empty()) aside, or "".
func notionViewFormulaSameResult(nodes []*databaseFormulaNode, propertyKind func(string) string, depth int) string {
	kind := ""
	for _, node := range nodes {
		next := notionViewFormulaNodeResult(node, propertyKind, depth)
		switch {
		case next == "":
			return ""
		case next == notionViewFormulaNoKind:
			if kind == "" {
				kind = next
			}
		case kind == "" || kind == notionViewFormulaNoKind:
			kind = next
		case next != kind:
			return ""
		}
	}
	return kind
}

// notionViewDateOneOperatorRefusal is Notion's refusal of a date condition
// naming more than one of its conditions, at the property filter's path
// (body.filter, body.filter.and[0]): what each kind of filter there should
// be, the date's saying it takes one, a top-level filter's compounds first,
// a nested one's last (seen 2026-11-11).
func notionViewDateOneOperatorRefusal(path, key string, raw json.RawMessage) error {
	alternatives := append(append([]string{}, notionViewFilterKeys...), "verification")
	if path == "body.filter" {
		alternatives = append([]string{"or", "and"}, alternatives...)
	} else if !strings.HasPrefix(path, "body.quick_filters") {
		alternatives = append(alternatives, "or", "and")
	}
	lines := []string{}
	for _, alternative := range alternatives {
		if alternative == key {
			lines = append(lines, fmt.Sprintf("%s.%s takes a date condition with one operator, not `%s`; for a date range, "+
				"put each bound in a filter of its own inside an `and` filter.", path, key, notionViewRefusedValue(raw)))
			continue
		}
		lines = append(lines, notionFaultMissing(path+"."+alternative))
	}
	return notionViewFilterInvalid("%s", notionFaultChoicesSaid("body", lines...))
}

// notionViewDateConditionCount is how many of a date's conditions an object
// names.
func notionViewDateConditionCount(raw json.RawMessage) int {
	conditions := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &conditions)
	count := 0
	for _, condition := range notionViewConditionLists["date"] {
		if _, named := conditions[condition.name]; named {
			count++
		}
	}
	return count
}

// notionAPIFilterDepth is how deep a filter given through the API nests its
// "and" and "or": a compound in the top one and no deeper, as Notion's API
// takes it (seen 2026-11-19: a third level, in a query or a view, is a 400
// validation_error), where its screen nests three (maxDatabaseQueryFilterDepth).
const notionAPIFilterDepth = 2
