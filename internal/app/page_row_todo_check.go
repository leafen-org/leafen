package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A to-do moved into a full-page database keeps its check, as Notion's does
// (seen 2026-11-11): the row's value in the source's checkbox property named
// "체크 표시됨" ("Checked") in the mover's language, which the first to-do
// moved in makes, last among the source's properties, and later ones find by
// its name — renamed, the next to-do makes another.

// keepToDoCheckInRow is the change that puts the page's row in its source:
// the row, holding the to-do's check when the request carries one, after the
// property the check needs when the source has none.
func keepToDoCheckInRow(
	ctx context.Context, app core.App, request pageToDatabaseRowRequest, row graphOperation,
) ([]graphOperation, error) {
	if request.Checked == nil || row.Row == nil {
		return []graphOperation{row}, nil
	}
	name := generatedNamesFor(ctx, app, request.PrincipalID).Checked
	var propertyID string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:sourceId} AND [[page_id]] = {:pageId} AND [[type]] = {:type} AND [[name]] = {:name}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]] LIMIT 1
	`).Bind(dbx.Params{
		"sourceId": request.DataSourceID, "pageId": request.ContainerPageID, "type": graphValueTypeCheckbox, "name": name,
	}).Row(&propertyID)
	operations := []graphOperation{}
	if errors.Is(err, sql.ErrNoRows) {
		property, made, err := toDoCheckProperty(app, request, name)
		if err != nil {
			return nil, err
		}
		propertyID = property
		operations = append(operations, made)
	} else if err != nil {
		return nil, err
	}
	value, err := json.Marshal(*request.Checked)
	if err != nil {
		return nil, err
	}
	values := map[string]graphRowValue{}
	for id, kept := range row.Row.Values {
		values[id] = kept
	}
	values[propertyID] = graphRowValue{Type: graphValueTypeCheckbox, Value: value}
	withCheck := *row.Row
	withCheck.Values = values
	return append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &withCheck}), nil
}

// toDoCheckProperty makes the checkbox a to-do's check goes in, after the
// source's last property, with the ID the request names or one of the move's.
func toDoCheckProperty(app core.App, request pageToDatabaseRowRequest, name string) (string, graphOperation, error) {
	id := request.CheckedPropertyID
	if id == "" {
		id = "checked-" + childPageBlockDigest(request.MutationID, request.DataSourceID)
	}
	id, err := normalizeGraphID(id)
	if err != nil {
		return "", graphOperation{}, err
	}
	var tail string
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[position]]), '') FROM {{leafen_database_properties}} WHERE [[data_source_id]] = {:sourceId}
	`).Bind(dbx.Params{"sourceId": request.DataSourceID}).Row(&tail); err != nil {
		return "", graphOperation{}, err
	}
	position, err := databasePositionAfter(tail)
	if err != nil {
		return "", graphOperation{}, err
	}
	return id, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
		ID: id, DataSourceID: request.DataSourceID, Type: graphValueTypeCheckbox, Name: name,
		Position: position, Config: json.RawMessage(`{}`),
	}}, nil
}
