package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	databaseContainerKindSource = "source"
	databaseContainerKindLinked = "linked"
)

func deleteDatabaseContainerBlocks(app core.App, blockIDs []string, actorID string) error {
	if len(blockIDs) == 0 {
		return nil
	}
	containers, err := databaseContainerBlocksAmong(app, blockIDs)
	if err != nil {
		return err
	}
	for _, blockID := range containers {
		trashed, err := databaseContainerIsTrashed(app, blockID)
		if err != nil {
			return err
		}
		if trashed {
			continue
		}
		// A source database whose block goes goes to the trash, with its
		// public ID (notion_deleted_database_reads.go).
		if err := recordDeletedDatabase(app, blockID, actorID); err != nil {
			return err
		}
		if err := deleteDatabaseContainerProjection(app, blockID); err != nil {
			return err
		}
		if err := keepDeletedDatabasePublicID(app, blockID); err != nil {
			return err
		}
	}
	return nil
}

func deleteDatabaseContainerProjection(app core.App, containerID string) error {
	if err := deleteDatabaseContainerMembers(app, containerID); err != nil {
		return err
	}
	_, err := app.DB().NewQuery("DELETE FROM {{leafen_database_containers}} WHERE [[id]] = {:id}").
		Bind(dbx.Params{"id": containerID}).Execute()
	return err
}

// deleteDatabaseContainerMembers takes away a container's data sources and
// views, leaving its row and what hangs from it: those who keep it among
// favorites and its public ID.
func deleteDatabaseContainerMembers(app core.App, containerID string) error {
	for _, table := range []string{"leafen_database_container_views", "leafen_database_container_sources"} {
		if _, err := app.DB().NewQuery(
			"DELETE FROM {{" + table + "}} WHERE [[container_id]] = {:id}",
		).Bind(dbx.Params{"id": containerID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// refreshDatabaseContainerPlacements puts a page's databases where their
// blocks stand. Every write that places a page's blocks ends here — a change,
// a move, a copy, a restored version — and an archive refuses a database
// whose place isn't its block's (workspace_import_archive_database_containers.go).
// A database whose block is in the trash keeps the place its trash keeps.
func refreshDatabaseContainerPlacements(app core.App, owner, pageID string) error {
	// Only the containers whose block moved are written: every block written
	// on a page rewrote all its databases' rows, each with its triggers
	// (2026-10-21).
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_containers}}
		SET [[parent_block_id]] = [[block]].[[parent_id]], [[position]] = [[block]].[[position]],
			[[updated]] = [[block]].[[updated]]
		FROM (SELECT {{leafen_database_containers}}.[[id]] AS [[container_id]], [[blocks]].[[parent_id]], [[blocks]].[[position]], [[blocks]].[[updated]]
			FROM {{leafen_database_containers}} JOIN {{leafen_blocks}} [[blocks]] ON [[blocks]].[[id]] = {{leafen_database_containers}}.[[block_id]]
			WHERE {{leafen_database_containers}}.[[owner]] = {:owner} AND {{leafen_database_containers}}.[[page_id]] = {:pageId}) AS [[block]]
		WHERE {{leafen_database_containers}}.[[id]] = [[block]].[[container_id]]
			AND ({{leafen_database_containers}}.[[parent_block_id]] IS NOT [[block]].[[parent_id]]
				OR {{leafen_database_containers}}.[[position]] IS NOT [[block]].[[position]]
				OR {{leafen_database_containers}}.[[updated]] IS NOT [[block]].[[updated]]);
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).Execute()
	return err
}

func moveDatabaseContainerProjection(
	app core.App,
	owner, sourcePageID, destinationPageID, blockID, parentBlockID string,
) error {
	now := time.Now().UTC().UnixMilli()
	// Webhooks hear of a database moved to another page
	// (event_webhook_database_events.go).
	moving := struct {
		WorkspaceID string `db:"workspace_id"`
		Kind        string `db:"kind"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[workspace_id]], [[kind]] FROM {{leafen_database_containers}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:sourcePageId} AND [[block_id]] = {:blockId}`).
		Bind(dbx.Params{"owner": owner, "sourcePageId": sourcePageID, "blockId": blockID}).One(&moving); err == nil && sourcePageID != destinationPageID {
		own := []string{}
		if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_container_sources}}
			WHERE [[container_id]] = {:id} AND [[source_container_id]] = '' ORDER BY [[position]]`).
			Bind(dbx.Params{"id": blockID}).Column(&own); err != nil {
			return err
		}
		// Its sources in the trash where they stand move with it, as they were.
		trashed, err := databaseSourcesTrashedInPlace(app, blockID)
		if err != nil {
			return err
		}
		defer noteDatabaseContainerProjection(app, moving.WorkspaceID, blockID,
			databaseContainerNote{Existed: true, PageBefore: sourcePageID, SourcesBefore: own, TrashedInPlace: trashed},
			destinationPageID, own, trashed, moving.Kind == databaseContainerKindLinked)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_containers}}
		SET [[workspace_id]] = COALESCE((
			SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:destinationPageId}
		), [[workspace_id]]),
		[[page_id]] = {:destinationPageId}, [[parent_block_id]] = {:parentBlockId},
		[[updated]] = {:now}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:sourcePageId} AND [[block_id]] = {:blockId}
	`).Bind(dbx.Params{
		"owner": owner, "sourcePageId": sourcePageID, "destinationPageId": destinationPageID,
		"blockId": blockID, "parentBlockId": parentBlockID, "now": now,
	}).Execute(); err != nil {
		return err
	}
	for _, table := range []string{
		"leafen_database_container_sources", "leafen_database_container_views",
	} {
		if _, err := app.DB().NewQuery(`
			UPDATE {{` + table + `}}
			SET [[workspace_id]] = COALESCE((
				SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:destinationPageId}
			), [[workspace_id]]),
			[[page_id]] = {:destinationPageId}, [[updated]] = {:now}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:sourcePageId}
				AND [[container_id]] = {:blockId}
		`).Bind(dbx.Params{
			"owner": owner, "sourcePageId": sourcePageID, "destinationPageId": destinationPageID,
			"blockId": blockID, "now": now,
		}).Execute(); err != nil {
			return err
		}
	}
	// Whoever keeps the database among favorites keeps it where it moved to.
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_favorites}}
		SET [[workspace_id]] = COALESCE((
			SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:destinationPageId}
		), [[workspace_id]]),
		[[page_id]] = {:destinationPageId}
		WHERE [[page_id]] = {:sourcePageId} AND [[container_id]] = {:blockId}
	`).Bind(dbx.Params{
		"sourcePageId": sourcePageID, "destinationPageId": destinationPageID, "blockId": blockID,
	}).Execute(); err != nil {
		return err
	}
	// So does the trash's record of its sources in the trash where they stand.
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_data_source_trash_in_place}}
		SET [[workspace_id]] = COALESCE((
			SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:destinationPageId}
		), [[workspace_id]]),
		[[page_id]] = {:destinationPageId}
		WHERE [[container_id]] = {:blockId}
	`).Bind(dbx.Params{"destinationPageId": destinationPageID, "blockId": blockID}).Execute(); err != nil {
		return err
	}
	return nil
}

func deleteDatabaseContainerViewProjection(
	app core.App, owner, workspaceID, pageID, viewID string,
) error {
	var dataSourceID string
	if err := app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_database_views}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": viewID}).Row(&dataSourceID); err != nil {
		return err
	}
	if err := deleteDatabaseEntityProjection(app, "leafen_database_views", owner, pageID, viewID); err != nil {
		return err
	}
	// A deleted view no longer groups, dates or otherwise uses a property, so
	// the property may be deleted.
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_view_property_refs}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[view_id]] = {:id}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": viewID}).Execute(); err != nil {
		return err
	}
	return refreshDatabaseContainerViewsForDataSource(app, owner, workspaceID, dataSourceID)
}
