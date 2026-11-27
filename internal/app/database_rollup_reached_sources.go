package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseRollupReachLimit is how many hops past a page's rollups are read,
// as far as a rollup of rollups is worked out at all.
const databaseRollupReachLimit = 8

type rollupReachedSource struct {
	DataSourceID string `db:"data_source_id"`
	SourcePageID string `db:"source_page_id"`
}

// loadRollupReachedSources is the other-page sources a page's computed
// values read past their own relations (databaseComputedReach): one showing
// the related rows' relation names that relation's rows, one rolling up the
// related rows' rollup goes on through its relation, and one rolling up a
// related formula reads what that formula reads, as Notion shows them
// wherever those databases are.
func loadRollupReachedSources(app core.App, owner, workspaceID, pageID string) ([]rollupReachedSource, error) {
	members := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[data_source_id]] FROM {{leafen_database_container_sources}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
		ORDER BY [[data_source_id]]
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).Column(&members); err != nil {
		return nil, err
	}
	reach := newDatabaseComputedReach()
	for _, sourceID := range members {
		properties, err := loadRollupReachProperties(app, owner, sourceID)
		if err != nil {
			return nil, err
		}
		reach.start(sourceID, properties)
	}
	for hop := 0; hop < databaseRollupReachLimit; hop++ {
		waiting := []string{}
		for _, sourceID := range reach.waiting() {
			if len(reach.pending[sourceID]) > 0 {
				waiting = append(waiting, sourceID)
			}
		}
		if len(waiting) == 0 {
			break
		}
		for _, sourceID := range waiting {
			properties, err := loadRollupReachProperties(app, owner, sourceID)
			if err != nil {
				return nil, err
			}
			reach.load(sourceID, properties)
		}
	}
	past := []string{}
	for sourceID, beyond := range reach.Reached {
		if beyond {
			past = append(past, sourceID)
		}
	}
	if len(past) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(past)
	if err != nil {
		return nil, err
	}
	reached := []rollupReachedSource{}
	// The sources are found by id; owner and workspace are read after (+),
	// or SQLite, which keeps no statistics, reads the workspace's every
	// source.
	err = app.DB().NewQuery(`
		SELECT [[id]] AS [[data_source_id]], [[page_id]] AS [[source_page_id]] FROM {{leafen_data_sources}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND +[[owner]] = {:owner}
			AND +[[workspace_id]] = {:workspaceId} AND [[page_id]] != {:pageId}
		ORDER BY [[id]]
	`).Bind(dbx.Params{"ids": string(encoded), "owner": owner, "workspaceId": workspaceID, "pageId": pageID}).All(&reached)
	return reached, err
}

// loadRollupReachProperties are a source's relations and computed values,
// which are all the walk follows.
func loadRollupReachProperties(app core.App, owner, sourceID string) ([]storedDatabaseQueryProperty, error) {
	properties := []storedDatabaseQueryProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[data_source_id]] = {:dataSourceId}
			AND [[type]] IN ('relation', 'formula', 'rollup') AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"owner": owner, "dataSourceId": sourceID, "limit": maxDatabaseQueryProperties}).All(&properties)
	return properties, err
}
