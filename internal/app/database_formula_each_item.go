package app

import (
	"slices"
	"sort"
	"unicode"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// The list functions that read an expression for each item, as current at
// index: map, filter, find, findIndex, some, every, count and sort. The
// editor reads them the same way (formulaParser.ts eachItem, formulaSort.ts).

func (environment *databaseFormulaEnvironment) eachItem(
	name string, value any, expression *databaseFormulaNode, scope databaseFormulaScope,
) (any, error) {
	items, err := databaseFormulaList(value)
	if err != nil {
		return nil, err
	}
	results := []any{}
	for index, item := range items {
		// sort() without an expression orders the items themselves.
		if name == "sort" && expression == databaseFormulaEmptyNode {
			results = append(results, item)
			continue
		}
		result, err := environment.evaluate(expression, scope.with("current", item).with("index", float64(index)))
		if err != nil {
			return nil, err
		}
		matched := databaseFormulaTruthy(result)
		switch name {
		case "map", "sort":
			results = append(results, result)
		case "filter", "count":
			if matched {
				results = append(results, item)
			}
		case "find":
			if matched {
				return item, nil
			}
		case "findIndex":
			if matched {
				return float64(index), nil
			}
		case "some":
			if matched {
				return true, nil
			}
		case "every":
			if !matched {
				return false, nil
			}
		}
	}
	switch name {
	case "find":
		// No item found is nothing, as Notion's (seen 2026-11-12).
		return nil, nil
	case "findIndex":
		return float64(-1), nil
	case "some":
		return false, nil
	case "every":
		return true, nil
	case "count":
		return float64(len(results)), nil
	case "sort":
		return environment.sortedList(items, results), nil
	}
	return results, databaseFormulaBudget(results)
}

// sortedList orders a list's items by their keys — the value its expression
// gives each, or each item itself — as Notion's sort() does (seen 2026-11-19
// through its API, 48 lists): the lowest first, equal keys kept in their
// order; numbers by number, dates by time and false before true while every
// key is one of those; any other mix by its keys' text, an empty one ("",
// nothing, an empty list) last, the rest as a person reads them — letters
// alike whatever their case or accent ("a", "B", "b", "C"; "e", "é", "f"),
// an upper case before its lower ("B" before "b"), the numbers in them by
// their worth ("x2", "X3", "x10"; "1.2", "1.5", "1.10"), and spaces and
// marks before digits and digits before letters ("a b", "a-b", "ab"). Text
// reads in English, as Notion's API reads it ("A", "b", "가", "나"); the
// editor reads it in its viewer's language, as Notion's screen does
// (formulaSort.ts).
func (environment *databaseFormulaEnvironment) sortedList(items, keys []any) []any {
	order := make([]int, len(items))
	for index := range order {
		order[index] = index
	}
	compare := environment.sortKeyOrder(keys)
	sort.SliceStable(order, func(left, right int) bool { return compare(order[left], order[right]) < 0 })
	sorted := make([]any, len(items))
	for index, from := range order {
		sorted[index] = items[from]
	}
	return sorted
}

// sortKeyOrder is the order sort() reads its keys in (sortedList).
func (environment *databaseFormulaEnvironment) sortKeyOrder(keys []any) func(left, right int) int {
	numbers, times, flags := make([]float64, len(keys)), make([]float64, len(keys)), make([]bool, len(keys))
	allNumbers, allTimes, allFlags := true, true, true
	for index, key := range keys {
		switch typed := key.(type) {
		case float64:
			numbers[index] = typed
		case int:
			numbers[index] = float64(typed)
		default:
			allNumbers = false
		}
		if date, ok := key.(databaseFormulaDate); ok {
			if moment, read := environment.momentOf(date); read {
				times[index] = float64(moment.at.UnixMilli())
			} else {
				allTimes = false
			}
		} else {
			allTimes = false
		}
		if flag, ok := key.(bool); ok {
			flags[index] = flag
		} else {
			allFlags = false
		}
	}
	switch {
	case len(keys) == 0:
		return func(int, int) int { return 0 }
	case allNumbers:
		return func(left, right int) int { return databaseFormulaSortNumbers(numbers[left], numbers[right]) }
	case allTimes:
		return func(left, right int) int { return databaseFormulaSortNumbers(times[left], times[right]) }
	case allFlags:
		return func(left, right int) int {
			if flags[left] == flags[right] {
				return 0
			}
			if flags[right] {
				return -1
			}
			return 1
		}
	}
	texts := make([]string, len(keys))
	for index, key := range keys {
		texts[index] = databaseFormulaText(key)
	}
	collator := collate.New(language.English, collate.Numeric, collate.IgnoreCase)
	return func(left, right int) int {
		switch leftText, rightText := texts[left], texts[right]; {
		case leftText == "" || rightText == "":
			return databaseFormulaSortEmptyLast(leftText == "", rightText == "")
		default:
			if order := collator.CompareString(leftText, rightText); order != 0 {
				return order
			}
			return databaseFormulaSortUpperFirst(leftText, rightText)
		}
	}
}

func databaseFormulaSortNumbers(left, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	}
	return 0
}

func databaseFormulaSortEmptyLast(leftEmpty, rightEmpty bool) int {
	switch {
	case leftEmpty == rightEmpty:
		return 0
	case leftEmpty:
		return 1
	}
	return -1
}

// databaseFormulaSortUpperFirst orders two texts alike but for their case
// by the first letter they differ in, its upper case first.
func databaseFormulaSortUpperFirst(left, right string) int {
	leftRunes, rightRunes := []rune(left), []rune(right)
	for index := 0; index < len(leftRunes) && index < len(rightRunes); index++ {
		if leftRunes[index] == rightRunes[index] {
			continue
		}
		leftUpper, rightUpper := unicode.IsUpper(leftRunes[index]), unicode.IsUpper(rightRunes[index])
		if leftUpper != rightUpper {
			if leftUpper {
				return -1
			}
			return 1
		}
		return slices.Compare(leftRunes[index:index+1], rightRunes[index:index+1])
	}
	return databaseFormulaSortNumbers(float64(len(leftRunes)), float64(len(rightRunes)))
}
