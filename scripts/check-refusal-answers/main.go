// Command check-refusal-answers fails when the server could refuse without
// saying why, or tell a refusal whose cause it knows as one that passes by
// trying again. An agent at work told "The database row changed. Try
// again." for a row it moved into another workspace, or "Leafen couldn't
// save the page … Try once more." for a column holding an odd option, sent
// the same call again and again, and learned why only from another tool or
// by taking things out one at a time (2026-11-26). It reads every error
// answer the server writes — apiError and its siblings, and refusedFor — and
// every refusal a tool makes (mcpInvalid, mcpDenied, mcpConfirmationNeeded,
// refuse), and refuses:
//
//   - a code that isn't one the screen and agents read as Leafen's
//     (LEAFEN_…, ORGANIZATION_…, or a contract's own family, contractCodes);
//   - blank words;
//   - words telling to try again on a refusal of the request itself (400,
//     401, 403, 404, 405, 410, 413, 415, 422), on a refusal for its reason
//     (refusedFor, which is never a failure's or a limit's status), or in a
//     tool's refusal, unless they name a cause that passes (toolRetries);
//   - a failure answered while an error is in hand and not told apart — in
//     the body of `if err != nil`, or in the default of a switch that tells
//     errors apart — without that error as its cause: the one error writer
//     finds a refusal for a reason in the cause and answers it as itself
//     (api_error_refused_not_changed.go), and a tool's failure is said as the
//     refusal its cause holds at the call's end (mcp_refused_failure.go);
//     dropped there, the refusal goes out as the failure's "try again".
//     A failure is an answer of 500 or more, or one whose words tell to try
//     again; a tool's is errMCPToolFailed or errMCPPageUpdateFailed, which
//     carries its cause by failedBecause.
//
// Codes and words are read where they are written out: a literal, a
// constant, a Sprintf format, a sum of them; words made elsewhere are their
// maker's. A site kept on purpose is in kept, with why.
//
//	go run ./scripts/check-refusal-answers
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

var sources = []string{"internal/app", "organization"}

// answer is where a writer takes its status, code, words and cause (-1:
// it takes none).
type answer struct{ status, code, words, cause int }

var writers = map[string]answer{
	"apiError":                   {1, 2, 3, -1},
	"apiErrorAndFlush":           {1, 2, 3, -1},
	"apiErrorFrom":               {1, 2, 3, 4},
	"apiErrorWithCause":          {1, 2, 3, 7},
	"writeAPIError":              {1, 2, 3, 7},
	"writeAPIErrorWithReference": {1, 2, 3, 8},
	"refusedFor":                 {0, 1, 2, -1},
}

// toolRefusals are where a tool's refusal makers take their words.
var toolRefusals = map[string]int{"mcpInvalid": 0, "mcpDenied": 0, "mcpConfirmationNeeded": 0, "refuse": 1}

// toolFailures are a tool's failures, which carry their cause.
var toolFailures = map[string]bool{"errMCPToolFailed": true, "errMCPPageUpdateFailed": true}

// contractCodes are code families a contract of its own names, read by its
// screen by that contract.
var contractCodes = map[string]string{
	"URL_METADATA_": "contracts/url-metadata-wire.json: the link preview's and the linked PDF's codes",
}

// toolRetries are a tool's refusals that tell to try again, by their words,
// with the cause that passes.
var toolRetries = map[string]string{
	"the template changed meanwhile or is gone: fetch it again and retry":                                              "a change met meanwhile: a fetch shows what is there now",
	"the page's content is being replaced: try again when that is done":                                                "a replacement under way, which ends",
	"it moved or changed while this call ran: fetch it again and retry":                                                "a change met meanwhile",
	"that page or item, or where it goes, changed while this call ran or is being converted: fetch them and try again": "a change or a conversion under way",
}

// retriesSaid are the toolRetries some tool says.
var retriesSaid = map[string]bool{}

// kept are the sites, by file and function, a rule leaves on purpose.
var kept = map[string]string{
	// Begin fails only when the server stops: no refusal can be in it.
	"internal/app/administrator_mutation_lifecycle.go Wrap": "its error is the server stopping its administrator changes",
}

var statuses = map[string]int{
	"StatusBadRequest": 400, "StatusUnauthorized": 401, "StatusPaymentRequired": 402, "StatusForbidden": 403, "StatusNotFound": 404,
	"StatusMethodNotAllowed": 405, "StatusNotAcceptable": 406, "StatusRequestTimeout": 408, "StatusConflict": 409, "StatusGone": 410,
	"StatusLengthRequired": 411, "StatusPreconditionFailed": 412, "StatusRequestEntityTooLarge": 413, "StatusRequestURITooLong": 414,
	"StatusUnsupportedMediaType": 415, "StatusRequestedRangeNotSatisfiable": 416, "StatusExpectationFailed": 417,
	"StatusMisdirectedRequest": 421, "StatusUnprocessableEntity": 422, "StatusLocked": 423, "StatusFailedDependency": 424,
	"StatusTooEarly": 425, "StatusUpgradeRequired": 426, "StatusPreconditionRequired": 428, "StatusTooManyRequests": 429,
	"StatusRequestHeaderFieldsTooLarge": 431, "StatusUnavailableForLegalReasons": 451, "StatusInternalServerError": 500,
	"StatusNotImplemented": 501, "StatusBadGateway": 502, "StatusServiceUnavailable": 503, "StatusGatewayTimeout": 504,
	"StatusInsufficientStorage": 507,
}

// requestRefusals are the statuses of a refusal of the request itself:
// what it asks, who asks it or what it names, none of which passes by
// asking again.
var requestRefusals = map[int]bool{400: true, 401: true, 403: true, 404: true, 405: true, 410: true, 413: true, 415: true, 422: true}

var leafenCode = regexp.MustCompile(`^(LEAFEN|ORGANIZATION)_[A-Z0-9_]+$`)

// retry finds words telling to try again: "try again", "try it once more",
// "… and retry", "retry later", "try later" — not "retry" named as a thing
// ("No failed delivery to retry.").
var retry = regexp.MustCompile(`(?i)\btry (it |this |that |the \w+ )?(again|once more)\b|(\band|\bthen|[,:;—]) ?retry\b|\bretry (it|this|that|later|shortly|soon|in)\b|\btry later\b|\bagain later\b`)

type site struct {
	path, function string
	position       token.Position
}

func main() {
	fileSet := token.NewFileSet()
	files := map[string]*ast.File{}
	paths := []string{}
	for _, source := range sources {
		found, err := filepath.Glob(filepath.Join(source, "*.go"))
		if err != nil || len(found) == 0 {
			fail("no Go source in %s", source)
		}
		for _, path := range found {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				fail("%v", err)
			}
			files[path] = file
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	words := constants(files)
	problems := []string{}
	used := map[string]bool{}
	read := 0
	for _, path := range paths {
		function := ""
		stack := []ast.Node{}
		ast.Inspect(files[path], func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, node)
			if declared, ok := node.(*ast.FuncDecl); ok {
				function = declared.Name.Name
			}
			at := site{path: path, function: function, position: fileSet.Position(node.Pos())}
			switch typed := node.(type) {
			case *ast.CallExpr:
				name := calledName(typed)
				if shape, ok := writers[name]; ok && len(typed.Args) > shape.words {
					read++
					problems = append(problems, checkAnswer(words, at, stack, name, shape, typed, used)...)
				}
				if index, ok := toolRefusals[name]; ok && len(typed.Args) > index {
					read++
					problems = append(problems, checkToolRefusal(words, at, name, typed.Args[index])...)
				}
			case *ast.ReturnStmt:
				for _, result := range typed.Results {
					if ident, ok := result.(*ast.Ident); ok && toolFailures[ident.Name] {
						if held := errorInHand(stack); held != "" && !keptAt(at, used) {
							problems = append(problems, fmt.Sprintf("%s: %s returned while %s is in hand drops it — failedBecause(%s, %s) carries it, so a refusal for a reason in it is said as itself", at.position, ident.Name, held, ident.Name, held))
						}
					}
				}
			}
			return true
		})
	}
	for key := range kept {
		if !used[key] {
			problems = append(problems, fmt.Sprintf("kept %q no longer names a site any rule refuses: take it out", key))
		}
	}
	for words := range toolRetries {
		if !retriesSaid[words] {
			problems = append(problems, fmt.Sprintf("toolRetries %q are no tool's words now: take them out", words))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		fail("Refusals that don't say why, or tell a known cause to try again (scripts/check-refusal-answers):\n%s", strings.Join(problems, "\n"))
	}
	fmt.Printf("Every refusal written out names its code and words, and a known cause is never told to try again: %d answers and refusals.\n", read)
}

func checkAnswer(words map[string]string, at site, stack []ast.Node, name string, shape answer, call *ast.CallExpr, used map[string]bool) []string {
	problems := []string{}
	status := statusOf(call.Args[shape.status])
	code, codeKnown := textOf(words, call.Args[shape.code])
	said, saidKnown := textOf(words, call.Args[shape.words])
	if codeKnown && !leafenCode.MatchString(code) && contractFamily(code) == "" {
		problems = append(problems, fmt.Sprintf("%s: %s answers with the code %q, not one read as Leafen's (LEAFEN_… or ORGANIZATION_…)", at.position, name, code))
	}
	if saidKnown && strings.TrimSpace(strings.Trim(said, ".:;, ")) == "" {
		problems = append(problems, fmt.Sprintf("%s: %s answers %s with no words: say what was refused and why", at.position, name, codeOr(code)))
	}
	telling := saidKnown && retry.MatchString(said)
	if name == "refusedFor" {
		if status >= 500 || status == 408 || status == 425 || status == 429 {
			problems = append(problems, fmt.Sprintf("%s: refusedFor with %d: a refusal for its reason is never a failure's or a limit's", at.position, status))
		}
		if telling {
			problems = append(problems, fmt.Sprintf("%s: refusedFor tells to try again (%q): a refusal for its reason says the reason and the way that does it", at.position, said))
		}
	} else if telling && requestRefusals[status] {
		problems = append(problems, fmt.Sprintf("%s: %s refuses the request (%d) but tells to try again (%q): its cause doesn't pass by asking again — say it and the way that does it", at.position, name, status, said))
	}
	if name == "refusedFor" || status != 0 && status < 500 && !telling {
		return problems
	}
	if status == 0 && !telling {
		return problems
	}
	held := errorInHand(stack)
	if held == "" || shape.cause >= 0 && len(call.Args) > shape.cause && !isNil(call.Args[shape.cause]) {
		return problems
	}
	if keptAt(at, used) {
		return problems
	}
	return append(problems, fmt.Sprintf("%s: %s answers a failure while %s is in hand without it as its cause — apiErrorFrom(…, %s) carries it, so a refusal for a reason in it is said as itself", at.position, name, held, held))
}

func checkToolRefusal(words map[string]string, at site, name string, argument ast.Expr) []string {
	said, known := textOf(words, argument)
	if !known {
		return nil
	}
	if strings.TrimSpace(said) == "" {
		return []string{fmt.Sprintf("%s: %s with no words: say what was refused and why", at.position, name)}
	}
	if retry.MatchString(said) && toolRetries[said] != "" {
		retriesSaid[said] = true
	}
	if retry.MatchString(said) && toolRetries[said] == "" {
		return []string{fmt.Sprintf("%s: %s tells to try again (%q): say the cause, and the way that does it; one that passes (a change met meanwhile, a hold that ends) goes in toolRetries with it", at.position, name, said)}
	}
	return nil
}

func keptAt(at site, used map[string]bool) bool {
	key := filepath.ToSlash(at.path) + " " + at.function
	if kept[key] != "" {
		used[key] = true
		return true
	}
	return false
}

func contractFamily(code string) string {
	for prefix, contract := range contractCodes {
		if strings.HasPrefix(code, prefix) {
			return contract
		}
	}
	return ""
}

func codeOr(code string) string {
	if code == "" {
		return "its code"
	}
	return code
}

// errorInHand is the error a node is reached with, not told apart: the
// one `if <error> != nil` (or `case <error> != nil:`) checked around it, or
// "the error" in the default of a switch that tells errors apart. A branch that tells it apart
// (errors.Is, errors.As, a comparison, an is… helper) knows what it is.
func errorInHand(stack []ast.Node) string {
	for index := len(stack) - 2; index >= 0; index-- {
		switch typed := stack[index].(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return ""
		case *ast.TypeSwitchStmt:
			return ""
		case *ast.CaseClause:
			for _, expression := range typed.List {
				if tellsApart(expression) {
					return ""
				}
			}
			for _, expression := range typed.List {
				if held := nilChecked(expression); held != "" {
					return held
				}
			}
			if typed.List == nil && index >= 2 {
				if outer, ok := stack[index-2].(*ast.SwitchStmt); ok && outer.Tag == nil && switchTellsApart(outer) {
					return "the error"
				}
			}
		case *ast.IfStmt:
			if index+1 >= len(stack) || stack[index+1] != typed.Body {
				continue
			}
			if tellsApart(typed.Cond) {
				return ""
			}
			if held := nilChecked(typed.Cond); held != "" {
				return held
			}
		}
	}
	return ""
}

func switchTellsApart(outer *ast.SwitchStmt) bool {
	for _, clause := range outer.Body.List {
		for _, expression := range clause.(*ast.CaseClause).List {
			if tellsApart(expression) {
				return true
			}
		}
	}
	return false
}

// tellsApart is whether a condition names what an error is: errors.Is or
// errors.As, an is… helper given it, or a comparison with a named error —
// not under a not.
func tellsApart(expression ast.Expr) bool {
	found := false
	var walk func(ast.Node, bool) bool
	walk = func(node ast.Node, negated bool) bool {
		switch typed := node.(type) {
		case *ast.UnaryExpr:
			if typed.Op == token.NOT {
				ast.Inspect(typed.X, func(inner ast.Node) bool { return inner == nil || walk(inner, true) })
				return false
			}
		case *ast.CallExpr:
			if negated {
				return true
			}
			switch called := typed.Fun.(type) {
			case *ast.SelectorExpr:
				if pkg, ok := called.X.(*ast.Ident); ok && pkg.Name == "errors" && (called.Sel.Name == "Is" || called.Sel.Name == "As") {
					found = true
				}
			case *ast.Ident:
				if strings.HasPrefix(called.Name, "is") && len(typed.Args) > 0 && namesError(typed.Args[0]) {
					found = true
				}
			}
		case *ast.BinaryExpr:
			if !negated && typed.Op == token.EQL && (namesError(typed.X) || namesError(typed.Y)) && !isNil(typed.X) && !isNil(typed.Y) {
				found = true
			}
		}
		return !found
	}
	ast.Inspect(expression, func(node ast.Node) bool { return node == nil || walk(node, false) })
	return found
}

// nilChecked is the error a condition finds there: `<error> != nil`, alone
// or in a sum of conditions.
func nilChecked(expression ast.Expr) string {
	binary, ok := expression.(*ast.BinaryExpr)
	if !ok {
		if paren, ok := expression.(*ast.ParenExpr); ok {
			return nilChecked(paren.X)
		}
		return ""
	}
	if binary.Op == token.LOR || binary.Op == token.LAND {
		if held := nilChecked(binary.X); held != "" {
			return held
		}
		return nilChecked(binary.Y)
	}
	if binary.Op != token.NEQ || !isNil(binary.Y) {
		return ""
	}
	if ident, ok := binary.X.(*ast.Ident); ok && namesError(ident) {
		return ident.Name
	}
	return ""
}

func namesError(expression ast.Expr) bool {
	ident, ok := expression.(*ast.Ident)
	if !ok {
		return false
	}
	name := ident.Name
	return name == "err" || strings.HasSuffix(name, "Err") || strings.HasSuffix(name, "Error") || name == "failure" || name == "cause"
}

func isNil(expression ast.Expr) bool {
	ident, ok := expression.(*ast.Ident)
	return ok && ident.Name == "nil"
}

func calledName(call *ast.CallExpr) string {
	if ident, ok := call.Fun.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

func statusOf(expression ast.Expr) int {
	switch typed := expression.(type) {
	case *ast.SelectorExpr:
		return statuses[typed.Sel.Name]
	case *ast.BasicLit:
		number, _ := strconv.Atoi(typed.Value)
		return number
	}
	return 0
}

// textOf is the words an expression writes out, and whether it does.
func textOf(words map[string]string, expression ast.Expr) (string, bool) {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		if typed.Kind == token.STRING {
			text, err := strconv.Unquote(typed.Value)
			return text, err == nil
		}
	case *ast.Ident:
		text, ok := words[typed.Name]
		return text, ok
	case *ast.BinaryExpr:
		if typed.Op == token.ADD {
			left, leftKnown := textOf(words, typed.X)
			right, rightKnown := textOf(words, typed.Y)
			return left + right, leftKnown && rightKnown
		}
	case *ast.CallExpr:
		if called, ok := typed.Fun.(*ast.SelectorExpr); ok && len(typed.Args) > 0 {
			if pkg, ok := called.X.(*ast.Ident); ok && pkg.Name == "fmt" && called.Sel.Name == "Sprintf" {
				return textOf(words, typed.Args[0])
			}
		}
	case *ast.ParenExpr:
		return textOf(words, typed.X)
	}
	return "", false
}

// constants are the words the package keeps at its top, by name.
func constants(files map[string]*ast.File) map[string]string {
	words := map[string]string{}
	for round := 0; round < 3; round++ {
		for _, file := range files {
			for _, declaration := range file.Decls {
				general, ok := declaration.(*ast.GenDecl)
				if !ok || general.Tok != token.CONST {
					continue
				}
				for _, spec := range general.Specs {
					value := spec.(*ast.ValueSpec)
					for index, expression := range value.Values {
						if index < len(value.Names) {
							if text, ok := textOf(words, expression); ok {
								words[value.Names[index].Name] = text
							}
						}
					}
				}
			}
		}
	}
	return words
}

func fail(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}
