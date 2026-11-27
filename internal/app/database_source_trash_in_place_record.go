package app

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The trash's record of the sources a database keeps in the trash where they
// stand (schema_baseline.json). Its block decides: each
// projection of a database's block records a source it newly lists with the
// time it went, and lets one it no longer lists go, so every way a block
// changes — the API, the editor, undo, a copy, an import — keeps the record.
// A block that leaves its page keeps its sources' records, which come back
// with it as they were. The trash lists them with the other databases in it
// (deleted_database_containers.go), and their 30 days end as a source's that
// left its database (database_source_trash_in_place_retention.go).

// databaseSourcesTrashedInPlace reads the sources a database's record keeps in
// the trash where they stand, in the order they went.
func databaseSourcesTrashedInPlace(app core.App, containerID string) ([]string, error) {
	sources := []string{}
	err := app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_data_source_trash_in_place}}
		WHERE [[container_id]] = {:containerId}
		ORDER BY [[deleted_at]], [[data_source_id]]
	`).Bind(dbx.Params{"containerId": containerID}).Column(&sources)
	return sources, err
}

// reconcileDatabaseSourceTrashInPlace keeps the record of the database's own
// sources its block keeps in the trash where they stand, and returns them as
// the record had them before and as the block has them now.
func reconcileDatabaseSourceTrashInPlace(
	app core.App, owner, workspaceID, pageID, containerID string, content databaseContainerBlockContent,
) ([]string, []string, error) {
	before, err := databaseSourcesTrashedInPlace(app, containerID)
	if err != nil {
		return nil, nil, err
	}
	after := []string{}
	if !content.LinkedDatabaseSource {
		for _, id := range content.ownDataSourceIDs() {
			if content.trashedInPlace(id) {
				after = append(after, id)
			}
		}
	}
	if slices.Equal(before, after) && len(after) == 0 {
		return before, after, nil
	}
	listed, err := json.Marshal(after)
	if err != nil {
		return nil, nil, err
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_data_source_trash_in_place}}
		WHERE [[container_id]] = {:containerId}
			AND [[data_source_id]] NOT IN (SELECT [[value]] FROM json_each({:listed}))
	`).Bind(dbx.Params{"containerId": containerID, "listed": string(listed)}).Execute(); err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	for _, id := range after {
		// One the record has keeps the time it went; its database's
		// place is the block's now.
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_data_source_trash_in_place}}
				([[data_source_id]],[[container_id]],[[owner]],[[workspace_id]],[[page_id]],[[deleted_at]],[[expires_at]])
			VALUES ({:id},{:containerId},{:owner},{:workspaceId},{:pageId},{:deletedAt},{:expiresAt})
			ON CONFLICT ([[data_source_id]]) DO UPDATE SET [[container_id]] = excluded.[[container_id]],
				[[owner]] = excluded.[[owner]], [[workspace_id]] = excluded.[[workspace_id]],
				[[page_id]] = excluded.[[page_id]]
		`).Bind(dbx.Params{
			"id": id, "containerId": containerID, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
			"deletedAt": now.UnixMilli(), "expiresAt": now.Add(recoverableBlockTrashRetention).UnixMilli(),
		}).Execute(); err != nil {
			return nil, nil, err
		}
	}
	return before, after, nil
}

// databaseSourcesTrashedInPlaceListSQL selects, as the trash lists its
// databases, the sources a workspace's databases keep in the trash where
// they stand, each named by its own ID; with a root block, only the source of
// that ID. One whose database's block left its page comes back with it, so it
// isn't listed on its own meanwhile. The kept sources are read first and
// their containers by id, the workspace after (+), or SQLite, which keeps no
// statistics, reads the workspace's every container.
func databaseSourcesTrashedInPlaceListSQL() string {
	return `
	SELECT [[kept]].[[data_source_id]] AS [[root_block_id]], [[kept_container]].[[page_id]],
		[[kept]].[[deleted_at]], [[kept]].[[expires_at]], [[kept]].[[data_source_id]], 1 AS [[in_place]],
		[[kept]].[[container_id]], 0 AS [[in_trashed_page]]
	FROM {{leafen_data_source_trash_in_place}} AS [[kept]]
	JOIN {{leafen_database_containers}} AS [[kept_container]]
		ON [[kept_container]].[[id]] = [[kept]].[[container_id]] AND [[kept_container]].[[kind]] = 'source'
	WHERE +[[kept_container]].[[workspace_id]] = {:workspaceId} AND [[kept]].[[expires_at]] > {:now}
		AND ({:rootBlockId} = '' OR [[kept]].[[data_source_id]] = {:rootBlockId})
		AND ` + databaseSourceTrashedInPlaceSQL("[[kept]].[[data_source_id]]")
}

// deleteDatabaseSourceTrashedInPlaceForever ends the source's time in the
// trash now: the trash's retention takes it, and its database's block with it
// when it was all the database had.
func deleteDatabaseSourceTrashedInPlaceForever(app core.App, dataSourceID string) error {
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_data_source_trash_in_place}} SET [[expires_at]] = MAX({:now}, [[deleted_at]] + 1)
		WHERE [[data_source_id]] = {:id} AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{"now": now, "id": dataSourceID}).Execute(); err != nil {
		return err
	}
	notifyRecoverableBlockTrashRetentionAfterCommit(app)
	return nil
}

// databaseKeepsOnlySourcesTrashedInPlace reports whether a database's own
// sources are all kept in the trash where they stand, which Notion marks the
// database deleted for (seen 2026-11-11: its page's Markdown tags it
// `deleted`, its MCP fetch too, and the MCP's fetch of its page leaves it
// out).
func databaseKeepsOnlySourcesTrashedInPlace(app core.App, containerID string) (bool, error) {
	var kept int
	err := app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_database_container_sources}} AS [[own]]
			WHERE [[own]].[[container_id]] = {:id} AND [[own]].[[source_container_id]] = ''
		) AND NOT EXISTS (
			SELECT 1 FROM {{leafen_database_container_sources}} AS [[own]]
			WHERE [[own]].[[container_id]] = {:id} AND [[own]].[[source_container_id]] = ''
				AND NOT ` + databaseSourceTrashedInPlaceSQL("[[own]].[[data_source_id]]") + `
		)
	`).Bind(dbx.Params{"id": containerID}).Row(&kept)
	return kept == 1, err
}
