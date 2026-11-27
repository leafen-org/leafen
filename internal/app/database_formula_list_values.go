package app

import (
	"math"
	"slices"
	"sort"
)

// List and number functions. A list given to a numeric function must hold
// only numbers; other arguments are read as numbers.

// databaseFormulaList is a list function's list; an empty value, as first()
// of an empty list gives, is an empty list.
func databaseFormulaList(value any) ([]any, error) {
	if value == nil {
		return []any{}, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errDatabaseFormulaInvalid
	}
	return list, nil
}

func (environment *databaseFormulaEnvironment) listFunction(name string, values []any) (any, bool, error) {
	switch name {
	case "concat":
		hasList := false
		for _, value := range values {
			if _, ok := value.([]any); ok {
				hasList = true
			}
		}
		if !hasList {
			var spans []graphTextSpan
			for _, value := range values {
				spans = append(spans, environment.spans(value)...)
			}
			return databaseFormulaRich(spans), true, nil
		}
		combined := []any{}
		for _, value := range values {
			if list, ok := value.([]any); ok {
				combined = append(combined, list...)
			} else {
				combined = append(combined, value)
			}
		}
		return combined, true, nil
	case "at", "first", "last", "slice", "splice", "sort", "reverse", "join", "unique", "includes", "flat":
	default:
		return nil, false, nil
	}
	list, err := databaseFormulaList(databaseFormulaArgument(values, 0))
	if err != nil {
		return nil, true, err
	}
	switch name {
	// An item that isn't there is an empty value, which a list then leaves
	// out, and at() counts a negative index from the end, as Notion's (seen
	// 2026-11-12 through its API: first([]) null, at([1, 2], -1) 2).
	case "at":
		index := math.Trunc(databaseFormulaFiniteNumber(databaseFormulaArgument(values, 1)))
		if index < 0 {
			index += float64(len(list))
		}
		if index < 0 || index >= float64(len(list)) {
			return nil, true, nil
		}
		return list[int(index)], true, nil
	case "first", "last":
		if len(list) == 0 {
			return nil, true, nil
		}
		if name == "first" {
			return list[0], true, nil
		}
		return list[len(list)-1], true, nil
	case "slice":
		start := databaseFormulaRelativeIndex(databaseFormulaArgument(values, 1), len(list))
		end := len(list)
		if len(values) > 2 {
			end = databaseFormulaRelativeIndex(values[2], len(list))
		}
		if end < start {
			end = start
		}
		return append([]any{}, list[start:end]...), true, nil
	case "splice":
		// As Notion's (seen 2026-10-08): nothing is removed unless a count is
		// given, and a start past the end adds at the end.
		start := databaseFormulaRelativeIndex(databaseFormulaArgument(values, 1), len(list))
		removed := 0
		if len(values) > 2 {
			if count := math.Trunc(databaseFormulaFiniteNumber(values[2])); count > 0 {
				removed = int(math.Min(count, float64(len(list)-start)))
			}
		}
		spliced := append([]any{}, list[:start]...)
		if len(values) > 3 {
			spliced = append(spliced, values[3:]...)
		}
		spliced = append(spliced, list[start+removed:]...)
		return spliced, true, databaseFormulaBudget(spliced)
	case "sort":
		// The evaluator orders a sort() by its expression (database_formula_each_item.go).
		return environment.sortedList(list, list), true, nil
	case "reverse":
		reversed := make([]any, len(list))
		for index, item := range list {
			reversed[len(list)-1-index] = item
		}
		return reversed, true, nil
	case "join":
		var spans []graphTextSpan
		separator := graphTextSpan{Text: databaseFormulaText(databaseFormulaArgument(values, 1))}
		for index, item := range list {
			if index > 0 {
				spans = append(spans, separator)
			}
			spans = append(spans, environment.spans(item)...)
		}
		return databaseFormulaRich(spans), true, nil
	case "unique":
		unique := []any{}
		for _, item := range list {
			seen := false
			for _, candidate := range unique {
				if databaseFormulaSame(candidate, item) {
					seen = true
					break
				}
			}
			if !seen {
				unique = append(unique, item)
			}
		}
		return unique, true, nil
	case "includes":
		wanted := databaseFormulaArgument(values, 1)
		for _, item := range list {
			if databaseFormulaSame(item, wanted) {
				return true, true, nil
			}
		}
		return false, true, nil
	}
	flat := []any{}
	for _, item := range list {
		if nested, ok := item.([]any); ok {
			flat = append(flat, nested...)
		} else {
			flat = append(flat, item)
		}
	}
	return flat, true, nil
}

// databaseFormulaRelativeIndex reads a slice() bound as JavaScript does: a
// negative bound counts from the end.
func databaseFormulaRelativeIndex(value any, length int) int {
	index := math.Trunc(databaseFormulaFiniteNumber(value))
	if index < 0 {
		return int(math.Max(0, float64(length)+index))
	}
	return int(math.Min(index, float64(length)))
}

func databaseFormulaNumbers(values []any) ([]float64, error) {
	numbers := []float64{}
	for _, value := range values {
		list, ok := value.([]any)
		if !ok {
			// An empty property isn't one of the numbers.
			if value != nil {
				numbers = append(numbers, databaseFormulaNumber(value))
			}
			continue
		}
		// A list's numbers may be infinite or no number ([1e308 * 10,
		// 2].max() is Infinity, as Notion's) and its empty items are left
		// out ([1 / 0, 2].sum() is 2); its other items fail it.
		for _, item := range list {
			if item == nil {
				continue
			}
			number, ok := item.(float64)
			if !ok {
				return nil, errDatabaseFormulaInvalid
			}
			numbers = append(numbers, number)
		}
	}
	return numbers, nil
}

// databaseFormulaNumbersOnly are the functions that take numbers only: an
// empty property given to one fails the formula, as Notion's (its code read
// 2026-11-14; seen: empty(abs(empty)), format(round(empty)), pow(2, empty)
// and round(2, empty) are no value, where empty(empty) is true), as the
// editor's (formulaFunctions.ts NUMBERS_ONLY).
var databaseFormulaNumbersOnly = map[string]bool{
	"pow": true, "mod": true, "sqrt": true, "cbrt": true, "exp": true, "ln": true, "log10": true, "log2": true,
	"sign": true, "abs": true, "floor": true, "ceil": true, "round": true,
}

func databaseFormulaNumberFunction(name string, values []any) (any, bool, error) {
	number := func(index int) float64 { return databaseFormulaNumber(databaseFormulaArgument(values, index)) }
	if databaseFormulaNumbersOnly[name] && slices.Contains(values, nil) {
		return nil, true, errDatabaseFormulaInvalid
	}
	switch name {
	case "min", "max", "sum", "mean", "median":
		numbers, err := databaseFormulaNumbers(values)
		if err != nil {
			return nil, true, err
		}
		// Only empty properties have no smallest, largest or middle; they add up to 0.
		if len(numbers) == 0 && name != "sum" && !slices.ContainsFunc(values, func(value any) bool { return value != nil }) {
			return nil, true, nil
		}
		result, err := databaseFormulaAggregate(name, numbers)
		return result, true, err
	// Arithmetic as Notion's (database_formula_decimal.go).
	case "add":
		total := float64(0)
		for index := range values {
			var err error
			if total, err = databaseFormulaAdd(total, number(index)); err != nil {
				return nil, true, err
			}
		}
		return total, true, nil
	case "multiply":
		if len(values) == 0 {
			return float64(0), true, nil
		}
		product := float64(1)
		for index := range values {
			var err error
			if product, err = databaseFormulaMultiply(product, number(index)); err != nil {
				return nil, true, err
			}
		}
		return product, true, nil
	case "subtract":
		result, err := databaseFormulaSubtract(number(0), number(1))
		return result, true, err
	case "divide":
		result, err := databaseFormulaDivide(number(0), number(1))
		return result, true, err
	case "mod":
		result, err := databaseFormulaRemainder(number(0), number(1))
		return result, true, err
	case "pow":
		result, err := databaseFormulaPower(number(0), number(1))
		return result, true, err
	case "sqrt":
		return math.Sqrt(number(0)), true, nil
	case "cbrt":
		return databaseFormulaJSCbrt(number(0)), true, nil
	case "exp":
		return databaseFormulaJSExp(number(0)), true, nil
	case "ln":
		return databaseFormulaJSLog(number(0)), true, nil
	case "log10":
		return databaseFormulaExactLog(number(0), 10, databaseFormulaJSLog10), true, nil
	case "log2":
		return databaseFormulaExactLog(number(0), 2, databaseFormulaJSLog2), true, nil
	case "sign":
		value := number(0)
		if value > 0 {
			return float64(1), true, nil
		}
		if value < 0 {
			return float64(-1), true, nil
		}
		return value, true, nil
	case "abs":
		return math.Abs(number(0)), true, nil
	case "floor":
		return math.Floor(number(0)), true, nil
	case "ceil":
		return math.Ceil(number(0)), true, nil
	case "round":
		result, err := databaseFormulaRoundTo(number(0), values)
		return result, true, err
	case "pi":
		return math.Pi, true, nil
	case "e":
		return math.E, true, nil
	}
	return nil, false, nil
}

func databaseFormulaAggregate(name string, numbers []float64) (float64, error) {
	if len(numbers) == 0 {
		return 0, nil
	}
	switch name {
	case "min", "max":
		// As JavaScript's Math.min and Math.max: no number among them is no
		// number (min(sqrt(-1), 1), as Notion's).
		result := numbers[0]
		for _, number := range numbers {
			if math.IsNaN(number) {
				return number, nil
			}
			if (name == "min" && number < result) || (name == "max" && number > result) {
				result = number
			}
		}
		return result, nil
	case "median":
		sorted := append([]float64{}, numbers...)
		sort.Float64s(sorted)
		middle := len(sorted) / 2
		if len(sorted)%2 == 1 {
			return sorted[middle], nil
		}
		return databaseFormulaMiddle(sorted[middle-1], sorted[middle])
	}
	// A sum and a mean are worked out on the numbers' digits, as Notion's
	// (database_formula_decimal.go).
	if name == "mean" {
		return databaseFormulaMean(numbers)
	}
	total, err := databaseFormulaSum(numbers)
	if err != nil {
		return 0, err
	}
	return total.number(), nil
}

// databaseFormulaRound is JavaScript's Math.round: halves round up.
func databaseFormulaRound(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	floor := math.Floor(value)
	if value-floor >= 0.5 {
		return floor + 1
	}
	return floor
}

// databaseFormulaMaxRoundPlaces is the most decimal places round() takes
// either way, as Notion's.
const databaseFormulaMaxRoundPlaces = 12

// databaseFormulaRoundTo is round(): to whole numbers without places or with
// 0, else to a whole number of places up to 12 either way by 10 to that
// power; other places — a fraction, more than 12 — fail the formula, as
// Notion's (seen 2026-11-14), as the editor's roundValue (formulaValues.ts).
func databaseFormulaRoundTo(value float64, arguments []any) (any, error) {
	if len(arguments) < 2 {
		return databaseFormulaRound(value), nil
	}
	// Read as it is: places that are no number or infinite are no whole
	// number either (round(2, 1e308 * 10)).
	precision, raw := arguments[1].(float64)
	if !raw {
		precision = databaseFormulaNumber(arguments[1])
	}
	if precision == 0 {
		return databaseFormulaRound(value), nil
	}
	if precision != math.Trunc(precision) || math.Abs(precision) > databaseFormulaMaxRoundPlaces {
		return nil, errDatabaseFormulaInvalid
	}
	// 10 to that power is the number nearest it (1e-11, not pow's last bit
	// off), as Notion's (seen 2026-11-14: round(6.02e23, -11) is
	// 6.0200000000000006e+23).
	factor := math.Pow10(int(precision))
	// The product is rounded on its own before it is rounded to a whole
	// number, as JavaScript's; fused into the rounding's subtraction it
	// was not (round(1.45, 1) was 1.4, round(-1250, -2) -1300).
	return databaseFormulaRound(float64(value*factor)) / factor, nil
}

// databaseFormulaExactLog gives exact results for exact powers, as
// JavaScript's Math.log10 and Math.log2 do.
func databaseFormulaExactLog(value, base float64, logarithm func(float64) float64) float64 {
	result := logarithm(value)
	if rounded := math.Round(result); math.Abs(result-rounded) < 1e-9 && databaseFormulaJSPow(base, rounded) == value {
		return rounded
	}
	return result
}
