package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"unicode"

	"github.com/pocketbase/pocketbase/core"
)

type databaseQueryComputedTargetState struct {
	RootRowID      string                             `json:"rootRowId"`
	RootPropertyID string                             `json:"rootPropertyId"`
	Frames         []databaseQueryComputedTargetFrame `json:"frames"`
}

type databaseQueryComputedTargetFrame struct {
	RowID              string                           `json:"rowId"`
	PropertyID         string                           `json:"propertyId"`
	RollupIndex        int                              `json:"rollupIndex"`
	RelationCursor     int                              `json:"relationCursor"`
	Seed               map[string]databaseComputedValue `json:"seed"`
	Accumulator        databaseQueryRollupAccumulator   `json:"accumulator"`
	WaitingTargetRowID string                           `json:"waitingTargetRowId"`
	WaitingOrdinal     int                              `json:"waitingOrdinal"`
}

func newDatabaseQueryComputedTargetState(rowID, propertyID string) *databaseQueryComputedTargetState {
	return &databaseQueryComputedTargetState{
		RootRowID: rowID, RootPropertyID: propertyID,
		Frames: []databaseQueryComputedTargetFrame{{
			RowID: rowID, PropertyID: propertyID,
			Seed: map[string]databaseComputedValue{},
		}},
	}
}

func databaseFormulaPropertyReferences(expression string) []string {
	runes := []rune(expression)
	references := []string{}
	for index := 0; index < len(runes); {
		if runes[index] == '\'' || runes[index] == '"' {
			_, index = databaseFormulaQuotedValue(runes, index)
			continue
		}
		if !databaseFormulaWordAt(runes, index, "prop") {
			index++
			continue
		}
		cursor := index + len([]rune("prop"))
		for cursor < len(runes) && unicode.IsSpace(runes[cursor]) {
			cursor++
		}
		if cursor >= len(runes) || runes[cursor] != '(' {
			index++
			continue
		}
		cursor++
		for cursor < len(runes) && unicode.IsSpace(runes[cursor]) {
			cursor++
		}
		if cursor >= len(runes) || (runes[cursor] != '\'' && runes[cursor] != '"') {
			index++
			continue
		}
		value, next := databaseFormulaQuotedValue(runes, cursor)
		if next <= cursor {
			index++
			continue
		}
		references = append(references, value)
		index = next
	}
	return references
}

func databaseFormulaQuotedValue(runes []rune, start int) (string, int) {
	if start >= len(runes) || (runes[start] != '\'' && runes[start] != '"') {
		return "", start
	}
	quote := runes[start]
	value := make([]rune, 0)
	for index := start + 1; index < len(runes); index++ {
		current := runes[index]
		if current == quote {
			return string(value), index + 1
		}
		if current == '\\' && index+1 < len(runes) {
			index++
			current = runes[index]
		}
		value = append(value, current)
	}
	return "", start
}

func databaseFormulaWordAt(runes []rune, start int, word string) bool {
	wordRunes := []rune(word)
	end := start + len(wordRunes)
	if start < 0 || end > len(runes) || string(runes[start:end]) != word {
		return false
	}
	isWord := func(value rune) bool {
		return unicode.IsLetter(value) || unicode.IsDigit(value) || value == '_'
	}
	return (start == 0 || !isWord(runes[start-1])) && (end == len(runes) || !isWord(runes[end]))
}

// databaseComputedRollupDependencies are the rollups a computed property
// reads on its own row, in their source's order: the property itself, or
// those its formula names, however deep. A related source's are read as the
// query's own are (2026-11-12: a related formula reading a rollup of a third
// database, read as 0 while only the query's own rollups were listed).
func databaseComputedRollupDependencies(
	propertyID string,
	context databaseQueryContext,
) ([]storedDatabaseQueryProperty, error) {
	wanted := map[string]bool{}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(currentID string) error {
		if visited[currentID] || visiting[currentID] {
			return nil
		}
		property, ok := context.PropertiesByID[currentID]
		if !ok {
			return nil
		}
		visiting[currentID] = true
		defer delete(visiting, currentID)
		switch property.Type {
		case graphValueTypeRollup:
			wanted[currentID] = true
		case graphValueTypeFormula:
			config, err := decodeDatabaseComputedConfig(property.Config)
			if err != nil {
				return err
			}
			expression := config.Formula
			if expression == "" {
				expression = config.Expression
			}
			for _, name := range databaseFormulaPropertyReferences(expression) {
				reference, ok := context.propertyNamed(property.DataSourceID, name)
				if ok && (reference.Type == graphValueTypeFormula || reference.Type == graphValueTypeRollup) {
					if err := visit(reference.ID); err != nil {
						return err
					}
				}
			}
		}
		visited[currentID] = true
		return nil
	}
	if err := visit(propertyID); err != nil {
		return nil, err
	}
	result := make([]storedDatabaseQueryProperty, 0, len(wanted))
	for id := range wanted {
		result = append(result, context.PropertiesByID[id])
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Position != result[right].Position {
			return result[left].Position < result[right].Position
		}
		return result[left].ID < result[right].ID
	})
	return result, nil
}

func computeDatabaseRowPropertyWithSeed(
	row storedDatabaseQueryRow,
	context databaseQueryContext,
	seed map[string]databaseComputedValue,
	propertyID string,
) (databaseComputedValue, error) {
	property, ok := context.PropertiesByID[propertyID]
	if !ok {
		return databaseComputedValue{}, errors.New(errorCodeDatabaseQueryInvalid)
	}
	computed := map[string]map[string]databaseComputedValue{row.ID: {}}
	for id, value := range seed {
		computed[row.ID][id] = value
	}
	row = context.rowAsRead(row)
	resolver := &databaseComputedResolver{
		context:  context,
		rows:     map[string]storedDatabaseQueryRow{row.ID: row},
		read:     map[string]bool{row.ID: true},
		computed: computed,
		visiting: map[string]bool{}, cyclic: map[string]bool{},
	}
	if _, err := resolver.evaluate(row, property); err != nil {
		return databaseComputedValue{}, err
	}
	value, ok := resolver.computed[row.ID][propertyID]
	if !ok {
		return databaseComputedValue{}, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	return value, nil
}

func advanceDatabaseQueryComputedTarget(
	app core.App,
	job storedDatabaseQueryJob,
	context databaseQueryContext,
	state *databaseQueryComputedTargetState,
	remaining *int,
) (bool, any, error) {
	if state == nil || len(state.Frames) == 0 || *remaining < 0 {
		return false, nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	loadedRows := map[string]storedDatabaseQueryRow{}
	for *remaining > 0 && len(state.Frames) > 0 {
		frameIndex := len(state.Frames) - 1
		frame := &state.Frames[frameIndex]
		row, ok := loadedRows[frame.RowID]
		if !ok {
			rows, err := loadDatabaseQueryComputedRowsByID(
				app, job.ContentOwnerID, context.readablePages(job.PageID), context.visibleRowIDs([]string{frame.RowID}),
			)
			if err != nil {
				return false, nil, err
			}
			row, ok = rows[frame.RowID]
			if !ok {
				return false, nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
			}
			loadedRows[frame.RowID] = row
		}
		rollups, err := databaseComputedRollupDependencies(frame.PropertyID, context)
		if err != nil {
			return false, nil, err
		}
		if frame.RollupIndex >= len(rollups) {
			computed, err := computeDatabaseRowPropertyWithSeed(row, context, frame.Seed, frame.PropertyID)
			if err != nil {
				return false, nil, err
			}
			value := databaseQueryRawValue(computed.Value)
			state.Frames = state.Frames[:frameIndex]
			*remaining--
			if len(state.Frames) == 0 {
				return true, value, nil
			}
			parent := &state.Frames[len(state.Frames)-1]
			if parent.WaitingTargetRowID != frame.RowID {
				return false, nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
			}
			parentRollups, err := databaseComputedRollupDependencies(parent.PropertyID, context)
			if err != nil || parent.RollupIndex >= len(parentRollups) {
				return false, nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
			}
			scope := databaseQueryComputedTargetScope(job, parent.RowID, parentRollups[parent.RollupIndex].ID)
			item := storedDatabaseQueryDirectDependency{
				RowID:       databaseQueryDependencySyntheticID(job.ID, "computed-item", fmt.Sprintf("%s:%s:%d", parent.RowID, parentRollups[parent.RollupIndex].ID, parent.WaitingOrdinal)),
				TargetRowID: parent.WaitingTargetRowID,
				Ordinal:     parent.WaitingOrdinal,
			}
			if err := accumulateDatabaseQueryRollup(app, job, scope, item, value, &parent.Accumulator); err != nil {
				return false, nil, err
			}
			parent.RelationCursor = parent.WaitingOrdinal + 1
			parent.WaitingTargetRowID = ""
			parent.WaitingOrdinal = 0
			continue
		}

		rollup := rollups[frame.RollupIndex]
		config, err := decodeDatabaseComputedConfig(rollup.Config)
		if err != nil {
			return false, nil, err
		}
		relationID, targetID, function, setUp := context.rollupSetUp(config)
		if !setUp {
			// It reads as empty however its row is computed.
			frame.RollupIndex++
			frame.RelationCursor = 0
			frame.Accumulator = databaseQueryRollupAccumulator{}
			*remaining--
			continue
		}
		if frame.Accumulator.Function == "" {
			frame.Accumulator.Function = function
			frame.Accumulator.GroupOptions = databaseRollupStatusGroupOptions(context.PropertiesByID[targetID], config.RollupStatusGroup)
		}
		if frame.Accumulator.Function != function || frame.WaitingTargetRowID != "" {
			return false, nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
		}
		unavailable := context.UnavailableRelations[relationID]
		targetProperty := context.PropertiesByID[targetID]
		if unavailable {
			if err := finishDatabaseQueryComputedTargetRollup(frame, rollup); err != nil {
				return false, nil, err
			}
			*remaining--
			continue
		}
		childRollups := []storedDatabaseQueryProperty{}
		if function != "count" &&
			(targetProperty.Type == graphValueTypeFormula || targetProperty.Type == graphValueTypeRollup) {
			childRollups, err = databaseComputedRollupDependencies(targetID, context)
			if err != nil {
				return false, nil, err
			}
		}
		scope := databaseQueryComputedTargetScope(job, frame.RowID, rollup.ID)
		if frame.Accumulator.ReducingMedian {
			complete, err := reduceDatabaseQueryMedian(app, job, scope, &frame.Accumulator)
			if err != nil {
				return false, nil, err
			}
			*remaining = 0
			if complete {
				if err := finishDatabaseQueryComputedTargetRollup(frame, rollup); err != nil {
					return false, nil, err
				}
			}
			continue
		}
		itemLimit := *remaining
		if len(childRollups) > 0 {
			itemLimit = 1
		}
		items, err := loadDatabaseQueryRelationItemWindow(
			app, job.ContentOwnerID, context.readablePages(job.PageID), frame.RowID, relationID,
			frame.RelationCursor, itemLimit,
		)
		if err != nil {
			return false, nil, err
		}
		if len(items) == 0 {
			if function == "median" && frame.Accumulator.NumberCount > 0 {
				frame.Accumulator.ReducingMedian = true
				continue
			}
			if err := finishDatabaseQueryComputedTargetRollup(frame, rollup); err != nil {
				return false, nil, err
			}
			*remaining--
			continue
		}
		windowIDs := make([]string, len(items))
		for index := range items {
			windowIDs[index] = items[index].TargetID
		}
		rows, err := loadDatabaseQueryComputedRowsByID(app, job.ContentOwnerID, context.readablePages(job.PageID), context.visibleRowIDs(windowIDs))
		if err != nil {
			return false, nil, err
		}
		for _, relation := range items {
			targetRowID := relation.TargetID
			ordinal := relation.Occurrence
			if context.hidden(targetRowID) {
				frame.RelationCursor = ordinal + 1
				*remaining--
				continue
			}
			frame.Accumulator.InputCount++
			target, visible := rows[targetRowID]
			if !visible {
				frame.RelationCursor = ordinal + 1
				*remaining--
				continue
			}
			frame.Accumulator.VisibleCount++
			if function == "count" {
				frame.RelationCursor = ordinal + 1
				*remaining--
				continue
			}
			target = context.rowAsRead(target)
			value := databaseRollupTargetValue(targetProperty, target)
			var leaf any
			if targetProperty.Type == graphValueTypeRelation {
				if leaf, value, err = loadDatabaseQueryRollupRelation(app, job.ContentOwnerID, context, targetRowID, targetID); err != nil {
					return false, nil, err
				}
			}
			if targetProperty.Type == graphValueTypeFormula || targetProperty.Type == graphValueTypeRollup {
				if len(childRollups) > 0 {
					if databaseQueryComputedTargetCycle(state.Frames, targetRowID, targetID) {
						value = ""
					} else {
						frame.WaitingTargetRowID = targetRowID
						frame.WaitingOrdinal = ordinal
						state.Frames = append(state.Frames, databaseQueryComputedTargetFrame{
							RowID: targetRowID, PropertyID: targetID,
							Seed: map[string]databaseComputedValue{},
						})
						*remaining--
						break
					}
				} else {
					computed, err := computeDatabaseRowPropertyWithSeed(target, context, nil, targetID)
					if err != nil {
						return false, nil, err
					}
					value = databaseQueryRawValue(computed.Value)
				}
			}
			if targetProperty.Type != graphValueTypeRelation {
				leaf = value
			}
			if databaseRollupShowsValues(function) {
				value = databaseRollupShownValue(targetProperty, value, context.People)
			}
			item := storedDatabaseQueryDirectDependency{
				RowID:       databaseQueryDependencySyntheticID(job.ID, "computed-item", fmt.Sprintf("%s:%s:%d", frame.RowID, rollup.ID, ordinal)),
				TargetRowID: targetRowID,
				Ordinal:     ordinal,
			}
			if err := accumulateDatabaseQueryRollupLeaf(app, job, scope, item, value, leaf, &frame.Accumulator); err != nil {
				return false, nil, err
			}
			frame.RelationCursor = ordinal + 1
			*remaining--
		}
	}
	return false, nil, nil
}

func databaseQueryComputedTargetScope(job storedDatabaseQueryJob, rowID, propertyID string) string {
	return databaseQueryDependencySyntheticID(job.ID, "computed-scope", rowID+"\x00"+propertyID)
}

func databaseQueryComputedTargetCycle(
	frames []databaseQueryComputedTargetFrame,
	rowID, propertyID string,
) bool {
	for _, frame := range frames {
		if frame.RowID == rowID && frame.PropertyID == propertyID {
			return true
		}
	}
	return false
}

func finishDatabaseQueryComputedTargetRollup(
	frame *databaseQueryComputedTargetFrame,
	property storedDatabaseQueryProperty,
) error {
	value, err := finalizeDatabaseQueryRollup(frame.Accumulator)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if frame.Seed == nil {
		frame.Seed = map[string]databaseComputedValue{}
	}
	frame.Seed[property.ID] = databaseComputedValue{
		Type: property.Type, Value: encoded, Text: databaseRollupText(frame.Accumulator.Function, value),
		Leaves: databaseRollupEncodedLeaves(frame.Accumulator.Function, frame.Accumulator.Leaves),
	}
	frame.RollupIndex++
	frame.RelationCursor = 0
	frame.Accumulator = databaseQueryRollupAccumulator{}
	return nil
}
