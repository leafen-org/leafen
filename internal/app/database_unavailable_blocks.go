package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database block the page can't show — no container holds it, and none of
// its own data sources is on the page: a body copied before its databases
// came with it, its database since gone — shows "데이터베이스를 사용할 수
// 없습니다" (InlineDatabaseBlock). It is chosen, moved and taken away as a
// block alone, nothing of a database going with it, and comes back as it was
// when its removal is undone or its trash restored, as Notion's block for a
// database it can't show does. A block naming a database it doesn't hold
// can't otherwise be written (validateDatabaseContainerMemberReferences):
// the page remembers each such block it took away, by the sources it named,
// and takes back only that one, so no block naming another's database can be
// made up to stand on a page.

const unavailableDatabaseBlocksTable = "leafen_unavailable_database_blocks"

// unavailableDatabaseBlockSources names the sources a database block names,
// in one order.
func unavailableDatabaseBlockSources(content databaseContainerBlockContent) string {
	members := slices.Clone(content.memberDataSourceIDs())
	slices.Sort(members)
	return strings.Join(members, ",")
}

// databaseBlockUnavailable says whether a database block is one its page
// can't show: no container holds it, and none of its own sources is on it.
func databaseBlockUnavailable(app core.App, pageID, blockID string, content databaseContainerBlockContent) (bool, error) {
	own, err := json.Marshal(content.dataSourceIDs())
	if err != nil {
		return false, err
	}
	var shown int
	err = app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_database_containers}} WHERE [[id]] = {:blockId})
			OR EXISTS(SELECT 1 FROM {{leafen_data_sources}} WHERE [[page_id]] = {:pageId}
				AND [[id]] IN (SELECT [[value]] FROM json_each({:own})))
	`).Bind(dbx.Params{"blockId": blockID, "pageId": pageID, "own": string(own)}).Row(&shown)
	return shown == 0, err
}

// rememberUnavailableDatabaseBlocks keeps, of the blocks a change takes off
// the page, each database block the page couldn't show, for its return.
func rememberUnavailableDatabaseBlocks(
	app core.App, workspaceID, pageID string, blocks map[string]structuralBlockMetadata, ids []string,
) error {
	for _, id := range ids {
		entry := blocks[id]
		if entry.Type != graphBlockTypeInlineDatabase {
			continue
		}
		raw, err := json.Marshal(entry.Content)
		if err != nil {
			return err
		}
		content, err := parseDatabaseContainerBlockContent(raw)
		if err != nil || content.DataSourceID == "" {
			continue
		}
		unavailable, err := databaseBlockUnavailable(app, pageID, id, content)
		if err != nil {
			return err
		}
		if !unavailable {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_unavailable_database_blocks}}
				([[workspace_id]], [[page_id]], [[block_id]], [[sources]], [[deleted_at]])
			VALUES ({:workspaceId}, {:pageId}, {:blockId}, {:sources}, {:now})
			ON CONFLICT ([[page_id]], [[block_id]]) DO UPDATE SET
				[[sources]] = excluded.[[sources]], [[deleted_at]] = excluded.[[deleted_at]]
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "pageId": pageID, "blockId": id,
			"sources": unavailableDatabaseBlockSources(content), "now": time.Now().UTC().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// unavailableDatabaseBlockReturns says whether a database block written to
// the page is one it took away while it couldn't show it, as it was then.
func unavailableDatabaseBlockReturns(app core.App, pageID, blockID string, content databaseContainerBlockContent) (bool, error) {
	sources := ""
	err := app.DB().NewQuery(`
		SELECT [[sources]] FROM {{leafen_unavailable_database_blocks}}
		WHERE [[page_id]] = {:pageId} AND [[block_id]] = {:blockId}
	`).Bind(dbx.Params{"pageId": pageID, "blockId": blockID}).Row(&sources)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || sources != unavailableDatabaseBlockSources(content) {
		return false, err
	}
	return databaseBlockUnavailable(app, pageID, blockID, content)
}

// loadReturningUnavailableDatabaseBlocks names the database blocks a change
// writes back as the page took them away (unavailableDatabaseBlockReturns).
func loadReturningUnavailableDatabaseBlocks(app core.App, pageID string, operations []graphOperation) (map[string]struct{}, error) {
	returning := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil ||
			operation.Block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		content, err := parseDatabaseContainerBlockContent(operation.Block.Content)
		if err != nil {
			continue
		}
		back, err := unavailableDatabaseBlockReturns(app, pageID, operation.Block.ID, content)
		if err != nil {
			return nil, err
		}
		if back {
			returning[operation.Block.ID] = struct{}{}
		}
	}
	return returning, nil
}

// takeBackUnavailableDatabaseBlock puts back a database block the page took
// away while it couldn't show it: as a block alone, no container of the
// database it names. It says whether the block was one.
func takeBackUnavailableDatabaseBlock(app core.App, pageID, blockID string, content databaseContainerBlockContent) (bool, error) {
	back, err := unavailableDatabaseBlockReturns(app, pageID, blockID, content)
	if err != nil || !back {
		return false, err
	}
	_, err = app.DB().NewQuery(`
		DELETE FROM {{leafen_unavailable_database_blocks}} WHERE [[page_id]] = {:pageId} AND [[block_id]] = {:blockId}
	`).Bind(dbx.Params{"pageId": pageID, "blockId": blockID}).Execute()
	return err == nil, err
}
