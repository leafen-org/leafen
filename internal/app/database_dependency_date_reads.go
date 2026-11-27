package app

import (
	"encoding/json"
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// These are read windows, never a limit on the number of affected tasks.
const databaseDependencyDateReadWindow = 128

type databaseDependencyDateChange struct {
	ID            string `json:"id"`
	StartChanged  bool   `json:"startChanged"`
	EndChanged    bool   `json:"endChanged"`
	before, after databaseTaskSpan
}

type databaseDependencyDateRow struct {
	ID          string `db:"row_id"`
	Position    string `db:"position"`
	Parent      string `db:"parent"`
	StartType   string `db:"start_type"`
	StartValue  string `db:"start_value"`
	EndType     string `db:"end_type"`
	EndValue    string `db:"end_value"`
	Sequence    int    `db:"sequence"`
	Predecessor bool   `db:"predecessor"`
}

func (row databaseDependencyDateRow) value(settings databaseDependencyShiftSettings, propertyID string) (any, bool) {
	raw, kind := row.StartValue, row.StartType
	if settings.separate() && propertyID == settings.EndDatePropertyID {
		raw, kind = row.EndValue, row.EndType
	}
	if kind != graphValueTypeDate || raw == "" {
		return nil, false
	}
	value, err := decodeBoundedGraphJSON(json.RawMessage(raw))
	return value, err == nil && value != nil
}

func databaseDependencyDateReadParams(owner, pageID, sourceID string, settings databaseDependencyShiftSettings) dbx.Params {
	start, end := settings.DatePropertyID, ""
	if settings.separate() {
		start, end = settings.StartDatePropertyID, settings.EndDatePropertyID
	}
	return dbx.Params{"owner": owner, "page": pageID, "source": sourceID, "start": start, "end": end}
}

const databaseDependencyDateRowColumns = `r.[[id]] AS [[row_id]], r.[[position]],
	COALESCE(r.[[subitem_parent_id]], '') AS [[parent]],
	COALESCE(s.[[type]], '') AS [[start_type]], COALESCE(s.[[value]], '') AS [[start_value]],
	COALESCE(t.[[type]], '') AS [[end_type]], COALESCE(t.[[value]], '') AS [[end_value]]`

const databaseDependencyDateRowJoins = `
	LEFT JOIN {{leafen_database_row_values}} s ON s.[[row_id]]=r.[[id]]
		AND s.[[owner]]={:owner} AND s.[[page_id]]={:page} AND s.[[property_id]]={:start}
	LEFT JOIN {{leafen_database_row_values}} t ON t.[[row_id]]=r.[[id]]
		AND t.[[owner]]={:owner} AND t.[[page_id]]={:page} AND t.[[property_id]]={:end}`

// The directly edited rows fit the existing request bound. The join reads
// their two endpoints together, without one read per row or property.
func loadDatabaseDependencyDateRows(app core.App, owner, pageID, sourceID string,
	settings databaseDependencyShiftSettings, ids []string,
) (map[string]databaseDependencyDateRow, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	params := databaseDependencyDateReadParams(owner, pageID, sourceID, settings)
	params["ids"] = string(encoded)
	rows := []databaseDependencyDateRow{}
	if err := app.DB().NewQuery(`SELECT ` + databaseDependencyDateRowColumns + `
		FROM json_each({:ids}) input JOIN {{leafen_database_rows}} r ON r.[[id]]=input.value
		` + databaseDependencyDateRowJoins + `
		WHERE r.[[owner]]={:owner} AND r.[[page_id]]={:page} AND r.[[data_source_id]]={:source}
		AND ` + databaseRowNotTrashedSQL("r.id")).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	result := make(map[string]databaseDependencyDateRow, len(rows))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

// Each frontier entry reads at most one indexed edge window per direction.
// Bounding candidates before merging avoids rescanning an entire wide fork
// for every output window. The merge retains the original first-visit order.
func databaseDependencyDateNeighborWindow(predecessor bool) string {
	neighbor, anchor, changed, direction := "successor_row_id", "predecessor_row_id", "endChanged", 0
	if predecessor {
		neighbor, anchor, changed, direction = "predecessor_row_id", "successor_row_id", "startChanged", 1
	}
	return fmt.Sprintf(`SELECT CAST(f.key AS INTEGER) AS [[sequence]], n.value AS [[row_id]], %d AS [[predecessor]]
		FROM json_each({:frontier}) f CROSS JOIN json_each(
			CASE WHEN json_extract(f.value, '$.%s') AND CAST(f.key AS INTEGER)>={:afterSequence}
			THEN (SELECT json_group_array([[row_id]]) FROM (
				SELECT e.[[%s]] AS [[row_id]] FROM {{leafen_database_dependency_edges}} e
				JOIN {{leafen_database_rows}} r ON r.[[id]]=e.[[%s]]
				WHERE e.[[owner]]={:owner} AND e.[[page_id]]={:page} AND e.[[data_source_id]]={:source} AND e.[[data_key]]={:key}
					AND e.[[%s]]=json_extract(f.value, '$.id')
					AND e.[[%s]]>=CASE WHEN CAST(f.key AS INTEGER)={:afterSequence} THEN {:afterID} ELSE '' END
					AND (CAST(f.key AS INTEGER), e.[[%s]], %d)>({:afterSequence}, {:afterID}, {:afterPredecessor})
					AND r.[[owner]]={:owner} AND r.[[page_id]]={:page} AND r.[[data_source_id]]={:source}
					AND %s
				ORDER BY e.[[%s]] LIMIT {:limit}
			)) ELSE '[]' END
		) n`, direction, changed, neighbor, neighbor, anchor, neighbor, neighbor, direction,
		databaseRowNotTrashedSQL("r.id"), neighbor)
}

// A wide fork continues after its last key; it cannot fill an unbounded read
// slice. Dates and row metadata share the mutation's transaction snapshot.
func loadDatabaseDependencyDateNeighbors(app core.App, owner, pageID, sourceID, dataKey string,
	settings databaseDependencyShiftSettings, frontier []databaseDependencyDateChange,
	after databaseDependencyDateRow,
) ([]databaseDependencyDateRow, error) {
	encoded, err := json.Marshal(frontier)
	if err != nil {
		return nil, err
	}
	params := databaseDependencyDateReadParams(owner, pageID, sourceID, settings)
	params["frontier"], params["key"] = string(encoded), dataKey
	params["afterSequence"], params["afterID"], params["afterPredecessor"] = after.Sequence, after.ID, after.Predecessor
	params["limit"] = databaseDependencyDateReadWindow
	rows := []databaseDependencyDateRow{}
	if err := app.DB().NewQuery(`WITH neighbors AS (` + databaseDependencyDateNeighborWindow(true) + ` UNION ALL ` +
		databaseDependencyDateNeighborWindow(false) + `), neighbor_window AS MATERIALIZED (
		SELECT * FROM neighbors ORDER BY [[sequence]], [[row_id]], [[predecessor]] LIMIT {:limit}
	)
	SELECT ` + databaseDependencyDateRowColumns + `, q.[[sequence]], q.[[predecessor]]
	FROM neighbor_window q JOIN {{leafen_database_rows}} r ON r.[[id]]=q.[[row_id]]
	` + databaseDependencyDateRowJoins + `
	ORDER BY q.[[sequence]], q.[[row_id]], q.[[predecessor]]`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}
