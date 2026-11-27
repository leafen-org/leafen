package app

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// copyDatabasePageLayouts gives each copied data source the page layout of
// the source it copies, its properties, relations and tabs naming the
// copy's own where they were copied too, as a duplicated database keeps its
// layout in Notion.
func copyDatabasePageLayouts(app core.App, jobID, actorID, workspaceID string, sourceIDs []string) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	copies, err := pageDuplicationReferenceMappings(app, jobID, "data_source", sourceIDs)
	if err != nil {
		return err
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(sourceIDs))
	for index, sourceID := range sourceIDs {
		key := fmt.Sprintf("layoutSource%d", index)
		params[key] = sourceID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	layouts := []struct {
		SourceID string `db:"data_source_id"`
		Layout   string `db:"layout"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[data_source_id]], [[layout]] FROM {{leafen_database_page_layouts}}
		WHERE [[data_source_id]] IN (` + joinSQL(placeholders) + `) ORDER BY [[data_source_id]]`).Bind(params).All(&layouts); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, layout := range layouts {
		target := copies[layout.SourceID]
		if target == "" {
			continue
		}
		remapped, err := remapPageDuplicationDatabaseJSON(app, jobID, []json.RawMessage{json.RawMessage(layout.Layout)})
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_database_page_layouts}}
				([[data_source_id]], [[workspace_id]], [[layout]], [[revision]], [[updated_by]], [[updated_at]])
			VALUES ({:source}, {:workspaceId}, {:layout}, 1, {:actor}, {:now})
			ON CONFLICT ([[data_source_id]]) DO NOTHING`).Bind(dbx.Params{
			"source": target, "workspaceId": workspaceID, "layout": string(remapped[0]), "actor": actorID, "now": now,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
