package app

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Notion's API checks a formula a request writes against the data source as
// the request leaves it, and refuses one with type errors as
// `Type error in formula "<name>": <each problem>`, the problems in the order
// they stand, each a sentence (seen 2026-11-12, API 2026-03-11); the
// sentences are Leafen's own, the kinds and their order Notion's: a name no
// function, let or list function gives (foo, or current outside map() and
// its kin), a property prop() names that the source hasn't by that name, a
// call with too few or too many values (a method's own value not counted),
// a method no function has, and a value of a type an operation, a
// comparison or a function can't take (math on text and number, a
// comparison of number and text — lists never compare —, a minus on text,
// each value of a call that doesn't fit, a method's target that doesn't
// fit, a value before parentheses that isn't a function), the same sentence
// once. A function its first kind had and today's hasn't is named so, with
// the operator that took its place or its new name (larger() and >(),
// start() and dateStart()). A formula's
// or rollup's property is of its result's type (notion_formula_property_kinds.go),
// a list's items, a page and a person of theirs (notion_formula_items.go); a
// value whose type it can't know — items of more than one type, a related
// page's property, a block_property reference — fits anything. A function's text
// values take anything ("upper(1)"). Where today's check fails Notion tries
// the formula as one of its first kind (notion_formula_dialect.go), whose
// rules the check keeps with legacy.

// notionFormulaRequestField holds, in a formula's planned config, the
// formula as the request wrote it and the name its messages give, for the
// check once the request's properties are known (checkNotionFormulaTypes).
const notionFormulaRequestField = "notionFormulaRequest"

const notionFormulaAnyCount = -1

// notionFormulaPatternFunctions are the functions whose second value is a
// pattern.
var notionFormulaPatternFunctions = map[string]bool{"test": true, "match": true, "replace": true, "replaceAll": true}

// notionFormulaArity is how many values each function takes, the fewest and
// the most, as Notion's checks a call (web formulaArity.ts); a function
// Notion reads with none as a failed call takes from none.
var notionFormulaArity = map[string][2]int{
	"if": {3, 3}, "ifs": {2, notionFormulaAnyCount}, "and": {0, notionFormulaAnyCount}, "or": {0, notionFormulaAnyCount},
	"not": {0, notionFormulaAnyCount}, "empty": {0, 1}, "length": {1, 1}, "format": {1, 1}, "equal": {2, 2}, "unequal": {2, 2},
	"let": {0, notionFormulaAnyCount}, "lets": {0, notionFormulaAnyCount}, "substring": {2, 3}, "contains": {2, 2},
	"test": {2, 2}, "match": {2, 2}, "replace": {2, 3}, "replaceAll": {2, 3}, "lower": {1, 1}, "upper": {1, 1},
	"trim": {1, 1}, "repeat": {2, 2}, "padStart": {3, 3}, "padEnd": {3, 3}, "link": {2, 2},
	"style": {1, notionFormulaAnyCount}, "unstyle": {1, notionFormulaAnyCount}, "concat": {0, notionFormulaAnyCount},
	"join": {2, 2}, "split": {1, 2}, "startsWith": {2, 2}, "endsWith": {2, 2}, "formatNumber": {1, 3},
	"add": {2, 2}, "subtract": {2, 2}, "multiply": {2, 2}, "mod": {2, 2}, "pow": {2, 2}, "divide": {2, 2},
	"min": {0, notionFormulaAnyCount}, "max": {0, notionFormulaAnyCount}, "sum": {0, notionFormulaAnyCount},
	"median": {0, notionFormulaAnyCount}, "mean": {0, notionFormulaAnyCount}, "abs": {1, 1},
	"round": {0, notionFormulaAnyCount}, "ceil": {1, 1}, "floor": {1, 1}, "sqrt": {1, 1}, "cbrt": {1, 1},
	"exp": {1, 1}, "ln": {1, 1}, "log10": {1, 1}, "log2": {1, 1}, "sign": {1, 1}, "pi": {0, 0}, "e": {0, 0},
	"toNumber": {1, 1}, "now": {0, 0}, "today": {0, 0}, "minute": {1, 1}, "hour": {1, 1}, "day": {1, 1},
	"date": {1, 1}, "week": {1, 1}, "month": {1, 1}, "year": {1, 1}, "dateAdd": {3, 3}, "dateSubtract": {3, 3},
	"dateBetween": {3, 3}, "dateRange": {2, 2}, "dateStart": {1, 1}, "dateEnd": {1, 1}, "timestamp": {1, 1},
	"fromTimestamp": {1, 1}, "formatDate": {2, 3}, "parseDate": {1, 1}, "nextWorkday": {1, 2},
	"addWorkdays": {2, 3}, "name": {1, 1}, "email": {1, 1}, "at": {2, 2}, "first": {1, 1}, "last": {1, 1},
	"slice": {2, 3}, "splice": {2, notionFormulaAnyCount}, "sort": {1, 2}, "reverse": {1, 1}, "unique": {1, 1},
	"includes": {2, 2}, "find": {2, 2}, "findIndex": {2, 2}, "filter": {2, 2}, "some": {2, 2}, "every": {2, 2},
	"map": {2, 2}, "flat": {1, 1}, "count": {1, 2}, "id": {0, 1},
}

// The functions whose every value is a list, as today's concat().
var notionFormulaOverLists = map[string]bool{"concat": true}

// What a function today's formulas no longer have became: the operator
// that took its place, or its new name.
var notionFormulaRemoved = map[string]string{
	"larger": "Formulas no longer have “larger()”; use >() instead.", "largerEq": "Formulas no longer have “largerEq()”; use >=() instead.",
	"smaller": "Formulas no longer have “smaller()”; use <() instead.", "smallerEq": "Formulas no longer have “smallerEq()”; use <=() instead.",
	"unaryMinus": "Formulas no longer have “unaryMinus()”; use -() instead.", "unaryPlus": "Formulas no longer have “unaryPlus()”; use +() instead.",
	"start": "“start()” is called “dateStart()” now.", "end": "“end()” is called “dateEnd()” now.",
}

var notionFormulaOverNumbers = map[string]bool{"sum": true, "min": true, "max": true, "mean": true, "median": true}

// The functions whose last value runs for each item with current and index.
var notionFormulaEachItem = map[string]bool{
	"map": true, "filter": true, "find": true, "findIndex": true, "some": true, "every": true, "count": true, "sort": true,
}

// What each function gives, where its type is known.
var notionFormulaReturns = func() map[string]string {
	returns := map[string]string{}
	for types, names := range map[string]string{
		"boolean": "and or not empty equal unequal contains test startsWith endsWith includes some every",
		"text":    "format substring replace replaceAll lower upper trim repeat padStart padEnd link style unstyle join formatNumber formatDate name email id",
		"number": "length add subtract multiply mod pow divide min max sum median mean abs round ceil floor sqrt cbrt exp ln log10 log2 " +
			"sign pi e toNumber minute hour day date week month year dateBetween timestamp findIndex count",
		"date":  "now today dateAdd dateSubtract dateRange dateStart dateEnd fromTimestamp parseDate nextWorkday addWorkdays",
		"array": "match concat split slice splice sort reverse unique filter map flat",
	} {
		for _, name := range strings.Fields(names) {
			returns[name] = types
		}
	}
	return returns
}()

type notionFormulaChecker struct {
	// properties are the source's, by name, with their types in a formula.
	properties map[string]string
	problems   []string
	// legacy checks by the rules of Notion's first kind of formula.
	legacy bool
	// pages gives a page's properties (notion_formula_pages.go); none, none checked.
	pages notionFormulaPages
	// lost is that a property named before wasn't found (notionFormulaNamed).
	lost bool
}

func (checker *notionFormulaChecker) problem(text string) {
	checker.problemBefore(len(checker.problems), text)
}

// problemBefore notes a problem before those from at on, the same sentence
// once, where it first stands.
func (checker *notionFormulaChecker) problemBefore(at int, text string) {
	if text == "" {
		return
	}
	for index, noted := range checker.problems {
		if noted != text {
			continue
		}
		if index < at {
			return
		}
		checker.problems = append(checker.problems[:index], checker.problems[index+1:]...)
		break
	}
	checker.problems = append(checker.problems[:at], append([]string{text}, checker.problems[at:]...)...)
}

// check gives a node's type, "" where it can't be known, noting its problems.
func (checker *notionFormulaChecker) check(node *databaseFormulaNode, scope map[string]string) string {
	switch node.kind {
	case databaseFormulaNodeNumber:
		return "number"
	case databaseFormulaNodeString:
		return "text"
	case databaseFormulaNodeIdentifier:
		return checker.name(node.text, scope)
	case databaseFormulaNodeList:
		items := make([]string, 0, len(node.children))
		for _, item := range node.children {
			items = append(items, checker.check(item, scope))
		}
		return notionFormulaListOf(notionFormulaAlike(items))
	case databaseFormulaNodeUnary:
		operand := checker.check(node.children[0], scope)
		if node.text == "!" {
			return "boolean"
		}
		if operand != "" && operand != "number" {
			checker.problem(notionFormulaSaid(operand) + " values can't take a minus sign.")
		}
		return "number"
	case databaseFormulaNodeBinary:
		return checker.binary(node, scope)
	case databaseFormulaNodeTernary:
		checker.check(node.children[0], scope)
		whenTrue, whenFalse := checker.check(node.children[1], scope), checker.check(node.children[2], scope)
		if checker.legacy && whenTrue != "" && whenFalse != "" && whenTrue != whenFalse {
			checker.problem("The two results to choose from must share a type, not " + notionFormulaSaid(whenTrue) + " and " + notionFormulaSaid(whenFalse) + ".")
		}
		return notionFormulaShared(whenTrue, whenFalse)
	case databaseFormulaNodeCall:
		return checker.call(node.text, node.children, false, scope)
	case databaseFormulaNodeMethod:
		return checker.call(node.text, node.children, true, scope)
	}
	return ""
}

func (checker *notionFormulaChecker) name(name string, scope map[string]string) string {
	if kind, bound := scope[name]; bound {
		return kind
	}
	switch {
	case name == "true" || name == "false":
		return "boolean"
	case notionFormulaBlockReference(name):
		return ""
	case name == "pi" || name == "e":
		return "number"
	}
	if _, function := databaseFormulaFunctionNames[name]; function {
		return ""
	}
	checker.problem(name + " is not defined.")
	return ""
}

func (checker *notionFormulaChecker) binary(node *databaseFormulaNode, scope map[string]string) string {
	left := checker.check(node.children[0], scope)
	for _, operand := range node.children[1:] {
		left = checker.operation(node.text, left, checker.check(operand, scope))
	}
	return left
}

// operation is the type an operator gives its two values, noting what they
// can't be.
func (checker *notionFormulaChecker) operation(operator, left, right string) string {
	known := left != "" && right != ""
	switch operator {
	case notionFormulaCalledValue:
		kind := left
		if kind == "" {
			kind = "unknown"
		}
		checker.problem(notionFormulaSaid(kind) + " values can't be called like a function.")
		return ""
	case "-", "*", "/", "%", "^":
		if known && (left != "number" || right != "number") {
			checker.problem("Math doesn't work between " + notionFormulaSaid(left) + " and " + notionFormulaSaid(right) + " values.")
		}
		return "number"
	case ">", ">=", "<", "<=":
		if known && (left != right || notionFormulaUncomparable(left)) {
			checker.problem(notionFormulaSaid(left) + " and " + notionFormulaSaid(right) + " values can't be compared.")
		}
		return "boolean"
	case "+":
		// The first kind joins two texts or adds two numbers, nothing else.
		if checker.legacy && known && (left != right || left != "text" && left != "number") {
			checker.problem(notionFormulaSaid(left) + " and " + notionFormulaSaid(right) + " values can't be added together.")
		}
		switch {
		case left == "text" || right == "text":
			return "text"
		case left == "number" && right == "number":
			return "number"
		case known && !checker.legacy:
			// Today's joins anything else as text (Notion's API, seen 2026-11-21:
			// (now() + 1) * 2, (true + 1) * 2 and a date property + 1, - 1 are
			// math on text and number; (if(true, 1, "a") + 1) * 2 is taken).
			return "text"
		}
		return ""
	}
	return "boolean"
}

// call checks a function's call, or a method's — the first of values its
// receiver — whose own value its counts and messages leave out.
func (checker *notionFormulaChecker) call(name string, values []*databaseFormulaNode, method bool, scope map[string]string) string {
	_, known := databaseFormulaFunctionNames[name]
	if !method && name == "" {
		// Joined texts in parentheses (notionFormulaConvert).
		return checker.check(values[0], scope)
	}
	if method && name == "prop" {
		// A related page's property, which its own source has.
		return checker.pageProperty(values, scope)
	}
	if !known && !method {
		removed := ""
		if became, gone := notionFormulaRemoved[name]; gone {
			removed = " " + became
		}
		checker.problem(name + " is not defined." + removed)
	}
	var types []string
	if method {
		// A method's target is named after what is inside it.
		types = append(types, checker.check(values[0], scope))
		if known {
			checker.problem(notionFormulaArgumentProblem(name, 0, types[0], method))
		}
	}
	if !known && method {
		checker.problem("Cannot find function " + name + "().")
	}
	switch {
	case !method && name == "prop":
		return checker.property(values, scope)
	case !method && (name == "let" || name == "lets"):
		return checker.let(name, values, scope)
	}
	arity, counted := notionFormulaArity[name]
	opened := len(checker.problems)
	// A value that doesn't fit, or one too many, is named before what is inside it, value by value, and a call short of
	// values before them all, as Notion's (seen 2026-11-14 and 2026-11-28: dateAdd(abs("a"), 1, "days") names dateAdd's
	// value, then abs's; abs(2, 1 - "a") the extra value, then the math; dateBetween(1, "a") the count, then 1 and "a").
	for index := len(types); index < len(values); index++ {
		inner := scope
		if notionFormulaEachItem[name] && index == 1 {
			inner = notionFormulaWith(scope, map[string]string{"current": notionFormulaItem(types[0]), "index": "number"})
		}
		before := len(checker.problems)
		kind := checker.check(values[index], inner)
		types = append(types, kind)
		if !known {
			continue
		}
		if counted && arity[1] != notionFormulaAnyCount && index >= arity[1] {
			checker.problemBefore(before, "Too many arguments for "+name+"().")
		}
		checker.problemBefore(before, notionFormulaArgumentProblem(name, index, kind, method))
	}
	if !known {
		return ""
	}
	given, offset := len(values), 0
	if method {
		offset = 1
	}
	if counted && given < arity[0] {
		checker.problemBefore(opened, name+"() is short of arguments: it needs "+strconv.Itoa(arity[0]-offset)+" and has "+strconv.Itoa(given-offset)+".")
	}
	// A pattern written in the formula that repeats a repetition is refused,
	// as Notion's API refuses it (seen 2026-11-14: test("aaa", "(a+)+"),
	// "(a+)?", "(?:a+)+"); one the formula works out fails where it runs
	// (databaseFormulaPattern), as the editor's (formulaTypes.ts).
	if notionFormulaPatternFunctions[name] && len(values) > 1 && values[1].kind == databaseFormulaNodeString &&
		databaseFormulaUnsafePattern(values[1].text) {
		checker.problem("Regular expressions can't repeat a group that already repeats, such as (a+)+.")
	}
	if checker.legacy && name == "concat" {
		// The first kind's concat() joins texts alone (notionFormulaConvert).
		checker.problem("concat() can't take these values here.")
	}
	switch name {
	case "if":
		if len(types) == 3 {
			return notionFormulaShared(types[1], types[2])
		}
		return ""
	case "ifs":
		results := []string{}
		for index := 1; index < len(types); index += 2 {
			results = append(results, types[index])
		}
		if len(types)%2 == 1 {
			results = append(results, types[len(types)-1])
		}
		return notionFormulaShared(results...)
	case "empty":
		return notionFormulaEmptyType(values, method)
	}
	if kind, listed := notionFormulaListResult(name, types); listed {
		return kind
	}
	return notionFormulaReturns[name]
}

func (checker *notionFormulaChecker) property(values []*databaseFormulaNode, scope map[string]string) string {
	if len(values) == 1 && values[0].kind == databaseFormulaNodeString {
		kind, exists := checker.properties[values[0].text]
		return checker.named(values[0].text, kind, exists)
	}
	for _, value := range values {
		checker.check(value, scope)
	}
	return ""
}

// let checks let(name, value, body) and lets(name, value, …, body): each
// name is the type of its value in what follows. Without its three values,
// let's are values like any other ("let(x)": x is not defined).
func (checker *notionFormulaChecker) let(name string, values []*databaseFormulaNode, scope map[string]string) string {
	if name == "let" && (len(values) < 3 || values[0].kind != databaseFormulaNodeIdentifier) {
		for _, value := range values {
			checker.check(value, scope)
		}
		return ""
	}
	if len(values) == 0 {
		return ""
	}
	inner := scope
	last := len(values) - 1
	if name == "let" {
		last = 2
	}
	for index := 0; index+1 < last; index += 2 {
		if values[index].kind != databaseFormulaNodeIdentifier {
			checker.check(values[index], inner)
		}
		kind := checker.check(values[index+1], inner)
		if values[index].kind == databaseFormulaNodeIdentifier {
			inner = notionFormulaWith(inner, map[string]string{values[index].text: kind})
		}
	}
	result := checker.check(values[last], inner)
	for _, extra := range values[last+1:] {
		checker.check(extra, inner)
	}
	return result
}

func notionFormulaWith(scope map[string]string, names map[string]string) map[string]string {
	next := make(map[string]string, len(scope)+len(names))
	for name, kind := range scope {
		next[name] = kind
	}
	for name, kind := range names {
		next[name] = kind
	}
	return next
}

// notionFormulaKept is the formula Leafen keeps for one a request writes —
// as written where it reads and checks as a formula of today, its text in
// double quotes, else as Notion rewrites one of its first kind — or Notion's
// refusal of it.
func notionFormulaKept(expression, label string, properties map[string]string, pages notionFormulaPages) (string, error) {
	reading := notionFormulaRead(expression)
	if expression == "" {
		return expression, nil
	}
	written, problems := expression, []string(nil)
	if reading.strict != nil {
		checker := &notionFormulaChecker{properties: properties, pages: pages}
		checker.check(reading.strict, map[string]string{})
		problems = checker.problems
	}
	switch {
	case reading.strict != nil && len(problems) == 0:
	case reading.strict != nil:
		migrated, ok := notionFormulaMigrated(reading.strict, reading.strictTokens, properties)
		if !ok {
			return "", notionDatabaseRowPageRequestInvalid("Type error in formula \"%s\": %s", label, notionFormulaProblemsSaid(problems))
		}
		written = migrated
	case reading.legacy != nil:
		migrated, ok := notionFormulaMigrated(reading.legacy, reading.legacyTokens, properties)
		if !ok {
			return "", notionDatabaseRowPageRequestInvalid("Parse error in formula \"%s\": %s", label, reading.fault)
		}
		written = migrated
	default:
		return "", notionDatabaseRowPageRequestInvalid("Parse error in formula \"%s\": %s", label, reading.fault)
	}
	// Text in single quotes, which Notion's API reads, is kept in double
	// ones, as Notion keeps it (database_formula_double_quoted.go).
	written, _ = databaseFormulaDoubleQuoted(written)
	if fault := notionFormulaLeafenFault(written); fault != "" {
		return "", notionDatabaseRowPageRequestInvalid("Parse error in formula \"%s\": %s", label, fault)
	}
	return written, nil
}

// checkNotionFormulaTypes keeps the formulas the operations write as Notion
// keeps them, checked against the source's properties as scope gives them
// (notionFormulaUpdateScope, notionFormulaNewSourceScope), and takes their
// requests out of their configs. It runs before their block_property
// references are stored (storeNotionFormulaReferences).
func checkNotionFormulaTypes(operations []graphOperation, scope []notionFormulaCheckProperty, own string, related notionFormulaRelated) error {
	properties, pages := notionFormulaScopeTypes(scope, own, related)
	for index := range operations {
		if err := keepNotionFormulaRequest(&operations[index], properties, pages); err != nil {
			return err
		}
	}
	return nil
}

// keepNotionFormulaRequest keeps the formula an operation writes, checked
// against properties (by name, their types in a formula).
func keepNotionFormulaRequest(operation *graphOperation, properties map[string]string, pages notionFormulaPages) error {
	property := operation.Property
	if operation.Kind != graphOperationKindUpsertProperty || property == nil {
		return nil
	}
	config := map[string]any{}
	if json.Unmarshal(property.Config, &config) != nil {
		return nil
	}
	request, pending := config[notionFormulaRequestField].(map[string]any)
	if !pending {
		return nil
	}
	delete(config, notionFormulaRequestField)
	expression, _ := request["expression"].(string)
	label, _ := request["label"].(string)
	written, err := notionFormulaKept(expression, label, properties, pages)
	if err != nil {
		return err
	}
	config["formula"] = written
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	rewritten := *property
	rewritten.Config = encoded
	operation.Property = &rewritten
	return nil
}
