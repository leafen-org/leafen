package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An option of a select, multi-select or status renamed — by a person, an
// agent or a Notion move run again — is found in search by its new name on
// every row holding it, as Notion finds it (2026-11-24: an option renamed in
// Notion and taken by a run again, or renamed in Leafen, was found by its old
// name alone). A row's search entry keeps its options' names as it was
// indexed (buildSearchRowProjection), so the write of the property marks the
// rows holding an option whose name changed — in the one place every write
// of a property goes through (applyDatabaseRecordMutation). A row an import
// still stages is indexed once it is published.

// writeDatabasePropertyRecordFound writes a property, and marks for search
// the rows holding an option it renames.
func writeDatabasePropertyRecordFound(app core.App, owner, workspaceID, pageID string, property graphProperty, now int64) error {
	renamed, err := databasePropertyOptionsRenamed(app, property)
	if err != nil {
		return err
	}
	if err := writeDatabasePropertyRecord(app, owner, workspaceID, pageID, property, now); err != nil {
		return err
	}
	return markDatabaseOptionRowsSearchDirty(app, workspaceID, property.ID, renamed)
}

// databasePropertyOptionsRenamed are the IDs of the options a write of a
// property gives another name than the one it has.
func databasePropertyOptionsRenamed(app core.App, property graphProperty) ([]string, error) {
	if _, options := databasePropertyConversionOptionTypes[property.Type]; !options {
		return nil, nil
	}
	stored := ""
	err := app.DB().NewQuery(`SELECT [[config]] FROM {{leafen_database_properties}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": property.ID}).Row(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := func(raw []byte) map[string]string {
		config := struct {
			Options []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"options"`
		}{}
		_ = json.Unmarshal(raw, &config)
		byID := map[string]string{}
		for _, option := range config.Options {
			byID[option.ID] = strings.TrimSpace(option.Name)
		}
		return byID
	}
	before, after := names([]byte(stored)), names(property.Config)
	renamed := []string{}
	for id, name := range after {
		if was, held := before[id]; held && was != name {
			renamed = append(renamed, id)
		}
	}
	return renamed, nil
}

// markDatabaseOptionRowsSearchDirty marks for search the rows holding any of
// a property's options named.
func markDatabaseOptionRowsSearchDirty(app core.App, workspaceID, propertyID string, options []string) error {
	if len(options) == 0 {
		return nil
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return err
	}
	rows := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[cell]].[[row_id]] FROM {{leafen_database_row_values}} AS [[cell]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[cell]].[[row_id]] AND [[page]].[[notion_import_staging]] = 0
		WHERE [[cell]].[[property_id]] = {:propertyId} AND json_valid([[cell]].[[value]]) AND EXISTS (
			SELECT 1 FROM json_each([[cell]].[[value]]) AS [[held]]
			WHERE [[held]].[[value]] IN (SELECT [[value]] FROM json_each({:options})))
	`).Bind(dbx.Params{"propertyId": propertyID, "options": string(encoded)}).Column(&rows); err != nil {
		return err
	}
	return markDatabaseRowsSearchDirty(context.Background(), app, workspaceID, rows)
}
