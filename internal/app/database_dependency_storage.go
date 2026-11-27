package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedDatabaseDependencyBinding struct {
	DataSourceID          string `db:"data_source_id"`
	Owner                 string `db:"owner"`
	PageID                string `db:"page_id"`
	PredecessorPropertyID string `db:"predecessor_property_id"`
	SuccessorPropertyID   string `db:"successor_property_id"`
	DataKey               string `db:"data_key"`
	Revision              int    `db:"revision"`
}

type storedDatabaseDependencyEdge struct {
	ID               string `db:"id"`
	PredecessorRowID string `db:"predecessor_row_id"`
	SuccessorRowID   string `db:"successor_row_id"`
	RelatedRowID     string `db:"related_row_id"`
}

func loadDatabaseDependencyBinding(
	app core.App,
	owner, pageID, dataSourceID string,
) (storedDatabaseDependencyBinding, error) {
	binding := storedDatabaseDependencyBinding{}
	err := app.DB().NewQuery(`
		SELECT [[binding]].[[data_source_id]], [[binding]].[[owner]], [[binding]].[[page_id]],
			[[binding]].[[predecessor_property_id]], [[binding]].[[successor_property_id]],
			[[binding]].[[data_key]], [[binding]].[[revision]]
		FROM {{leafen_database_dependency_bindings}} AS [[binding]]
		WHERE [[binding]].[[data_source_id]] = {:dataSourceId}
			AND [[binding]].[[owner]] = {:owner} AND [[binding]].[[page_id]] = {:pageId}
			AND ` + databaseSourceNotTrashedSQL("[[binding]].[[data_source_id]]") + `
	`).Bind(dbx.Params{
		"dataSourceId": dataSourceID, "owner": owner, "pageId": pageID,
	}).One(&binding)
	if errors.Is(err, sql.ErrNoRows) {
		return binding, dependencyNotFoundError()
	}
	return binding, err
}

func confirmDatabaseDependencyBinding(
	app core.App,
	expected storedDatabaseDependencyBinding,
) error {
	actual, err := loadDatabaseDependencyBinding(
		app, expected.Owner, expected.PageID, expected.DataSourceID,
	)
	if err != nil {
		return dependencyStaleError()
	}
	if actual.Revision != expected.Revision || actual.DataKey != expected.DataKey ||
		actual.PredecessorPropertyID != expected.PredecessorPropertyID ||
		actual.SuccessorPropertyID != expected.SuccessorPropertyID {
		return dependencyStaleError()
	}
	return nil
}

func requireDatabaseDependencyRows(
	app core.App,
	owner, pageID, dataSourceID string,
	rowIDs []string,
) error {
	params := dbx.Params{
		"owner": owner, "pageId": pageID, "dataSourceId": dataSourceID,
	}
	placeholders := make([]string, len(rowIDs))
	for index, rowID := range rowIDs {
		key := fmt.Sprintf("row%d", index)
		params[key] = rowID
		placeholders[index] = "{:" + key + "}"
	}
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_rows}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[data_source_id]] = {:dataSourceId}
			AND [[id]] IN (` + strings.Join(placeholders, ",") + `)
			AND ` + databaseRowNotTrashedSQL("[[id]]") + `
	`).Bind(params).Row(&count)
	if err != nil {
		return err
	}
	if count != len(rowIDs) {
		return dependencyNotFoundError()
	}
	return nil
}

func loadDatabaseDependencyEdgeWindow(
	app core.App,
	owner, pageID string,
	binding storedDatabaseDependencyBinding,
	input databaseDependencyEdgeRequest,
	cursor databaseDependencyCursor,
) ([]storedDatabaseDependencyEdge, bool, error) {
	rowColumn := "[[predecessor_row_id]]"
	relatedColumn := "[[successor_row_id]]"
	if input.Direction == databaseDependencyDirectionPredecessors {
		rowColumn, relatedColumn = "[[successor_row_id]]", "[[predecessor_row_id]]"
	}
	rows := []storedDatabaseDependencyEdge{}
	query := `
		SELECT [[edges]].[[id]], [[edges]].[[predecessor_row_id]], [[edges]].[[successor_row_id]],
			[[edges]].` + relatedColumn + ` AS [[related_row_id]]
		FROM {{leafen_database_dependency_edges}} AS [[edges]]
		JOIN {{leafen_database_rows}} AS [[related]]
			ON [[related]].[[id]] = [[edges]].` + relatedColumn + `
			AND [[related]].[[owner]] = [[edges]].[[owner]]
			AND [[related]].[[page_id]] = [[edges]].[[page_id]]
			AND [[related]].[[data_source_id]] = [[edges]].[[data_source_id]]
		WHERE [[edges]].[[owner]] = {:owner} AND [[edges]].[[page_id]] = {:pageId}
			AND [[edges]].[[data_source_id]] = {:dataSourceId} AND [[edges]].[[data_key]] = {:dataKey}
			AND [[edges]].` + rowColumn + ` = {:rowId}
			AND ` + databaseRowNotTrashedSQL("[[related]].[[id]]") + `
			AND ([[edges]].` + relatedColumn + ` > {:relatedRowId}
				OR ([[edges]].` + relatedColumn + ` = {:relatedRowId} AND [[edges]].[[id]] > {:edgeId}))
		ORDER BY [[edges]].` + relatedColumn + `, [[edges]].[[id]]
		LIMIT {:limit}`
	err := app.DB().NewQuery(query).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "dataSourceId": binding.DataSourceID,
		"dataKey": binding.DataKey, "rowId": input.RowID,
		"relatedRowId": cursor.RelatedRowID, "edgeId": cursor.EdgeID,
		"limit": input.Limit + 1,
	}).All(&rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > input.Limit
	if hasMore {
		rows = rows[:input.Limit]
	}
	return rows, hasMore, nil
}

func loadVisibleDatabaseDependencyGraph(
	app core.App,
	owner, pageID string,
	binding storedDatabaseDependencyBinding,
	visibleRowIDs []string,
	limit int,
	cursor databaseDependencyCursor,
) ([]storedDatabaseDependencyEdge, bool, error) {
	params := dbx.Params{
		"owner": owner, "pageId": pageID, "dataSourceId": binding.DataSourceID,
		"dataKey": binding.DataKey, "successorRowId": cursor.SuccessorRowID,
		"predecessorRowId": cursor.PredecessorRowID, "edgeId": cursor.EdgeID,
		"limit": limit + 1,
	}
	placeholders := make([]string, len(visibleRowIDs))
	for index, rowID := range visibleRowIDs {
		key := fmt.Sprintf("visible%d", index)
		params[key] = rowID
		placeholders[index] = "{:" + key + "}"
	}
	visibleSet := strings.Join(placeholders, ",")
	rows := []storedDatabaseDependencyEdge{}
	err := app.DB().NewQuery(`
		SELECT [[edges]].[[id]], [[edges]].[[predecessor_row_id]], [[edges]].[[successor_row_id]],
			'' AS [[related_row_id]]
		FROM {{leafen_database_dependency_edges}} AS [[edges]]
		JOIN {{leafen_database_rows}} AS [[predecessors]]
			ON [[predecessors]].[[id]] = [[edges]].[[predecessor_row_id]]
			AND [[predecessors]].[[owner]] = [[edges]].[[owner]]
			AND [[predecessors]].[[page_id]] = [[edges]].[[page_id]]
			AND [[predecessors]].[[data_source_id]] = [[edges]].[[data_source_id]]
		JOIN {{leafen_database_rows}} AS [[successors]]
			ON [[successors]].[[id]] = [[edges]].[[successor_row_id]]
			AND [[successors]].[[owner]] = [[edges]].[[owner]]
			AND [[successors]].[[page_id]] = [[edges]].[[page_id]]
			AND [[successors]].[[data_source_id]] = [[edges]].[[data_source_id]]
		WHERE [[edges]].[[owner]] = {:owner} AND [[edges]].[[page_id]] = {:pageId}
			AND [[edges]].[[data_source_id]] = {:dataSourceId} AND [[edges]].[[data_key]] = {:dataKey}
			AND [[edges]].[[predecessor_row_id]] IN (` + visibleSet + `)
			AND [[edges]].[[successor_row_id]] IN (` + visibleSet + `)
			AND ([[edges]].[[successor_row_id]] > {:successorRowId}
				OR ([[edges]].[[successor_row_id]] = {:successorRowId}
					AND [[edges]].[[predecessor_row_id]] > {:predecessorRowId})
				OR ([[edges]].[[successor_row_id]] = {:successorRowId}
					AND [[edges]].[[predecessor_row_id]] = {:predecessorRowId}
					AND [[edges]].[[id]] > {:edgeId}))
		ORDER BY [[edges]].[[successor_row_id]], [[edges]].[[predecessor_row_id]], [[edges]].[[id]]
		LIMIT {:limit}
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return rows, hasMore, nil
}
