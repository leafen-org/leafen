package app

import (
	"math"
	"regexp"
	"time"
)

// databaseFormulaEnvironment is what a formula reads besides its own text:
// its row's properties, the row's ID, and the clock and time zone its dates
// are read in.
type databaseFormulaEnvironment struct {
	property func(string) (any, error)
	// related reads a property of a related page, for .prop() on it; left
	// out, related pages read as empty.
	related  func(databaseFormulaReference, string) (any, error)
	location *time.Location
	now      time.Time
	// language is the language dates read as text in, "" English
	// (database_formula_locale.go).
	language string
	pageID   string
	steps    int
}

type databaseFormulaScope map[string]any

func (scope databaseFormulaScope) with(name string, value any) databaseFormulaScope {
	next := make(databaseFormulaScope, len(scope)+1)
	for key, current := range scope {
		next[key] = current
	}
	next[name] = value
	return next
}

// evaluateDatabaseFormula evaluates an expression in UTC at the current time.
func evaluateDatabaseFormula(expression string, property func(string) (any, error)) (any, error) {
	return evaluateDatabaseFormulaIn(expression, &databaseFormulaEnvironment{property: property})
}

func evaluateDatabaseFormulaIn(expression string, environment *databaseFormulaEnvironment) (any, error) {
	return evaluateDatabaseFormulaInScope(expression, environment, databaseFormulaScope{})
}

// evaluateDatabaseFormulaInScope evaluates an expression with names already
// bound, as an automation's variables are.
func evaluateDatabaseFormulaInScope(
	expression string, environment *databaseFormulaEnvironment, scope databaseFormulaScope,
) (any, error) {
	if len([]rune(expression)) == 0 || len([]rune(expression)) > maxDatabaseFormulaExpressionRunes {
		return nil, errDatabaseFormulaInvalid
	}
	if environment.location == nil {
		environment.location = time.UTC
	}
	if environment.now.IsZero() {
		environment.now = time.Now()
	}
	root, err := parseDatabaseFormula(expression)
	if err != nil {
		return nil, err
	}
	value, err := environment.evaluate(root, scope)
	if err != nil {
		return nil, err
	}
	if err := databaseFormulaBudget(value); err != nil {
		return nil, err
	}
	return value, nil
}

func (environment *databaseFormulaEnvironment) evaluate(node *databaseFormulaNode, scope databaseFormulaScope) (any, error) {
	environment.steps++
	if environment.steps > maxDatabaseFormulaSteps {
		return nil, errDatabaseFormulaInvalid
	}
	switch node.kind {
	case databaseFormulaNodeNumber:
		return node.number, nil
	case databaseFormulaNodeString:
		return node.text, nil
	case databaseFormulaNodeIdentifier:
		switch node.text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		if value, ok := scope[node.text]; ok {
			return value, nil
		}
		return "", nil
	case databaseFormulaNodeList:
		items := make([]any, 0, len(node.children))
		for _, child := range node.children {
			value, err := environment.evaluate(child, scope)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		return items, databaseFormulaBudget(items)
	case databaseFormulaNodeUnary:
		value, err := environment.evaluate(node.children[0], scope)
		if err != nil {
			return nil, err
		}
		if node.text == "!" {
			return !databaseFormulaTruthy(value), nil
		}
		// Of an empty property it fails the formula, as Notion's (seen
		// 2026-11-14: format(-empty) is no value).
		if value == nil {
			return nil, errDatabaseFormulaInvalid
		}
		return -databaseFormulaNumber(value), nil
	case databaseFormulaNodeTernary:
		condition, err := environment.evaluate(node.children[0], scope)
		if err != nil {
			return nil, err
		}
		if databaseFormulaTruthy(condition) {
			return environment.evaluate(node.children[1], scope)
		}
		return environment.evaluate(node.children[2], scope)
	case databaseFormulaNodeBinary:
		return environment.binary(node, scope)
	case databaseFormulaNodeCall:
		value, err := environment.call(node.text, node.children, nil, false, scope)
		if err != nil {
			return nil, err
		}
		return value, databaseFormulaBudget(value)
	case databaseFormulaNodeMethod:
		receiver, err := environment.evaluate(node.children[0], scope)
		if err != nil {
			return nil, err
		}
		if node.text == "prop" {
			return environment.relatedProperty(receiver, node.children[1:], scope)
		}
		value, err := environment.call(node.text, node.children[1:], receiver, true, scope)
		if err != nil {
			return nil, err
		}
		return value, databaseFormulaBudget(value)
	}
	return nil, errDatabaseFormulaInvalid
}

// relatedProperty is page.prop("Name"): a property of a related page, as
// it reads now. Only a page has properties; which pages the environment
// reads is its related's (a database's rows, a page button's own page).
func (environment *databaseFormulaEnvironment) relatedProperty(
	receiver any, arguments []*databaseFormulaNode, scope databaseFormulaScope,
) (any, error) {
	reference, ok := receiver.(databaseFormulaReference)
	if !ok || reference.Kind != "page" {
		return nil, errDatabaseFormulaInvalid
	}
	name := ""
	if len(arguments) > 0 {
		value, err := environment.evaluate(arguments[0], scope)
		if err != nil {
			return nil, err
		}
		name = databaseFormulaText(value)
	}
	if environment.related == nil {
		return "", nil
	}
	value, err := environment.related(reference, name)
	if err != nil {
		return nil, err
	}
	return databaseFormulaPropertyValue(value), nil
}

func (environment *databaseFormulaEnvironment) binary(node *databaseFormulaNode, scope databaseFormulaScope) (any, error) {
	left, err := environment.evaluate(node.children[0], scope)
	if err != nil {
		return nil, err
	}
	switch node.text {
	case "&&":
		if !databaseFormulaTruthy(left) {
			return false, nil
		}
	case "||":
		if databaseFormulaTruthy(left) {
			return true, nil
		}
	}
	right, err := environment.evaluate(node.children[1], scope)
	if err != nil {
		return nil, err
	}
	switch node.text {
	case "&&", "||":
		return databaseFormulaTruthy(right), nil
	case "==":
		return databaseFormulaSame(left, right), nil
	case "!=":
		return !databaseFormulaSame(left, right), nil
	case ">", ">=", "<", "<=":
		order := environment.compare(left, right)
		switch node.text {
		case ">":
			return order == 1, nil
		case ">=":
			return order == 1 || order == 0, nil
		case "<":
			return order == -1, nil
		}
		return order == -1 || order == 0, nil
	case "+":
		return environment.add(left, right)
	case "^":
		if left == nil || right == nil {
			return nil, nil
		}
		return databaseFormulaPower(databaseFormulaNumber(left), databaseFormulaNumber(right))
	case "/":
		if left == nil && right == nil {
			return float64(0), nil
		}
	}
	// Arithmetic as Notion's (database_formula_decimal.go).
	leftNumber, rightNumber := databaseFormulaNumber(left), databaseFormulaNumber(right)
	switch node.text {
	case "-":
		return databaseFormulaSubtract(leftNumber, rightNumber)
	case "*":
		return databaseFormulaMultiply(leftNumber, rightNumber)
	case "/":
		return databaseFormulaDivide(leftNumber, rightNumber)
	case "%":
		return databaseFormulaRemainder(leftNumber, rightNumber)
	}
	return nil, errDatabaseFormulaInvalid
}

func (environment *databaseFormulaEnvironment) add(left, right any) (any, error) {
	if databaseFormulaJoinsAsText(left) || databaseFormulaJoinsAsText(right) {
		return databaseFormulaRich(append(environment.spans(left), environment.spans(right)...)), nil
	}
	return databaseFormulaAdd(databaseFormulaNumber(left), databaseFormulaNumber(right))
}

func databaseFormulaJoinsAsText(value any) bool {
	switch value.(type) {
	case string, databaseFormulaDate, databaseFormulaRichText, []any, databaseFormulaReference:
		return true
	}
	return false
}

// databaseFormulaUnordered is the order of NaN against anything.
const databaseFormulaUnordered = 2

var databaseFormulaDateShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// compare orders two values as Notion does: dates by time, text by
// character, and anything else by number. NaN and an empty property compare
// as unordered, even with each other (seen 2026-11-04: 5 >= empty, 0 <= empty
// and empty >= empty are all false); empty text is text.
func (environment *databaseFormulaEnvironment) compare(left, right any) int {
	if databaseFormulaBlank(left) || databaseFormulaBlank(right) {
		return databaseFormulaUnordered
	}
	if databaseFormulaIsText(left) && databaseFormulaIsText(right) {
		leftText, rightText := databaseFormulaText(left), databaseFormulaText(right)
		if databaseFormulaDateShape.MatchString(leftText) && databaseFormulaDateShape.MatchString(rightText) {
			leftDate, leftOK := environment.parseDate(leftText)
			rightDate, rightOK := environment.parseDate(rightText)
			if leftOK && rightOK {
				return databaseFormulaOrder(float64(leftDate.at.UnixMilli()), float64(rightDate.at.UnixMilli()))
			}
		}
		if math.IsNaN(databaseFormulaParseNumber(leftText)) || math.IsNaN(databaseFormulaParseNumber(rightText)) {
			return databaseFormulaCompareText(leftText, rightText)
		}
	}
	return databaseFormulaOrder(databaseFormulaNumber(left), databaseFormulaNumber(right))
}

func databaseFormulaOrder(left, right float64) int {
	switch {
	case math.IsNaN(left) || math.IsNaN(right):
		return databaseFormulaUnordered
	case left < right:
		return -1
	case left > right:
		return 1
	}
	return 0
}

// databaseFormulaCompareText orders text by UTF-16 unit, as JavaScript does.
func databaseFormulaCompareText(left, right string) int {
	leftUnits, rightUnits := databaseFormulaUnits(left), databaseFormulaUnits(right)
	for index := 0; index < len(leftUnits) && index < len(rightUnits); index++ {
		if leftUnits[index] != rightUnits[index] {
			if leftUnits[index] < rightUnits[index] {
				return -1
			}
			return 1
		}
	}
	return databaseFormulaOrder(float64(len(leftUnits)), float64(len(rightUnits)))
}

var databaseFormulaVariableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// databaseFormulaBindingName is a let() name: one identifier or one quoted
// name.
func databaseFormulaBindingName(node *databaseFormulaNode) string {
	if node.kind != databaseFormulaNodeIdentifier && node.kind != databaseFormulaNodeString {
		return ""
	}
	if !databaseFormulaVariableName.MatchString(node.text) {
		return ""
	}
	return node.text
}

var databaseFormulaEmptyNode = &databaseFormulaNode{kind: databaseFormulaNodeString}

// call evaluates a function or a method. A method's receiver is its first
// argument. if(), ifs(), and(), or(), let() and the list functions that
// take an expression evaluate their arguments only as they need them.
func (environment *databaseFormulaEnvironment) call(
	name string, arguments []*databaseFormulaNode, receiver any, hasReceiver bool, scope databaseFormulaScope,
) (any, error) {
	count := len(arguments)
	if hasReceiver {
		count++
	}
	argument := func(index int) (any, error) {
		if hasReceiver {
			if index == 0 {
				return receiver, nil
			}
			index--
		}
		if index >= len(arguments) {
			return "", nil
		}
		return environment.evaluate(arguments[index], scope)
	}
	if !hasReceiver && (name == "let" || name == "lets") {
		return environment.let(arguments, name == "lets", scope)
	}
	switch name {
	case "if":
		condition, err := argument(0)
		if err != nil {
			return nil, err
		}
		if databaseFormulaTruthy(condition) {
			return argument(1)
		}
		return argument(2)
	case "ifs":
		for index := 0; index+1 < count; index += 2 {
			condition, err := argument(index)
			if err != nil {
				return nil, err
			}
			if databaseFormulaTruthy(condition) {
				return argument(index + 1)
			}
		}
		if count%2 == 1 {
			return argument(count - 1)
		}
		// With no condition met and nothing else to give, nothing (Notion's,
		// seen 2026-11-12: ifs(false, 1) null).
		return nil, nil
	case "and", "or":
		for index := 0; index < count; index++ {
			value, err := argument(index)
			if err != nil {
				return nil, err
			}
			if databaseFormulaTruthy(value) == (name == "or") {
				return name == "or", nil
			}
		}
		return name == "and", nil
	case "map", "filter", "find", "findIndex", "some", "every", "count", "sort":
		list, err := argument(0)
		if err != nil {
			return nil, err
		}
		expressionIndex := 1
		if hasReceiver {
			expressionIndex = 0
		}
		expression := databaseFormulaEmptyNode
		if expressionIndex < len(arguments) {
			expression = arguments[expressionIndex]
		} else if name == "count" {
			// count() counts every item, or with a condition the items it holds for.
			items, err := databaseFormulaList(list)
			if err != nil {
				return nil, err
			}
			return float64(len(items)), nil
		}
		return environment.eachItem(name, list, expression, scope)
	}
	values := make([]any, count)
	for index := range values {
		value, err := argument(index)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	switch name {
	case "prop":
		value, err := environment.property(databaseFormulaText(databaseFormulaArgument(values, 0)))
		if err != nil {
			return nil, err
		}
		return databaseFormulaPropertyValue(value), nil
	case "id":
		if len(values) == 0 {
			return environment.pageID, nil
		}
		if reference, ok := values[0].(databaseFormulaReference); ok {
			return reference.ID, nil
		}
		return "", nil
	}
	if value, handled, err := environment.dateFunction(name, values); handled {
		return value, err
	}
	if value, handled, err := environment.listFunction(name, values); handled {
		return value, err
	}
	if value, handled, err := databaseFormulaNumberFunction(name, values); handled {
		return value, err
	}
	if value, handled, err := environment.textFunction(name, values); handled {
		return value, err
	}
	return "", nil
}

func databaseFormulaArgument(values []any, index int) any {
	if index < len(values) {
		return values[index]
	}
	return nil
}

func (environment *databaseFormulaEnvironment) let(
	arguments []*databaseFormulaNode, multiple bool, scope databaseFormulaScope,
) (any, error) {
	if len(arguments) < 3 || (multiple && len(arguments)%2 == 0) {
		return "", nil
	}
	pairs := 1
	if multiple {
		pairs = (len(arguments) - 1) / 2
	}
	for index := 0; index < pairs; index++ {
		name := databaseFormulaBindingName(arguments[index*2])
		if name == "" {
			return "", nil
		}
		value, err := environment.evaluate(arguments[index*2+1], scope)
		if err != nil {
			return nil, err
		}
		scope = scope.with(name, value)
	}
	body := arguments[len(arguments)-1]
	if !multiple {
		body = arguments[2]
	}
	return environment.evaluate(body, scope)
}
