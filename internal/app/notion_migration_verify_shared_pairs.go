package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
)

// The check's other sides of shared columns: a two-way column the plan has
// hold another property's rows — a relation gathered into it, a merged
// database's property its target's column stands for, a split row's in its
// target's — has its other side name them back (notion_migration_shared_pairs.go),
// which Notion's other side doesn't. A cell of that side Notion holds empty
// and Leafen holds only such rows in says the plan changed it
// (notionMigrationVerifyPlanChanged), and so does a formula or rollup
// reading that side (2026-11-24: the other side of a relation another was
// gathered into, empty in Notion, was said to have the gathered row only in
// Leafen, with no known reason). A column several of Notion's properties
// became is a shared one, as the computed values' check counts it.

// sharedPairs are the other sides of the shared two-way columns the run
// made, each with the column it is the other side of.
func (v *notionMigrationVerifier) sharedPairs() (map[string]string, error) {
	columns := []struct {
		ID     string `db:"id"`
		Config string `db:"config"`
	}{}
	if err := v.app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[config]] FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[property]].[[type]]={:relation} AND [[property]].[[id]] IN (
			SELECT [[native_id]] FROM {{leafen_notion_import_mappings}}
			WHERE [[job_id]]={:jobId} AND [[kind]]='property' GROUP BY [[native_id]] HAVING COUNT(*) > 1)
		ORDER BY [[property]].[[id]]
	`).Bind(dbx.Params{"jobId": v.jobID, "relation": graphValueTypeRelation}).All(&columns); err != nil {
		return nil, err
	}
	pairs := map[string]string{}
	for _, column := range columns {
		config := map[string]any{}
		if json.Unmarshal([]byte(column.Config), &config) != nil {
			continue
		}
		if other, _ := config["relatedPropertyId"].(string); other != "" && other != column.ID {
			pairs[other] = column.ID
		}
	}
	return pairs, nil
}

// sharedPairSources are the Notion properties the other sides of shared
// two-way columns were made from.
func (v *notionMigrationVerifier) sharedPairSources() (map[string]bool, error) {
	pairs, err := v.sharedPairs()
	if err != nil || len(pairs) == 0 {
		return map[string]bool{}, err
	}
	encoded, err := json.Marshal(workspaceExportSortedKeys(pairs))
	if err != nil {
		return nil, err
	}
	sources := []string{}
	if err := v.app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='property' AND [[native_id]] IN (SELECT [[value]] FROM json_each({:pairs}))
	`).Bind(dbx.Params{"jobId": v.jobID, "pairs": string(encoded)}).Column(&sources); err != nil {
		return nil, err
	}
	named := map[string]bool{}
	for _, source := range sources {
		named[source] = true
	}
	return named, nil
}

// sharedRows says whether each row a cell of a shared column's other side
// holds names the cell's row from another property its shared column holds.
func (v *notionMigrationVerifier) sharedRows(rowID, shared, value string) (bool, error) {
	held := graphRelationRowIDs(json.RawMessage(value))
	if len(held) == 0 {
		return false, nil
	}
	named := ""
	if err := v.app.DB().NewQuery(`
		SELECT [[source_id]] FROM {{leafen_notion_import_mappings}} WHERE [[job_id]]={:jobId} AND [[kind]]='row' AND [[native_id]]={:row} LIMIT 1
	`).Bind(dbx.Params{"jobId": v.jobID, "row": rowID}).Row(&named); err != nil || named == "" {
		return false, nil
	}
	for _, holder := range held {
		names, err := v.namesShared(holder, shared, named)
		if err != nil || !names {
			return false, err
		}
	}
	return true, nil
}

// namesShared says whether a row's staged relations name a row, by its
// source, through the shared column: its own key's column, the role a
// relation is gathered into, or the split's column where a split sent it.
func (v *notionMigrationVerifier) namesShared(holder, shared, named string) (bool, error) {
	row := struct {
		Database string `db:"parent_source_id"`
		Text     string `db:"text"`
		Placed   string `db:"placed"`
		Own      string `db:"own"`
	}{}
	if err := v.app.DB().NewQuery(`
		SELECT [[source]].[[parent_source_id]], [[source]].[[text]], COALESCE([[row]].[[data_source_id]], '') AS [[placed]],
			COALESCE((SELECT [[native_id]] FROM {{leafen_notion_import_mappings}} WHERE [[job_id]]=[[mapping]].[[job_id]]
				AND [[kind]]='data_source' AND [[source_id]]=[[source]].[[parent_source_id]]), '') AS [[own]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_notion_import_sources}} AS [[source]] ON [[source]].[[job_id]]=[[mapping]].[[job_id]]
			AND [[source]].[[source_id]]=[[mapping]].[[source_id]] AND [[source]].[[kind]]='row'
		LEFT JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]]=[[mapping]].[[native_id]]
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='row' AND [[mapping]].[[native_id]]={:row}
		LIMIT 1
	`).Bind(dbx.Params{"jobId": v.jobID, "row": holder}).One(&row); err != nil {
		return false, nil
	}
	values := map[string]notionDatabaseRowValuePlan{}
	if json.Unmarshal([]byte(row.Text), &values) != nil {
		return false, nil
	}
	split := row.Own != "" && row.Placed != "" && row.Own != row.Placed
	for _, key := range workspaceExportSortedKeys(values) {
		value := values[key]
		related, _ := value.Value.([]any)
		if value.Type != graphValueTypeRelation || !slices.ContainsFunc(related, func(item any) bool {
			id, _ := item.(string)
			return notionImportSameID(id, named)
		}) {
			continue
		}
		if split {
			column := key
			databaseID, propertyID, _ := strings.Cut(key, ":property:")
			if into := v.into[notionMigrationSourceKey(databaseID)][notionImportPropertyKey(propertyID)]; into != "" {
				column = notionPropertySourceID(databaseID, strings.TrimSpace(into))
			}
			key = notionSplitColumnSource(column, row.Placed)
		}
		column, err := notionImportMappedID(v.app, v.jobID, key, "property")
		if err != nil {
			return false, err
		}
		if column == shared {
			return true, nil
		}
	}
	return false, nil
}
