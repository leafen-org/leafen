package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A query's rollup of a related computed value that itself reads rollups (a
// sum of a related row's rollup, say) works out a window of related rows'
// values at once, from the rows they read loaded together, when those fit
// the bound a row read on its own has (database_row_dependency_closure.go);
// past it, one related row at a time, kept turn by turn
// (databaseQueryComputedTargetState). One at a time, a database of 50 rows
// each relating to 100 such rows took some 200 requests and 10 seconds to
// query (seen 2026-11-12).

// databaseQueryTargetValues keeps the related rows' computed values a
// request's steps work out, by property and row, for the steps after them:
// rows relating to the same rows read their values once a request, not once
// each (2026-11-12: 97 rows each relating to the same 100 rows, whose sums
// read five rows each, worked out the 100 sums again for each row). The
// request's transaction holds the database's one writer and its steps write
// the job's own progress alone (advanceDatabaseQueryJobTurns), so what they
// read stays as it was; the next request works them out afresh.
type databaseQueryTargetValues struct {
	byProperty map[string]map[string]any
}

func newDatabaseQueryTargetValues() *databaseQueryTargetValues {
	return &databaseQueryTargetValues{byProperty: map[string]map[string]any{}}
}

// computeDatabaseQueryTargetWindow works out targetID on the related rows of
// window, nil when what they read is more than the bound. Values the
// request's steps already worked out (context.TargetValues) are kept.
func computeDatabaseQueryTargetWindow(
	app core.App,
	job storedDatabaseQueryJob,
	context databaseQueryContext,
	window []storedDatabaseQueryDirectDependency,
	targetID string,
) (map[string]any, error) {
	property, ok := context.PropertiesByID[targetID]
	if !ok {
		return nil, errors.New(errorCodeDatabaseQueryInvalid)
	}
	values := make(map[string]any, len(window))
	var known map[string]any
	if context.TargetValues != nil {
		if known = context.TargetValues.byProperty[targetID]; known == nil {
			known = map[string]any{}
			context.TargetValues.byProperty[targetID] = known
		}
		missing := make([]storedDatabaseQueryDirectDependency, 0, len(window))
		for _, item := range window {
			if value, found := known[item.TargetRowID]; found {
				values[item.TargetRowID] = value
			} else {
				missing = append(missing, item)
			}
		}
		if len(missing) == 0 {
			return values, nil
		}
		window = missing
	}
	ids := make([]string, 0, len(window))
	for _, item := range window {
		ids = append(ids, item.TargetRowID)
	}
	targets, err := loadDatabaseQueryRowsByID(
		app, job.ContentOwnerID, context.readablePages(job.PageID), context.visibleRowIDs(ids),
	)
	if err != nil {
		return nil, err
	}
	related, within, err := loadDatabaseTargetDependencyClosure(
		app, job.ContentOwnerID, job.PageID, targets, targetID, context,
	)
	if err != nil || !within {
		return nil, err
	}
	base := make(map[string]storedDatabaseQueryRow, len(related)+len(targets))
	for id, row := range related {
		base[id] = row
	}
	for id, row := range targets {
		base[id] = row
	}
	resolver := &databaseComputedResolver{
		context: context, rows: map[string]storedDatabaseQueryRow{}, base: base, read: map[string]bool{},
		computed: map[string]map[string]databaseComputedValue{},
		visiting: map[string]bool{}, cyclic: map[string]bool{},
	}
	for _, item := range window {
		row, found := resolver.row(item.TargetRowID)
		var value any
		if found {
			if _, err := resolver.evaluate(row, property); err != nil {
				return nil, err
			}
			value = databaseQueryRawValue(resolver.computed[item.TargetRowID][targetID].Value)
			values[item.TargetRowID] = value
		}
		// A row the person can't read is known too: it reads as nothing.
		if known != nil {
			known[item.TargetRowID] = value
		}
	}
	return values, nil
}
