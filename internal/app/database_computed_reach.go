package app

import (
	"sort"
)

// A data source's formulas and rollups read past its own relations: a
// rollup of a related formula that reads a rollup there reads that rollup's
// relation's rows too, and a related formula naming its relation's pages
// reads theirs, however far, as Notion works them out wherever those
// databases are (seen 2026-11-12: a sum of a related formula reading a
// rollup of a third database, 17). databaseComputedReach follows the
// computed values a source's own read, source by source, as each source's
// properties are loaded, and names the sources they reach.

type databaseComputedReach struct {
	// byID and byName are each loaded source's properties.
	byID   map[string]map[string]storedDatabaseQueryProperty
	byName map[string]map[string]storedDatabaseQueryProperty
	// pending are the properties read on a source not loaded yet, by name
	// or ID.
	pending map[string][]string
	done    map[string]bool
	// starts are the sources the walk began from; a relation of theirs
	// is their own, any other's is past one.
	starts map[string]bool
	// Reached are the sources the computed values read rows of, each true
	// when only past a relation of a starting source.
	Reached map[string]bool
	// Targets are the relations followed, each with the source it points
	// to.
	Targets map[string]string
}

func newDatabaseComputedReach() *databaseComputedReach {
	return &databaseComputedReach{
		byID: map[string]map[string]storedDatabaseQueryProperty{}, byName: map[string]map[string]storedDatabaseQueryProperty{},
		pending: map[string][]string{}, done: map[string]bool{}, starts: map[string]bool{},
		Reached: map[string]bool{}, Targets: map[string]string{},
	}
}

// start begins the walk from a source: every relation of its own is
// reached, as its formulas may read any related page, and each of its
// computed values is read.
func (reach *databaseComputedReach) start(sourceID string, properties []storedDatabaseQueryProperty) {
	reach.starts[sourceID] = true
	reach.load(sourceID, properties)
	for _, property := range properties {
		if property.DataSourceID != sourceID {
			continue
		}
		switch property.Type {
		case graphValueTypeRelation, graphValueTypeFormula, graphValueTypeRollup:
			reach.read(sourceID, property.ID)
		}
	}
}

// loaded is whether a source's properties are known to the walk.
func (reach *databaseComputedReach) loaded(sourceID string) bool {
	_, ok := reach.byID[sourceID]
	return ok
}

// waiting are the reached sources whose properties the walk still reads,
// sorted.
func (reach *databaseComputedReach) waiting() []string {
	sources := []string{}
	for sourceID := range reach.pending {
		if !reach.loaded(sourceID) {
			sources = append(sources, sourceID)
		}
	}
	sort.Strings(sources)
	return sources
}

// load gives the walk a source's properties, nil for one it can't read,
// and goes on with what was read there.
func (reach *databaseComputedReach) load(sourceID string, properties []storedDatabaseQueryProperty) {
	if reach.loaded(sourceID) {
		return
	}
	byID, byName := map[string]storedDatabaseQueryProperty{}, map[string]storedDatabaseQueryProperty{}
	for _, property := range properties {
		if property.DataSourceID != sourceID {
			continue
		}
		byID[property.ID] = property
		if _, taken := byName[property.Name]; !taken {
			byName[property.Name] = property
		}
	}
	reach.byID[sourceID], reach.byName[sourceID] = byID, byName
	waiting := reach.pending[sourceID]
	delete(reach.pending, sourceID)
	for _, reference := range waiting {
		reach.read(sourceID, reference)
	}
}

// lookup is a loaded source's property by name, or by ID as a formula may
// name it.
func (reach *databaseComputedReach) lookup(sourceID, reference string) (storedDatabaseQueryProperty, bool) {
	if property, found := reach.byName[sourceID][reference]; found {
		return property, true
	}
	property, found := reach.byID[sourceID][reference]
	return property, found
}

// read follows a property read on a source to what it reads in turn.
func (reach *databaseComputedReach) read(sourceID, reference string) {
	if reference == "" {
		return
	}
	if !reach.loaded(sourceID) {
		reach.pending[sourceID] = append(reach.pending[sourceID], reference)
		return
	}
	property, found := reach.lookup(sourceID, reference)
	key := sourceID + "\x00" + property.ID
	if !found || reach.done[key] {
		return
	}
	reach.done[key] = true
	switch property.Type {
	case graphValueTypeRelation:
		reach.follow(property)
	case graphValueTypeRollup:
		config, err := decodeDatabaseComputedConfig(property.Config)
		if err != nil {
			return
		}
		relationID, targetID, function := config.rollupFields()
		relation, found := reach.byID[sourceID][relationID]
		if !found || relation.Type != graphValueTypeRelation {
			return
		}
		target := reach.follow(relation)
		if function != "count" && target != "" {
			reach.read(target, targetID)
		}
	case graphValueTypeFormula:
		config, err := decodeDatabaseComputedConfig(property.Config)
		if err != nil {
			return
		}
		expression := config.Formula
		if expression == "" {
			expression = config.Expression
		}
		names := databaseFormulaPropertyReferences(expression)
		for _, name := range names {
			named, found := reach.lookup(sourceID, name)
			if !found {
				continue
			}
			reach.read(sourceID, named.ID)
			if named.Type != graphValueTypeRelation {
				continue
			}
			// The related pages' properties it names, as current.prop() in
			// a map over them reads them.
			if target := databaseRelationTargetSource(named); target != sourceID {
				for _, other := range names {
					reach.read(target, other)
				}
			}
		}
	}
}

// follow reaches the source a relation points to and names it.
func (reach *databaseComputedReach) follow(relation storedDatabaseQueryProperty) string {
	target := graphRelationTarget(relation.Type, []byte(relation.Config))
	if target == "" {
		return ""
	}
	reach.Targets[relation.ID] = target
	past := !reach.starts[relation.DataSourceID]
	if earlier, known := reach.Reached[target]; !known || earlier && !past {
		reach.Reached[target] = past
	}
	if !reach.loaded(target) {
		if _, waiting := reach.pending[target]; !waiting {
			reach.pending[target] = nil
		}
	}
	return target
}
