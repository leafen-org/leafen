package app

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type databaseQueryDependencyEvaluationState struct {
	Results     map[string]databaseComputedValue  `json:"results"`
	Accumulator databaseQueryRollupAccumulator    `json:"accumulator"`
	Target      *databaseQueryComputedTargetState `json:"target,omitempty"`
}

type databaseQueryRollupAccumulator struct {
	Function     string  `json:"function"`
	InputCount   int     `json:"inputCount"`
	VisibleCount int     `json:"visibleCount"`
	PresentCount int     `json:"presentCount"`
	NumberCount  int     `json:"numberCount"`
	Sum          float64 `json:"sum"`
	Minimum      float64 `json:"minimum"`
	Maximum      float64 `json:"maximum"`
	HasNumber    bool    `json:"hasNumber"`
	Text         string  `json:"text"`
	// Leaves are the related values one by one, empty ones too, of a rollup
	// shown as them: what a filter on it reads.
	Leaves           []any           `json:"leaves,omitempty"`
	Seen             map[string]bool `json:"seen,omitempty"`
	ReducingMedian   bool            `json:"reducingMedian"`
	ReductionSeen    int             `json:"reductionSeen"`
	ReductionNumber  float64         `json:"reductionNumber"`
	ReductionOrdinal int             `json:"reductionOrdinal"`
	ReductionID      string          `json:"reductionId"`
	MedianLower      *float64        `json:"medianLower,omitempty"`
	MedianUpper      *float64        `json:"medianUpper,omitempty"`
	// The ticked boxes or the rows in a status group, and the earliest and
	// latest dates (database_rollup_functions.go).
	CheckedCount int    `json:"checkedCount,omitzero"`
	EarliestDate string `json:"earliestDate,omitempty"`
	LatestDate   string `json:"latestDate,omitempty"`
	// The different values' length, kept within what a computed text may hold.
	SeenBytes int `json:"seenBytes,omitzero"`
	// The options of the status group a rollup counts.
	GroupOptions []string `json:"groupOptions,omitempty"`
	// TargetsOneByOne is set once a window of related computed values read
	// more rows than the bound (computeDatabaseQueryTargetWindow): the rest
	// are worked out one at a time.
	TargetsOneByOne bool `json:"targetsOneByOne,omitzero"`
	// Booleans are the present values that are checkboxes, which the counts
	// of values leave out (databaseRollupCountsValue); Values are the values
	// the count of values counts, a list's items each
	// (databaseRollupValueCount).
	Booleans int `json:"booleans,omitzero"`
	Values   int `json:"values,omitempty"`
}

type storedDatabaseQueryDirectDependency struct {
	RowID       string `db:"dependency_row_id"`
	TargetRowID string `db:"target_row_id"`
	Ordinal     int    `db:"ordinal"`
}

type storedDatabaseQueryMedianValue struct {
	RowID   string  `db:"dependency_row_id"`
	Ordinal int     `db:"ordinal"`
	Number  float64 `db:"evaluation_number"`
}

func advanceDatabaseQueryDependencyEvaluation(
	app core.App,
	job storedDatabaseQueryJob,
	row storedDatabaseQueryRow,
	context databaseQueryContext,
	plan databaseQueryPlan,
) (storedDatabaseQueryJob, *databaseQueryEvaluatedRow, error) {
	state, err := decodeDatabaseQueryDependencyEvaluation(job.DependencyEvaluationJSON)
	if err != nil {
		return job, nil, err
	}
	remaining := databaseQueryDependencyRowsPerTurn
	for job.DependencyEvaluationProperty < len(context.Properties) {
		property := context.Properties[job.DependencyEvaluationProperty]
		if property.Type != graphValueTypeRollup {
			job.DependencyEvaluationProperty++
			job.DependencyEvaluationCursor = -1
			continue
		}
		config, err := decodeDatabaseComputedConfig(property.Config)
		if err != nil {
			return job, nil, err
		}
		relationID, targetID, function, setUp := context.rollupSetUp(config)
		if !setUp {
			finishDatabaseQueryUnsetRollup(&state, property, &job)
			continue
		}
		if state.Accumulator.Function == "" {
			state.Accumulator.Function = function
			state.Accumulator.GroupOptions = databaseRollupStatusGroupOptions(context.PropertiesByID[targetID], config.RollupStatusGroup)
		}
		if state.Accumulator.Function != function {
			return job, nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
		if state.Accumulator.ReducingMedian {
			complete, err := reduceDatabaseQueryMedian(app, job, property.ID, &state.Accumulator)
			if err != nil {
				return job, nil, err
			}
			if !complete {
				return persistDatabaseQueryEvaluationState(app, job, state, nil)
			}
			if err := finishDatabaseQueryRollup(&state, property, &job); err != nil {
				return job, nil, err
			}
			return persistDatabaseQueryEvaluationState(app, job, state, nil)
		}
		unavailable := context.UnavailableRelations[relationID]
		targetProperty := context.PropertiesByID[targetID]
		if unavailable {
			if err := finishDatabaseQueryRollup(&state, property, &job); err != nil {
				return job, nil, err
			}
			continue
		}
		directLimit := remaining
		// A related value that reads rollups of its own is worked out a
		// window at a time while what the window reads fits, else one
		// related row at a time (database_query_target_window.go).
		windowTargets := false
		if state.Target != nil {
			directLimit = 1
		} else if function != "count" &&
			(targetProperty.Type == graphValueTypeFormula || targetProperty.Type == graphValueTypeRollup) {
			targetRollups, err := databaseComputedRollupDependencies(targetID, context)
			if err != nil {
				return job, nil, err
			}
			if len(targetRollups) > 0 {
				windowTargets = !state.Accumulator.TargetsOneByOne
				if !windowTargets {
					directLimit = 1
				}
			}
		}
		direct := []storedDatabaseQueryDirectDependency{}
		if err := app.DB().NewQuery(`
			SELECT [[dependency_row_id]], [[target_row_id]], [[ordinal]]
			FROM {{leafen_database_query_dependency_values}}
			WHERE [[job_id]] = {:jobId} AND [[source_row_id]] = {:sourceRowId}
				AND [[kind]] = 'direct' AND [[relation_property]] = {:relationProperty}
				AND [[ordinal]] > {:cursor}
			ORDER BY [[ordinal]], [[dependency_row_id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourceRowId": row.ID, "relationProperty": relationID,
			"cursor": job.DependencyEvaluationCursor, "limit": directLimit,
		}).All(&direct); err != nil {
			return job, nil, err
		}
		var targetValues map[string]any
		if windowTargets && len(direct) > 0 {
			if targetValues, err = computeDatabaseQueryTargetWindow(app, job, context, direct, targetID); err != nil {
				return job, nil, err
			}
			if targetValues == nil {
				state.Accumulator.TargetsOneByOne = true
				direct, directLimit = direct[:1], 1
			}
		}
		ids := make([]string, len(direct))
		for index := range direct {
			ids[index] = direct[index].TargetRowID
		}
		loaded, err := loadDatabaseQueryComputedRowsByID(app, job.ContentOwnerID, context.readablePages(job.PageID), context.visibleRowIDs(ids))
		if err != nil {
			return job, nil, err
		}
		for _, item := range direct {
			if remaining < 1 {
				break
			}
			// An item hidden from the person is left out, not counted empty.
			if context.hidden(item.TargetRowID) {
				job.DependencyEvaluationCursor = item.Ordinal
				remaining--
				continue
			}
			target, visible := loaded[item.TargetRowID]
			var value, leaf any
			if visible && function != "count" {
				target = context.rowAsRead(target)
				value = databaseRollupTargetValue(targetProperty, target)
				if targetProperty.Type == graphValueTypeRelation {
					if leaf, value, err = loadDatabaseQueryRollupRelation(app, job.ContentOwnerID, context, item.TargetRowID, targetID); err != nil {
						return job, nil, err
					}
				}
				if targetValues != nil {
					value = targetValues[item.TargetRowID]
				} else if targetProperty.Type == graphValueTypeFormula || targetProperty.Type == graphValueTypeRollup {
					if state.Target == nil {
						state.Target = newDatabaseQueryComputedTargetState(item.TargetRowID, targetID)
					}
					if state.Target.RootRowID != item.TargetRowID || state.Target.RootPropertyID != targetID {
						return job, nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
					}
					targetRemaining := remaining - 1
					complete, computed, err := advanceDatabaseQueryComputedTarget(
						app, job, context, state.Target, &targetRemaining,
					)
					if err != nil {
						return job, nil, err
					}
					remaining = targetRemaining + 1
					if !complete {
						return persistDatabaseQueryEvaluationState(app, job, state, nil)
					}
					value = computed
					state.Target = nil
				}
				if targetProperty.Type != graphValueTypeRelation {
					leaf = value
				}
				if databaseRollupShowsValues(function) {
					value = databaseRollupShownValue(targetProperty, value, context.People)
				}
			}
			state.Accumulator.InputCount++
			if visible {
				state.Accumulator.VisibleCount++
				if function != "count" {
					if err := accumulateDatabaseQueryRollupLeaf(
						app, job, property.ID, item, value, leaf, &state.Accumulator,
					); err != nil {
						return job, nil, err
					}
				}
			}
			job.DependencyEvaluationCursor = item.Ordinal
			remaining--
		}
		if remaining == 0 {
			return persistDatabaseQueryEvaluationState(app, job, state, nil)
		}
		// A full window may have more after it: one read item by item, as
		// each computed target is worked out, goes on with the next
		// (2026-11-12: a sum of a related formula reading a rollup kept only
		// its first related row's value).
		if len(direct) == directLimit {
			continue
		}
		if function == "median" && state.Accumulator.NumberCount > 0 {
			state.Accumulator.ReducingMedian = true
			return persistDatabaseQueryEvaluationState(app, job, state, nil)
		}
		if err := finishDatabaseQueryRollup(&state, property, &job); err != nil {
			return job, nil, err
		}
	}
	fullRows, err := loadDatabaseQuerySourceRowsByID(app, job, []string{row.ID})
	if err != nil {
		return job, nil, err
	}
	fullRow, ok := fullRows[row.ID]
	if !ok || fullRow.Position != row.Position {
		return job, nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	// Formulas read the related pages, and through them the rows their
	// computed values read, in one go when they fit the bound a row read on
	// its own has (database_row_dependency_closure.go), else the related
	// pages alone when they fit one turn's; the rollups above were already
	// reduced turn by turn.
	related, within, err := loadDatabaseRowDependencyClosure(
		app, job.ContentOwnerID, job.PageID, fullRow, context, state.Results,
	)
	if err != nil {
		return job, nil, err
	}
	if !within {
		related = nil
		if _, ids, boundErr := boundedDatabaseQueryRows([]storedDatabaseQueryRow{fullRow}, context); boundErr == nil && len(ids) > 0 {
			if related, err = loadDatabaseQueryRowsByID(app, job.ContentOwnerID, context.readablePages(job.PageID), context.visibleRowIDs(ids)); err != nil {
				return job, nil, err
			}
		}
	}
	evaluated, accepted, err := evaluateDatabaseQueryRow(
		fullRow, related, state.Results, context, plan, job.PrincipalID, job.PageID,
	)
	if err != nil {
		return job, nil, err
	}
	if !accepted {
		evaluated = databaseQueryEvaluatedRow{}
	}
	return job, &evaluated, nil
}

func decodeDatabaseQueryDependencyEvaluation(raw string) (databaseQueryDependencyEvaluationState, error) {
	state := databaseQueryDependencyEvaluationState{}
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return state, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	if state.Results == nil {
		state.Results = map[string]databaseComputedValue{}
	}
	return state, nil
}

func persistDatabaseQueryEvaluationState(
	app core.App,
	job storedDatabaseQueryJob,
	state databaseQueryDependencyEvaluationState,
	evaluated *databaseQueryEvaluatedRow,
) (storedDatabaseQueryJob, *databaseQueryEvaluatedRow, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return job, nil, err
	}
	job.DependencyEvaluationJSON = string(encoded)
	if err := persistDatabaseQueryDependencyProgress(app, job); err != nil {
		return job, nil, err
	}
	return job, evaluated, nil
}

func finishDatabaseQueryRollup(
	state *databaseQueryDependencyEvaluationState,
	property storedDatabaseQueryProperty,
	job *storedDatabaseQueryJob,
) error {
	value, err := finalizeDatabaseQueryRollup(state.Accumulator)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	state.Results[property.ID] = databaseComputedValue{
		Type: property.Type, Value: encoded, Text: databaseRollupText(state.Accumulator.Function, value),
		Leaves: databaseRollupEncodedLeaves(state.Accumulator.Function, state.Accumulator.Leaves),
	}
	state.Accumulator = databaseQueryRollupAccumulator{}
	job.DependencyEvaluationProperty++
	job.DependencyEvaluationCursor = -1
	return nil
}

// finishDatabaseQueryUnsetRollup records a rollup that can't be computed
// (databaseQueryContext.rollupSetUp) as empty.
func finishDatabaseQueryUnsetRollup(
	state *databaseQueryDependencyEvaluationState,
	property storedDatabaseQueryProperty,
	job *storedDatabaseQueryJob,
) {
	state.Results[property.ID] = databaseComputedValue{Type: property.Type, Value: json.RawMessage("null")}
	state.Accumulator = databaseQueryRollupAccumulator{}
	job.DependencyEvaluationProperty++
	job.DependencyEvaluationCursor = -1
}

func accumulateDatabaseQueryRollup(
	app core.App,
	job storedDatabaseQueryJob,
	propertyID string,
	item storedDatabaseQueryDirectDependency,
	value any,
	accumulator *databaseQueryRollupAccumulator,
) error {
	return accumulateDatabaseQueryRollupLeaf(app, job, propertyID, item, value, value, accumulator)
}

// accumulateDatabaseQueryRollupLeaf adds a related value to a rollup and
// keeps leaf as the value its filters read and its counts count: a
// relation's rows by ID, which it shows by name (an untitled one is a page
// all the same, as Notion counts it).
func accumulateDatabaseQueryRollupLeaf(
	app core.App,
	job storedDatabaseQueryJob,
	propertyID string,
	item storedDatabaseQueryDirectDependency,
	value, leaf any,
	accumulator *databaseQueryRollupAccumulator,
) error {
	if databaseRollupKeepsLeaves(accumulator.Function) {
		accumulator.Leaves = append(accumulator.Leaves, leaf)
	}
	if leaf == nil || leaf == "" {
		return nil
	}
	accumulator.PresentCount++
	accumulator.Values += databaseRollupValueCount(leaf)
	if !databaseRollupCountsValue(leaf) {
		accumulator.Booleans++
	}
	switch accumulator.Function {
	case "count_values", "not_empty", "empty", "percent_empty", "percent_not_empty":
		return nil
	case databaseRollupPercentInGroup, databaseRollupCountInGroup:
		if databaseRollupInGroup(value, accumulator.GroupOptions) {
			accumulator.CheckedCount++
		}
		return nil
	case "unique":
		if accumulator.Seen == nil {
			accumulator.Seen = map[string]bool{}
		}
		if !databaseRollupCountsValue(leaf) {
			return nil
		}
		for _, piece := range databaseRollupPieces(leaf) {
			if accumulator.Seen[piece] {
				continue
			}
			if accumulator.SeenBytes+len(piece) > maxDatabaseQueryComputedStringBytes {
				return errors.New(errorCodeDatabaseQueryDependencyLimit)
			}
			accumulator.Seen[piece] = true
			accumulator.SeenBytes += len(piece)
		}
		return nil
	case "checked", "unchecked", "percent_checked", "percent_unchecked":
		if databaseRollupChecked(value) {
			accumulator.CheckedCount++
		}
		return nil
	case "earliest_date", "latest_date", "date_range":
		for _, date := range databaseRollupDates(value) {
			accumulator.EarliestDate, accumulator.LatestDate = databaseRollupDateSpan(
				accumulator.EarliestDate, accumulator.LatestDate, date,
			)
		}
		return nil
	case "show_original", "show_unique", "count_per_group", "percent_per_group":
		if value == nil || value == "" {
			return nil
		}
		text := databaseQueryValueText(value)
		if accumulator.Function == "show_unique" {
			if accumulator.Seen == nil {
				accumulator.Seen = map[string]bool{}
			}
			if accumulator.Seen[text] {
				return nil
			}
			accumulator.Seen[text] = true
		}
		if accumulator.Text != "" {
			text = ", " + text
		}
		if len(accumulator.Text)+len(text) > maxDatabaseQueryComputedStringBytes {
			return errors.New(errorCodeDatabaseQueryDependencyLimit)
		}
		accumulator.Text += text
		return nil
	case "sum", "average", "median", "min", "max", "range":
		number, ok := databaseQueryNumber(value)
		if !ok {
			return nil
		}
		accumulator.NumberCount++
		accumulator.Sum += number
		if !accumulator.HasNumber || number < accumulator.Minimum {
			accumulator.Minimum = number
		}
		if !accumulator.HasNumber || number > accumulator.Maximum {
			accumulator.Maximum = number
		}
		accumulator.HasNumber = true
		if accumulator.Function != "median" {
			return nil
		}
		medianID := databaseQueryDependencySyntheticID(
			job.ID, databaseQueryDependencyKindMedian, propertyID+"\x00"+item.RowID,
		)
		_, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_database_query_dependency_values}}
				([[job_id]], [[source_row_id]], [[dependency_row_id]], [[target_row_id]],
				 [[ordinal]], [[processed]], [[row_json]], [[kind]], [[relation_property]], [[evaluation_number]])
			VALUES ({:jobId}, {:sourceRowId}, {:dependencyRowId}, '', {:ordinal}, 1, '',
				'median', {:relationProperty}, {:number})
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourceRowId": job.DependencySourceID,
			"dependencyRowId": medianID, "ordinal": item.Ordinal,
			"relationProperty": propertyID, "number": number,
		}).Execute()
		return err
	default:
		return errors.New(errorCodeDatabaseQueryInvalid)
	}
}

func reduceDatabaseQueryMedian(
	app core.App,
	job storedDatabaseQueryJob,
	propertyID string,
	accumulator *databaseQueryRollupAccumulator,
) (bool, error) {
	values := []storedDatabaseQueryMedianValue{}
	query := `
		SELECT [[dependency_row_id]], [[ordinal]], [[evaluation_number]]
		FROM {{leafen_database_query_dependency_values}}
		WHERE [[job_id]] = {:jobId} AND [[source_row_id]] = {:sourceRowId}
			AND [[kind]] = 'median' AND [[relation_property]] = {:relationProperty}`
	params := dbx.Params{
		"jobId": job.ID, "sourceRowId": job.DependencySourceID,
		"relationProperty": propertyID, "limit": databaseQueryDependencyRowsPerTurn,
	}
	if accumulator.ReductionID != "" {
		query += ` AND (
			[[evaluation_number]] > {:number}
			OR ([[evaluation_number]] = {:number} AND [[ordinal]] > {:ordinal})
			OR ([[evaluation_number]] = {:number} AND [[ordinal]] = {:ordinal}
				AND [[dependency_row_id]] > {:dependencyRowId})
		)`
		params["number"] = accumulator.ReductionNumber
		params["ordinal"] = accumulator.ReductionOrdinal
		params["dependencyRowId"] = accumulator.ReductionID
	}
	query += ` ORDER BY [[evaluation_number]], [[ordinal]], [[dependency_row_id]] LIMIT {:limit}`
	if err := app.DB().NewQuery(query).Bind(params).All(&values); err != nil {
		return false, err
	}
	lowerIndex := (accumulator.NumberCount - 1) / 2
	upperIndex := accumulator.NumberCount / 2
	for _, value := range values {
		index := accumulator.ReductionSeen
		if index == lowerIndex {
			copy := value.Number
			accumulator.MedianLower = &copy
		}
		if index == upperIndex {
			copy := value.Number
			accumulator.MedianUpper = &copy
		}
		accumulator.ReductionSeen++
		accumulator.ReductionNumber = value.Number
		accumulator.ReductionOrdinal = value.Ordinal
		accumulator.ReductionID = value.RowID
	}
	return accumulator.ReductionSeen > upperIndex, nil
}
