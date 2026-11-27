package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// An option pages hold is deleted only when a change says so (user decision
// 2026-11-26, "추천대로"). Notion's API and MCP remove an option a change of a
// column's options leaves out and clear it from its pages without a word
// (measured 2026-11-26 through the API: 200, the row's value gone), so an
// agent adding one option to a column of 70 sent all 70 again, and any it
// missed took its pages' values with it. Leafen refuses an agent's or the
// API's change that leaves out options pages hold, naming each and how many
// pages hold it, and changes nothing; an option deleted on purpose — sent
// with "delete": true, or notion-update-data-source's DROP OPTION — goes,
// cleared from its pages as Notion clears it, and one option is added alone
// (ADD OPTION) without the others. An option no page holds may be left out,
// as before. The screen deletes an option from its menu, asking first, as
// Notion's does (2026-11-26, Chrome: "이 옵션을 삭제하시겠습니까?"), and is
// not refused. A change's new options are checked here too
// (database_option_names.go).

// databaseOptionDeleteKey marks an option a change deletes on purpose.
const databaseOptionDeleteKey = "delete"

// databaseOptionChangesChecked is a change's check of the options of the
// columns and values it writes, before its operations apply
// (CheckOperations): an agent's or the API's (asked) is refused for options
// it leaves out that pages hold. It changes nothing but the operations'
// own settings, so the change still says what it changed.
func databaseOptionChangesChecked(asked bool) func(core.App, pageAuthorization, *crdt.Doc, []graphOperation) error {
	return func(app core.App, _ pageAuthorization, _ *crdt.Doc, operations []graphOperation) error {
		return checkDatabaseOptionChanges(app, operations, asked)
	}
}

// checkDatabaseOptionChanges takes out of each select's, multi-select's or
// status's settings the options marked deleted, then refuses new options
// databaseNewOptionsReason refuses and, when asked, options left out that
// pages hold.
func checkDatabaseOptionChanges(app core.App, operations []graphOperation, asked bool) error {
	if err := checkDatabaseOptionConfigs(app, operations, asked); err != nil {
		return err
	}
	if !asked {
		return nil
	}
	return refuseUnknownDatabaseOptionValues(app, operations)
}

// checkDatabaseOptionConfigs is checkDatabaseOptionChanges for the columns'
// settings the change writes.
func checkDatabaseOptionConfigs(app core.App, operations []graphOperation, asked bool) error {
	for _, operation := range operations {
		property := operation.Property
		if operation.Kind != graphOperationKindUpsertProperty || property == nil {
			continue
		}
		if _, options := databasePropertyConversionOptionTypes[property.Type]; !options {
			continue
		}
		stored := struct {
			Type   string `db:"type"`
			Config string `db:"config"`
		}{}
		err := app.DB().NewQuery(`SELECT [[type]], [[config]] FROM {{leafen_database_properties}} WHERE [[id]]={:id} AND [[data_source_id]]={:dataSourceId}`).
			Bind(dbx.Params{"id": property.ID, "dataSourceId": property.DataSourceID}).One(&stored)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// One read cut short keeps what it didn't show (database_row_page_agent_options.go).
		property.Config = databaseOptionsCutKept(json.RawMessage(stored.Config), property.Config)
		deleted, config, err := takeDeletedDatabaseOptions(property.Config)
		if err != nil {
			return graphRefused(errorCodeGraphInvalid, "Check the column's options.", "column %s: its settings don't read as JSON", strconvQuoteJS(property.Name))
		}
		property.Config = config
		if code, reason := databaseNewOptionsReason(property.Name, json.RawMessage(stored.Config), config); reason != "" {
			return graphRefused(code, "Check the column's options.", "%s", reason)
		}
		if _, held := databasePropertyConversionOptionTypes[stored.Type]; !asked || !held {
			continue
		}
		reason, err := databaseOptionsLeftOutReason(app, property, json.RawMessage(stored.Config), deleted)
		if err != nil {
			return err
		}
		if reason != "" {
			return graphRefused(errorCodeDatabaseOptionsInUse, "Check the column's options.", "%s", reason)
		}
	}
	return nil
}

// takeDeletedDatabaseOptions takes the options marked deleted out of a
// column's settings, and out of a status's groups: their IDs, and the
// settings without them.
func takeDeletedDatabaseOptions(raw json.RawMessage) (map[string]bool, json.RawMessage, error) {
	config := map[string]any{}
	if len(raw) > 0 && json.Unmarshal(raw, &config) != nil {
		return nil, raw, errors.New("config")
	}
	options, _ := config["options"].([]any)
	deleted, kept := map[string]bool{}, make([]any, 0, len(options))
	for _, raw := range options {
		option, _ := raw.(map[string]any)
		if marked, _ := option[databaseOptionDeleteKey].(bool); marked {
			id, _ := option["id"].(string)
			deleted[strings.TrimSpace(id)] = true
			continue
		}
		kept = append(kept, raw)
	}
	if len(deleted) == 0 {
		return deleted, raw, nil
	}
	config["options"] = kept
	groups, _ := config["statusGroups"].([]any)
	for _, raw := range groups {
		group, _ := raw.(map[string]any)
		ids, _ := group["optionIds"].([]any)
		left := make([]any, 0, len(ids))
		for _, id := range ids {
			if text, _ := id.(string); !deleted[text] {
				left = append(left, id)
			}
		}
		if group != nil {
			group["optionIds"] = left
		}
	}
	encoded, err := json.Marshal(config)
	return deleted, encoded, err
}

// databaseOptionsLeftOutReason says which of a column's options (stored) its
// new settings leave out, not deleted on purpose, that pages hold, and how
// many hold each: "" for none.
func databaseOptionsLeftOutReason(app core.App, property *graphProperty, stored json.RawMessage, deleted map[string]bool) (string, error) {
	type option struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	read := func(raw json.RawMessage) []option {
		config := struct {
			Options []option `json:"options"`
		}{}
		_ = json.Unmarshal(raw, &config)
		return config.Options
	}
	kept := map[string]bool{}
	for _, given := range read(property.Config) {
		kept[strings.TrimSpace(given.ID)] = true
	}
	held, err := databaseOptionRowCounts(app, property.ID)
	if err != nil {
		return "", err
	}
	left := []string{}
	for _, option := range read(stored) {
		id := strings.TrimSpace(option.ID)
		if kept[id] || deleted[id] || held[id] == 0 {
			continue
		}
		pages := "pages hold"
		if held[id] == 1 {
			pages = "page holds"
		}
		left = append(left, fmt.Sprintf("%s (%d %s it)", strconvQuoteJS(option.Name), held[id], pages))
	}
	if len(left) == 0 {
		return "", nil
	}
	sort.Strings(left)
	return fmt.Sprintf("column %s: this leaves out options pages hold — %s — so nothing was changed. "+
		"List them to keep them; to add an option, send the ones there with it (notion-update-data-source: ALTER COLUMN %s ADD OPTION 'Name':color); "+
		"to delete one on purpose, which clears it from its pages, send it with \"delete\": true (notion-update-data-source: ALTER COLUMN %s DROP OPTION 'Name')",
		strconvQuoteJS(property.Name), strings.Join(left, ", "), strconvQuoteJS(property.Name), strconvQuoteJS(property.Name)), nil
}

// databaseOptionRowCounts is how many rows hold each of a property's
// options, a select's or status's by its value, a multi-select's among its.
func databaseOptionRowCounts(app core.App, propertyID string) (map[string]int, error) {
	counts := []struct {
		ID   string `db:"option_id"`
		Rows int    `db:"rows"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[held]].[[value]] AS [[option_id]], COUNT(DISTINCT [[cell]].[[row_id]]) AS [[rows]]
		FROM {{leafen_database_row_values}} AS [[cell]], json_each(CASE WHEN json_type([[cell]].[[value]]) = 'array'
			THEN [[cell]].[[value]] ELSE json_array(json([[cell]].[[value]])) END) AS [[held]]
		WHERE [[cell]].[[property_id]] = {:propertyId} AND json_valid([[cell]].[[value]])
		GROUP BY [[held]].[[value]]
	`).Bind(dbx.Params{"propertyId": propertyID}).All(&counts)
	held := make(map[string]int, len(counts))
	for _, count := range counts {
		held[count.ID] = count.Rows
	}
	return held, err
}
