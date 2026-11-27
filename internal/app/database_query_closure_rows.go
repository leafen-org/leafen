package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A row whose computed values take the durable dependency path
// (databaseQueryRequiresDurableDependencies) went through it a step a turn,
// the job's state saved at each: its dependencies admitted, each rollup
// reduced, its result stored, its dependencies cleared — some thirty
// statements a row in five or more turns (2026-11-12: an agent's read of 300
// invoices, each showing its contract's status, a formula over rollups of
// them, ran 11,000 statements; 40 contracts with ten rollups and eight
// formulas, 3,000). When the rows the window starts with that take it read
// no more related rows, through every remove, than a row read on its own
// does at once (database_row_dependency_closure.go), they are worked out in
// one turn from those rows, by the same resolver a row read on its own
// computes with; past that bound the first of them takes the durable path as
// before.

// advanceDatabaseQueryClosureRows works out the run of rows at the start of
// sourceRows that take the durable path, up to a turn's rows, from the
// related rows they read loaded at once, and stores them as the turn's;
// false when those related rows are too many, the job left as it was.
func advanceDatabaseQueryClosureRows(
	app core.App,
	job storedDatabaseQueryJob,
	cursor databaseQueryCursorState,
	input databaseQueryRequest,
	sourceRows []storedDatabaseQueryRow,
	walk databaseQueryWalk,
	shapes map[string]databaseQueryRelationShape,
	context databaseQueryContext,
	plan databaseQueryPlan,
) (databaseQueryResponse, int, bool, error) {
	limit := databaseQuerySourceRowsPerTurn
	formulas := 0
	for _, property := range context.Properties {
		if property.Type == graphValueTypeFormula {
			formulas++
		}
	}
	if formulas > 0 {
		limit = max(1, databaseQueryFormulaEvaluationsPerTurn/formulas)
	}
	run := 0
	for run < len(sourceRows) && run < limit &&
		databaseQueryRequiresDurableDependencies(shapes[sourceRows[run].ID], context) {
		run++
	}
	if run == 0 {
		return databaseQueryResponse{}, 0, false, nil
	}
	// The rows as the durable path reads each (advanceDatabaseQueryDependencyEvaluation).
	loaded, err := loadDatabaseQuerySourceRowsByID(app, job, databaseQueryRowIDs(sourceRows[:run]))
	if err != nil {
		return databaseQueryResponse{}, 0, false, err
	}
	rows := make([]storedDatabaseQueryRow, run)
	for index, header := range sourceRows[:run] {
		row, found := loaded[header.ID]
		if !found || row.Position != header.Position {
			return databaseQueryResponse{}, 0, false, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
		rows[index] = row
	}
	related, within, err := loadDatabaseRowsDependencyClosure(
		app, job.ContentOwnerID, job.PageID, rows, context, nil,
	)
	if err != nil || !within {
		return databaseQueryResponse{}, 0, false, err
	}
	// A row of the run another one relates to is read from the run, as a
	// row on its own reads it among its related rows.
	for _, row := range rows {
		if _, found := related[row.ID]; !found {
			related[row.ID] = row
		}
	}
	cells := make(map[string]map[string]graphRowValue, len(related))
	for id, row := range related {
		cells[id] = row.Values
	}
	context.warmMentions(cells)
	evaluated, evaluatedRows, err := evaluateDatabaseQueryRows(
		rows, related, context, plan, job.PrincipalID, job.PageID,
	)
	if err != nil {
		return databaseQueryResponse{}, 0, false, err
	}
	rows = rows[:evaluatedRows]
	finished := len(rows) == len(sourceRows)
	if !finished {
		walk = databaseQueryWalk{}
	}
	complete := finished && walk.end
	job, err = storeDatabaseQueryTurn(app, job, rows, evaluated, complete, walk)
	if err != nil {
		return databaseQueryResponse{}, 0, false, err
	}
	if !complete {
		response, status, err := pendingDatabaseQueryResponse(job, cursor)
		return response, status, true, err
	}
	response, status, err := readyDatabaseQueryResponse(app, job, cursor, input)
	return response, status, true, err
}
