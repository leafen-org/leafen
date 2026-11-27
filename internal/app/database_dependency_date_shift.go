package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// When a task's endpoints change, its related tasks move as the
// database's dependency settings say: only when the dates now overlap
// (read to the minute, database_dependency_date_overlap.go), by the same
// amount to keep their spacing, or not at all, optionally never starting or
// ending on a weekend and keeping their working-day duration. A moved task
// moves by days and keeps each value's time, offset, time zone and reminder
// (an approved difference): Notion's gives it the time its predecessor ends
// at, keeps its working time to whole days only and drops its time zones and
// reminder (seen 2026-11-21). The moves join the same content-graph mutation
// on every writing surface.

type databaseDependencyShiftSettings struct {
	Enabled             bool   `json:"enabled"`
	ShiftMode           string `json:"shiftMode"`
	AvoidWeekends       *bool  `json:"avoidWeekends"`
	DateMode            string `json:"dateMode"`
	DatePropertyID      string `json:"datePropertyId"`
	StartDatePropertyID string `json:"startDatePropertyId"`
	EndDatePropertyID   string `json:"endDatePropertyId"`
}

func (settings databaseDependencyShiftSettings) separate() bool {
	return settings.DateMode == "separate"
}

// databaseTaskSpan is a task's dates: a start and end day plus whatever
// time and offset each value carried after its date, and the time zone of
// each value.
type databaseTaskSpan struct {
	Start, End             time.Time
	StartSuffix, EndSuffix string
	StartZone, EndZone     string
	HasEnd                 bool
}

func parseDatabaseTaskDay(value string) (time.Time, string, bool) {
	if len(value) < 10 {
		return time.Time{}, "", false
	}
	day, err := time.Parse(time.DateOnly, value[:10])
	return day, value[10:], err == nil
}

func loadDatabaseDependencyShiftSettings(app core.App, owner, dataSourceID string) (databaseDependencyShiftSettings, error) {
	settings := databaseDependencyShiftSettings{}
	raw := ""
	err := app.DB().NewQuery(`
		SELECT [[dependencies_json]] FROM {{leafen_database_task_features}}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[owner]] = {:owner}
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "owner": owner}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (raw == "" || raw == "null") {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if json.Unmarshal([]byte(raw), &settings) != nil {
		return databaseDependencyShiftSettings{}, nil
	}
	if settings.ShiftMode == "" {
		settings.ShiftMode = "overlap"
	}
	return settings, nil
}

// databaseTaskSpanFromValues reads a row's dates through `value`.
func databaseTaskSpanFromValues(
	settings databaseDependencyShiftSettings,
	value func(propertyID string) (any, bool),
) (databaseTaskSpan, bool) {
	dateOf := func(propertyID string) (string, string, string, bool) {
		raw, exists := value(propertyID)
		object, valid := canonicalGraphDateValue(raw)
		if !exists || !valid {
			return "", "", "", false
		}
		start, _ := object["start"].(string)
		end, _ := object["end"].(string)
		zone, _ := object["timeZone"].(string)
		return start, end, zone, start != ""
	}
	span := databaseTaskSpan{}
	var startValue, endValue string
	if settings.separate() {
		start, _, startZone, startOK := dateOf(settings.StartDatePropertyID)
		end, _, endZone, endOK := dateOf(settings.EndDatePropertyID)
		if !startOK {
			return span, false
		}
		startValue, endValue, span.HasEnd = start, end, endOK
		span.StartZone, span.EndZone = startZone, endZone
	} else {
		start, end, zone, ok := dateOf(settings.DatePropertyID)
		if !ok {
			return span, false
		}
		startValue, endValue, span.HasEnd = start, end, end != ""
		span.StartZone, span.EndZone = zone, zone
	}
	var ok bool
	if span.Start, span.StartSuffix, ok = parseDatabaseTaskDay(startValue); !ok {
		return span, false
	}
	if !span.HasEnd {
		span.End, span.EndSuffix, span.EndZone = span.Start, span.StartSuffix, span.StartZone
		return span, true
	}
	if span.End, span.EndSuffix, ok = parseDatabaseTaskDay(endValue); !ok || span.End.Before(span.Start) {
		span.End, span.EndSuffix, span.EndZone, span.HasEnd = span.Start, span.StartSuffix, span.StartZone, false
	}
	return span, true
}

func databaseTaskWeekend(day time.Time) bool {
	return day.Weekday() == time.Saturday || day.Weekday() == time.Sunday
}

func databaseTaskCalendarDays(start, end time.Time) int {
	return int((end.Unix() - start.Unix()) / 86400)
}

// Count full weeks at once; only the last partial week needs day-by-day work.
func databaseTaskWeekdays(start, end time.Time) int {
	if end.Before(start) {
		return 0
	}
	days := databaseTaskCalendarDays(start, end) + 1
	count := days / 7 * 5
	for offset := 0; offset < days%7; offset++ {
		if !databaseTaskWeekend(start.AddDate(0, 0, offset)) {
			count++
		}
	}
	return count
}

// Advance by working days. A weekend's first working day consumes one step.
func databaseTaskAdvanceWeekdays(day time.Time, days int) time.Time {
	if databaseTaskWeekend(day) {
		for databaseTaskWeekend(day) {
			day = day.AddDate(0, 0, 1)
		}
		if days > 0 {
			days--
		}
	}
	offset := days / 5 * 7
	remainder := days % 5
	if int(day.Weekday())+remainder > int(time.Friday) {
		offset += 2
	}
	return day.AddDate(0, 0, offset+remainder)
}

func databaseTaskRetreatWeekdays(day time.Time, days int) time.Time {
	if databaseTaskWeekend(day) {
		for databaseTaskWeekend(day) {
			day = day.AddDate(0, 0, -1)
		}
		if days > 0 {
			days--
		}
	}
	offset := days / 5 * 7
	remainder := days % 5
	if int(day.Weekday())-remainder < int(time.Monday) {
		offset += 2
	}
	return day.AddDate(0, 0, -offset-remainder)
}

// shiftDatabaseTaskSpan moves a dependent task after its blocking task moved
// from `before` to `after`, or reports that it stays. An overlap moves it
// to the first day after the blocking task's last; one between times in
// different time zones, whose written days can disagree, never moves it
// earlier.
func shiftDatabaseTaskSpan(
	settings databaseDependencyShiftSettings,
	before, after, dependent databaseTaskSpan,
) (databaseTaskSpan, bool) {
	if settings.AvoidWeekends == nil || *settings.AvoidWeekends {
		gap := 1
		switch settings.ShiftMode {
		case "maintain_spacing":
			if after.End.Equal(before.End) {
				return dependent, false
			}
			gap = max(1, databaseTaskWeekdays(before.End.AddDate(0, 0, 1), dependent.Start))
		case "overlap":
			if !databaseTaskOverlaps(after, dependent) {
				return dependent, false
			}
		default:
			return dependent, false
		}
		moved := dependent
		moved.Start = databaseTaskAdvanceWeekdays(after.End, gap)
		if settings.ShiftMode == "overlap" && moved.Start.Before(dependent.Start) {
			return dependent, false
		}
		duration := max(1, databaseTaskWeekdays(dependent.Start, dependent.End))
		moved.End = databaseTaskAdvanceWeekdays(moved.Start, duration-1)
		return moved, !moved.Start.Equal(dependent.Start) || !moved.End.Equal(dependent.End)
	}
	days := 0
	switch settings.ShiftMode {
	case "maintain_spacing":
		days = databaseTaskCalendarDays(before.End, after.End)
	case "overlap":
		if databaseTaskOverlaps(after, dependent) {
			days = max(0, databaseTaskCalendarDays(dependent.Start, after.End.AddDate(0, 0, 1)))
		}
	}
	if days == 0 {
		return dependent, false
	}
	moved := dependent
	moved.Start, moved.End = dependent.Start.AddDate(0, 0, days), dependent.End.AddDate(0, 0, days)
	return moved, !moved.Start.Equal(dependent.Start)
}

// shiftDatabasePredecessorSpan moves a blocking task for the task it blocks,
// which starts at `after` (from `before`: a date change reads predecessors
// only for a start that changed, a new link only for an overlap), or
// reports that it stays. Spacing, read by days, holds while the start keeps
// its day; an overlap, read as for successors, never moves it later.
func shiftDatabasePredecessorSpan(
	settings databaseDependencyShiftSettings,
	before, after, predecessor databaseTaskSpan,
) (databaseTaskSpan, bool) {
	if settings.AvoidWeekends == nil || *settings.AvoidWeekends {
		gap := 1
		switch settings.ShiftMode {
		case "maintain_spacing":
			if after.Start.Equal(before.Start) {
				return predecessor, false
			}
			gap = max(1, databaseTaskWeekdays(predecessor.End.AddDate(0, 0, 1), before.Start))
		case "overlap":
			if !databaseTaskOverlaps(predecessor, after) {
				return predecessor, false
			}
		default:
			return predecessor, false
		}
		moved := predecessor
		moved.End = databaseTaskRetreatWeekdays(after.Start, gap)
		if settings.ShiftMode == "overlap" && moved.End.After(predecessor.End) {
			return predecessor, false
		}
		duration := max(1, databaseTaskWeekdays(predecessor.Start, predecessor.End))
		moved.Start = databaseTaskRetreatWeekdays(moved.End, duration-1)
		return moved, !moved.Start.Equal(predecessor.Start) || !moved.End.Equal(predecessor.End)
	}
	days := 0
	switch settings.ShiftMode {
	case "maintain_spacing":
		days = databaseTaskCalendarDays(before.Start, after.Start)
	case "overlap":
		if databaseTaskOverlaps(predecessor, after) {
			days = min(0, databaseTaskCalendarDays(predecessor.End, after.Start.AddDate(0, 0, -1)))
		}
	}
	if days == 0 {
		return predecessor, false
	}
	moved := predecessor
	moved.Start, moved.End = predecessor.Start.AddDate(0, 0, days), predecessor.End.AddDate(0, 0, days)
	return moved, true
}

// databaseTaskSpanValues writes a moved task back as date values, keeping
// each value's other fields (time zone, reminder) as they were.
func databaseTaskSpanValues(
	settings databaseDependencyShiftSettings,
	span databaseTaskSpan,
	current func(propertyID string) map[string]any,
) (map[string]graphRowValue, error) {
	values := map[string]graphRowValue{}
	put := func(propertyID string, fields map[string]any) error {
		next := map[string]any{}
		for key, value := range current(propertyID) {
			next[key] = value
		}
		for key, value := range fields {
			next[key] = value
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		values[propertyID] = graphRowValue{Type: graphValueTypeDate, Value: encoded}
		return nil
	}
	start := span.Start.Format(time.DateOnly) + span.StartSuffix
	end := span.End.Format(time.DateOnly) + span.EndSuffix
	if settings.separate() {
		if err := put(settings.StartDatePropertyID, map[string]any{"start": start}); err != nil {
			return nil, err
		}
		if span.HasEnd {
			if err := put(settings.EndDatePropertyID, map[string]any{"start": end}); err != nil {
				return nil, err
			}
		}
		return values, nil
	}
	fields := map[string]any{"start": start}
	if span.HasEnd {
		fields["end"] = end
	}
	return values, put(settings.DatePropertyID, fields)
}

// databaseDependencyShiftOperation writes a moved task's dates over its row.
func databaseDependencyShiftOperation(
	settings databaseDependencyShiftSettings, dataSourceID string, row databaseDependencyDateRow, moved databaseTaskSpan,
) (graphOperation, error) {
	values, err := databaseTaskSpanValues(settings, moved, func(propertyID string) map[string]any {
		fields, _ := row.value(settings, propertyID)
		object, _ := fields.(map[string]any)
		return object
	})
	return graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
		ID: row.ID, DataSourceID: dataSourceID, Position: row.Position,
		SubitemParentID: optionalDatabaseRowParent(row.Parent), Values: values,
	}}, err
}

// expandDatabaseDependencyDateShifts follows start changes toward predecessors
// and end changes toward successors, from the rows the mutation moves and
// from those a link it adds moves (database_dependency_link_shift.go). Rows
// the mutation edits itself keep the values it gives them. A row the writer
// may not change (writable: the refusals their own edit of it meets) stays
// where it is and moves no row past it, as Notion's (seen 2026-11-08); the
// moves it would have made come back as denied, which the writer's notice
// tells.
func expandDatabaseDependencyDateShifts(
	app core.App,
	pageID, owner string,
	operations []graphOperation,
	writable func(graphOperation) (bool, error),
) ([]graphOperation, []graphOperation, error) {
	edited := map[string]bool{}
	bySource := map[string][]*graphDatabaseRow{}
	edgesBySource := map[string][]*graphDependencyEdge{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			edited[operation.Row.ID] = true
			bySource[operation.Row.DataSourceID] = append(bySource[operation.Row.DataSourceID], operation.Row)
		}
		if edge := operation.DependencyEdge; operation.Kind == graphOperationKindUpsertDependencyEdge && edge != nil {
			edgesBySource[edge.DataSourceID] = append(edgesBySource[edge.DataSourceID], edge)
			if _, found := bySource[edge.DataSourceID]; !found {
				bySource[edge.DataSourceID] = nil
			}
		}
	}
	derived, denied := []graphOperation{}, []graphOperation{}
	// admit keeps a move the writer may make; one they may not is denied.
	admit := func(operation graphOperation) (bool, error) {
		allowed, err := writable(operation)
		if err == nil && !allowed {
			denied = append(denied, operation)
		}
		return allowed, err
	}
	for dataSourceID, rows := range bySource {
		settings, err := loadDatabaseDependencyShiftSettings(app, owner, dataSourceID)
		if err != nil {
			return nil, nil, err
		}
		binding, bound, err := loadDatabaseDependencyRecord(app, pageID, dataSourceID)
		if err != nil {
			return nil, nil, err
		}
		if !bound || !settings.Enabled || settings.ShiftMode == "none" {
			continue
		}
		ids := make([]string, len(rows))
		for index, row := range rows {
			ids[index] = row.ID
		}
		stored, err := loadDatabaseDependencyDateRows(app, owner, pageID, dataSourceID, settings, ids)
		if err != nil {
			return nil, nil, err
		}
		change := func(id string, before, after databaseTaskSpan) databaseDependencyDateChange {
			return databaseDependencyDateChange{ID: id, before: before, after: after,
				StartChanged: before.startDiffers(after), EndChanged: before.endDiffers(after)}
		}
		// An edited row's dates as the mutation leaves them.
		spanOf := func(row *graphDatabaseRow) (databaseTaskSpan, bool) {
			return databaseTaskSpanFromValues(settings, func(propertyID string) (any, bool) {
				if written, found := row.Values[propertyID]; found {
					decoded, err := decodeBoundedGraphJSON(written.Value)
					return decoded, err == nil && decoded != nil
				}
				return stored[row.ID].value(settings, propertyID)
			})
		}
		queue := []databaseDependencyDateChange{}
		for _, row := range rows {
			before, hadDates := databaseTaskSpanFromValues(settings, func(propertyID string) (any, bool) {
				return stored[row.ID].value(settings, propertyID)
			})
			after, hasDates := spanOf(row)
			if hadDates && hasDates && (before.startDiffers(after) || before.endDiffers(after)) {
				queue = append(queue, change(row.ID, before, after))
			}
		}
		visited := map[string]bool{}
		moves, err := databaseDependencyLinkMoves(app, owner, pageID, binding, settings, rows, edgesBySource[dataSourceID], stored, spanOf, edited)
		if err != nil {
			return nil, nil, err
		}
		for _, move := range moves {
			operation, err := databaseDependencyShiftOperation(settings, dataSourceID, move.row, move.after)
			if err != nil {
				return nil, nil, err
			}
			visited[move.row.ID] = true
			if allowed, err := admit(operation); err != nil {
				return nil, nil, err
			} else if !allowed {
				continue
			}
			derived = append(derived, operation)
			queue = append(queue, change(move.row.ID, move.before, move.after))
		}
		for len(queue) > 0 {
			count := min(len(queue), databaseDependencyDateReadWindow)
			frontier := queue[:count]
			queue = queue[count:]
			cursor := databaseDependencyDateRow{Sequence: -1}
			for {
				neighbors, err := loadDatabaseDependencyDateNeighbors(app, owner, pageID, dataSourceID, binding.DataKey, settings, frontier, cursor)
				if err != nil {
					return nil, nil, err
				}
				for _, neighbor := range neighbors {
					if edited[neighbor.ID] || visited[neighbor.ID] || neighbor.Position == "" {
						continue
					}
					current := frontier[neighbor.Sequence]
					value := func(propertyID string) (any, bool) { return neighbor.value(settings, propertyID) }
					relatedDates, dated := databaseTaskSpanFromValues(settings, value)
					if !dated {
						continue
					}
					var moved databaseTaskSpan
					var shifted bool
					if neighbor.Predecessor {
						moved, shifted = shiftDatabasePredecessorSpan(settings, current.before, current.after, relatedDates)
					} else {
						moved, shifted = shiftDatabaseTaskSpan(settings, current.before, current.after, relatedDates)
					}
					if !shifted {
						continue
					}
					visited[neighbor.ID] = true
					operation, err := databaseDependencyShiftOperation(settings, dataSourceID, neighbor, moved)
					if err != nil {
						return nil, nil, err
					}
					if allowed, err := admit(operation); err != nil {
						return nil, nil, err
					} else if !allowed {
						continue
					}
					derived = append(derived, operation)
					queue = append(queue, change(neighbor.ID, relatedDates, moved))
				}
				if len(neighbors) < databaseDependencyDateReadWindow {
					break
				}
				cursor = neighbors[len(neighbors)-1]
			}
			clear(frontier)
		}

	}
	if len(derived) == 0 {
		return operations, denied, nil
	}
	return append(append(make([]graphOperation, 0, len(operations)+len(derived)), operations...), derived...), denied, nil
}
