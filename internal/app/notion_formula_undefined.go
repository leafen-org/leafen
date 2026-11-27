package app

// empty() with nothing to test gives nothing, a value of no type that
// Notion's check names "undefined" (seen 2026-11-20 through its API: math on
// undefined and number for empty() * 2 and for let(x, empty(), x * 2), a
// value abs() can't take for abs(empty()), and a minus on it), while what
// takes any value takes it (upper(empty()), empty() + 1, not empty(),
// empty().length()). Where a formula gives it or another value, an if()'s or
// ifs()'s results, they are of the other's type: if(false, 1, empty()) > "a"
// compares number and text, if(false, "a", empty()) * 2 does math on text and
// number, and if(false, empty(), empty()) * 2 names undefined. What a formula gives reads
// the same way (notionViewFormulaNoKind).
const notionFormulaUndefined = "undefined"

// notionFormulaEmptyType is the type empty() gives: a boolean, whether its
// value (a method's own value its first) is empty, or undefined with none.
func notionFormulaEmptyType(values []*databaseFormulaNode, method bool) string {
	if len(values) == 0 && !method {
		return notionFormulaUndefined
	}
	return notionFormulaReturns["empty"]
}

// notionFormulaShared is the type values share, undefined ones aside, or "".
func notionFormulaShared(types ...string) string {
	shared := ""
	for _, kind := range types {
		switch {
		case kind == "":
			return ""
		case kind == notionFormulaUndefined:
			if shared == "" {
				shared = kind
			}
		case shared == "" || shared == notionFormulaUndefined:
			shared = kind
		case kind != shared:
			return ""
		}
	}
	return shared
}
