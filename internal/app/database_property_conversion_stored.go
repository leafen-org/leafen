package app

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A cell keeps, through changes of its property's type, the value last
// written in it, which each type reads its own way, as Notion's does on the
// screen and through the API (seen 2026-11-09: text made a number and then a
// select read "A", "B" and "12 apples" as those options, not the number's
// 12; a select made a date and then text had its names back; a status made a
// select with Done alone and then text had "In progress"; a number made a
// checkbox and then text had "3" and "12.5"; a cell written as a number in
// between read as that number). Each conversion keeps every cell's value
// before it and what it left there. A cell still holding what the
// conversions before this one left reads from the value they started from;
// a cell written since reads from itself. A change back to a type the
// property had is the same reading of the same value, so an undo restores
// the cells as they were. A change in place (a status made a select, text a
// URL) leaves the cells as they are; a value named by the settings the
// property had before it reads by those.

// databasePropertyConversionStored is the value a conversion reads a cell
// from: the type it was written as, its settings then, and nil for none.
type databasePropertyConversionStored struct {
	valueType string
	value     json.RawMessage
	config    map[string]any
}

// databasePropertyConversionKept is a cell as an earlier conversion kept it:
// its value before (type "" when it had none) and, from migration 723, what
// the conversion left there ("null" for nothing).
type databasePropertyConversionKept struct {
	ConversionID string         `db:"conversion_id"`
	RowID        string         `db:"row_id"`
	Type         string         `db:"value_type"`
	Value        string         `db:"value"`
	Produced     sql.NullString `db:"produced"`
}

// databasePropertyConversionPast is one of the conversions a conversion
// follows on from, with the settings its source values read by and the
// settings it converted to.
type databasePropertyConversionPast struct {
	conversion storedDatabasePropertyConversion
	config     map[string]any
	next       map[string]any
	inPlace    bool
	kept       map[string]databasePropertyConversionKept
	rules      *databasePropertyConversion
}

// databasePropertyConversionHistory is what a conversion reads its cells by:
// the settings of its own source values and the conversions before it,
// newest first.
type databasePropertyConversionHistory struct {
	own   map[string]any
	past  []databasePropertyConversionPast
	names databasePropertyConversionNames
}

// loadDatabasePropertyConversionHistory follows a conversion back through the
// conversions that took the property to its source type, each finished and
// still kept. A change by hand to a relation reads its own cells alone.
func loadDatabasePropertyConversionHistory(
	app core.App, conversion storedDatabasePropertyConversion,
) (databasePropertyConversionHistory, error) {
	history := databasePropertyConversionHistory{own: map[string]any{}, names: databasePropertyConversionNamesIn(app)}
	if json.Unmarshal([]byte(conversion.FromConfig), &history.own) != nil {
		return history, errNotionDatabaseRowPageProjectionInvalid
	}
	if conversion.ToType == graphValueTypeRelation && !conversion.RestoreOf.Valid {
		return history, nil
	}
	rows := []struct {
		storedDatabasePropertyConversion
		ExpiresAt sql.NullInt64 `db:"expires_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[property_id]],[[actor_id]],
			[[from_type]],[[from_config]],[[to_type]],[[status]],[[processed]],[[restore_of]],[[listed_options]],[[expires_at]]
		FROM {{leafen_database_property_conversions}}
		WHERE [[property_id]]={:propertyId}
			AND ([[created_at]],[[id]]) < (SELECT [[created_at]],[[id]] FROM {{leafen_database_property_conversions}} WHERE [[id]]={:id})
		ORDER BY [[created_at]] DESC,[[id]] DESC`,
	).Bind(dbx.Params{"propertyId": conversion.PropertyID, "id": conversion.ID}).All(&rows); err != nil {
		return history, err
	}
	now, expect, next := time.Now().UTC().UnixMilli(), conversion.FromType, history.own
	for _, row := range rows {
		if row.Status == databasePropertyConversionPending || !row.ExpiresAt.Valid || row.ExpiresAt.Int64 <= now ||
			!graphValueTypesShareRepresentation(row.ToType, expect) {
			break
		}
		config := map[string]any{}
		if json.Unmarshal([]byte(row.FromConfig), &config) != nil {
			return history, errNotionDatabaseRowPageProjectionInvalid
		}
		history.past = append(history.past, databasePropertyConversionPast{
			conversion: row.storedDatabasePropertyConversion, config: config, next: next,
			inPlace: graphValueTypesShareRepresentation(row.FromType, row.ToType),
		})
		expect, next = row.FromType, config
	}
	// A value written before a change in place is named by the settings then.
	history.own = databasePropertyConversionWithEarlierOptions(history.own, history.past)
	for index := range history.past {
		past := &history.past[index]
		past.config = databasePropertyConversionWithEarlierOptions(past.config, history.past[index+1:])
	}
	return history, nil
}

// databasePropertyConversionWithEarlierOptions adds to config the options of
// the changes in place just before it that it does not name itself.
func databasePropertyConversionWithEarlierOptions(config map[string]any, earlier []databasePropertyConversionPast) map[string]any {
	items, _ := config["options"].([]any)
	known := map[string]bool{}
	for _, option := range databasePropertyConversionOptions(config) {
		if id, _ := option["id"].(string); id != "" {
			known[id] = true
		}
	}
	merged := append([]any{}, items...)
	for _, past := range earlier {
		if !past.inPlace {
			break
		}
		for _, option := range databasePropertyConversionOptions(past.config) {
			if id, _ := option["id"].(string); id != "" && !known[id] {
				known[id] = true
				merged = append(merged, option)
			}
		}
	}
	if len(merged) == len(items) {
		return config
	}
	copied := make(map[string]any, len(config)+1)
	for key, value := range config {
		copied[key] = value
	}
	copied["options"] = merged
	return copied
}

// converted lists the conversions before this one that converted cells.
func (history databasePropertyConversionHistory) converted() []string {
	ids := []string{}
	for _, past := range history.past {
		if !past.inPlace {
			ids = append(ids, past.conversion.ID)
		}
	}
	return ids
}

// readKept loads what the conversions before kept of these rows.
func (history *databasePropertyConversionHistory) readKept(app core.App, rowIDs []string) error {
	ids := history.converted()
	if len(ids) == 0 || len(rowIDs) == 0 {
		return nil
	}
	encodedIDs, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	encodedRows, err := json.Marshal(rowIDs)
	if err != nil {
		return err
	}
	kept := []databasePropertyConversionKept{}
	if err := app.DB().NewQuery(`SELECT [[conversion_id]],[[row_id]],[[value_type]],[[value]],[[produced]]
		FROM {{leafen_database_property_conversion_cells}}
		WHERE [[conversion_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND [[row_id]] IN (SELECT [[value]] FROM json_each({:rows}))`,
	).Bind(dbx.Params{"ids": string(encodedIDs), "rows": string(encodedRows)}).All(&kept); err != nil {
		return err
	}
	byConversion := map[string]map[string]databasePropertyConversionKept{}
	for _, cell := range kept {
		if byConversion[cell.ConversionID] == nil {
			byConversion[cell.ConversionID] = map[string]databasePropertyConversionKept{}
		}
		byConversion[cell.ConversionID][cell.RowID] = cell
	}
	for index := range history.past {
		history.past[index].kept = byConversion[history.past[index].conversion.ID]
	}
	return nil
}

// emptyRows lists rows with no value now that a conversion before kept a
// value of and this one has not read yet.
func (history databasePropertyConversionHistory) emptyRows(
	app core.App, conversion storedDatabasePropertyConversion, limit int,
) ([]databasePropertyConversionCell, error) {
	ids := history.converted()
	if len(ids) == 0 || limit <= 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	cells := []databasePropertyConversionCell{}
	err = app.DB().NewQuery(`SELECT DISTINCT [[cells]].[[row_id]] AS [[row_id]]
		FROM {{leafen_database_property_conversion_cells}} AS [[cells]]
		JOIN {{leafen_database_rows}} AS [[rows]] ON [[rows]].[[id]]=[[cells]].[[row_id]]
			AND [[rows]].[[owner]]={:owner} AND [[rows]].[[page_id]]={:pageId}
		WHERE [[cells]].[[conversion_id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[cells]].[[value_type]]!=''
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_values}} AS [[values]]
				WHERE [[values]].[[row_id]]=[[cells]].[[row_id]] AND [[values]].[[property_id]]={:propertyId})
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_property_conversion_cells}} AS [[read]]
				WHERE [[read]].[[conversion_id]]={:id} AND [[read]].[[row_id]]=[[cells]].[[row_id]])
		ORDER BY [[cells]].[[row_id]] LIMIT {:limit}`,
	).Bind(dbx.Params{
		"ids": string(encoded), "owner": conversion.Owner, "pageId": conversion.PageID,
		"propertyId": conversion.PropertyID, "id": conversion.ID, "limit": limit,
	}).All(&cells)
	return cells, err
}

// storedValue is the value a cell holding current (nil for none) reads from:
// walking back while each conversion before left the cell as it is now, the
// value that conversion started from.
func (history *databasePropertyConversionHistory) storedValue(
	conversion storedDatabasePropertyConversion, rowID string, current json.RawMessage,
) (databasePropertyConversionStored, error) {
	stored := databasePropertyConversionStored{valueType: conversion.FromType, value: current, config: history.own}
	for index := range history.past {
		past := &history.past[index]
		if past.inPlace {
			continue
		}
		kept, found := past.kept[rowID]
		var before json.RawMessage
		if found && kept.Type != "" {
			if !graphValueTypesShareRepresentation(kept.Type, past.conversion.FromType) {
				break
			}
			before = json.RawMessage(kept.Value)
		}
		left, err := past.left(kept, found, before, history.names)
		if err != nil {
			return stored, err
		}
		if !databasePropertyConversionSameCell(stored.value, left) {
			break
		}
		stored = databasePropertyConversionStored{valueType: past.conversion.FromType, value: before, config: past.config}
	}
	return stored, nil
}

// left is what a conversion left in a cell: as it recorded, or for a cell
// kept before that was recorded, what its rules make of the value before.
func (past *databasePropertyConversionPast) left(
	kept databasePropertyConversionKept, found bool, before json.RawMessage, names databasePropertyConversionNames,
) (json.RawMessage, error) {
	if !found {
		return nil, nil
	}
	if kept.Produced.Valid {
		return json.RawMessage(kept.Produced.String), nil
	}
	if past.rules == nil {
		rules, err := databasePropertyConversionRulesFor(past.conversion.ListedOptions, past.conversion.FromType,
			past.conversion.ToType, past.config, past.next, names)
		if err != nil {
			return nil, err
		}
		past.rules = rules
	}
	return past.rules.Convert(before)
}

func databasePropertyConversionSameCell(left, right json.RawMessage) bool {
	empty := func(value json.RawMessage) bool { return len(value) == 0 || string(value) == "null" }
	if empty(left) || empty(right) {
		return empty(left) && empty(right)
	}
	return databasePropertyConversionSameJSON(left, right)
}

// planDatabasePropertyConversionStored adds to a turn the rows with no value
// now that a conversion before kept one of, and the value each cell reads from.
func planDatabasePropertyConversionStored(app core.App, turn databasePropertyConversionTurn) (databasePropertyConversionTurn, error) {
	history, err := loadDatabasePropertyConversionHistory(app, turn.conversion)
	if err != nil {
		return turn, err
	}
	turn.ownConfig = history.own
	if turn.refilled, err = history.emptyRows(app, turn.conversion, databasePropertyConversionCellsPerTurn); err != nil {
		return turn, err
	}
	// A turn reads the first cells by their rows' ids of both, the rest
	// waiting for the turns after it.
	turn.complete = len(turn.originals)+len(turn.refilled) < databasePropertyConversionCellsPerTurn
	if !turn.complete {
		read := turn.cells()[:databasePropertyConversionCellsPerTurn]
		last := read[len(read)-1].RowID
		for len(turn.originals) > 0 && turn.originals[len(turn.originals)-1].RowID > last {
			turn.originals = turn.originals[:len(turn.originals)-1]
		}
		for len(turn.refilled) > 0 && turn.refilled[len(turn.refilled)-1].RowID > last {
			turn.refilled = turn.refilled[:len(turn.refilled)-1]
		}
	}
	cells := append(append([]databasePropertyConversionCell{}, turn.originals...), turn.refilled...)
	rowIDs := make([]string, 0, len(cells))
	for _, cell := range cells {
		rowIDs = append(rowIDs, cell.RowID)
	}
	if err := history.readKept(app, rowIDs); err != nil {
		return turn, err
	}
	turn.stored = make(map[string]databasePropertyConversionStored, len(cells))
	for _, cell := range cells {
		var current json.RawMessage
		if cell.Value != "" {
			current = json.RawMessage(cell.Value)
		}
		if turn.stored[cell.RowID], err = history.storedValue(turn.conversion, cell.RowID, current); err != nil {
			return turn, err
		}
	}
	return turn, nil
}
