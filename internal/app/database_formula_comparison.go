package app

import (
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// Comparisons don't chain, as Notion's reading takes them: "1 > 2 > 3",
// "1 < 2 <= 3" and "true == false == true" stop at the second ("End of input
// expected.", and in "if(1 > 2 > 3, 1, 2)" the call's closing parenthesis
// expected there), while "1 > 2 == true" and "(1 > 2) == true" read (seen
// 2026-11-28 through its API and in two Chrome tabs). The web editor reads
// them the same (formulaAst.ts). Leafen read them chained left to right
// before, and migration 870 writes the formulas it kept so with the
// parentheses that keep that reading ("(1 > 2) > 3").

func (syntax *databaseFormulaSyntax) equality() (*databaseFormulaNode, error) {
	return syntax.comparedOnce(syntax.comparison, "==", "!=")
}

func (syntax *databaseFormulaSyntax) comparison() (*databaseFormulaNode, error) {
	return syntax.comparedOnce(syntax.term, ">", ">=", "<", "<=")
}

// comparedOnce reads one operator of the level at most, the reading stopping
// at a second — or, reading as Leafen did before (chained), each one after
// the first with the parentheses that keep that reading noted.
func (syntax *databaseFormulaSyntax) comparedOnce(
	next func() (*databaseFormulaNode, error), operators ...string,
) (*databaseFormulaNode, error) {
	first := syntax.index
	left, err := next()
	for read := 0; err == nil; read++ {
		token := syntax.peek()
		if token == nil || token.kind != databaseFormulaTokenOperator || !slices.Contains(operators, token.value) {
			return left, nil
		}
		if read > 0 {
			if syntax.chained == nil {
				return left, nil
			}
			syntax.chained.opens = append(syntax.chained.opens, syntax.tokens[first].start)
			syntax.chained.closes = append(syntax.chained.closes, syntax.tokens[syntax.index-1].end)
		}
		syntax.index++
		var right *databaseFormulaNode
		if right, err = next(); err == nil {
			left = databaseFormulaBinary(token.value, left, right)
		}
	}
	return nil, err
}

// databaseFormulaChainParens are the parentheses that keep a chained
// comparison's reading, as places in the formula's characters: an opening
// one at each of opens, a closing one at each of closes.
type databaseFormulaChainParens struct{ opens, closes []int }

// databaseFormulaComparisonsUnchained is the formula with each comparison
// Leafen read chained left to right written with the parentheses that keep
// that reading — "1 > 2 > 3" as "(1 > 2) > 3", "1 == 2 == 3 == 4" as
// "((1 == 2) == 3) == 4" — its other characters, comments and spaces as they
// were, and whether it changed. A formula that doesn't read even so, or
// holds no chain, stays as it is.
func databaseFormulaComparisonsUnchained(expression string) (string, bool) {
	runes := []rune(expression)
	tokens, _, invalid := scanDatabaseFormulaTokens(runes)
	if invalid || len(tokens) == 0 {
		return expression, false
	}
	parens := &databaseFormulaChainParens{}
	syntax := &databaseFormulaSyntax{tokens: tokens, chained: parens}
	if _, err := syntax.expression(); err != nil || syntax.index < len(tokens) || len(parens.opens) == 0 {
		return expression, false
	}
	opens, closes := make([]int, len(runes)+1), make([]int, len(runes)+1)
	for _, at := range parens.opens {
		opens[at]++
	}
	for _, at := range parens.closes {
		closes[at]++
	}
	var written strings.Builder
	for at := 0; at <= len(runes); at++ {
		// A group closing where another opens closes first.
		written.WriteString(strings.Repeat(")", closes[at]))
		written.WriteString(strings.Repeat("(", opens[at]))
		if at < len(runes) {
			written.WriteRune(runes[at])
		}
	}
	return written.String(), true
}

// unchainDatabaseFormulaComparisons is migration 870's content: the formulas
// Leafen kept with comparisons chained, written with the parentheses that
// keep their reading, so each reads and works out as it did.
func unchainDatabaseFormulaComparisons(app core.App) error {
	return rewriteStoredDatabaseFormulas(app, storedDatabaseFormulaRewrite{
		rewrite: databaseFormulaComparisonsUnchained,
		// A chain is written with two comparisons, < and > in JSON maybe escaped.
		needles: []string{">", "<", `\u003e`, `\u003c`, `\u003E`, `\u003C`, "==", "!="},
		why:     "formula-comparisons-unchained", logged: "formulas with chained comparisons written with parentheses",
	})
}
