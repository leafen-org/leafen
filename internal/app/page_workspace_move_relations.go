package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A relation between a moved database and one left behind can't reach
// across workspaces, so the move cuts it, both of its sides, with the
// rollups through either: they go to their databases' Deleted properties
// (database_property_trash.go), as a person's deletion puts them, so a move
// back finds them there with their values. A copy into another workspace
// leaves the same properties out (crossWorkspaceLeftOutProperties).

type pageWorkspaceMoveProperty struct {
	ID           string `db:"id"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	Owner        string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	Type         string `db:"type"`
	Config       string `db:"config"`
}

type pageWorkspaceMoveRelationCutList struct {
	relations []pageWorkspaceMoveProperty
	rollups   []pageWorkspaceMoveProperty
}

// pageWorkspaceMoveRelationCuts finds the relations the move cuts — a moved
// database's to one it doesn't take, and a database's left behind to one it
// takes — and the rollups through them.
func pageWorkspaceMoveRelationCuts(app core.App, move storedPageWorkspaceMove) (pageWorkspaceMoveRelationCutList, error) {
	list := pageWorkspaceMoveRelationCutList{}
	params := dbx.Params{"jobId": move.JobID, "relation": graphValueTypeRelation, "rollup": graphValueTypeRollup}
	pages, sources := pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPage), pageWorkspaceMoveInBundle(pageWorkspaceMoveItemSource)
	live := databasePropertyNotTrashedSQL("[[property]].[[id]]")
	moved := []pageWorkspaceMoveProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[data_source_id]],[[owner]],[[workspace_id]],[[type]],[[config]]
		FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[page_id]] IN ` + pages + ` AND [[type]] IN ({:relation},{:rollup}) AND ` + live + `
		ORDER BY [[id]]
	`).Bind(params).All(&moved); err != nil {
		return list, err
	}
	// Left behind: a relation whose target is a moved data source.
	behind := []pageWorkspaceMoveProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[page_id]],[[data_source_id]],[[owner]],[[workspace_id]],[[type]],[[config]]
		FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[page_id]] NOT IN ` + pages + ` AND ` + live + ` AND (
			([[type]] = {:relation} AND [[id]] IN (
				SELECT [[property_id]] FROM {{leafen_database_relation_property_targets}}
				WHERE [[target_source_id]] IN ` + sources + `))
			OR ([[type]] = {:rollup} AND [[data_source_id]] IN (
				SELECT [[source_id]] FROM {{leafen_database_relation_property_targets}}
				WHERE [[target_source_id]] IN ` + sources + ` AND [[page_id]] NOT IN ` + pages + `)))
		ORDER BY [[id]]
	`).Bind(params).All(&behind); err != nil {
		return list, err
	}
	inBundle := map[string]bool{}
	bundleSources := []string{}
	if err := app.DB().NewQuery(`SELECT [[item_id]] FROM ` + sources + ` AS [[moved]]`).Bind(params).Column(&bundleSources); err != nil {
		return list, err
	}
	for _, source := range bundleSources {
		inBundle[source] = true
	}
	cut := map[string]bool{}
	for _, side := range []struct {
		properties []pageWorkspaceMoveProperty
		cuts       func(target string) bool
	}{
		{moved, func(target string) bool { return !inBundle[target] }},
		{behind, func(target string) bool { return inBundle[target] }},
	} {
		for _, property := range side.properties {
			target := graphRelationTarget(property.Type, json.RawMessage(property.Config))
			if property.Type == graphValueTypeRelation && target != "" && side.cuts(target) {
				cut[property.ID] = true
				list.relations = append(list.relations, property)
			}
		}
	}
	for _, property := range append(moved, behind...) {
		if property.Type != graphValueTypeRollup {
			continue
		}
		if config, err := decodeDatabaseComputedConfig(property.Config); err == nil {
			if relationID, _, _ := config.rollupFields(); cut[relationID] {
				list.rollups = append(list.rollups, property)
			}
		}
	}
	return list, nil
}

// counts says how many relations and rollups are cut and how many values
// the relations held.
func (list pageWorkspaceMoveRelationCutList) counts(app core.App) (int, int, int, error) {
	ids := make([]any, 0, len(list.relations))
	for _, property := range list.relations {
		ids = append(ids, property.ID)
	}
	values := 0
	if len(ids) > 0 {
		if err := app.DB().Select("COUNT(*)").From("leafen_database_relation_items").
			Where(dbx.In("property_id", ids...)).Row(&values); err != nil {
			return 0, 0, 0, err
		}
	}
	return len(list.relations), len(list.rollups), values, nil
}

// cut moves the relations and rollups to their databases' Deleted
// properties, as the mover's deletion, and has each database's page say so.
func (list pageWorkspaceMoveRelationCutList) cut(app core.App, move storedPageWorkspaceMove, now time.Time) error {
	pages := map[string]databasePropertyTrashTarget{}
	for _, property := range append(append([]pageWorkspaceMoveProperty{}, list.relations...), list.rollups...) {
		// Read again: a moved property's workspace changed since it was found.
		current := pageWorkspaceMoveProperty{}
		if err := app.DB().NewQuery(`
			SELECT [[id]],[[page_id]],[[data_source_id]],[[owner]],[[workspace_id]],[[type]],[[config]]
			FROM {{leafen_database_properties}} WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": property.ID}).One(&current); err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_database_property_trash}}
				([[property_id]], [[owner]], [[workspace_id]], [[deleted_by]], [[deleted_at]], [[deleting]])
			VALUES ({:propertyId}, {:owner}, {:workspaceId}, {:actorId}, {:now}, 0)
			ON CONFLICT ([[property_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"propertyId": current.ID, "owner": current.Owner, "workspaceId": current.WorkspaceID,
			"actorId": move.ActorID, "now": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		pages[current.PageID] = databasePropertyTrashTarget{Owner: current.Owner, PageID: current.PageID}
	}
	for _, page := range pages {
		if err := recordDatabasePropertyTrashOn(app, page, "workspace-move:"+move.JobID); err != nil {
			return err
		}
	}
	return nil
}
