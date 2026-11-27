package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The trash's databases also hold a source database that went to the trash
// of deleted blocks (database_container_trash.go) — through a data source's
// "휴지통으로 이동", the API, or an undone wiki — rather than by its block
// leaving its page. They list beside the deleted databases, restore through
// the data source's own restore — where the database was, or at its page's
// end when that place is gone — and are deleted forever by bringing their
// retention deletion forward. A source its database keeps in the trash where
// it stands lists the same way, named by its own ID, as Notion's trash lists
// it (seen 2026-11-11: the source's name and the page holding its database,
// with restore and delete) while it is one named source of several
// (deleted_database_list.go); it restores where it stands, and its deletion
// forever ends its time in the trash now
// (database_source_trash_in_place_record.go). An inline database in a page in
// the trash lists here too, by its block (database_in_trashed_page.go).

type trashedDatabaseContainer struct {
	RootBlockID  string `db:"root_block_id"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	Title        string `db:"title"`
	DeletedAt    int64  `db:"deleted_at"`
	ExpiresAt    int64  `db:"expires_at"`
	// InPlace is a source its database keeps in the trash where it stands,
	// RootBlockID its own ID; ContainerID is the block of its database.
	InPlace     bool   `db:"in_place"`
	ContainerID string `db:"container_id"`
	// InTrashedPage is an inline database in a page in the trash, or under
	// one (database_in_trashed_page.go), RootBlockID its block. EditedAt is a
	// database's own last edit, where the trash places it: such a one's, or
	// one whose block is in the trash — when its block went there if that
	// edited it (persistRecoverableBlockTrash), never after.
	InTrashedPage bool  `db:"in_trashed_page"`
	EditedAt      int64 `db:"edited_at"`
}

// listTrashedDatabaseContainers reads a workspace's source databases in the
// trash of deleted blocks, and those in pages in the trash, newest first from
// after on, up to limit; with a root block, only that one. A database inside
// another trashed block comes back with that block.
func listTrashedDatabaseContainers(app core.App, workspaceID, rootBlockID string, after deletedDatabaseCursor, limit int) ([]trashedDatabaseContainer, error) {
	keyset := ""
	if after.ID != "" {
		keyset = `AND [[listed]].[[deleted_at]] <= {:at} AND ([[listed]].[[deleted_at]] < {:at} OR [[listed]].[[root_block_id]] > {:id})`
	}
	items := []trashedDatabaseContainer{}
	err := app.DB().NewQuery(`
		WITH ` + trashedPageTreeCTE() + `
		SELECT [[listed]].*, COALESCE((SELECT [[source]].[[title]] FROM {{leafen_data_sources}} AS [[source]]
			WHERE [[source]].[[id]] = [[listed]].[[data_source_id]]), '') AS [[title]],
			CASE WHEN [[listed]].[[in_place]] = 0 THEN COALESCE((SELECT MIN([[held]].[[updated]], [[listed]].[[deleted_at]])
				FROM {{leafen_database_containers}} AS [[held]] WHERE [[held]].[[id]] = [[listed]].[[container_id]]), 0)
			ELSE 0 END AS [[edited_at]]
		FROM (
			SELECT [[trash]].[[root_block_id]], [[tombstone]].[[page_id]], [[tombstone]].[[deleted_at]],
				[[tombstone]].[[expires_at]], COALESCE((
					SELECT [[membership]].[[data_source_id]] FROM {{leafen_database_container_sources}} AS [[membership]]
					WHERE [[membership]].[[container_id]] = [[trash]].[[container_id]]
						AND [[membership]].[[source_container_id]] = ''
					ORDER BY [[membership]].[[position]], [[membership]].[[data_source_id]] LIMIT 1
				), '') AS [[data_source_id]], 0 AS [[in_place]], [[trash]].[[container_id]], 0 AS [[in_trashed_page]]
			FROM {{leafen_database_container_trash}} AS [[trash]]
			JOIN {{leafen_block_trash_tombstones}} AS [[tombstone]]
				ON [[tombstone]].[[root_block_id]] = [[trash]].[[root_block_id]]
			WHERE [[trash]].[[kind]] = 'source' AND [[trash]].[[container_id]] = [[trash]].[[root_block_id]]
				AND [[tombstone]].[[workspace_id]] = {:workspaceId} AND [[tombstone]].[[expires_at]] > {:now}
				AND ({:rootBlockId} = '' OR [[trash]].[[root_block_id]] = {:rootBlockId})
			UNION ALL` + databaseSourcesTrashedInPlaceListSQL() + `
			UNION ALL` + databasesInTrashedPagesListSQL() + `
		) AS [[listed]]
		WHERE [[listed]].[[data_source_id]] != '' ` + keyset + `
		ORDER BY [[listed]].[[deleted_at]] DESC, [[listed]].[[root_block_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "rootBlockId": rootBlockID, "now": time.Now().UTC().UnixMilli(),
		"at": after.At, "id": after.ID, "limit": limit, "pageTrashRetention": pageTrashRetention.Milliseconds(),
	}).All(&items)
	return items, err
}

// loadTrashedDatabaseContainer reads one such database for a person who may
// edit its page.
func loadTrashedDatabaseContainer(app core.App, userID, workspaceID, rootBlockID string) (trashedDatabaseContainer, error) {
	items, err := listTrashedDatabaseContainers(app, workspaceID, rootBlockID, deletedDatabaseCursor{}, 2)
	if err != nil {
		return trashedDatabaseContainer{}, err
	}
	if len(items) != 1 {
		return trashedDatabaseContainer{}, sql.ErrNoRows
	}
	if _, err := authorizePageBody(app, userID, items[0].PageID, permissionWorkspaceWrite); err != nil {
		return trashedDatabaseContainer{}, err
	}
	return items[0], nil
}

// restoreTrashedDatabaseContainer brings the database back as its data
// source's restore does.
func restoreTrashedDatabaseContainer(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, userID, workspaceID string, item trashedDatabaseContainer,
) error {
	identity := notionOAuthBearerIdentity{PrincipalID: userID, WorkspaceID: workspaceID}
	authorization, state, err := authorizeNotionDataSourceAnyState(event.App, identity, item.DataSourceID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	if !state.trashed() || state.InPlace != item.InPlace || !item.InPlace && state.RootBlockID.String != item.RootBlockID {
		return sql.ErrNoRows
	}
	return restoreNotionDataSource(event, ygo, authorization, state, true)
}

// deleteTrashedDatabaseContainerForever makes the database's retention
// deletion due now, which deletes it with its rows and takes it out of the
// trash.
func deleteTrashedDatabaseContainerForever(app core.App, item trashedDatabaseContainer) error {
	if item.InPlace {
		return deleteDatabaseSourceTrashedInPlaceForever(app, item.DataSourceID)
	}
	now := time.Now().UTC().UnixMilli()
	for _, statement := range []string{
		`UPDATE {{leafen_block_trash_tombstones}} SET [[expires_at]] = {:now}
			WHERE [[root_block_id]] = {:rootBlockId} AND [[expires_at]] > {:now}`,
		`UPDATE {{leafen_database_container_trash}} SET [[expires_at]] = {:now}
			WHERE [[root_block_id]] = {:rootBlockId} AND [[expires_at]] > {:now}`,
		`UPDATE {{leafen_database_mutation_jobs}} SET [[not_before]] = {:now}, [[updated_at]] = {:now}
			WHERE [[status]] = 'pending' AND [[not_before]] > {:now}
				AND instr([[request_id]], 'block-trash-retention:' || {:rootBlockId} || ':') = 1`,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(dbx.Params{"now": now, "rootBlockId": item.RootBlockID}).Execute(); err != nil {
			return err
		}
	}
	notifyDatabaseMutationDeletionAfterCommit(app)
	notifyRecoverableBlockTrashRetentionAfterCommit(app)
	return nil
}

func isTrashedDatabaseContainerMissing(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, errDatabaseRowPageNotFound)
}

// firstViewOfSourceIn is the first of a database's views that shows a data
// source, in the database's order of its views; "" when none does.
func firstViewOfSourceIn(app core.App, containerID, dataSourceID string) (string, error) {
	var viewID string
	err := app.DB().NewQuery(`
		SELECT [[membership]].[[view_id]] FROM {{leafen_database_container_views}} AS [[membership]]
		JOIN {{leafen_database_views}} AS [[view]] ON [[view]].[[id]] = [[membership]].[[view_id]]
		WHERE [[membership]].[[container_id]] = {:containerId} AND [[view]].[[data_source_id]] = {:dataSourceId}
		ORDER BY [[membership]].[[position]], [[membership]].[[view_id]] LIMIT 1
	`).Bind(dbx.Params{"containerId": containerID, "dataSourceId": dataSourceID}).Row(&viewID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return viewID, err
}
