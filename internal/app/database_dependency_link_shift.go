package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A link a change adds moves the row at its other end from the row edited,
// as Notion's does (seen 2026-11-08): naming a Blocked by moves that
// predecessor to end before the edited row starts; naming a Blocking, or
// drawing a link on a timeline, moves that successor to start after the
// predecessor ends. Only "only when dates overlap" moves them, by the same working-day or calendar rule a date
// change follows; spacing kept, or no moving, leaves them. A link that
// closes a cycle, a row without dates on either end, and a row the change
// edits itself move nothing. The moves then carry on through the moved
// row's own links like any date change.

// databaseDependencyLinkMove is a row a new link moves, with its dates.
type databaseDependencyLinkMove struct {
	row           databaseDependencyDateRow
	before, after databaseTaskSpan
}

// databaseDependencyLinkedRows lists the rows a row already links to, on
// the given side of its links.
func databaseDependencyLinkedRows(
	app core.App, owner, pageID string, binding databaseDependencyBindingAuthority, column, other, rowID string,
) (map[string]bool, error) {
	ids := []string{}
	err := app.DB().NewQuery(`SELECT [[` + other + `]] FROM {{leafen_database_dependency_edges}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:page} AND [[data_source_id]] = {:source}
			AND [[data_key]] = {:key} AND [[` + column + `]] = {:row}`).Bind(dbx.Params{
		"owner": owner, "page": pageID, "source": binding.DataSourceID, "key": binding.DataKey, "row": rowID,
	}).Column(&ids)
	linked := make(map[string]bool, len(ids))
	for _, id := range ids {
		linked[id] = true
	}
	return linked, err
}

// databaseDependencyReaches tells whether `to` already follows `from`
// through the links drawn, so a link from `to` to `from` closes a cycle.
func databaseDependencyReaches(
	app core.App, owner, pageID string, binding databaseDependencyBindingAuthority, from, to string,
) (bool, error) {
	reaches := false
	err := app.DB().NewQuery(`WITH RECURSIVE reach(id) AS (
			SELECT {:from}
			UNION
			-- CROSS JOIN steps from the row by key (predecessor): SQLite's
			-- own order read the database's every link for each row.
			SELECT e.[[successor_row_id]] FROM reach CROSS JOIN {{leafen_database_dependency_edges}} e
				ON e.[[predecessor_row_id]] = reach.id AND e.[[owner]] = {:owner} AND e.[[page_id]] = {:page}
				AND e.[[data_source_id]] = {:source} AND e.[[data_key]] = {:key}
		)
		SELECT EXISTS(SELECT 1 FROM reach WHERE id = {:to})`).Bind(dbx.Params{
		"owner": owner, "page": pageID, "source": binding.DataSourceID, "key": binding.DataKey, "from": from, "to": to,
	}).Row(&reaches)
	return reaches, err
}

// databaseDependencyLinkMoves finds the links the edited rows' Blocked by
// and Blocking add, and those drawn on a timeline, and how each moves the
// row at its other end. spanOf reads an edited row's dates as the change
// leaves them.
func databaseDependencyLinkMoves(
	app core.App, owner, pageID string,
	binding databaseDependencyBindingAuthority, settings databaseDependencyShiftSettings,
	rows []*graphDatabaseRow, edges []*graphDependencyEdge, stored map[string]databaseDependencyDateRow,
	spanOf func(row *graphDatabaseRow) (databaseTaskSpan, bool), edited map[string]bool,
) ([]databaseDependencyLinkMove, error) {
	if settings.ShiftMode != "overlap" {
		return nil, nil
	}
	// A link goes from the row it is drawn from to the row it moves.
	type link struct {
		from, other   string
		otherPrecedes bool
	}
	links, read, editedRows := []link{}, []string{}, map[string]*graphDatabaseRow{}
	sides := []struct {
		property, column, other string
		otherPrecedes           bool
	}{
		{binding.PredecessorPropertyID, "successor_row_id", "predecessor_row_id", true},
		{binding.SuccessorPropertyID, "predecessor_row_id", "successor_row_id", false},
	}
	for _, row := range rows {
		editedRows[row.ID] = row
		if _, existed := stored[row.ID]; !existed {
			continue // A row the change makes links as it is made, moving nothing.
		}
		for _, side := range sides {
			value, written := row.Values[side.property]
			named := []string{}
			if !written || !graphRowValueIsCleared(value.Value) && json.Unmarshal(value.Value, &named) != nil {
				continue
			}
			linked, err := databaseDependencyLinkedRows(app, owner, pageID, binding, side.column, side.other, row.ID)
			if err != nil {
				return nil, err
			}
			for _, id := range named {
				if id != row.ID && !linked[id] && !edited[id] {
					links = append(links, link{row.ID, id, side.otherPrecedes})
					read = append(read, id)
				}
			}
		}
	}
	// A timeline draws a link from a predecessor to the successor it is
	// dropped on, which moves, as Notion's (seen 2026-11-08).
	for _, edge := range edges {
		if edge.PredecessorRowID == edge.SuccessorRowID || edited[edge.SuccessorRowID] {
			continue
		}
		linked, err := databaseDependencyLinkedRows(app, owner, pageID, binding, "predecessor_row_id", "successor_row_id", edge.PredecessorRowID)
		if err != nil {
			return nil, err
		}
		if !linked[edge.SuccessorRowID] {
			links = append(links, link{edge.PredecessorRowID, edge.SuccessorRowID, false})
			read = append(read, edge.PredecessorRowID, edge.SuccessorRowID)
		}
	}
	if len(links) == 0 {
		return nil, nil
	}
	loaded, err := loadDatabaseDependencyDateRows(app, owner, pageID, binding.DataSourceID, settings, read)
	if err != nil {
		return nil, err
	}
	spanOfID := func(id string) (databaseTaskSpan, bool) {
		if row := editedRows[id]; row != nil {
			return spanOf(row)
		}
		dateRow, found := loaded[id]
		if !found {
			return databaseTaskSpan{}, false
		}
		return databaseTaskSpanFromValues(settings, func(propertyID string) (any, bool) {
			return dateRow.value(settings, propertyID)
		})
	}
	moves, moved := []databaseDependencyLinkMove{}, map[string]bool{}
	for _, link := range links {
		other, found := loaded[link.other]
		if !found || moved[link.other] {
			continue
		}
		otherSpan, otherDated := spanOfID(link.other)
		fromSpan, fromDated := spanOfID(link.from)
		if !otherDated || !fromDated {
			continue
		}
		predecessor, successor := link.other, link.from
		if !link.otherPrecedes {
			predecessor, successor = link.from, link.other
		}
		if cycle, err := databaseDependencyReaches(app, owner, pageID, binding, successor, predecessor); err != nil {
			return nil, err
		} else if cycle {
			continue
		}
		var next databaseTaskSpan
		var shifted bool
		if link.otherPrecedes {
			next, shifted = shiftDatabasePredecessorSpan(settings, fromSpan, fromSpan, otherSpan)
		} else {
			next, shifted = shiftDatabaseTaskSpan(settings, fromSpan, fromSpan, otherSpan)
		}
		if shifted {
			moved[link.other] = true
			moves = append(moves, databaseDependencyLinkMove{row: other, before: otherSpan, after: next})
		}
	}
	return moves, nil
}

// databaseDependencyShiftsAreTasks tells whether the rows a change moved
// are all a task database's, which its notice names "작업" rather than
// "항목", as Notion's (seen 2026-11-07 and 2026-11-08).
func databaseDependencyShiftsAreTasks(app core.App, derived []graphOperation) (bool, error) {
	seen := map[string]bool{}
	for _, operation := range derived {
		if operation.Row == nil || seen[operation.Row.DataSourceID] {
			continue
		}
		seen[operation.Row.DataSourceID] = true
		roles, err := loadDatabaseTaskRoles(app, operation.Row.DataSourceID)
		if err != nil || !roles.Enabled {
			return false, err
		}
	}
	return len(seen) > 0, nil
}

// databaseDependencyLinkProperties names the Blocked by of each data source
// the operations write rows of, whose links a date command takes back with
// the dates they moved.
func databaseDependencyLinkProperties(app core.App, pageID string, operations []graphOperation) (map[string]bool, error) {
	properties, seen := map[string]bool{}, map[string]bool{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil || seen[operation.Row.DataSourceID] {
			continue
		}
		seen[operation.Row.DataSourceID] = true
		binding, bound, err := loadDatabaseDependencyRecord(app, pageID, operation.Row.DataSourceID)
		if err != nil {
			return nil, err
		}
		if bound {
			properties[binding.PredecessorPropertyID] = true
		}
	}
	return properties, nil
}
