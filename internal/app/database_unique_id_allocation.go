package app

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Every row a database gains gets the next number of each of its ID
// properties, as Notion numbers every page: rows added in the editor, by
// form, through the API or MCP, from templates, duplicates, moves and
// imports. The unique ID allocator is the one authority. A row that already
// carries a number the server wrote, as an import keeps Notion's, keeps it.
// A row coming back as it was made — a new row left with Escape and brought
// back with ⌘Z — keeps the number it was given: written with a number the
// property gave out before and no row of its database holds now, it keeps
// that number, as Notion's does (seen 2026-11-14: Escape, ⌘Z, ⌘⇧Z, ⌘Z kept
// 3); any other number it is written with gives way to the next one.
// Until an ID property has numbered the rows that existed before it, new
// rows wait for the unique ID runner, which numbers them in the order they
// were created.

type databaseUniqueIDAllocation struct {
	PropertyID  string        `db:"id"`
	Updated     int64         `db:"updated"`
	NextValue   sql.NullInt64 `db:"next_value"`
	Initialized sql.NullBool  `db:"initialized"`
	Backfilled  sql.NullBool  `db:"backfilled"`
}

func (allocation databaseUniqueIDAllocation) ready() bool {
	return allocation.Initialized.Bool && allocation.Backfilled.Bool && allocation.NextValue.Valid &&
		allocation.NextValue.Int64 >= 1 && allocation.NextValue.Int64 <= maxDatabaseUniqueIDValue
}

func loadDatabaseUniqueIDAllocations(
	app core.App,
	owner, pageID, dataSourceID string,
) ([]databaseUniqueIDAllocation, error) {
	allocations := []databaseUniqueIDAllocation{}
	err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[updated]], [[allocator]].[[next_value]],
			[[allocator]].[[initialized]], [[allocator]].[[backfilled]]
		FROM {{leafen_database_properties}} [[property]]
		LEFT JOIN {{leafen_database_unique_id_allocators}} [[allocator]]
			ON [[allocator]].[[owner]] = [[property]].[[owner]]
			AND [[allocator]].[[page_id]] = [[property]].[[page_id]]
			AND [[allocator]].[[property_id]] = [[property]].[[id]]
		WHERE [[property]].[[owner]] = {:owner} AND [[property]].[[page_id]] = {:pageId}
			AND [[property]].[[data_source_id]] = {:sourceId} AND [[property]].[[type]] = 'unique_id'
		ORDER BY [[property]].[[id]]
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "sourceId": dataSourceID}).All(&allocations)
	return allocations, err
}

// assignDatabaseRowUniqueIDs gives the rows a mutation adds to a database
// their numbers. Rows it moves within the page's databases count as added
// to the one they move to. The caller's operations are left unchanged. The
// count is of the rows given a number, which the writer's screen reads again.
func assignDatabaseRowUniqueIDs(
	app core.App,
	owner, pageID string,
	operations []graphOperation,
	runtimeWritable map[string]struct{},
) ([]graphOperation, map[string]struct{}, int, error) {
	waiting := false
	changing := map[string]struct{}{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil:
			changing[operation.Property.ID] = struct{}{}
			waiting = waiting || operation.Property.Type == graphValueTypeUniqueID
		case operation.Kind == graphOperationKindDeleteProperty && operation.PropertyDeletion != nil:
			changing[operation.PropertyDeletion.ID] = struct{}{}
		}
	}
	allocationsBySource := map[string][]databaseUniqueIDAllocation{}
	revisions := map[string]int64{}
	next := map[string]int64{}
	var assigned []graphOperation
	numbered := 0
	writable, copiedWritable := runtimeWritable, false
	keptBack := map[string]struct{}{}
	for index, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		if storedSource, err := databaseRowRecordSource(app, pageID, operation.Row.ID); err != nil {
			return nil, nil, 0, err
		} else if storedSource == operation.Row.DataSourceID {
			continue
		}
		allocations, loaded := allocationsBySource[operation.Row.DataSourceID]
		if !loaded {
			var err error
			allocations, err = loadDatabaseUniqueIDAllocations(app, owner, pageID, operation.Row.DataSourceID)
			if err != nil {
				return nil, nil, 0, err
			}
			allocationsBySource[operation.Row.DataSourceID] = allocations
		}
		var values map[string]graphRowValue
		for _, allocation := range allocations {
			if given, isGiven := operation.Row.Values[allocation.PropertyID]; isGiven {
				if _, allowed := runtimeWritable[allocation.PropertyID]; allowed {
					continue
				}
				back, err := databaseUniqueIDGivenBack(app, owner, pageID, operation.Row, allocation, given)
				if err != nil {
					return nil, nil, 0, err
				}
				if back {
					keptBack[allocation.PropertyID] = struct{}{}
					continue
				}
			}
			if _, changed := changing[allocation.PropertyID]; changed {
				continue
			}
			if !allocation.ready() {
				waiting = true
				continue
			}
			value, seen := next[allocation.PropertyID]
			if !seen {
				value = allocation.NextValue.Int64
			}
			if value > maxDatabaseUniqueIDValue {
				return nil, nil, 0, graphInputError(errorCodeGraphInvalid, "The ID property has no numbers left.")
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, nil, 0, err
			}
			if values == nil {
				values = make(map[string]graphRowValue, len(operation.Row.Values)+len(allocations))
				for propertyID, rowValue := range operation.Row.Values {
					values[propertyID] = rowValue
				}
			}
			values[allocation.PropertyID] = graphRowValue{Type: graphValueTypeUniqueID, Value: encoded}
			next[allocation.PropertyID] = value + 1
			revisions[allocation.PropertyID] = allocation.Updated
		}
		if values == nil {
			continue
		}
		if assigned == nil {
			assigned = append([]graphOperation(nil), operations...)
		}
		numbered++
		row := *operation.Row
		row.Values = values
		assigned[index].Row = &row
		if !copiedWritable {
			writable = make(map[string]struct{}, len(runtimeWritable)+len(next))
			for propertyID := range runtimeWritable {
				writable[propertyID] = struct{}{}
			}
			copiedWritable = true
		}
		for propertyID := range values {
			if _, allocated := next[propertyID]; allocated {
				writable[propertyID] = struct{}{}
			}
		}
	}
	now := time.Now().UTC().UnixMilli()
	for propertyID, value := range next {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_unique_id_allocators}}
			SET [[next_value]] = MAX([[next_value]], {:next}), [[maximum_seen]] = MAX([[maximum_seen]], {:seen}),
				[[property_revision]] = {:revision}, [[updated_at]] = {:now}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[property_id]] = {:propertyId}
		`).Bind(dbx.Params{
			"next": value, "seen": value - 1, "revision": revisions[propertyID], "now": now,
			"owner": owner, "pageId": pageID, "propertyId": propertyID,
		}).Execute(); err != nil {
			return nil, nil, 0, err
		}
	}
	if waiting {
		notifyDatabaseUniqueIDRunnerAfterCommit(app)
	}
	if len(keptBack) > 0 && !copiedWritable {
		writable = make(map[string]struct{}, len(runtimeWritable)+len(keptBack))
		for propertyID := range runtimeWritable {
			writable[propertyID] = struct{}{}
		}
	}
	for propertyID := range keptBack {
		writable[propertyID] = struct{}{}
	}
	if assigned == nil {
		return operations, writable, 0, nil
	}
	return assigned, writable, numbered, nil
}

// databaseUniqueIDGivenBack says whether a row the database is gaining keeps
// the number it is written with: one the property gave out before, which no
// other row of the database holds now (a row left as made and brought back).
func databaseUniqueIDGivenBack(
	app core.App,
	owner, pageID string,
	row *graphDatabaseRow,
	allocation databaseUniqueIDAllocation,
	given graphRowValue,
) (bool, error) {
	if given.Type != graphValueTypeUniqueID || !allocation.ready() {
		return false, nil
	}
	number, ok := parseDatabaseUniqueIDValue(string(given.Value))
	if !ok || number >= allocation.NextValue.Int64 {
		return false, nil
	}
	held := 0
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_row_values}} [[value]]
		JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[value]].[[row_id]]
		WHERE [[value]].[[owner]] = {:owner} AND [[value]].[[page_id]] = {:pageId}
			AND [[value]].[[property_id]] = {:propertyId} AND [[value]].[[value]] = {:number}
			AND [[value]].[[row_id]] <> {:rowId} AND [[row]].[[data_source_id]] = {:sourceId}
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "propertyId": allocation.PropertyID,
		"number": strconv.FormatInt(number, 10), "rowId": row.ID, "sourceId": row.DataSourceID,
	}).Row(&held)
	return err == nil && held == 0, err
}
