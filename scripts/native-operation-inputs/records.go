package main

import (
	"go/ast"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type field struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Values   []string `json:"values,omitempty"`
	Required bool     `json:"required,omitempty"` // required.go
	// Keys are the keys a map takes, and RequiredKeys those it is refused
	// without (shapes.go).
	Keys         []string `json:"keys,omitempty"`
	RequiredKeys []string `json:"requiredKeys,omitempty"`
	// Closed says its values are all it takes: a check refuses any other
	// (shapes.go); closedValues gathers them.
	Closed       bool `json:"closed,omitempty"`
	closedValues []string
	// madeValues are those a record written from it lets through, and
	// refusedValues those a closing check turns away by name (shapes.go).
	madeValues, refusedValues []string
	// defaulted is a field its handler sets when it comes empty (defaults.go).
	defaulted bool
	// goName is its Go name, which a check reads it by where its JSON name
	// is another (device_code: DeviceCode).
	goName string
}

// named says whether the field goes by a name a check reads.
func (entry field) named(name string) bool {
	return name != "" && (strings.EqualFold(entry.Name, name) || entry.goName == name)
}

// input is what an operation's handler reads, and what it answers.
type input struct {
	Record  string             `json:"record,omitempty"`
	Records map[string][]field `json:"records,omitempty"`
	// Actions are the records of a body read again by what its field says.
	Actions map[string]string `json:"actions,omitempty"`
	Query   []field           `json:"query,omitempty"`  // query.go
	Answer  string            `json:"answer,omitempty"` // answer.go
}

// The records a body is read into: the type a variable is declared with,
// and each struct's fields by their JSON names and types.

// declaredType reads the type a function declares a variable with: its name,
// or the struct written where it is declared.
func declaredType(body *ast.BlockStmt, variable string) (string, *ast.StructType) {
	kind := ""
	var inline *ast.StructType
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			for index, left := range node.Lhs {
				ident, isIdent := left.(*ast.Ident)
				if !isIdent || ident.Name != variable || index >= len(node.Rhs) {
					continue
				}
				if literal, isLiteral := node.Rhs[index].(*ast.CompositeLit); isLiteral {
					if named, isNamed := literal.Type.(*ast.Ident); isNamed {
						kind = named.Name
					}
					if written, isStruct := literal.Type.(*ast.StructType); isStruct {
						inline = written
					}
				}
			}
		case *ast.ValueSpec:
			for _, name := range node.Names {
				if name.Name == variable {
					if named, isNamed := node.Type.(*ast.Ident); isNamed {
						kind = named.Name
					}
					if written, isStruct := node.Type.(*ast.StructType); isStruct {
						inline = written
					}
				}
			}
		}
		return true
	})
	return kind, inline
}

// addRecord writes a struct's fields and those of the structs it holds.
func (code source) addRecord(records map[string][]field, name string) {
	if _, done := records[name]; done {
		return
	}
	body := code.structs[name]
	records[name] = []field{}
	fields := []field{}
	for _, item := range body.Fields.List {
		tag := ""
		if item.Tag != nil {
			if value, err := strconv.Unquote(item.Tag.Value); err == nil {
				tag = structTag(value)
			}
		}
		if tag == "-" {
			continue
		}
		if len(item.Names) == 0 {
			if embedded, isNamed := item.Type.(*ast.Ident); isNamed && code.structs[embedded.Name] != nil {
				code.addRecord(records, embedded.Name)
				fields = append(fields, records[embedded.Name]...)
			}
			continue
		}
		for _, itemName := range item.Names {
			if !itemName.IsExported() {
				continue
			}
			jsonName := tag
			if jsonName == "" {
				jsonName = strings.ToLower(itemName.Name[:1]) + itemName.Name[1:]
			}
			kind := code.typeName(records, item.Type)
			if named, raw := rawRecords[jsonName]; raw && kind == "any" && code.structs[named] != nil {
				code.addRecord(records, named)
				kind = named
			}
			entry := field{Name: jsonName, Type: kind, Required: neededUnseen[name+"."+jsonName] != "", goName: itemName.Name}
			if named := strings.TrimSuffix(strings.TrimPrefix(kind, "*"), "[]"); len(code.typed[named]) > 0 {
				entry.Values = append([]string{}, code.typed[named]...)
				sort.Strings(entry.Values)
			}
			if named, listed := stringValues[name+"."+jsonName]; listed {
				for _, constant := range named {
					if constant != "" && !strings.HasSuffix(constant, "*") {
						entry.Values = append(entry.Values, code.consts[constant])
						continue
					}
					for other, value := range code.consts {
						if strings.HasPrefix(other, strings.TrimSuffix(constant, "*")) {
							entry.Values = append(entry.Values, value)
						}
					}
				}
				sort.Strings(entry.Values)
			}
			if len(entry.Values) == 0 {
				entry.Values = slices.Clone(code.choices[name][itemName.Name])
			}
			fields = append(fields, entry)
		}
	}
	records[name] = fields
}

func structTag(tag string) string {
	const key = `json:"`
	at := strings.Index(tag, key)
	if at < 0 {
		return ""
	}
	rest := tag[at+len(key):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	name, _, _ := strings.Cut(rest[:end], ",")
	return name
}

func (code source) typeName(records map[string][]field, expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		if code.structs[expression.Name] != nil {
			code.addRecord(records, expression.Name)
		}
		return expression.Name
	case *ast.StarExpr:
		return code.typeName(records, expression.X)
	case *ast.ArrayType:
		return code.typeName(records, expression.Elt) + "[]"
	case *ast.MapType:
		return "map of " + code.typeName(records, expression.Value)
	case *ast.SelectorExpr:
		if expression.Sel.Name == "RawMessage" {
			return "any"
		}
		return expression.Sel.Name
	case *ast.InterfaceType:
		return "any"
	case *ast.StructType:
		return "object"
	}
	return "any"
}
