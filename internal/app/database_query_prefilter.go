package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A filtered query's turn walks up to databaseQuerySourceWindowRowsPerTurn
// rows in SQL and loads only those that can meet the conditions every
// result meets and a row's kept values decide: the options a select,
// multi-select or status names, the people or rows a person or relation
// names, a checked checkbox. The whole filter still decides each loaded row,
// so the walk only spares the rows that can't match; a query without such
// conditions walks a hundred rows a turn as before. When those conditions
// are the whole filter, SQL decides it alone, and a query that only counts
// counts each walked window there.

type databaseQueryPrefilter struct {
	clauses []string
	params  dbx.Params
	// exact: the clauses decide the whole filter.
	exact bool
}

// databaseQueryWalk is how far a turn walked: the last row it passed and how
// many rows that was. A turn that stopped at a row it couldn't finish leaves
// it unset, and the query goes on after the last row it finished.
type databaseQueryWalk struct {
	set          bool
	position, id string
	count        int
	end          bool
	// counted: the rows a counting turn found, kept as a count alone.
	counted   int
	countOnly bool
}

func planDatabaseQueryPrefilter(
	plan databaseQueryPlan,
	context databaseQueryContext,
	principalID, pageID string,
) databaseQueryPrefilter {
	prefilter := databaseQueryPrefilter{params: dbx.Params{}, exact: plan.Search == ""}
	group := plan.FilterGroup
	if group == nil {
		return prefilter
	}
	if group.Conjunction != databaseQueryConjunctionAnd || len(group.Groups) > 0 {
		prefilter.exact = false
		if group.Conjunction != databaseQueryConjunctionAnd {
			return prefilter
		}
	}
	for _, filter := range group.Filters {
		property, ok := context.PropertiesByID[filter.PropertyID]
		if !ok || (filter.Operator != "equals" && filter.Operator != "contains") {
			prefilter.exact = false
			continue
		}
		expected := resolveDatabaseQueryFilterValue(databaseQueryRawValue(filter.Value), principalID, pageID, nil)
		var targets []string
		switch property.Type {
		case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
			// A status with a default reads it on rows that keep none; the
			// evaluator reads it.
			if property.Type == graphValueTypeStatus && databaseStatusDefaultOptionID(property.Config) != "" {
				prefilter.exact = false
				continue
			}
			// No target matches no row.
			for target := range databaseQueryOptionTargets(property, expected) {
				targets = append(targets, target)
			}
			if len(targets) == 0 {
				prefilter.clauses = append(prefilter.clauses, "0")
				continue
			}
		case graphValueTypePerson, graphValueTypeRelation:
			// A person that defaults to the creator reads someone the row
			// doesn't keep, and a relation closed to the reading keeps rows
			// it reads as none (rowAsRead); the evaluator reads them.
			if databasePersonDefaultsToCreator(property.Type, property.Config) || context.ClosedRelations[property.ID] {
				prefilter.exact = false
				continue
			}
			// No target matches every row.
			targets = databaseQueryValueStrings(expected)
			if len(targets) == 0 {
				continue
			}
		case graphValueTypeCheckbox:
			if filter.Operator != "equals" || !databaseQueryBoolean(expected) {
				prefilter.exact = false
				continue
			}
			// A kept value is written as JSON once: checked is true or "true".
			targets = []string{"true", `"true"`}
			prefilter.clauses = append(prefilter.clauses, prefilter.kept(filter.PropertyID, nil)+
				" AND "+prefilter.keptText(filter.PropertyID, targets))
			continue
		default:
			prefilter.exact = false
			continue
		}
		prefilter.clauses = append(prefilter.clauses, prefilter.kept(filter.PropertyID, targets))
	}
	return prefilter
}

// keptText is a row's kept value for the property written as one of the
// texts.
func (prefilter databaseQueryPrefilter) keptText(propertyID string, texts []string) string {
	index := len(prefilter.params)
	property := fmt.Sprintf("prefilterProperty%d", index)
	prefilter.params[property] = propertyID
	placeholders := make([]string, len(texts))
	for position, text := range texts {
		key := fmt.Sprintf("%sText%d", property, position)
		prefilter.params[key] = text
		placeholders[position] = "{:" + key + "}"
	}
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM {{leafen_database_row_values}} AS [[prefilterText%[1]d]]
		WHERE [[prefilterText%[1]d]].[[row_id]] = [[row]].[[id]] AND [[prefilterText%[1]d]].[[property_id]] = {:%[2]s}
			AND [[prefilterText%[1]d]].[[value]] IN (%[3]s))`, index, property, strings.Join(placeholders, ", "))
}

// kept is a row's kept value for the property, holding one of the targets
// when there are any: a single value or an array's item, trimmed as the
// filter compares them.
func (prefilter databaseQueryPrefilter) kept(propertyID string, targets []string) string {
	index := len(prefilter.params)
	value := fmt.Sprintf("prefilterValue%d", index)
	property := fmt.Sprintf("prefilterProperty%d", index)
	prefilter.params[property] = propertyID
	clause := fmt.Sprintf(`EXISTS (SELECT 1 FROM {{leafen_database_row_values}} AS [[%[1]s]]
		WHERE [[%[1]s]].[[row_id]] = [[row]].[[id]] AND [[%[1]s]].[[property_id]] = {:%[2]s}`, value, property)
	if len(targets) == 0 {
		return clause + ")"
	}
	placeholders := make([]string, len(targets))
	for position, target := range targets {
		key := fmt.Sprintf("%sTarget%d", property, position)
		prefilter.params[key] = target
		placeholders[position] = "{:" + key + "}"
	}
	return clause + fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM json_each(CASE WHEN json_valid([[%[1]s]].[[value]]) THEN [[%[1]s]].[[value]] ELSE 'null' END) AS [[%[1]sItem]]
			WHERE trim([[%[1]sItem]].[[value]]) IN (%[2]s)
		))`, value, strings.Join(placeholders, ", "))
}

// databaseQuerySourceScope is the rows a query reads after its cursor: its
// data source's rows in its subitem and archive scope, outside the trash.
func databaseQuerySourceScope(job storedDatabaseQueryJob, input databaseQueryRequest) (string, dbx.Params) {
	params := dbx.Params{
		"owner": job.ContentOwnerID, "pageId": job.PageID,
		"dataSourceId": job.DataSourceID, "position": job.SourcePosition, "rowId": job.SourceID,
	}
	scope := ""
	switch input.SubitemScope {
	case databaseSubitemScopeParents:
		scope = " AND [[row]].[[subitem_parent_id]] = ''"
	case databaseSubitemScopeChildren:
		scope = " AND [[row]].[[subitem_parent_id]] = {:subitemParentId}"
		params["subitemParentId"] = *input.SubitemParentID
	}
	// A database page's archive state is on its page record.
	scope += databaseRowArchiveScopeSQL(input.ArchiveScope, "[[row_record]]")
	scope += " AND " + databaseRowNotTrashedSQL("[[row]].[[id]]")
	params["hiddenRows"] = "[]"
	if len(job.hiddenRows) > 0 {
		encoded, _ := json.Marshal(job.hiddenRows)
		scope += " AND [[row]].[[id]] NOT IN (SELECT [[value]] FROM json_each({:hiddenRows}))"
		params["hiddenRows"] = string(encoded)
	}
	return `
		FROM {{leafen_database_rows}} AS [[row]]
		LEFT JOIN {{leafen_database_row_pages}} AS [[page]] ON [[page]].[[row_id]] = [[row]].[[id]]
		LEFT JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]] = [[row]].[[id]]
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:pageId}
			AND [[row]].[[data_source_id]] = {:dataSourceId}` + scope + `
			AND ([[row]].[[position]] > {:position} OR ([[row]].[[position]] = {:position} AND [[row]].[[id]] > {:rowId}))`, params
}

// databaseQueryReaderChildCountSQL is how many sub-items [[row]] has to the
// query's reader: not those in the trash or hidden from them
// ({:hiddenRows}). Every row a query answers counts them so, however it is
// worked out (databaseQuerySourceHeadersSQL, loadDatabaseQuerySourceRowsByID).
var databaseQueryReaderChildCountSQL = `[[row]].[[subitem_child_count]] - ` + databaseRowTrashedChildrenSQL("[[row]]") + ` - (
				SELECT COUNT(*) FROM json_each({:hiddenRows}) AS [[hidden]]
				JOIN {{leafen_database_rows}} AS [[hidden_child]] ON [[hidden_child]].[[id]] = [[hidden]].[[value]]
				WHERE [[hidden_child]].[[subitem_parent_id]] = [[row]].[[id]] AND ` + databaseRowNotTrashedSQL("[[hidden_child]].[[id]]") + `
			)`

// databaseQuerySourceHeadersSQL selects what a query keeps of each row it
// reads; a row counts the sub-items its reader sees
// (databaseQueryReaderChildCountSQL).
var databaseQuerySourceHeadersSQL = `
		SELECT [[row]].[[id]], [[row]].[[position]], [[row]].[[subitem_parent_id]],
			` + databaseQueryReaderChildCountSQL + ` AS [[subitem_child_count]],
			COALESCE([[page]].[[created]], [[row]].[[created]]) AS [[created]],
			[[row]].[[created]] AS [[row_created]],
			COALESCE([[page]].[[updated]], [[row]].[[updated]]) AS [[updated]],
			COALESCE([[row_record]].[[created_by]], '') AS [[created_by]],
			COALESCE([[row_record]].[[last_edited_by]], '') AS [[last_edited_by]]`

// countDatabaseQuerySourceWindow counts, for a query that only counts and
// whose filter SQL decides, the rows of the turn's window that match.
func countDatabaseQuerySourceWindow(
	app core.App,
	job storedDatabaseQueryJob,
	input databaseQueryRequest,
	prefilter databaseQueryPrefilter,
) (databaseQueryWalk, error) {
	from, params := databaseQuerySourceScope(job, input)
	for key, value := range prefilter.params {
		params[key] = value
	}
	walk, within, err := databaseQuerySourceBoundary(app, from, params)
	if err != nil {
		return walk, err
	}
	walk.set = true
	matching := ""
	if len(prefilter.clauses) > 0 {
		matching = " AND " + strings.Join(prefilter.clauses, " AND ")
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*)` + from + within + matching).Bind(params).Row(&walk.counted); err != nil {
		return walk, err
	}
	if walk.end {
		// The query is done: its cursor stays where it was.
		walk.position, walk.id = job.SourcePosition, job.SourceID
		return walk, app.DB().NewQuery(`SELECT COUNT(*)` + from).Bind(params).Row(&walk.count)
	}
	walk.position, walk.id = params["boundaryPosition"].(string), params["boundaryId"].(string)
	walk.count = databaseQuerySourceWindowRowsPerTurn
	return walk, nil
}

// databaseQuerySourceBoundary finds the last row a turn may walk and the
// condition that keeps the turn to it; none when the rest is shorter.
func databaseQuerySourceBoundary(app core.App, from string, params dbx.Params) (databaseQueryWalk, string, error) {
	params["window"] = databaseQuerySourceWindowRowsPerTurn - 1
	boundary := struct {
		Position string `db:"position"`
		ID       string `db:"id"`
	}{}
	walk := databaseQueryWalk{}
	err := app.DB().NewQuery(`SELECT [[row]].[[position]], [[row]].[[id]]` + from + `
		ORDER BY [[row]].[[position]], [[row]].[[id]] LIMIT 1 OFFSET {:window}`).Bind(params).One(&boundary)
	switch {
	case err == nil:
		params["boundaryPosition"], params["boundaryId"] = boundary.Position, boundary.ID
		return walk, databaseQueryWithinBoundarySQL, nil
	case errors.Is(err, sql.ErrNoRows):
		walk.end = true
		return walk, "", nil
	default:
		return walk, "", err
	}
}

const databaseQueryWithinBoundarySQL = ` AND ([[row]].[[position]] < {:boundaryPosition} OR ([[row]].[[position]] = {:boundaryPosition} AND [[row]].[[id]] <= {:boundaryId}))`

// loadDatabaseQuerySourceWindow loads a turn's rows and says how far the
// turn walked.
func loadDatabaseQuerySourceWindow(
	app core.App,
	job storedDatabaseQueryJob,
	input databaseQueryRequest,
	prefilter databaseQueryPrefilter,
) ([]storedDatabaseQueryRow, databaseQueryWalk, error) {
	if len(prefilter.clauses) == 0 {
		rows, err := loadDatabaseQuerySourceRowHeaders(app, job, input)
		return rows, databaseQueryWalk{end: len(rows) < databaseQuerySourceRowsPerTurn}, err
	}
	from, params := databaseQuerySourceScope(job, input)
	for key, value := range prefilter.params {
		params[key] = value
	}
	walk, within, err := databaseQuerySourceBoundary(app, from, params)
	if err != nil {
		return nil, walk, err
	}
	boundaryPosition, _ := params["boundaryPosition"].(string)
	boundaryID, _ := params["boundaryId"].(string)
	params["limit"] = databaseQuerySourceRowsPerTurn
	rows := []storedDatabaseQueryRow{}
	if err := app.DB().NewQuery(databaseQuerySourceHeadersSQL + from + within + `
			AND ` + strings.Join(prefilter.clauses, " AND ") + `
		ORDER BY [[row]].[[position]], [[row]].[[id]] LIMIT {:limit}`).Bind(params).All(&rows); err != nil {
		return nil, walk, err
	}
	for index := range rows {
		rows[index].Values = map[string]graphRowValue{}
		rows[index].Computed = map[string]databaseComputedValue{}
	}
	walk.set = true
	if len(rows) == databaseQuerySourceRowsPerTurn {
		// More may match before the boundary: the turn walked to its last row.
		last := rows[len(rows)-1]
		walk.end = false
		walk.position, walk.id = last.Position, last.ID
		params["boundaryPosition"], params["boundaryId"] = last.Position, last.ID
		within = databaseQueryWithinBoundarySQL
	} else if walk.end {
		walk.position, walk.id = job.SourcePosition, job.SourceID
	} else {
		walk.position, walk.id = boundaryPosition, boundaryID
		walk.count = databaseQuerySourceWindowRowsPerTurn
		return rows, walk, nil
	}
	err = app.DB().NewQuery(`SELECT COUNT(*)` + from + within).Bind(params).Row(&walk.count)
	return rows, walk, err
}
