package main

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// An operation's answer is the record its handler sends back when it does
// what was asked, read from the same source as what it takes, so the
// operation list can't name another (2026-11-16, an agent calling every
// operation: a property's trash and restore answered a content change's
// receipt while the contract's DatabasePropertyTrashResponse looked like
// theirs). It is the type of the value the handler hands event.JSON with a
// status under 400 — itself, in a function it hands the event to, or in one
// it hands the event and that value to (writePageHistoryResult(event,
// response, …)). A value whose type the source doesn't name (a map, a
// struct written in place) gives none.

// answerDepth is how many functions deep an answer is followed.
const answerDepth = 3

// answerNone is the answer of a handler that answers with no body.
const answerNone = "none"

// errorRecord is every refusal's form (internal/app/diagnostic.go), which a
// handler's refusals hand the event to write: never what it answers.
const errorRecord = "errorResponse"

// addAnswers adds to each route's handler the record it answers with, by the
// name the operation list gives it (nativeOperationHandler): a named
// function or method by its name, a closure or a maker's by its route.
func (code source) addAnswers(inputs map[string]input, files []*ast.File) {
	code.eachRoute(files, func(method, path string, handler ast.Expr, scope *ast.FuncDecl) {
		name, record := code.routeAnswer(handler, scope)
		if record == "" {
			return
		}
		if name == "" {
			name = closureName(method, path)
		}
		found := inputs[name]
		found.Answer = record
		inputs[name] = found
	})
}

// routeAnswer is the record a route's handler answers with, and its name
// where it has one of its own.
func (code source) routeAnswer(expression ast.Expr, scope *ast.FuncDecl) (string, string) {
	var function *ast.FuncDecl
	name := ""
	switch expression := expression.(type) {
	case *ast.FuncLit:
		record, _ := code.answer(answerScope{nil, expression.Type.Params, expression.Body}, eventParameter(expression.Type.Params), 0)
		return "", record
	case *ast.Ident:
		function, name = code.functions[expression.Name], expression.Name
	case *ast.SelectorExpr:
		if variable, named := expression.X.(*ast.Ident); named {
			name = code.variableType(scope, variable.Name) + "." + expression.Sel.Name
			function = code.methods[name]
		}
		if function == nil {
			function, name = code.functions[expression.Sel.Name], expression.Sel.Name
		}
	case *ast.CallExpr:
		// A wrapper takes the handler last; a maker returns it.
		if len(expression.Args) > 0 {
			if _, record := code.routeAnswer(expression.Args[len(expression.Args)-1], scope); record != "" {
				return "", record
			}
		}
		if maker, named := expression.Fun.(*ast.Ident); named && code.functions[maker.Name] != nil && code.functions[maker.Name].Body != nil {
			record, _ := code.answer(funcScope(code.functions[maker.Name]), "", 0)
			return "", record
		}
		return "", ""
	}
	if function == nil || function.Body == nil || !readsRequest(function) {
		return "", ""
	}
	record, _ := code.answer(funcScope(function), eventParameter(function.Type.Params), 0)
	return name, record
}

// answerScope is where a value's type is read: a function's receiver,
// parameters and body.
type answerScope struct {
	recv, params *ast.FieldList
	body         *ast.BlockStmt
}

func funcScope(function *ast.FuncDecl) answerScope {
	return answerScope{function.Recv, function.Type.Params, function.Body}
}

// answer is the record the handler of scope answers with, its event named
// event; a parameter's index when it answers with what it was handed.
func (code source) answer(scope answerScope, event string, depth int) (string, int) {
	record, parameter, noBody := "", -1, false
	ast.Inspect(scope.body, func(node ast.Node) bool {
		if record != "" || parameter >= 0 {
			return false
		}
		if literal, isLiteral := node.(*ast.FuncLit); isLiteral {
			// A maker's answer is its handler's.
			if name := eventParameter(literal.Type.Params); name != "" {
				record, parameter = code.answer(answerScope{nil, literal.Type.Params, literal.Body}, name, depth)
				return false
			}
			return true
		}
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		// An answer with no body is named none.
		if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "NoContent" && len(call.Args) == 1 {
			if receiver, named := selector.X.(*ast.Ident); named && receiver.Name == event && successStatus(call.Args[0]) {
				// A body answered elsewhere comes first.
				noBody = true
				return false
			}
		}
		if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "JSON" && len(call.Args) == 2 {
			if receiver, named := selector.X.(*ast.Ident); named && receiver.Name == event && successStatus(call.Args[0]) {
				if index := parameterIndex(scope.params, call.Args[1]); index >= 0 {
					parameter = index
				} else if record = code.valueType(scope, call.Args[1]); record == errorRecord {
					record = ""
				}
				return false
			}
		}
		if depth >= answerDepth {
			return true
		}
		// A function handed the event answers for it.
		callee := code.callee(scope, call)
		if callee == nil || callee.Body == nil || callee.Type.Params == nil {
			return true
		}
		for index, argument := range call.Args {
			if ident, isIdent := argument.(*ast.Ident); !isIdent || ident.Name != event {
				continue
			}
			name := parameterName(callee, index)
			if name == "" {
				continue
			}
			found, handed := code.answer(funcScope(callee), name, depth+1)
			switch {
			case found == errorRecord:
			case found == answerNone:
				noBody = true
			case found != "":
				record = found
			case handed >= 0 && handed < len(call.Args):
				if index := parameterIndex(scope.params, call.Args[handed]); index >= 0 {
					parameter = index
				} else if record = code.valueType(scope, call.Args[handed]); record == errorRecord {
					record = ""
				}
			}
			break
		}
		return record == "" && parameter < 0
	})
	if record == "" && parameter < 0 && noBody {
		record = answerNone
	}
	return record, parameter
}

// eventParameter is the name of a request event among parameters.
func eventParameter(params *ast.FieldList) string {
	if params == nil {
		return ""
	}
	for _, group := range params.List {
		if star, isStar := group.Type.(*ast.StarExpr); isStar {
			if selector, isSelector := star.X.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "RequestEvent" && len(group.Names) == 1 {
				return group.Names[0].Name
			}
		}
	}
	return ""
}

// successStatus tells a status under 400, or one the source picks at run time.
func successStatus(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.SelectorExpr:
		return slices.Contains([]string{
			"StatusOK", "StatusCreated", "StatusAccepted", "StatusNonAuthoritativeInfo", "StatusNoContent",
			"StatusResetContent", "StatusPartialContent", "StatusMultiStatus", "StatusAlreadyReported",
		}, expression.Sel.Name)
	case *ast.BasicLit:
		value, err := strconv.Atoi(expression.Value)
		return err == nil && value < 400
	case *ast.Ident:
		return !strings.Contains(strings.ToLower(expression.Name), "error")
	}
	return false
}

// parameterIndex is the index of the parameter an expression names.
func parameterIndex(params *ast.FieldList, expression ast.Expr) int {
	ident, isIdent := expression.(*ast.Ident)
	if !isIdent || params == nil {
		return -1
	}
	at := 0
	for _, group := range params.List {
		for _, name := range group.Names {
			if name.Name == ident.Name {
				return at
			}
			at++
		}
	}
	return -1
}

// callee is the function or method a call names, where the source has it.
func (code source) callee(scope answerScope, call *ast.CallExpr) *ast.FuncDecl {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		// A method by its name alone is a variable holding a function.
		if found := code.functions[function.Name]; found != nil && found.Recv == nil {
			return found
		}
		return nil
	case *ast.SelectorExpr:
		if variable, named := function.X.(*ast.Ident); named {
			if method := code.methods[code.scopeType(scope, variable.Name)+"."+function.Sel.Name]; method != nil {
				return method
			}
		}
		return code.functions[function.Sel.Name]
	}
	return nil
}

// valueType is the type an answered value has: written out, a variable's,
// or what the function that made it returns in its place.
func (code source) valueType(scope answerScope, expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.UnaryExpr:
		if value.Op == token.AND {
			return code.valueType(scope, value.X)
		}
	case *ast.CompositeLit:
		return answerTypeName(value.Type)
	case *ast.CallExpr:
		if maker, isIdent := value.Fun.(*ast.Ident); isIdent && maker.Name == "make" && len(value.Args) > 0 {
			return answerTypeName(value.Args[0])
		}
		return code.resultType(scope, value, 0)
	case *ast.Ident:
		return code.scopeType(scope, value.Name)
	}
	return ""
}

// resultType is the type of a call's result at index.
func (code source) resultType(scope answerScope, call *ast.CallExpr, index int) string {
	function := code.callee(scope, call)
	if function == nil || function.Type.Results == nil {
		return ""
	}
	at := 0
	for _, group := range function.Type.Results.List {
		count := max(len(group.Names), 1)
		if index < at+count {
			return answerTypeName(group.Type)
		}
		at += count
	}
	return ""
}

// scopeType is the type a variable of scope holds: a parameter's or the
// receiver's, the one it is declared with, or what it is first given.
func (code source) scopeType(scope answerScope, variable string) string {
	for _, fields := range []*ast.FieldList{scope.recv, scope.params} {
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				if name.Name == variable {
					return answerTypeName(field.Type)
				}
			}
		}
	}
	kind := ""
	ast.Inspect(scope.body, func(node ast.Node) bool {
		if kind != "" {
			return false
		}
		switch node := node.(type) {
		case *ast.ValueSpec:
			for _, name := range node.Names {
				if name.Name == variable && node.Type != nil {
					kind = answerTypeName(node.Type)
				}
			}
		case *ast.AssignStmt:
			for index, left := range node.Lhs {
				if ident, isIdent := left.(*ast.Ident); !isIdent || ident.Name != variable {
					continue
				}
				if len(node.Rhs) == len(node.Lhs) {
					kind = code.valueType(scope, node.Rhs[index])
				} else if call, isCall := node.Rhs[0].(*ast.CallExpr); isCall && len(node.Rhs) == 1 {
					kind = code.resultType(scope, call, index)
				}
			}
		}
		return kind == ""
	})
	return kind
}

// answerTypeName names a record's type, or a list of one ([]record); an
// error, a map or a type from another package names none.
func answerTypeName(expression ast.Expr) string {
	if list, isList := expression.(*ast.ArrayType); isList && list.Len == nil {
		if element := answerTypeName(list.Elt); element != "" {
			return "[]" + element
		}
		return ""
	}
	if name := typeIdent(expression); name != "error" && name != "any" && name != "string" && name != "bool" && name != "int" {
		return name
	}
	return ""
}
