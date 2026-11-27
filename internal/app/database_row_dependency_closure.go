package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// A row read on its own — its page through the Public API, a page in a
// query's answer, an automation's webhook — computes its formulas and
// rollups from the related rows they read, loaded at once: the rows it
// relates to, and through them, remove by remove, the rows the computed
// values they roll up or name read (a rollup of a formula there that reads a
// rollup, a formula naming a related page's formula). A query's turn reads
// such rows as durable dependencies, turn by turn (database_query.go); a row
// on its own reads them in one go, as Notion gives the values (seen
// 2026-11-12: a rollup of a related formula, of a relation, beside an
// earliest date, all with their values).

// databaseRowDependencyClosureRows is how many related rows a row on its
// own reads at once for its computed values, ten turns' worth of a query's
// (databaseQueryDependencyRowsPerTurn); past it its computed values read as
// unsupported.
const databaseRowDependencyClosureRows = 10 * databaseQueryDependencyRowsPerTurn

// databaseRowDependencyStep is a computed property worked out on a row.
type databaseRowDependencyStep struct{ rowID, propertyID string }

// loadDatabaseRowDependencyClosure loads the related rows row's computed
// values read, false when they are more than databaseRowDependencyClosureRows.
// Its values already worked out (seed, a query's rollups reduced turn by
// turn) read none.
func loadDatabaseRowDependencyClosure(
	app core.App, owner, pageID string, row storedDatabaseQueryRow, context databaseQueryContext,
	seed map[string]databaseComputedValue,
) (map[string]storedDatabaseQueryRow, bool, error) {
	return loadDatabaseRowsDependencyClosure(app, owner, pageID, []storedDatabaseQueryRow{row}, context, seed)
}

// loadDatabaseRowsDependencyClosure loads the related rows the computed
// values of rows read, together, as each row's own closure reaches them;
// seed names the values already worked out on each. The rows themselves are
// not among those it gives.
func loadDatabaseRowsDependencyClosure(
	app core.App, owner, pageID string, rows []storedDatabaseQueryRow, context databaseQueryContext,
	seed map[string]databaseComputedValue,
) (map[string]storedDatabaseQueryRow, bool, error) {
	own := make(map[string]storedDatabaseQueryRow, len(rows))
	for _, row := range rows {
		own[row.ID] = row
	}
	closure := newDatabaseRowDependencyClosure(context, own)
	formulas := false
	for _, property := range context.Properties {
		formulas = formulas || property.Type == graphValueTypeFormula
	}
	for _, row := range rows {
		for _, property := range context.Properties {
			switch property.Type {
			case graphValueTypeRelation:
				// The row's formulas may read any of its related pages. With
				// none, its rollups reach what they read (dependencySteps), or
				// read nothing once reduced (seed): a query of 97 rows each
				// related to 100 read them all for each row (seen 2026-11-12).
				if formulas {
					closure.reach(databaseQueryStringSlice(row.Values[property.ID].Value))
				}
			case graphValueTypeFormula, graphValueTypeRollup:
				closure.steps = append(closure.steps, databaseRowDependencyStep{row.ID, property.ID})
			}
		}
		for propertyID := range seed {
			closure.done[databaseRowDependencyStep{row.ID, propertyID}] = true
		}
	}
	return closure.load(app, owner, pageID)
}

// loadDatabaseTargetDependencyClosure loads the related rows the computed
// property propertyID reads on each of rows, as a row's own closure reaches
// its related rows, false past databaseRowDependencyClosureRows.
func loadDatabaseTargetDependencyClosure(
	app core.App, owner, pageID string, rows map[string]storedDatabaseQueryRow, propertyID string,
	context databaseQueryContext,
) (map[string]storedDatabaseQueryRow, bool, error) {
	closure := newDatabaseRowDependencyClosure(context, rows)
	for id := range rows {
		closure.steps = append(closure.steps, databaseRowDependencyStep{id, propertyID})
	}
	return closure.load(app, owner, pageID)
}

// databaseRowDependencyClosure is the related rows reached from own, the
// rows whose computed values are worked out, step by computed step.
type databaseRowDependencyClosure struct {
	context databaseQueryContext
	own     map[string]storedDatabaseQueryRow
	rows    map[string]storedDatabaseQueryRow
	reached map[string]bool
	pending []string
	steps   []databaseRowDependencyStep
	done    map[databaseRowDependencyStep]bool
}

func newDatabaseRowDependencyClosure(
	context databaseQueryContext, own map[string]storedDatabaseQueryRow,
) *databaseRowDependencyClosure {
	reached := make(map[string]bool, len(own))
	for id := range own {
		reached[id] = true
	}
	return &databaseRowDependencyClosure{
		context: context, own: own, rows: map[string]storedDatabaseQueryRow{},
		reached: reached, done: map[databaseRowDependencyStep]bool{},
	}
}

func (closure *databaseRowDependencyClosure) reach(ids []string) {
	for _, id := range closure.context.visibleRowIDs(ids) {
		if !closure.reached[id] {
			closure.reached[id] = true
			closure.pending = append(closure.pending, id)
		}
	}
}

func (closure *databaseRowDependencyClosure) load(
	app core.App, owner, pageID string,
) (map[string]storedDatabaseQueryRow, bool, error) {
	for len(closure.steps) > 0 || len(closure.pending) > 0 {
		if len(closure.pending) > 0 {
			if len(closure.rows)+len(closure.pending) > databaseRowDependencyClosureRows {
				return nil, false, nil
			}
			loaded, err := loadDatabaseQueryRowsByID(app, owner, closure.context.readablePages(pageID), closure.pending)
			if err != nil {
				return nil, false, err
			}
			for id, related := range loaded {
				closure.rows[id] = related
			}
			closure.pending = closure.pending[:0]
		}
		next := []databaseRowDependencyStep{}
		for _, step := range closure.steps {
			if closure.done[step] {
				continue
			}
			closure.done[step] = true
			current, found := closure.own[step.rowID]
			if !found {
				current, found = closure.rows[step.rowID]
			}
			if found {
				next = append(next, closure.context.dependencySteps(current, step.propertyID, closure.reach)...)
			}
		}
		closure.steps = next
	}
	return closure.rows, true, nil
}

// dependencySteps reaches the related rows a computed property reads on row
// and names the computed values it reads on them.
func (context databaseQueryContext) dependencySteps(
	row storedDatabaseQueryRow, propertyID string, reach func([]string),
) []databaseRowDependencyStep {
	property, ok := context.PropertiesByID[propertyID]
	if !ok {
		return nil
	}
	if property.Type == graphValueTypeRelation {
		reach(databaseQueryStringSlice(row.Values[property.ID].Value))
		return nil
	}
	config, err := decodeDatabaseComputedConfig(property.Config)
	if err != nil {
		return nil
	}
	steps := []databaseRowDependencyStep{}
	onRelated := func(ids []string, target storedDatabaseQueryProperty) {
		if target.Type != graphValueTypeFormula && target.Type != graphValueTypeRollup {
			return
		}
		for _, id := range context.visibleRowIDs(ids) {
			steps = append(steps, databaseRowDependencyStep{id, target.ID})
		}
	}
	switch property.Type {
	case graphValueTypeRollup:
		relationID, targetID, function, setUp := context.rollupSetUp(config)
		if !setUp || context.UnavailableRelations[relationID] {
			return nil
		}
		ids := databaseQueryStringSlice(row.Values[relationID].Value)
		reach(ids)
		if function != "count" {
			onRelated(ids, context.PropertiesByID[targetID])
		}
		// A relation's pages it shows read by their titles
		// (relationTitles): their rows are read too.
		if context.PropertiesByID[targetID].Type == graphValueTypeRelation && databaseRollupShowsValues(function) {
			for _, id := range context.visibleRowIDs(ids) {
				steps = append(steps, databaseRowDependencyStep{id, targetID})
			}
		}
	case graphValueTypeFormula:
		expression := config.Formula
		if expression == "" {
			expression = config.Expression
		}
		names := databaseFormulaPropertyReferences(expression)
		for _, name := range names {
			named, found := context.propertyNamed(property.DataSourceID, name)
			if !found || named.DataSourceID != property.DataSourceID {
				continue
			}
			switch named.Type {
			case graphValueTypeFormula, graphValueTypeRollup:
				steps = append(steps, databaseRowDependencyStep{row.ID, named.ID})
			case graphValueTypeRelation:
				ids := databaseQueryStringSlice(row.Values[named.ID].Value)
				reach(ids)
				// The related pages' properties it names, as current.prop() in
				// a map over them reads them.
				source := databaseRelationTargetSource(named)
				for _, other := range names {
					if related, found := context.propertyNamed(source, other); found && related.DataSourceID == source {
						onRelated(ids, related)
					}
				}
			}
		}
	}
	return steps
}

// databaseRelationTargetSource is the data source a relation points to, its
// own for one within it.
func databaseRelationTargetSource(relation storedDatabaseQueryProperty) string {
	config := struct {
		Target string `json:"relationDatabaseId"`
	}{}
	if json.Unmarshal([]byte(relation.Config), &config) != nil || config.Target == "" {
		return relation.DataSourceID
	}
	return config.Target
}
