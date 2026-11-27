// Command check-word-formats fails when a person could read a format verb
// left in the server's words: every table of words in two languages
// (mcpWords) one of whose words holds a verb (%s, %d, %v …) is read with .in
// only as the format of a fmt call — Sprintf, Errorf, Printf, Fprintf — right
// where it is read (2026-11-23: an agent's request to connect asked the
// person "until you disconnect it in Leafen's %s", its words read without
// fmt). A table is named by the variable it is kept in; words handed on in
// a parameter are for the code that reads them.
//
//	go run ./scripts/check-word-formats
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const source = "internal/app"

var verb = regexp.MustCompile(`%(\[\d+\])?[-+# 0]*\d*(\.\d+)?[sdvqxXfgtTcbeEoOpU]`)

// formats are the fmt calls whose format argument is at the index named.
var formats = map[string]int{"Sprintf": 0, "Errorf": 0, "Printf": 0, "Fprintf": 1, "Appendf": 1}

func main() {
	paths, err := filepath.Glob(filepath.Join(source, "*.go"))
	if err != nil || len(paths) == 0 {
		fail("no Go source in %s", source)
	}
	sort.Strings(paths)
	fileSet := token.NewFileSet()
	files := []*ast.File{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			fail("%v", err)
		}
		files = append(files, file)
	}
	tables := formatTables(files)
	unformatted := []string{}
	for _, file := range files {
		stack := []ast.Node{}
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, node)
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			read, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || read.Sel.Name != "in" || !tables[tableName(read.X)] || formatOf(stack, call) {
				return true
			}
			unformatted = append(unformatted, fmt.Sprintf("%s: %s.in(…) is read outside a fmt call's format", fileSet.Position(call.Pos()), tableName(read.X)))
			return true
		})
	}
	if len(unformatted) > 0 {
		fail("Words holding a format verb, read without fmt (a person would read the verb):\n%s", strings.Join(unformatted, "\n"))
	}
	fmt.Printf("Server words with format verbs are read as formats: %d tables.\n", len(tables))
}

// formatTables are the variables kept at a file's top whose words in two
// languages hold a format verb.
func formatTables(files []*ast.File) map[string]bool {
	tables := map[string]bool{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				for index, expression := range value.Values {
					if index < len(value.Names) && holds(expression, isWordsType) && holds(expression, hasVerb) {
						tables[value.Names[index].Name] = true
					}
				}
			}
		}
	}
	return tables
}

func isWordsType(node ast.Node) bool {
	ident, ok := node.(*ast.Ident)
	return ok && ident.Name == "mcpWords"
}

func hasVerb(node ast.Node) bool {
	literal, ok := node.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return false
	}
	text, err := strconv.Unquote(literal.Value)
	return err == nil && verb.MatchString(strings.ReplaceAll(text, "%%", ""))
}

func holds(expression ast.Expr, test func(ast.Node) bool) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		found = found || node != nil && test(node)
		return !found
	})
	return found
}

// tableName is the variable a read's words come from: a table, or a map's.
func tableName(expression ast.Expr) string {
	for {
		switch typed := expression.(type) {
		case *ast.Ident:
			return typed.Name
		case *ast.IndexExpr:
			expression = typed.X
		case *ast.ParenExpr:
			expression = typed.X
		default:
			return ""
		}
	}
}

// formatOf says whether a read is the format argument of the fmt call
// around it.
func formatOf(stack []ast.Node, read *ast.CallExpr) bool {
	if len(stack) < 2 {
		return false
	}
	parent, ok := stack[len(stack)-2].(*ast.CallExpr)
	if !ok {
		return false
	}
	function, ok := parent.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if pkg, ok := function.X.(*ast.Ident); !ok || pkg.Name != "fmt" {
		return false
	}
	position, known := formats[function.Sel.Name]
	return known && len(parent.Args) > position && parent.Args[position] == ast.Expr(read)
}

func fail(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
