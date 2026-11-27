// Command check-written-answers fails when a helper that may write the
// answer itself tells its caller so only through its error. A written answer's
// error is nil (apiError returns what event.JSON returns), so a caller that
// stops on err != nil goes on after the answer and writes a second one
// (2026-11-24: a data source move signed out answered 401, went on with an
// empty authorization and wrote a second error after it — two JSON objects
// in one body; signed out, six other routes answered 401 and then panicked
// on the missing person). Such a helper says it answered in a bool beside
// its error (the ok, handled or answered its callers read), never by the
// error alone.
//
// It refuses:
//   - a function with more than one result whose error can be a written
//     answer and which has no bool to say so;
//   - a call that discards that bool;
//   - a guard that goes on when a helper which answers on only some of its
//     paths returns nil (`if err := guard(event); err != nil { return err }`
//     followed by more): that nil may be an answer already written. A guard
//     that also reads the event's status or whether it was written is
//     answered by that.
//
// A writer is apiError and its siblings, an answer an event writes itself
// (event.JSON, NoContent …), and any function returning only an error that
// returns a writer's result.
//
//	go run ./scripts/check-written-answers
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var sources = []string{"internal/app", "organization"}

// writers are the functions that write an error answer.
var writers = []string{"apiError", "apiErrorFrom", "apiErrorWithCause", "apiErrorAndFlush", "writeAPIError", "writeAPIErrorWithReference"}

// eventWriters are the answers a request event writes itself.
var eventWriters = map[string]bool{
	"JSON": true, "NoContent": true, "String": true, "HTML": true, "Blob": true, "Stream": true,
	"Redirect": true, "XML": true, "FileFS": true,
}

// function is one function's results and own body (not its closures').
type function struct {
	results []ast.Expr
	body    *ast.BlockStmt
}

func main() {
	problems := []string{}
	for _, source := range sources {
		problems = append(problems, checkPackage(source)...)
	}
	if len(problems) > 0 {
		fail("Helpers that may write the answer tell it by their error alone (nil once written), so the caller goes on and answers again — say it in a bool beside the error (scripts/check-written-answers):\n%s", strings.Join(problems, "\n"))
	}
	fmt.Println("Every helper that may write the answer says so beside its error.")
}

func checkPackage(source string) []string {
	paths, err := filepath.Glob(filepath.Join(source, "*.go"))
	if err != nil || len(paths) == 0 {
		fail("no Go source in %s", source)
	}
	sort.Strings(paths)
	fileSet := token.NewFileSet()
	files := []*ast.File{}
	named := map[string][]*function{}
	methods := map[string][]*function{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			fail("%v", err)
		}
		files = append(files, file)
		for _, declaration := range file.Decls {
			declared, ok := declaration.(*ast.FuncDecl)
			if !ok || declared.Body == nil {
				continue
			}
			each, name := &function{results: resultTypes(declared.Type), body: declared.Body}, declared.Name.Name
			if declared.Recv != nil {
				methods[name] = append(methods[name], each)
			} else {
				named[name] = append(named[name], each)
			}
		}
	}
	answers := answering(named, methods)
	problems := []string{}
	// Every function, its closures too: one that answers through its error
	// beside other results says so in a bool.
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			var kind *ast.FuncType
			var body *ast.BlockStmt
			name := "a closure"
			switch typed := node.(type) {
			case *ast.FuncDecl:
				kind, body, name = typed.Type, typed.Body, typed.Name.Name
			case *ast.FuncLit:
				kind, body = typed.Type, typed.Body
			default:
				return true
			}
			results := resultTypes(kind)
			if body == nil {
				return true
			}
			if indexes := answers.sayingBools(results, body); indexes != nil && len(indexes) == 0 {
				problems = append(problems, fmt.Sprintf("%s: %s may return a written answer as its error with no bool beside it that says so", fileSet.Position(kind.Pos()), name))
			}
			return true
		})
	}
	// Every call: none drops the bool, no guard goes on after a nil that may
	// be an answer.
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				problems = append(problems, droppedSay(fileSet, answers, typed)...)
			case *ast.BlockStmt:
				problems = append(problems, guardsGoingOn(fileSet, answers, typed.List)...)
			case *ast.CaseClause:
				problems = append(problems, guardsGoingOn(fileSet, answers, typed.Body)...)
			case *ast.CommClause:
				problems = append(problems, guardsGoingOn(fileSet, answers, typed.Body)...)
			}
			return true
		})
	}
	return problems
}

// answerSet is what each function of one package does with the answer, its
// functions and its methods apart (a method is called on a value, a function
// by its name; a local value named as a function is neither).
type answerSet struct {
	functions, methods answerTable
	// saying names the functions with more results that may return a written
	// answer as their error, with the indexes of the bools that say so.
	saying map[string][]int
}

type answerTable struct {
	// writer names those returning only an error that may return a written
	// answer; partly those of them that may also return nil unanswered.
	writer, partly map[string]bool
}

// called is the table and name a call is read by, if it calls a function or
// a method by name and hands it the request event: an answer is written to
// the event, so a call that is not given it writes none (a method named as
// some other type's writer is not one).
func (set answerSet) called(expression ast.Expr) (answerTable, string, bool) {
	call, ok := unparen(expression).(*ast.CallExpr)
	if !ok || !passesEvent(call) {
		return answerTable{}, "", false
	}
	switch called := call.Fun.(type) {
	case *ast.Ident:
		// A value declared in the function (a closure kept in a variable) is
		// not the package's function of that name.
		if called.Obj != nil && called.Obj.Kind != ast.Fun {
			return answerTable{}, "", false
		}
		return set.functions, called.Name, true
	case *ast.SelectorExpr:
		return set.methods, called.Sel.Name, true
	}
	return answerTable{}, "", false
}

func (set answerSet) writes(expression ast.Expr) bool {
	if call, ok := unparen(expression).(*ast.CallExpr); ok {
		if called, ok := call.Fun.(*ast.SelectorExpr); ok && eventWriters[called.Sel.Name] {
			if receiver, ok := called.X.(*ast.Ident); ok && isEventName(receiver.Name) {
				return true
			}
		}
	}
	table, name, ok := set.called(expression)
	return ok && table.writer[name]
}

func (set answerSet) partlyWrites(expression ast.Expr) bool {
	table, name, ok := set.called(expression)
	return ok && table.partly[name]
}

// answering finds the writers to a fixed point: a function returning only an
// error is one when one of its returns is a writer's result.
func answering(named, methods map[string][]*function) answerSet {
	set := answerSet{
		functions: answerTable{writer: map[string]bool{}, partly: map[string]bool{}},
		methods:   answerTable{writer: map[string]bool{}, partly: map[string]bool{}},
		saying:    map[string][]int{},
	}
	for _, name := range writers {
		set.functions.writer[name] = true
	}
	tables := []struct {
		table     answerTable
		functions map[string][]*function
		named     bool
	}{{set.functions, named, true}, {set.methods, methods, false}}
	for changed := true; changed; {
		changed = false
		for _, each := range tables {
			for name, functions := range each.functions {
				for _, function := range functions {
					if each.table.writer[name] || len(function.results) != 1 || !isError(function.results[0]) {
						continue
					}
					eachReturn(function.body, func(statement *ast.ReturnStmt) {
						if len(statement.Results) == 1 && !each.table.writer[name] && set.writes(statement.Results[0]) {
							each.table.writer[name], changed = true, true
						}
					})
				}
			}
		}
	}
	// partly: a writer one of whose returns is no writer's result (nil, an
	// error from elsewhere, a guard that answers only partly).
	for changed := true; changed; {
		changed = false
		for _, each := range tables {
			for name, functions := range each.functions {
				if !each.table.writer[name] || each.table.partly[name] || each.named && isBaseWriter(name) {
					continue
				}
				for _, function := range functions {
					eachReturn(function.body, func(statement *ast.ReturnStmt) {
						if len(statement.Results) == 1 && !each.table.partly[name] &&
							(!set.writes(statement.Results[0]) || set.partlyWrites(statement.Results[0])) {
							each.table.partly[name], changed = true, true
						}
					})
				}
			}
		}
	}
	for name, functions := range named {
		for _, function := range functions {
			if indexes := set.sayingBools(function.results, function.body); len(indexes) > 0 {
				set.saying[name] = indexes
			}
		}
	}
	return set
}

// sayingBools are the bools of a function with more results that may return
// a written answer as its error which say so: the same true or false beside
// every written answer (ok false, handled true …). None when it returns no
// written answer; an empty list when nothing says it.
func (set answerSet) sayingBools(results []ast.Expr, body *ast.BlockStmt) []int {
	if len(results) < 2 || !isError(results[len(results)-1]) {
		return nil
	}
	last := len(results) - 1
	said := map[int]map[string]bool{}
	writes := false
	eachReturn(body, func(statement *ast.ReturnStmt) {
		if len(statement.Results) != len(results) || !set.writes(statement.Results[last]) {
			return
		}
		writes = true
		for _, index := range boolIndexes(results) {
			if said[index] == nil {
				said[index] = map[string]bool{}
			}
			said[index][identName(statement.Results[index])] = true
		}
	})
	if !writes {
		return nil
	}
	indexes := []int{}
	for _, index := range boolIndexes(results) {
		if len(said[index]) == 1 && (said[index]["true"] || said[index]["false"]) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

// droppedSay is a call of a function that says it answered whose bool is
// thrown away.
func droppedSay(fileSet *token.FileSet, set answerSet, statement *ast.AssignStmt) []string {
	if len(statement.Rhs) != 1 || len(statement.Lhs) < 2 {
		return nil
	}
	call, ok := statement.Rhs[0].(*ast.CallExpr)
	if !ok {
		return nil
	}
	called, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil
	}
	indexes, ok := set.saying[called.Name]
	if !ok {
		return nil
	}
	for _, index := range indexes {
		if index >= len(statement.Lhs) || identName(statement.Lhs[index]) != "_" {
			return nil
		}
	}
	return []string{fmt.Sprintf("%s: the bool saying %s answered is dropped", fileSet.Position(statement.Pos()), called.Name)}
}

// guardsGoingOn are the guards in a list of statements that go on after a
// helper which answers on only some of its paths returned nil.
func guardsGoingOn(fileSet *token.FileSet, set answerSet, statements []ast.Stmt) []string {
	problems := []string{}
	for index, statement := range statements {
		if index == len(statements)-1 {
			continue
		}
		var call ast.Expr
		var errName string
		var condition ast.Expr
		switch typed := statement.(type) {
		case *ast.IfStmt:
			assigned, ok := typed.Init.(*ast.AssignStmt)
			if !ok || len(assigned.Lhs) != 1 || len(assigned.Rhs) != 1 {
				continue
			}
			call, errName, condition = assigned.Rhs[0], identName(assigned.Lhs[0]), typed.Cond
		case *ast.AssignStmt:
			check, ok := statements[index+1].(*ast.IfStmt)
			if !ok || len(typed.Lhs) != 1 || len(typed.Rhs) != 1 || index+1 == len(statements)-1 {
				continue
			}
			call, errName, condition = typed.Rhs[0], identName(typed.Lhs[0]), check.Cond
		default:
			continue
		}
		if errName == "" || errName == "_" || !set.partlyWrites(call) || !checksNotNil(condition, errName) || readsAnswer(condition) {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s: goes on when %s returns nil, which may be an answer it wrote", fileSet.Position(call.Pos()), calledName(call)))
	}
	return problems
}

func checksNotNil(condition ast.Expr, name string) bool {
	found := false
	ast.Inspect(condition, func(node ast.Node) bool {
		binary, ok := node.(*ast.BinaryExpr)
		if ok && binary.Op == token.NEQ && identName(binary.X) == name && identName(binary.Y) == "nil" {
			found = true
		}
		return !found
	})
	return found
}

// readsAnswer is a condition reading the answer itself: its status or
// whether one was written.
func readsAnswer(condition ast.Expr) bool {
	found := false
	ast.Inspect(condition, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && (selector.Sel.Name == "Status" || selector.Sel.Name == "Written") {
			found = true
		}
		return !found
	})
	return found
}

func eachReturn(body *ast.BlockStmt, visit func(*ast.ReturnStmt)) {
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			visit(typed)
		}
		return true
	})
}

func resultTypes(kind *ast.FuncType) []ast.Expr {
	types := []ast.Expr{}
	if kind == nil || kind.Results == nil {
		return types
	}
	for _, field := range kind.Results.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			types = append(types, field.Type)
		}
	}
	return types
}

func boolIndexes(types []ast.Expr) []int {
	indexes := []int{}
	for index, each := range types {
		if identName(each) == "bool" {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func isError(expression ast.Expr) bool { return identName(expression) == "error" }

func isBaseWriter(name string) bool {
	for _, each := range writers {
		if each == name {
			return true
		}
	}
	return false
}

func passesEvent(call *ast.CallExpr) bool {
	for _, argument := range call.Args {
		if isEventName(identName(argument)) {
			return true
		}
	}
	return false
}

func isEventName(name string) bool {
	return name == "event" || name == "e" || name == "request"
}

func identName(expression ast.Expr) string {
	ident, ok := unparen(expression).(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

func calledName(expression ast.Expr) string {
	call, ok := unparen(expression).(*ast.CallExpr)
	if !ok {
		return ""
	}
	switch called := call.Fun.(type) {
	case *ast.Ident:
		return called.Name
	case *ast.SelectorExpr:
		return called.Sel.Name
	}
	return ""
}

func unparen(expression ast.Expr) ast.Expr {
	for {
		paren, ok := expression.(*ast.ParenExpr)
		if !ok {
			return expression
		}
		expression = paren.X
	}
}

func fail(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
