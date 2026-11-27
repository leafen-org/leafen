package main

import (
	"go/ast"
	"go/token"
	"slices"
	"sort"
	"strings"
)

// What a body's fields take, as the checks that turn a value away say it
// (2026-11-26, real agents: an export's options, a map of true/false, gave
// no key, so every try was refused; a webhook's eventTypes gave no event,
// so page.updated was sent again and again). Beside the values a check
// compares a field with (values.go), a field takes only these where a check
// refuses the request otherwise — its values are then all it takes
// (closed):
//
//   - a value not in a list (!slices.Contains(list, value), !list[value],
//     _, ok := list[value]; !ok), the list written out (lists.go), or one a
//     predicate turns away (!validTLSMode(value), predicates.go);
//   - a value that isn't one written out (value != "a" && value != "b"), or
//     a switch on it whose default refuses;
//   - in a valid… function, an answer that holds only when it is in such a
//     list (value == nil || validDisplay(*value)).
//
// A text field takes the values, a list's every entry does (ranged over),
// and a map only them as keys. A map read by a key written out
// (values["includeFiles"]) has that key, and one refused without it
// (_, present := values["includeFiles"]; if !present { refuse }) needs it.
//
// Read wherever the handler holds a field: the body's own fields and those
// of the records they hold (input.SMTP.TLSMode), a record written from a
// field (entry := T{method: input.Method}; entry.method), a variable given
// one, the entries of a list ranged over, and in what it hands the body, a
// record of it or a field to, as deep as shapeDepth.

// shapeDepth is how many calls deep a handler's checks are read: an
// automation's schedule is checked five below its route, a button's block
// template seven.
const shapeDepth = 8

type shapeMode int

const (
	shapeSelf shapeMode = iota
	shapeEntry
	shapeKey
)

// shapeSubject is the field a variable or an expression reads, and how:
// itself, a list's entry or a map's key.
type shapeSubject struct {
	field *field
	mode  shapeMode
	// made is a field read through a record written from it.
	made bool
}

// shapeScope is what one function's variables hold: a record (vars), a
// field (named), or, for a record written out, a field by its key (made).
type shapeScope struct {
	code    source
	records map[string][]field
	vars    map[string]string
	named   map[string]shapeSubject
	made    map[string]shapeSubject
	local   map[string][]string
	// validator is a valid… function's, whose answer the request needs;
	// validating one whose false answer turns a value away (refuses);
	// problems one whose text answer names what is wrong (…Problem).
	validator, validating, problems bool
	// lookups are the variables a lookup in a list gives (_, known :=
	// list[value]), each with the value and the list.
	lookups map[string]shapeLookup
	// mapped are the variables given a call on one value (limit :=
	// intervalLimit(input.Frequency)), each with the call; closures those
	// given a function written in place (validSections := func(…) bool {…}).
	mapped   map[string]ast.Expr
	closures map[string]*ast.FuncLit
	// lists are the variables holding a list of one of the input's records
	// (a map of lists' entry), and madeLists the lists of records written
	// out from its parts, each with its entries' fields by their paths.
	lists     map[string]string
	madeLists map[string]map[string]shapeSubject
	// refusals are the variables given a refusal (invalid := errInvalid…).
	refusals map[string]bool
	// guards are the steps a branch takes when a field is given, each with
	// that field, and guard the one of the check being read.
	guards map[ast.Stmt]*field
	guard  *field
	// collect takes what a switch case's checks close, and mention each
	// field its steps read (caseChecks); depth is how many calls deep.
	collect func(shapeSubject, []string)
	mention func(*field)
	forbid  func(*field)
	depth   int
}

// shapeLookup is a value looked up in a list, and the list.
type shapeLookup struct {
	value  ast.Expr
	values []string
}

// shapes adds to every field of an input's records what the checks of its
// handler, and of what it hands them to, say it takes; record is the body's.
func (code source) shapes(body *ast.BlockStmt, variable string, records map[string][]field, record string) {
	scope := code.newShapeScope(body, records)
	scope.vars[variable] = record
	scope.walk(body, map[string]bool{})
	// A field a check closes takes what that check lets through, with any
	// value another step compares it with (a path it didn't read may let it).
	for name := range records {
		for index := range records[name] {
			held := &records[name][index]
			closed := held.closedValues
			if len(closed) == 0 {
				closed = held.madeValues
			}
			for _, value := range closed {
				held.Values, held.Closed = sortedAdd(held.Values, value), true
			}
			// One a closing check turns away by name (blockType == "code").
			if held.Closed {
				held.Values = slices.DeleteFunc(held.Values, func(value string) bool { return slices.Contains(held.refusedValues, value) })
			}
		}
	}
}

func (code source) newShapeScope(body *ast.BlockStmt, records map[string][]field) *shapeScope {
	return &shapeScope{code: code, records: records, vars: map[string]string{}, named: map[string]shapeSubject{},
		made: map[string]shapeSubject{}, local: code.localLists(body), lookups: map[string]shapeLookup{},
		mapped: map[string]ast.Expr{}, closures: map[string]*ast.FuncLit{}, lists: map[string]string{},
		madeLists: map[string]map[string]shapeSubject{}, refusals: map[string]bool{}, guards: map[ast.Stmt]*field{}}
}

// fieldOf is a record's field by its Go name.
func (scope *shapeScope) fieldOf(record, name string) *field {
	fields := scope.records[record]
	for index := range fields {
		// grant_type is GrantType.
		if strings.EqualFold(strings.ReplaceAll(fields[index].Name, "_", ""), name) {
			return &fields[index]
		}
	}
	return nil
}

// recordNamed is the record a field's type names, and whether it is a list of it.
func (scope *shapeScope) recordNamed(kind string) (string, bool) {
	list := strings.HasSuffix(kind, "[]")
	name := strings.TrimSuffix(strings.TrimPrefix(kind, "*"), "[]")
	if _, known := scope.records[name]; known {
		return name, list
	}
	return "", false
}

// recordOf is the record an expression holds: a variable holding one, one
// written out, a field holding one, or an entry of a field holding a list
// of them.
func (scope *shapeScope) recordOf(expression ast.Expr) string {
	switch expression := unstarred(expression).(type) {
	case *ast.Ident:
		return scope.vars[expression.Name]
	case *ast.CompositeLit:
		// One of the input's records written out from its parts.
		if record, list := scope.recordNamed(typeIdent(expression.Type)); !list {
			return record
		}
	case *ast.SelectorExpr:
		owner := scope.recordOf(expression.X)
		if found := scope.fieldOf(owner, expression.Sel.Name); found != nil {
			if record, list := scope.recordNamed(found.Type); !list {
				return record
			}
		}
		// input.mcpServiceAddition: a struct the record embeds, its fields
		// the record's own.
		if owner != "" && scope.code.structs[expression.Sel.Name] != nil {
			return owner
		}
	case *ast.IndexExpr:
		if held, known := scope.subject(expression.X); known && held.mode == shapeSelf {
			if record, list := scope.recordNamed(held.field.Type); list {
				return record
			}
		}
	}
	return ""
}

// subject is the field an expression reads, through a call that takes it
// alone (strings.TrimSpace, strings.ToLower).
func (scope *shapeScope) subject(expression ast.Expr) (shapeSubject, bool) {
	for {
		call, isCall := unstarred(expression).(*ast.CallExpr)
		if !isCall || len(call.Args) != 1 {
			break
		}
		expression = call.Args[0]
	}
	switch expression := unstarred(expression).(type) {
	case *ast.Ident:
		held, known := scope.named[expression.Name]
		return held, known
	case *ast.IndexExpr:
		// An entry of a list: kinds[index] (a map's entry is its value).
		if held, known := scope.subject(expression.X); known && held.mode == shapeSelf && strings.HasSuffix(held.field.Type, "[]") &&
			!strings.HasPrefix(held.field.Type, "map of ") {
			return shapeSubject{field: held.field, mode: shapeEntry}, true
		}
	case *ast.SelectorExpr:
		if owner, isIdent := expression.X.(*ast.Ident); isIdent {
			if held, known := scope.made[owner.Name+"."+expression.Sel.Name]; known {
				held.made = true
				return held, true
			}
		}
		if found := scope.fieldOf(scope.recordOf(expression.X), expression.Sel.Name); found != nil {
			if scope.mention != nil {
				scope.mention(found)
			}
			return shapeSubject{field: found, mode: shapeSelf}, true
		}
	}
	return shapeSubject{}, false
}

// hold reads what a function's variables are given: a field or a record of
// the input, a record written from its fields, a list's entries or a map's
// keys ranged over.
func (scope *shapeScope) hold(body *ast.BlockStmt) {
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			scope.listHeld(node)
			// _, known := list[value]
			if len(node.Lhs) == 2 && len(node.Rhs) == 1 {
				index, isIndex := node.Rhs[0].(*ast.IndexExpr)
				present, isIdent := node.Lhs[1].(*ast.Ident)
				if isIndex && isIdent {
					// A name two lookups of different values give (ok) says neither.
					values, isList := scope.code.listNamed(index.X, scope.local)
					if earlier, given := scope.lookups[present.Name]; given && !scope.sameSubject(earlier.value, index.Index) {
						values = nil
					}
					if isList || values == nil {
						scope.lookups[present.Name] = shapeLookup{index.Index, values}
					}
				}
			}
			for index, left := range node.Lhs {
				ident, isIdent := left.(*ast.Ident)
				if !isIdent || ident.Name == "_" || len(node.Lhs) != len(node.Rhs) {
					continue
				}
				right := node.Rhs[index]
				if pointer, isPointer := right.(*ast.UnaryExpr); isPointer && pointer.Op == token.AND {
					right = pointer.X
				}
				if record := scope.recordOf(right); record != "" {
					scope.vars[ident.Name] = record
				} else if held, known := scope.subject(right); known {
					scope.named[ident.Name] = held
				}
				if call, isCall := right.(*ast.CallExpr); isCall && len(call.Args) == 1 {
					scope.mapped[ident.Name] = call
				}
				if closure, isClosure := right.(*ast.FuncLit); isClosure {
					scope.closures[ident.Name] = closure
				}
				if given, isIdent := right.(*ast.Ident); isIdent && given.Name != "err" && strings.HasPrefix(given.Name, "err") {
					scope.refusals[ident.Name] = true
				}
				// added := options[propertyID]: a map of lists' entry.
				if index, isIndex := right.(*ast.IndexExpr); isIndex {
					if held, known := scope.subject(index.X); known && held.mode == shapeSelf {
						if kind, isMap := strings.CutPrefix(held.field.Type, "map of "); isMap {
							if record, list := scope.recordNamed(kind); record != "" && list {
								scope.lists[ident.Name] = record
							} else if record != "" {
								scope.vars[ident.Name] = record
							}
						}
					}
				}
				// choice := personChoice(person): a function answering one of
				// a record's fields, as it is or in its stead.
				if call, isCall := right.(*ast.CallExpr); isCall && len(call.Args) == 1 {
					if field := scope.code.fieldAnswered(calledName(call)); field != "" {
						if found := scope.fieldOf(scope.recordOf(call.Args[0]), field); found != nil {
							scope.named[ident.Name] = shapeSubject{field: found, mode: shapeSelf}
						}
					}
				}
				if literal, isLiteral := right.(*ast.CompositeLit); isLiteral {
					for _, element := range literal.Elts {
						pair, isPair := element.(*ast.KeyValueExpr)
						if !isPair {
							continue
						}
						if key, isKey := pair.Key.(*ast.Ident); isKey {
							if held, known := scope.subject(pair.Value); known {
								scope.made[ident.Name+"."+key.Name] = held
							}
						}
					}
				}
			}
		case *ast.CallExpr:
			// json.Unmarshal(input.ActionDocument, &document): the document is
			// the record the field holds as raw JSON.
			if calledName(node) == "Unmarshal" && len(node.Args) == 2 {
				if target, isIdent := unstarred(node.Args[1]).(*ast.Ident); isIdent && node.Args[1] != ast.Expr(target) {
					if record := scope.recordOf(node.Args[0]); record != "" {
						scope.vars[target.Name] = record
					}
				}
			}
		case *ast.RangeStmt:
			value, _ := node.Value.(*ast.Ident)
			// A list a map of lists holds (for _, added := range options),
			// or one a record written out holds (layout.Tabs of tabs made
			// from the body's parts).
			if list, isIdent := node.X.(*ast.Ident); isIdent && value != nil && scope.lists[list.Name] != "" {
				scope.vars[value.Name] = scope.lists[list.Name]
			}
			if made := scope.madeLists[shapePath(node.X)]; value != nil && made != nil {
				for rest, held := range made {
					scope.made[value.Name+"."+rest] = held
				}
			}
			held, known := scope.subject(node.X)
			if !known || held.mode != shapeSelf {
				return true
			}
			if kind, isMap := strings.CutPrefix(held.field.Type, "map of "); isMap && value != nil {
				if record, list := scope.recordNamed(kind); record != "" && list {
					scope.lists[value.Name] = record
					return true
				} else if record != "" {
					scope.vars[value.Name] = record
					return true
				}
			}
			if key, isIdent := node.Key.(*ast.Ident); isIdent && key.Name != "_" && strings.HasPrefix(held.field.Type, "map of ") {
				scope.named[key.Name] = shapeSubject{field: held.field, mode: shapeKey}
			}
			if value == nil || value.Name == "_" || !strings.HasSuffix(held.field.Type, "[]") {
				return true
			}
			if record, list := scope.recordNamed(held.field.Type); list {
				scope.vars[value.Name] = record
			} else {
				scope.named[value.Name] = shapeSubject{field: held.field, mode: shapeEntry}
			}
		}
		return true
	})
}

// close keeps a list's texts as all the subject's field takes.
func (held shapeSubject) close(values []string) {
	switch {
	case held.mode == shapeKey:
		for _, value := range values {
			held.field.Keys = sortedAdd(held.field.Keys, value)
		}
	case held.made && (held.mode == shapeEntry || held.field.Type == "string"):
		// What a record written from the field lets through (an entry made
		// for another record's check) counts where the field's own checks
		// say nothing.
		for _, value := range values {
			held.field.madeValues = sortedAdd(held.field.madeValues, value)
		}
	case held.mode == shapeEntry || held.field.Type == "string":
		for _, value := range values {
			held.field.closedValues = sortedAdd(held.field.closedValues, value)
		}
	}
}

// unstarred is an expression without the pointer, its address or brackets
// around it.
func unstarred(expression ast.Expr) ast.Expr {
	for {
		switch current := expression.(type) {
		case *ast.StarExpr:
			expression = current.X
		case *ast.ParenExpr:
			expression = current.X
		case *ast.UnaryExpr:
			if current.Op != token.AND {
				return expression
			}
			expression = current.X
		default:
			return expression
		}
	}
}

// calledName is a call's function name, a method's alone.
func calledName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fun.Sel.Name
	case *ast.Ident:
		return fun.Name
	}
	return ""
}

func sortedAdd(values []string, value string) []string {
	if value == "" || slices.Contains(values, value) {
		return values
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

// sameSubject tells two expressions that read the same field the same way.
func (scope *shapeScope) sameSubject(one, other ast.Expr) bool {
	first, known := scope.subject(one)
	second, alsoKnown := scope.subject(other)
	return known && alsoKnown && first == second
}
