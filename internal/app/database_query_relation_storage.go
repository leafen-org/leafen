package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type databaseQueryRelationItem struct {
	PropertyIndex int    `db:"-"`
	PropertyID    string `db:"-"`
	TargetID      string `db:"target_row_id"`
	Occurrence    int    `db:"ordinal"`
}

type databaseQueryRelationShape struct {
	RowID          string `db:"row_id"`
	NestedRelation int    `db:"nested_relation"`
	ExceedsBound   int    `db:"exceeds_bound"`
}

// loadDatabaseQueryRelationShapes reads, per row, whether it relates through
// one of nestedRelationIDs and whether it has more related rows than a turn
// reads.
func loadDatabaseQueryRelationShapes(
	app core.App,
	owner, pageID string,
	rowIDs, nestedRelationIDs []string,
) (map[string]databaseQueryRelationShape, error) {
	result := make(map[string]databaseQueryRelationShape, len(rowIDs))
	if len(rowIDs) == 0 {
		return result, nil
	}
	nested, err := json.Marshal(nestedRelationIDs)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{
		"owner": owner, "pageId": pageID,
		"turnBound": databaseQueryDependencyRowsPerTurn, "nestedRelations": string(nested),
	}
	placeholders := make([]string, len(rowIDs))
	for index, rowID := range rowIDs {
		key := fmt.Sprintf("row%d", index)
		params[key] = rowID
		placeholders[index] = "{:" + key + "}"
	}
	shapes := []databaseQueryRelationShape{}
	if err := app.DB().NewQuery(`
		SELECT source.[[id]] AS [[row_id]],
			EXISTS (
				SELECT 1 FROM {{leafen_database_relation_items}} AS relation
				WHERE relation.[[owner]] = {:owner} AND relation.[[page_id]] = {:pageId}
					AND relation.[[row_id]] = source.[[id]]
					AND relation.[[property_id]] IN (SELECT [[value]] FROM json_each({:nestedRelations}))
				LIMIT 1
			) AS [[nested_relation]],
			EXISTS (
				SELECT 1 FROM {{leafen_database_relation_items}} AS relation
				WHERE relation.[[owner]] = {:owner} AND relation.[[page_id]] = {:pageId}
					AND relation.[[row_id]] = source.[[id]]
				LIMIT 1 OFFSET {:turnBound}
			) AS [[exceeds_bound]]
		FROM {{leafen_database_rows}} AS source
		WHERE source.[[owner]] = {:owner} AND source.[[page_id]] = {:pageId}
			AND source.[[id]] IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY source.[[id]]
	`).Bind(params).All(&shapes); err != nil {
		return nil, err
	}
	for _, shape := range shapes {
		result[shape.RowID] = shape
	}
	return result, nil
}

func databaseQueryRelationWindow(
	app core.App,
	owner, pageID, rowID string,
	context databaseQueryContext,
	propertyCursor, itemCursor, limit int,
) ([]databaseQueryRelationItem, int, int, bool, error) {
	if propertyCursor < 0 || itemCursor < 0 || limit < 1 {
		return nil, 0, 0, false, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	items := make([]databaseQueryRelationItem, 0, limit)
	for propertyCursor < len(context.Properties) {
		property := context.Properties[propertyCursor]
		if property.Type != graphValueTypeRelation {
			propertyCursor++
			itemCursor = 0
			continue
		}
		windowLimit := limit - len(items)
		window, err := loadDatabaseQueryRelationItemWindow(
			app, owner, []string{pageID}, rowID, property.ID, itemCursor, windowLimit,
		)
		if err != nil {
			return nil, 0, 0, false, err
		}
		for index := range window {
			window[index].PropertyIndex = propertyCursor
			window[index].PropertyID = property.ID
		}
		items = append(items, window...)
		if len(window) == windowLimit {
			last := window[len(window)-1]
			return items, propertyCursor, last.Occurrence + 1, false, nil
		}
		propertyCursor++
		itemCursor = 0
		if len(items) == limit {
			return items, propertyCursor, itemCursor, propertyCursor == len(context.Properties), nil
		}
	}
	return items, propertyCursor, itemCursor, true, nil
}

func loadDatabaseQueryRelationItemWindow(
	app core.App,
	owner string, pages []string, rowID, propertyID string,
	cursor, limit int,
) ([]databaseQueryRelationItem, error) {
	if cursor < 0 || limit < 1 {
		return nil, errors.New(errorCodeDatabaseQueryCursorInvalid)
	}
	window := []databaseQueryRelationItem{}
	err := app.DB().NewQuery(`
		SELECT [[ordinal]], [[target_row_id]]
		FROM {{leafen_database_relation_items}}
		WHERE [[owner]] = {:owner} AND [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
			AND [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
			AND [[ordinal]] >= {:cursor}
			AND ` + databaseRowNotTrashedSQL("[[target_row_id]]") + `
		ORDER BY [[ordinal]], [[target_row_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": owner, "pages": databaseQueryPagesJSON(pages), "rowId": rowID,
		"propertyId": propertyID, "cursor": cursor, "limit": limit,
	}).All(&window)
	return window, err
}
