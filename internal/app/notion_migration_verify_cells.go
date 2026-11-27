package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
)

// The check's cells: each cell of a row Notion gave the run that Notion holds
// empty and Leafen holds a value in (2026-11-24: a run again kept the values
// Notion had emptied — a number, an option, a date, a link, a file, a cut
// relation — and the check said nothing differed). A run again empties what
// Notion emptied since the run before it (notion_migration_rerun_emptied.go),
// so one still here was put in Leafen — kept, as a cell Notion left as it was
// keeps a change made in Leafen — or was left by a run from before runs
// emptied cells; the check can't tell which, so it lists them for the person
// and gives an agent nothing to set. A column stands for the Notion property
// it was made for: its own, a split's in the database a row went to, one
// beside a relation holding the rows a split sent elsewhere, and the names
// kept beside objects — each empty when the property is. A gathered role's
// column, and a relation gathered into it, hold more than one property's
// values, so neither is compared (2026-11-24: a relation gathered into
// another, empty in Notion, was said to have the other's rows only in
// Leafen). A row standing for a template, one Notion has in its trash and one
// in Leafen's trash aren't compared. The other side of a two-way column
// holding another property's rows says the plan changed it when only those
// rows are in it (notion_migration_verify_shared_pairs.go).

// A cell Notion holds empty has the setting notionMigrationVerifyEmptyInNotion
// (contracts/notion-import-wire.json).
const (
	// notionMigrationVerifyLeafenOnly is why: only Leafen holds a value.
	notionMigrationVerifyLeafenOnly = "leafen_only"
	// notionMigrationVerifyCellShown bounds a cell's value as the check says it.
	notionMigrationVerifyCellShown = 40
)

// notionMigrationVerifyCellTypes are the types of the cells compared: those
// a row keeps a value of that can be empty — not a checkbox, which is never
// empty, nor what Leafen computes.
func notionMigrationVerifyCellTypes() string {
	types := []string{}
	for kind := range graphClearableValueTypes {
		if kind != graphValueTypeCheckbox {
			types = append(types, kind)
		}
	}
	encoded, _ := json.Marshal(types)
	return string(encoded)
}

// notionMigrationVerifyEmptySQL is true of a staged row value, by the JSON
// path to it, that holds nothing: none, null, "" or [].
func notionMigrationVerifyEmptySQL(path string) string {
	value := path + ` || '.value'`
	return `(COALESCE(json_type([[source]].[[text]], ` + value + `), 'null') = 'null'
		OR json_type([[source]].[[text]], ` + value + `) = 'text' AND json_extract([[source]].[[text]], ` + value + `) = ''
		OR json_type([[source]].[[text]], ` + value + `) = 'array' AND json_array_length([[source]].[[text]], ` + value + `) = 0)`
}

type notionMigrationVerifyCell struct {
	RowID    string `db:"row_id"`
	Column   string `db:"property_id"`
	Database string `db:"parent_source_id"`
	Row      string `db:"row_title"`
	Property string `db:"property_source"`
	Name     string `db:"name"`
	Type     string `db:"type"`
	Config   string `db:"config"`
	Value    string `db:"value"`
}

func (v *notionMigrationVerifier) cells() error {
	cells := []notionMigrationVerifyCell{}
	// The Notion property a column's key names: up to the ':' after the
	// property's ID, which Notion gives escaped ("<property>:split:…",
	// ":to:…", ":kept").
	rest := `substr([[property_map]].[[source_id]], length([[source]].[[parent_source_id]]) + 11)`
	property := `CASE instr(` + rest + `, ':') WHEN 0 THEN [[property_map]].[[source_id]]
		ELSE substr([[property_map]].[[source_id]], 1, length([[source]].[[parent_source_id]]) + 9 + instr(` + rest + `, ':')) END`
	key := `'$."' || ` + property + ` || '"'`
	shown := `'$."` + notionImportRowShownKey + `".value."' || ` + property + ` || '"'`
	if err := v.app.DB().NewQuery(`
		SELECT [[row_map]].[[native_id]] AS [[row_id]], [[source]].[[parent_source_id]], COALESCE([[page]].[[title]], '') AS [[row_title]],
			` + property + ` AS [[property_source]], [[property]].[[id]] AS [[property_id]], [[property]].[[name]], [[property]].[[type]], [[property]].[[config]],
			[[cell]].[[value]]
		FROM {{leafen_notion_import_sources}} AS [[source]]
		JOIN {{leafen_notion_import_mappings}} AS [[row_map]] ON [[row_map]].[[job_id]]=[[source]].[[job_id]]
			AND [[row_map]].[[kind]]='row' AND [[row_map]].[[source_id]]=[[source]].[[source_id]]
		JOIN {{leafen_notion_import_mappings}} AS [[property_map]] ON [[property_map]].[[job_id]]=[[source]].[[job_id]]
			AND [[property_map]].[[kind]]='property'
			AND [[property_map]].[[source_id]] > [[source]].[[parent_source_id]] || ':property:'
			AND [[property_map]].[[source_id]] < [[source]].[[parent_source_id]] || ':property;'
		JOIN {{leafen_database_row_values}} AS [[cell]] ON [[cell]].[[row_id]]=[[row_map]].[[native_id]]
			AND [[cell]].[[property_id]]=[[property_map]].[[native_id]]
		JOIN {{leafen_database_properties}} AS [[property]] ON [[property]].[[id]]=[[cell]].[[property_id]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]]=[[row_map]].[[native_id]]
		WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[kind]]='row' AND json_valid([[source]].[[text]])
			AND instr([[source]].[[text]], {:countedTemplate})=0 AND instr([[source]].[[text]], {:countedTrashed})=0
			AND NOT ` + pageInTrashSQL("[[page]]") + `
			AND [[cell]].[[type]] IN (SELECT [[value]] FROM json_each({:cellTypes}))
			AND [[cell]].[[value]] NOT IN ('', 'null', '""', '[]', '{}', 'false')
			AND ` + notionMigrationVerifyEmptySQL(key) + `
			AND COALESCE(json_extract([[source]].[[text]], ` + shown + `), '') = ''
		ORDER BY [[source]].[[sequence]], [[property]].[[position]], [[property]].[[id]]
	`).Bind(notionImportCountedParams(dbx.Params{"jobId": v.jobID, "cellTypes": notionMigrationVerifyCellTypes()})).All(&cells); err != nil {
		return err
	}
	titles, err := v.cellRowTitles(cells)
	if err != nil {
		return err
	}
	untitled := notionMigrationVerifyText[accountLanguageEn]["untitled"]
	if text := notionMigrationVerifyText[v.language]; text != nil {
		untitled = text["untitled"]
	}
	pairs, err := v.sharedPairs()
	if err != nil {
		return err
	}
	for _, cell := range cells {
		database, property, _ := strings.Cut(cell.Property, ":property:")
		// A column a relation of the plan's gathers into holds others' too,
		// and the gathered relation stands for that column.
		if v.gathered(database, property) {
			continue
		}
		reason := notionMigrationVerifyLeafenOnly
		// The other side of a column holding another property's rows names
		// them (notion_migration_verify_shared_pairs.go).
		if shared, found := pairs[cell.Column]; found && cell.Type == graphValueTypeRelation {
			if plan, err := v.sharedRows(cell.RowID, shared, cell.Value); err != nil {
				return err
			} else if plan {
				reason = notionMigrationVerifyPlanChanged
			}
		}
		row := cmpOr(strings.TrimSpace(cell.Row), untitled)
		v.differ(notionMigrationVerifyDifference{
			Kind: notionMigrationVerifyKindCell, Setting: notionMigrationVerifyEmptyInNotion, NotionID: notionMigrationVerifyDatabase(cell.Database), ID: cell.RowID,
			Title: row + " · " + cell.Name, Notion: "", Leafen: notionMigrationVerifyCellValue(cell, titles, untitled),
			Reason: reason,
		})
	}
	return nil
}

// gathered says whether the plan gathers a relation of a database into
// another of its properties, or another relation into it.
func (v *notionMigrationVerifier) gathered(database, property string) bool {
	gathering := v.into[notionMigrationSourceKey(database)]
	if _, gathers := gathering[notionImportPropertyKey(property)]; gathers {
		return true
	}
	for _, into := range gathering {
		if notionImportPropertyKey(into) == notionImportPropertyKey(property) {
			return true
		}
	}
	return false
}

// cellRowTitles are the titles of the rows the check's relation cells name.
func (v *notionMigrationVerifier) cellRowTitles(cells []notionMigrationVerifyCell) (map[string]string, error) {
	ids := map[string]struct{}{}
	for _, cell := range cells {
		if cell.Type == graphValueTypeRelation {
			for _, id := range graphRelationRowIDs(json.RawMessage(cell.Value)) {
				ids[id] = struct{}{}
			}
		}
	}
	titles := map[string]string{}
	if len(ids) == 0 {
		return titles, nil
	}
	encoded, err := json.Marshal(workspaceExportSortedKeys(ids))
	if err != nil {
		return nil, err
	}
	rows := []struct {
		ID    string `db:"id"`
		Title string `db:"title"`
	}{}
	if err := v.app.DB().NewQuery(`SELECT [[id]], [[title]] FROM {{leafen_pages}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).
		Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		titles[row.ID] = row.Title
	}
	return titles, nil
}

// notionMigrationVerifyCellValue is a cell's value as the check says it: its
// words, its options' and related rows' names, a date, its files' names.
func notionMigrationVerifyCellValue(cell notionMigrationVerifyCell, titles map[string]string, untitled string) string {
	var decoded any
	if json.Unmarshal([]byte(cell.Value), &decoded) != nil {
		return notionMigrationVerifyCut(cell.Value)
	}
	names := map[string]string{}
	if notionMigrationOptionTypes[cell.Type] {
		config := map[string]any{}
		_ = json.Unmarshal([]byte(cell.Config), &config)
		for _, option := range mcpPropertyOptions(config) {
			names[option.ID] = option.Name
		}
	}
	name := func(item any) string {
		switch value := item.(type) {
		case string:
			if cell.Type == graphValueTypeRelation {
				return cmpOr(strings.TrimSpace(titles[value]), untitled)
			}
			return cmpOr(names[value], value)
		case map[string]any:
			for _, field := range []string{"name", "displayName", "email", "start", "state", "address"} {
				if text, ok := value[field].(string); ok && text != "" {
					if end, ok := value["end"].(string); ok && field == "start" && end != "" {
						return text + " → " + end
					}
					return text
				}
			}
		}
		return fmt.Sprint(item)
	}
	if list, many := decoded.([]any); many {
		said := make([]string, 0, len(list))
		for _, item := range list {
			said = append(said, name(item))
		}
		return notionMigrationVerifyCut(strings.Join(said, ", "))
	}
	return notionMigrationVerifyCut(name(decoded))
}

// notionMigrationVerifyCut shortens what the check says of a value.
func notionMigrationVerifyCut(text string) string {
	if utf8.RuneCountInString(text) <= notionMigrationVerifyCellShown {
		return text
	}
	return string([]rune(text)[:notionMigrationVerifyCellShown]) + "…"
}
