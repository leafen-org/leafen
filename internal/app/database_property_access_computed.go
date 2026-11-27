package app

import (
	"database/sql"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A formula or rollup shows no more than it reads under the property access
// rules (database_property_access.go): where it reads a value the reader may
// not see — a property of the row its formula names, a related page's
// property its formula reads, or the related property a rollup rolls up —
// its value is left out, showing empty as Notion's (seen 2026-10-12: the
// column stays, "비어 있음", the API's value null), and it can't filter or sort a view.

// databasePropertyAccessRead is a property a formula or rollup reads: in
// the same row, or in the rows a relation leads to.
type databasePropertyAccessRead struct {
	DataSourceID string
	PropertyID   string
	SameRow      bool
}

type databasePropertyAccessComputedSource struct {
	ID           string         `db:"id"`
	DataSourceID string         `db:"data_source_id"`
	Type         string         `db:"type"`
	Name         string         `db:"name"`
	Config       string         `db:"config"`
	Target       sql.NullString `db:"target_source_id"`
}

// databasePropertyAccessKey is one data source's property.
type databasePropertyAccessKey struct {
	DataSourceID string
	PropertyID   string
}

// loadDatabasePropertyAccessSources is the data sources and those their
// relations lead to, however far, each once.
func loadDatabasePropertyAccessSources(app core.App, dataSourceIDs []string) ([]string, error) {
	encoded, err := json.Marshal(dataSourceIDs)
	if err != nil {
		return nil, err
	}
	sources := []string{}
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[reached]] ([[id]]) AS (
			SELECT [[value]] FROM json_each({:ids})
			UNION
			SELECT [[target]].[[target_source_id]]
			FROM [[reached]]
			JOIN {{leafen_database_relation_property_targets}} [[target]] ON [[target]].[[source_id]] = [[reached]].[[id]]
			WHERE [[target]].[[target_source_id]] != '' AND ` + databasePropertyNotTrashedSQL("[[target]].[[property_id]]") + `
		)
		SELECT [[id]] FROM [[reached]] ORDER BY [[id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&sources)
	return sources, err
}

// loadDatabasePropertyAccessReads reads what the formulas and rollups of
// the data sources read (loadDatabasePropertyAccessSources: with those their
// relations lead to, whose properties the reads name).
func loadDatabasePropertyAccessReads(app core.App, sources []string) (map[string]map[string][]databasePropertyAccessRead, error) {
	properties, err := loadDatabasePropertyAccessComputedSources(app, sources)
	if err != nil {
		return nil, err
	}
	bySource := map[string][]databasePropertyAccessComputedSource{}
	for _, property := range properties {
		bySource[property.DataSourceID] = append(bySource[property.DataSourceID], property)
	}
	reads := map[string]map[string][]databasePropertyAccessRead{}
	for source, sourceProperties := range bySource {
		for _, property := range sourceProperties {
			found := databasePropertyAccessComputedReads(property, sourceProperties, bySource)
			if len(found) == 0 {
				continue
			}
			if reads[source] == nil {
				reads[source] = map[string][]databasePropertyAccessRead{}
			}
			reads[source][property.ID] = found
		}
	}
	return reads, nil
}

func loadDatabasePropertyAccessComputedSources(app core.App, dataSourceIDs []string) ([]databasePropertyAccessComputedSource, error) {
	encoded, err := json.Marshal(dataSourceIDs)
	if err != nil {
		return nil, err
	}
	properties := []databasePropertyAccessComputedSource{}
	err = app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[data_source_id]], [[property]].[[type]], [[property]].[[name]],
			[[property]].[[config]], [[target]].[[target_source_id]]
		FROM {{leafen_database_properties}} [[property]]
		LEFT JOIN {{leafen_database_relation_property_targets}} [[target]]
			ON [[target]].[[property_id]] = [[property]].[[id]] AND [[target]].[[source_id]] = [[property]].[[data_source_id]]
		WHERE [[property]].[[data_source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
		ORDER BY [[property]].[[data_source_id]], [[property]].[[position]], [[property]].[[id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&properties)
	return properties, err
}

// databasePropertyAccessComputedReads is what one formula or rollup reads.
// Where that can't be told — its settings don't read, its formula doesn't
// parse, or a prop() name is worked out rather than written — it is taken to
// read everything it could reach, so a restricted value stays hidden rather
// than shown (2026-10-13: those cases read as reading nothing).
func databasePropertyAccessComputedReads(property databasePropertyAccessComputedSource, siblings []databasePropertyAccessComputedSource,
	targets map[string][]databasePropertyAccessComputedSource) []databasePropertyAccessRead {
	if property.Type != graphValueTypeFormula && property.Type != graphValueTypeRollup {
		return nil
	}
	everything := func() []databasePropertyAccessRead {
		reads := []databasePropertyAccessRead{}
		for _, sibling := range siblings {
			if sibling.ID == property.ID {
				continue
			}
			reads = append(reads, databasePropertyAccessRead{DataSourceID: property.DataSourceID, PropertyID: sibling.ID, SameRow: true})
			if sibling.Target.Valid && sibling.Target.String != "" {
				for _, related := range targets[sibling.Target.String] {
					reads = append(reads, databasePropertyAccessRead{DataSourceID: related.DataSourceID, PropertyID: related.ID})
				}
			}
		}
		return reads
	}
	config, err := decodeDatabaseComputedConfig(property.Config)
	if err != nil {
		return everything()
	}
	// A name reads what the evaluator could take it for: a property of that
	// name — every one, where names repeat — or of that id.
	named := func(candidates []databasePropertyAccessComputedSource, reference string) []databasePropertyAccessComputedSource {
		found := []databasePropertyAccessComputedSource{}
		for _, candidate := range candidates {
			if candidate.Name == reference || candidate.ID == reference {
				found = append(found, candidate)
			}
		}
		return found
	}
	reads := []databasePropertyAccessRead{}
	if property.Type == graphValueTypeRollup {
		relationID, targetID, _ := config.rollupFields()
		for _, sibling := range siblings {
			if sibling.ID == relationID {
				reads = append(reads, databasePropertyAccessRead{DataSourceID: property.DataSourceID, PropertyID: relationID, SameRow: true})
				if sibling.Target.Valid && targetID != "" {
					reads = append(reads, databasePropertyAccessRead{DataSourceID: sibling.Target.String, PropertyID: targetID})
				}
			}
		}
		return reads
	}
	expression := config.Formula
	if expression == "" {
		expression = config.Expression
	}
	own, related, dynamic := databaseFormulaPropReferences(expression)
	if dynamic {
		return everything()
	}
	for _, name := range own {
		for _, read := range named(siblings, name) {
			if read.ID != property.ID {
				reads = append(reads, databasePropertyAccessRead{DataSourceID: property.DataSourceID, PropertyID: read.ID, SameRow: true})
			}
		}
	}
	// A related page's property may be reached through any page the
	// formula holds — a relation, a rollup, another formula — so a name is
	// looked for in every data source it could lead to.
	for _, name := range related {
		for source, candidates := range targets {
			for _, read := range named(candidates, name) {
				reads = append(reads, databasePropertyAccessRead{DataSourceID: source, PropertyID: read.ID})
			}
		}
	}
	return reads
}

// databaseFormulaPropReferences are the names a formula's prop() calls give,
// read from the evaluator's own tree (parseDatabaseFormula): on the row,
// prop("Name"), and on related pages, page.prop("Name"). dynamic holds when
// a name is worked out rather than written — prop("A" + "B"), a let
// variable — or the formula doesn't parse, so what it reads can't be told.
func databaseFormulaPropReferences(expression string) (own, related []string, dynamic bool) {
	root, err := parseDatabaseFormula(expression)
	if err != nil {
		return nil, nil, true
	}
	stack := []*databaseFormulaNode{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node == nil {
			continue
		}
		switch {
		case node.kind == databaseFormulaNodeCall && node.text == "prop":
			if len(node.children) == 1 && node.children[0].kind == databaseFormulaNodeString {
				own = append(own, node.children[0].text)
			} else {
				dynamic = true
			}
		case node.kind == databaseFormulaNodeMethod && node.text == "prop":
			if len(node.children) == 2 && node.children[1].kind == databaseFormulaNodeString {
				related = append(related, node.children[1].text)
			} else {
				dynamic = true
			}
		}
		stack = append(stack, node.children...)
	}
	return own, related, dynamic
}

// setReads takes what the formulas and rollups read and marks those that
// read a restricted property, however far: walking back from each
// restricted property to what reads it, each once (a cycle ends).
func (reader *databasePropertyAccessReader) setReads(reads map[string]map[string][]databasePropertyAccessRead) {
	reader.reads = reads
	reader.derived = map[string]map[string]bool{}
	readBy := map[databasePropertyAccessKey][]databasePropertyAccessKey{}
	for dataSourceID, properties := range reads {
		for propertyID, propertyReads := range properties {
			for _, read := range propertyReads {
				key := databasePropertyAccessKey{read.DataSourceID, read.PropertyID}
				readBy[key] = append(readBy[key], databasePropertyAccessKey{dataSourceID, propertyID})
			}
		}
	}
	pending := []databasePropertyAccessKey{}
	for dataSourceID, properties := range reader.rules {
		for propertyID, rule := range properties {
			if rule != nil {
				pending = append(pending, databasePropertyAccessKey{dataSourceID, propertyID})
			}
		}
	}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, next := range readBy[current] {
			if reader.derived[next.DataSourceID][next.PropertyID] {
				continue
			}
			if reader.derived[next.DataSourceID] == nil {
				reader.derived[next.DataSourceID] = map[string]bool{}
			}
			reader.derived[next.DataSourceID][next.PropertyID] = true
			pending = append(pending, next)
		}
	}
}

// restrictedProperties are the data source's properties a rule covers,
// itself or through what they read.
func (reader *databasePropertyAccessReader) restrictedProperties(dataSourceID string) []string {
	properties := []string{}
	for propertyID := range reader.rules[dataSourceID] {
		properties = append(properties, propertyID)
	}
	for propertyID := range reader.derived[dataSourceID] {
		if reader.rules[dataSourceID][propertyID] == nil {
			properties = append(properties, propertyID)
		}
	}
	return properties
}

// readsHidden is whether a formula or rollup reads, however far, a value
// the reader may not see: following the restricted properties it reads (in
// the row, then on the rows a relation leads to), each once.
func (reader *databasePropertyAccessReader) readsHidden(dataSourceID, propertyID string, row *databasePropertyAccessRow) (bool, error) {
	if !reader.derived[dataSourceID][propertyID] {
		return false, nil
	}
	type step struct {
		key databasePropertyAccessKey
		row *databasePropertyAccessRow
	}
	type seenStep struct {
		key   databasePropertyAccessKey
		inRow bool
	}
	start := databasePropertyAccessKey{dataSourceID, propertyID}
	seen := map[seenStep]bool{{start, row != nil}: true}
	pending := []step{{start, row}}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, read := range reader.reads[current.key.DataSourceID][current.key.PropertyID] {
			if !reader.restricted(read.DataSourceID, read.PropertyID) {
				continue
			}
			var readRow *databasePropertyAccessRow
			if read.SameRow {
				readRow = current.row
			}
			key := databasePropertyAccessKey{read.DataSourceID, read.PropertyID}
			if seen[seenStep{key, readRow != nil}] {
				continue
			}
			seen[seenStep{key, readRow != nil}] = true
			level, err := reader.ownLevel(read.DataSourceID, read.PropertyID, readRow)
			if err != nil || !propertyAccessShowsValues(level) {
				return err == nil, err
			}
			if reader.derived[read.DataSourceID][read.PropertyID] {
				pending = append(pending, step{key, readRow})
			}
		}
	}
	return false, nil
}
