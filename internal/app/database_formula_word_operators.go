package app

// and, or and not written as words, which the formula's grammar reads as
// its operators or as names and calls (database_formula_syntax.go).

// keyword matches and/or/not written as words, which are operators unless a
// call's parenthesis follows right after: "not (true)" is not before a value
// in parentheses and "not(true)" a call, as Notion reads them ("not (true,
// false)" is ")" expected at [9,10], "true and (false)" reads and "true
// and(false)" doesn't; seen 2026-11-28 in Chrome). The web's parser reads
// them the same (formulaAst.ts).
func (syntax *databaseFormulaSyntax) keyword(value string) bool {
	token := syntax.peek()
	if token == nil || token.kind != databaseFormulaTokenIdentifier || token.value != value {
		return false
	}
	if syntax.calledRightAfter(syntax.index) {
		return false
	}
	syntax.index++
	return true
}

// calledRightAfter is whether a call's parenthesis follows the token at index
// with nothing between.
func (syntax *databaseFormulaSyntax) calledRightAfter(index int) bool {
	next := index + 1
	return next < len(syntax.tokens) && syntax.tokens[next].kind == databaseFormulaTokenParen &&
		syntax.tokens[next].value == "(" && !syntax.tokens[next].spaced
}

// notOperator matches not written as a word as an operator only when a value
// may follow it; alone it reads as a name, as Notion reads it.
func (syntax *databaseFormulaSyntax) notOperator() bool {
	if next := syntax.index + 1; next >= len(syntax.tokens) {
		return false
	} else if token := syntax.tokens[next]; token.kind == databaseFormulaTokenComma ||
		token.kind == databaseFormulaTokenColon || token.kind == databaseFormulaTokenQuestion ||
		token.kind == databaseFormulaTokenParen && token.value == ")" ||
		token.kind == databaseFormulaTokenBracket && token.value == "]" {
		return false
	}
	return syntax.keyword("not")
}
