package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A date command keeps its actual direct and derived changes in its existing
// mutation receipt. Only its opaque ID crosses the wire, including for someone
// who can edit one row without seeing the other rows it schedules. It takes
// back the dependency links the same change drew with the dates, so a step
// back from a link that moved dates removes the link too, as Notion's.
type databaseDateCommand struct {
	ActorID string                     `json:"actorId"`
	RowID   string                     `json:"rowId,omitempty"`
	Changes []databaseDateCommandValue `json:"changes"`
}

type databaseDateCommandValue struct {
	RowID      string `json:"rowId"`
	PropertyID string `json:"propertyId"`
	// Type is "relation" for a Blocked by, empty for a date.
	Type   string          `json:"type,omitempty"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

func (value databaseDateCommandValue) valueType() string {
	if value.Type == graphValueTypeRelation {
		return graphValueTypeRelation
	}
	return graphValueTypeDate
}

func sameDatabaseCommandDate(left, right json.RawMessage) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}

func captureDatabaseDateCommand(
	app core.App, pageID string, capture *pageHistoryRowCapture, actorID, rowID string, operations []graphOperation,
) (*databaseDateCommand, error) {
	if capture == nil || actorID == "" {
		return nil, nil
	}
	command := &databaseDateCommand{ActorID: actorID, RowID: rowID, Changes: capturedDatabaseCommandValues(capture, operations, nil)}
	if len(command.Changes) == 0 {
		return nil, nil
	}
	links, err := databaseDependencyLinkProperties(app, pageID, operations)
	if err != nil {
		return nil, err
	}
	if len(links) > 0 {
		command.Changes = append(command.Changes, capturedDatabaseCommandValues(capture, operations, links)...)
	}
	return command, nil
}

// capturedDatabaseCommandValues lists the date values the operations change,
// or with links the values of those Blocked by properties.
func capturedDatabaseCommandValues(
	capture *pageHistoryRowCapture, operations []graphOperation, links map[string]bool,
) []databaseDateCommandValue {
	kind, valueType := graphValueTypeDate, ""
	if links != nil {
		kind, valueType = graphValueTypeRelation, graphValueTypeRelation
	}
	changes := []databaseDateCommandValue{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		before, existed := capture.before[operation.Row.ID]
		if !existed { // Creating a row is a different command, not a date move.
			continue
		}
		ids := []string{}
		for id, value := range operation.Row.Values {
			if value.Type == kind && (links == nil || links[id]) {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			old := json.RawMessage("null")
			if value, found := before[id]; found {
				if value.Type != kind {
					continue
				}
				old = json.RawMessage(value.Value)
			}
			next := operation.Row.Values[id].Value
			if !sameDatabaseCommandDate(old, next) {
				changes = append(changes, databaseDateCommandValue{
					RowID: operation.Row.ID, PropertyID: id, Type: valueType, Before: old, After: next,
				})
			}
		}
	}
	return changes
}

// Restore against the current values under the graph transaction's lock.
// A later edit, deletion, move to another page or property conversion wins.
// Restoring a command never schedules a second round of dependent moves.
func restoreDatabaseDateCommand(
	app core.App, ownerID, pageID, actorID, rowID string, operations []graphOperation,
) ([]graphOperation, bool, error) {
	var restore *graphDateCommandRestore
	for _, operation := range operations {
		if operation.Kind == graphOperationKindRestoreDateCommand {
			restore = operation.DateRestore
			if restore == nil || len(operations) != 1 || graphOperationPayloadCount(operation) != 1 || strings.TrimSpace(restore.MutationID) == "" {
				return nil, true, graphInputError(errorCodeGraphInvalid, "Check the content change details.")
			}
		}
	}
	if restore == nil {
		return operations, false, nil
	}
	raw := ""
	err := app.DB().NewQuery(`SELECT [[counts_json]] FROM {{leafen_graph_mutations}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:page} AND [[mutation_id]]={:mutation}`).
		Bind(dbx.Params{"owner": ownerID, "page": pageID, "mutation": restore.MutationID}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, true, graphNotFoundError()
	}
	if err != nil {
		return nil, true, err
	}
	receipt := storedGraphMutationReceipt{}
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		return nil, true, err
	}
	command := receipt.DateUndo
	if command == nil || command.ActorID != actorID || rowID != "" && command.RowID != rowID {
		return nil, true, graphNotFoundError()
	}
	params := dbx.Params{"page": pageID, "owner": ownerID}
	rowIDs, propertyIDs := map[string]bool{}, map[string]bool{}
	for _, value := range command.Changes {
		rowIDs[value.RowID], propertyIDs[value.PropertyID] = true, true
	}
	encodeIDs := func(ids map[string]bool) string {
		keys := make([]string, 0, len(ids))
		for id := range ids {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		encoded, _ := json.Marshal(keys)
		return string(encoded)
	}
	params["rows"], params["properties"] = encodeIDs(rowIDs), encodeIDs(propertyIDs)
	// Internal effects can outgrow one request's row count. ID sets are one
	// bound JSON value each, independent of SQLite's variable-count ceiling.
	rowsIn, propertiesIn := "SELECT value FROM json_each({:rows})", "SELECT value FROM json_each({:properties})"
	rows := []struct {
		ID       string `db:"id"`
		Source   string `db:"source"`
		Position string `db:"position"`
		Parent   string `db:"parent"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[data_source_id]] AS [[source]],
		[[position]], COALESCE([[subitem_parent_id]], '') AS [[parent]]
		FROM {{leafen_database_rows}} AS r WHERE [[owner]]={:owner} AND [[page_id]]={:page}
		AND [[id]] IN (` + rowsIn + `) AND ` + databaseRowNotTrashedSQL("r.id")).Bind(params).All(&rows); err != nil {
		return nil, true, err
	}
	properties := []struct {
		ID     string `db:"id"`
		Source string `db:"data_source_id"`
		Type   string `db:"type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[data_source_id]], [[type]] FROM {{leafen_database_properties}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:page} AND [[type]] IN ('date', 'relation')
		AND [[id]] IN (` + propertiesIn + `)`).Bind(params).All(&properties); err != nil {
		return nil, true, err
	}
	propertySources, propertyTypes := map[string]string{}, map[string]string{}
	for _, property := range properties {
		propertySources[property.ID], propertyTypes[property.ID] = property.Source, property.Type
	}
	current := []struct {
		RowID      string `db:"row_id"`
		PropertyID string `db:"property_id"`
		Value      string `db:"value"`
	}{}
	if err := app.DB().NewQuery(`SELECT v.[[row_id]], v.[[property_id]], v.[[value]]
		FROM {{leafen_database_row_values}} v JOIN {{leafen_database_properties}} p ON p.[[id]]=v.[[property_id]]
		WHERE v.[[owner]]={:owner} AND v.[[page_id]]={:page} AND v.[[type]]=p.[[type]] AND p.[[type]] IN ('date', 'relation')
		AND v.[[row_id]] IN (` + rowsIn + `) AND v.[[property_id]] IN (` + propertiesIn + `)`).Bind(params).All(&current); err != nil {
		return nil, true, err
	}
	values := map[string]json.RawMessage{}
	for _, cell := range current {
		values[cell.RowID+"/"+cell.PropertyID] = json.RawMessage(cell.Value)
	}
	byID := map[string]*graphDatabaseRow{}
	for _, row := range rows {
		byID[row.ID] = &graphDatabaseRow{ID: row.ID, DataSourceID: row.Source, Position: row.Position,
			SubitemParentID: optionalDatabaseRowParent(row.Parent), Values: map[string]graphRowValue{}}
	}
	for _, change := range command.Changes {
		row := byID[change.RowID]
		if row == nil || propertySources[change.PropertyID] != row.DataSourceID || propertyTypes[change.PropertyID] != change.valueType() {
			continue
		}
		expected, target := change.After, change.Before
		if restore.Forward {
			expected, target = change.Before, change.After
		}
		value := values[change.RowID+"/"+change.PropertyID]
		if value == nil {
			value = json.RawMessage("null")
		}
		if sameDatabaseCommandDate(value, expected) {
			row.Values[change.PropertyID] = graphRowValue{Type: change.valueType(), Value: target}
		}
	}
	result := []graphOperation{}
	for _, row := range rows {
		if restored := byID[row.ID]; len(restored.Values) > 0 {
			result = append(result, graphOperation{Kind: graphOperationKindUpsertRow, Row: restored})
		}
	}
	return result, true, nil
}
