package app

import "sort"

// databaseFormulaFunctionNames are the functions the server's formula
// evaluator knows, the same set the web's evaluator knows
// (web/src/components/database/formulaSyntax.ts); the two must agree.
var databaseFormulaFunctionNames = map[string]struct{}{}

func init() {
	for _, name := range []string{
		"prop", "id", "if", "ifs", "let", "lets", "and", "or", "not", "empty", "equal", "unequal",
		"concat", "repeat", "format", "formatNumber", "toNumber", "lower", "upper", "trim",
		"startsWith", "endsWith", "contains", "substring", "replace", "replaceAll", "test", "match",
		"split", "join", "length", "padStart", "padEnd", "link", "style", "unstyle", "name", "email",
		"add", "subtract", "multiply", "divide", "mod", "pow", "min", "max", "sum", "mean", "median",
		"sqrt", "cbrt", "exp", "ln", "log10", "log2", "sign", "pi", "e", "round", "floor", "ceil", "abs",
		"now", "today", "dateAdd", "dateSubtract", "dateBetween", "dateRange", "parseDate", "dateStart",
		"dateEnd", "timestamp", "fromTimestamp", "formatDate", "year", "month", "day", "date", "week",
		"hour", "minute", "nextWorkday", "addWorkdays",
		"at", "first", "last", "slice", "sort", "reverse", "unique", "includes", "find", "findIndex",
		"filter", "some", "every", "map", "flat", "splice", "count",
	} {
		databaseFormulaFunctionNames[name] = struct{}{}
	}
}

// databaseFormulaUnknownFunctions names the functions an expression calls
// that the evaluator does not know, each once, in order of name. An
// expression that does not parse names none.
func databaseFormulaUnknownFunctions(expression string) []string {
	root, err := parseDatabaseFormula(expression)
	if err != nil {
		return nil
	}
	unknown := map[string]struct{}{}
	var visit func(*databaseFormulaNode)
	visit = func(node *databaseFormulaNode) {
		if node == nil {
			return
		}
		if node.kind == databaseFormulaNodeCall || node.kind == databaseFormulaNodeMethod {
			if _, known := databaseFormulaFunctionNames[node.text]; !known {
				unknown[node.text] = struct{}{}
			}
		}
		for _, child := range node.children {
			visit(child)
		}
	}
	visit(root)
	names := make([]string, 0, len(unknown))
	for name := range unknown {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
