package app

import (
	"encoding/json"
)

// A formula's check reads a property it names as Notion's does (seen
// 2026-11-20 through its API, a synthetic database under a Notion test page): a
// formula by the type the check gives its expression, so prop("F") * 2 is
// judged as F written out — text for "a" (math on text and number), array
// for a list or a person's, undefined for empty() alone,
// number for if(false, 1, empty()), none for an if() whose results differ,
// which takes anything —, and a rollup by its calculation: a number for a
// count, sum, average, percent and the like, a date for the earliest, latest
// or range of dates, and text for the values shown as they are or each once
// (so first() on it is refused; Notion's editor reads those as a list,
// formulaPropertyTypes.ts). A request changing a
// data source checks the formulas it writes against the source with each of
// its other changes made but none of the formulas and rollups it writes: a
// new one isn't there yet (X not found), a changed one reads
// as before and a property turned into one as the type it had, while a new
// number, a type turned into another and a formula renamed alone are as the
// request leaves them. Once they check, one that comes back to itself through
// what it names is refused (notionFormulaCycleRefusal).

// notionFormulaCheckProperty is a property as the formula check reads it;
// Was is the name the request changed.
type notionFormulaCheckProperty struct {
	ID, Name, Type, Config, Was string
}

func notionFormulaCheckOf(property *graphProperty) notionFormulaCheckProperty {
	return notionFormulaCheckProperty{ID: property.ID, Name: property.Name, Type: property.Type, Config: string(property.Config)}
}

// notionFormulaNewSourceScope is a new source's properties as the request
// writes them; each formula reads those notion_formula_request_order.go says.
func notionFormulaNewSourceScope(operations []graphOperation) []notionFormulaCheckProperty {
	scope := []notionFormulaCheckProperty{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil {
			scope = append(scope, notionFormulaCheckOf(operation.Property))
		}
	}
	return scope
}

// notionFormulasWritten are the formulas and rollups a request writes, by ID:
// those it gives an expression or a rollup's settings, or whose expression it
// changes (read before the rollups' settings are resolved).
func notionFormulasWritten(operations []graphOperation, stored []storedNotionDataSourceProperty) map[string]bool {
	before := make(map[string]storedNotionDataSourceProperty, len(stored))
	for _, property := range stored {
		before[property.ID] = property
	}
	written := map[string]bool{}
	for _, operation := range operations {
		property := operation.Property
		if operation.Kind != graphOperationKindUpsertProperty || property == nil {
			continue
		}
		config := map[string]any{}
		_ = json.Unmarshal(property.Config, &config)
		_, formula := config[notionFormulaRequestField]
		_, rollup := config[notionRollupRequestField]
		was, existed := before[property.ID]
		kind := normalizeNotionDatabaseRowPagePropertyType(property.Type)
		changed := kind == graphValueTypeFormula && (!existed ||
			notionViewFormulaExpression(kind, string(property.Config)) != notionViewFormulaExpression(normalizeNotionDatabaseRowPagePropertyType(was.Type), was.Config))
		if formula || rollup || changed {
			written[property.ID] = true
		}
	}
	return written
}

// notionFormulaUpdateScope is the source a request's formulas are checked
// against: the stored properties, in their order, with the request's changes
// made but those to the formulas and rollups it writes.
func notionFormulaUpdateScope(stored []storedNotionDataSourceProperty, operations []graphOperation, written map[string]bool) []notionFormulaCheckProperty {
	scope := make([]notionFormulaCheckProperty, 0, len(stored))
	at := make(map[string]int, len(stored))
	for _, property := range stored {
		at[property.ID] = len(scope)
		scope = append(scope, notionFormulaCheckProperty{ID: property.ID, Name: property.Name, Type: property.Type, Config: property.Config})
	}
	gone := map[string]bool{}
	for _, operation := range operations {
		switch {
		case operation.PropertyDeletion != nil:
			gone[operation.PropertyDeletion.ID] = true
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil && !written[operation.Property.ID]:
			if index, kept := at[operation.Property.ID]; kept {
				was := scope[index].Name
				scope[index] = notionFormulaCheckOf(operation.Property)
				scope[index].Was = was
			} else {
				at[operation.Property.ID] = len(scope)
				scope = append(scope, notionFormulaCheckOf(operation.Property))
			}
		}
	}
	left := scope[:0]
	for _, property := range scope {
		if !gone[property.ID] {
			left = append(left, property)
		}
	}
	return left
}

// notionFormulaScopeTypes are the types the scope's properties have in a
// formula, by name — the first property with it — and by a name each had
// that no property has now (database_formula_former_names.go, and the name
// the request changed).
func notionFormulaScopeTypes(scope []notionFormulaCheckProperty, own string, related notionFormulaRelated) (map[string]string, notionFormulaPages) {
	kinds := newNotionFormulaPropertyKinds(scope).withSources(own, related)
	types := make(map[string]string, len(scope))
	for _, names := range []map[string]int{kinds.byName, kinds.byFormer} {
		for name, index := range names {
			if _, taken := types[name]; !taken {
				types[name] = kinds.of(index)
			}
		}
	}
	return types, notionFormulaRequestPages(own, types)
}

// notionFormulaPropertyKinds works out each property's type in a formula,
// once; a formula reached again while its own is worked out (a cycle) has
// none.
type notionFormulaPropertyKinds struct {
	scope    []notionFormulaCheckProperty
	byName   map[string]int
	byID     map[string]int
	byFormer map[string]int
	known    map[int]string
	visiting map[int]bool
	// own is the source the scope is of; related reads others', each once
	// in sources (notion_formula_pages.go).
	own     string
	related notionFormulaRelated
	sources map[string]*notionFormulaPropertyKinds
}

func newNotionFormulaPropertyKinds(scope []notionFormulaCheckProperty) *notionFormulaPropertyKinds {
	kinds := &notionFormulaPropertyKinds{scope: scope, byName: map[string]int{}, byID: map[string]int{}, byFormer: map[string]int{},
		known: map[int]string{}, visiting: map[int]bool{}}
	for index, property := range scope {
		if _, taken := kinds.byName[property.Name]; !taken {
			kinds.byName[property.Name] = index
		}
		kinds.byID[property.ID] = index
		config := map[string]any{}
		_ = json.Unmarshal([]byte(property.Config), &config)
		for _, name := range append([]string{property.Was}, databasePropertyFormerNames(config)...) {
			if _, taken := kinds.byFormer[name]; !taken && name != "" {
				kinds.byFormer[name] = index
			}
		}
	}
	return kinds
}

// named finds the property a formula names: by its name, else by its ID (a
// formula written by block_property references names IDs), as the evaluator
// does, else by a name it had.
func (kinds *notionFormulaPropertyKinds) named(name string) (int, bool) {
	for _, names := range []map[string]int{kinds.byName, kinds.byID, kinds.byFormer} {
		if index, found := names[name]; found {
			return index, true
		}
	}
	return 0, false
}

func (kinds *notionFormulaPropertyKinds) of(index int) string {
	if kind, done := kinds.known[index]; done {
		return kind
	}
	property := kinds.scope[index]
	nativeType := normalizeNotionDatabaseRowPagePropertyType(property.Type)
	kind := ""
	switch nativeType {
	case graphValueTypeFormula:
		if kinds.visiting[index] {
			return ""
		}
		kinds.visiting[index] = true
		kind = kinds.formula(notionViewFormulaExpression(nativeType, property.Config))
		delete(kinds.visiting, index)
	case graphValueTypeRollup:
		kind = notionFormulaRollupType(property.Config)
	default:
		kind = notionFormulaPropertyKind(nativeType, property.Config)
	}
	kinds.known[index] = kind
	return kind
}

// formula is the type the check gives an expression, the properties it
// names of theirs.
func (kinds *notionFormulaPropertyKinds) formula(expression string) string {
	root := notionFormulaRead(expression).strict
	if root == nil {
		return ""
	}
	properties := map[string]string{}
	for _, name := range notionFormulaNamedProperties(root) {
		if index, found := kinds.named(name); found {
			properties[name] = kinds.of(index)
		}
	}
	checker := &notionFormulaChecker{properties: properties}
	if kinds.sources != nil {
		checker.pages = kinds.storedPage
	}
	return checker.check(root, map[string]string{})
}

// notionFormulaRollupType is a rollup's type in a formula, by its
// calculation (notionViewRollupResult): text for its values themselves.
func notionFormulaRollupType(config string) string {
	switch notionViewRollupResult(graphValueTypeRollup, config) {
	case "number":
		return "number"
	case "date":
		return "date"
	}
	return "text"
}

// notionFormulaNamedProperties are the names a formula's prop() calls give,
// a related page's (a method's) aside.
func notionFormulaNamedProperties(node *databaseFormulaNode) []string {
	if node == nil {
		return nil
	}
	names := []string{}
	if node.kind == databaseFormulaNodeCall && node.text == "prop" && len(node.children) == 1 && node.children[0].kind == databaseFormulaNodeString {
		names = append(names, node.children[0].text)
	}
	for _, child := range node.children {
		names = append(names, notionFormulaNamedProperties(child)...)
	}
	return names
}

// notionFormulaCycleRefusal refuses a formula the request writes (written)
// that comes back to itself through the formulas it names, in the source as
// the request leaves it (final), as Notion's API does (seen 2026-11-20:
// prop("F") in F, or F and G written to name each other, is refused as a
// formula that refers to itself, once the formulas' types check). A rollup's
// values come from other rows: it joins no cycle (a formula naming a rollup
// of itself through a relation to its own source is taken).
func notionFormulaCycleRefusal(final []notionFormulaCheckProperty, written map[string]bool) error {
	kinds := newNotionFormulaPropertyKinds(final)
	names := make([][]int, len(final))
	for index, property := range final {
		nativeType := normalizeNotionDatabaseRowPagePropertyType(property.Type)
		if nativeType != graphValueTypeFormula {
			continue
		}
		reading := notionFormulaRead(notionViewFormulaExpression(nativeType, property.Config))
		root := reading.strict
		if root == nil {
			root = reading.legacy
		}
		for _, name := range notionFormulaNamedProperties(root) {
			if target, found := kinds.named(name); found &&
				normalizeNotionDatabaseRowPagePropertyType(final[target].Type) == graphValueTypeFormula {
				names[index] = append(names[index], target)
			}
		}
	}
	for start, property := range final {
		if !written[property.ID] {
			continue
		}
		seen := map[int]bool{}
		waiting := append([]int(nil), names[start]...)
		for len(waiting) > 0 {
			next := waiting[len(waiting)-1]
			waiting = waiting[:len(waiting)-1]
			if next == start {
				return notionDatabaseRowPageRequestInvalid("This formula ends up referring to itself.")
			}
			if !seen[next] {
				seen[next] = true
				waiting = append(waiting, names[next]...)
			}
		}
	}
	return nil
}
