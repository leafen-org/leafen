package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// validateDatabaseContainerDeletionReferences keeps a source database while
// another database links any of its sources, whether that database only links
// or also owns sources. A database the same change rewrites links only what
// its new content links, as a source taken back to the database it left on
// the same page stops being linked through the database it goes from. A
// deletion takes the blocks inside the deleted block as the change leaves
// them, as the body applies it (childPageChangeParents): a database the
// change moves out of it stays, one it moves into it goes.
func validateDatabaseContainerDeletionReferences(
	app core.App,
	owner, workspaceID, pageID string,
	operations []graphOperation,
) error {
	blockIDs := []string{}
	rewritten, links := []string{}, []string{}
	placed := childPageChangeParents{}
	for _, operation := range operations {
		placed.note(operation)
		switch {
		case operation.Kind == graphOperationKindDeleteBlock:
			blockIDs = append(blockIDs, operation.Deletion.BlockID)
		case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil &&
			operation.Block.Type == graphBlockTypeInlineDatabase:
			content, err := parseDatabaseContainerBlockContent(operation.Block.Content)
			if err != nil {
				continue
			}
			rewritten = append(rewritten, operation.Block.ID)
			for dataSourceID, sourceContainerID := range content.linkedSourceContainers() {
				links = append(links, operation.Block.ID+"|"+dataSourceID+"|"+sourceContainerID)
			}
		}
	}
	if len(blockIDs) == 0 {
		return nil
	}
	encodedBlocks, err := json.Marshal(blockIDs)
	if err != nil {
		return err
	}
	encodedRewritten, err := json.Marshal(rewritten)
	if err != nil {
		return err
	}
	encodedLinks, err := json.Marshal(links)
	if err != nil {
		return err
	}
	encodedPlaced, err := json.Marshal(map[string]string(placed))
	if err != nil {
		return err
	}
	var referenced int
	err = app.DB().NewQuery(`
		WITH RECURSIVE [[placed]]([[id]], [[parent_id]]) AS (
			SELECT [[key]], [[value]] FROM json_each({:placed})
		), [[deleted_blocks]]([[id]]) AS (
			SELECT [[value]] FROM json_each({:blockIds})
			UNION
			-- CROSS JOIN steps from the tree by key (owner, page, parent): SQLite's
			-- own order read the page's every block for each block (2026-11-08).
			SELECT [[child]].[[id]]
			FROM [[deleted_blocks]] AS [[parent]]
			CROSS JOIN {{leafen_blocks}} AS [[child]] ON [[child]].[[parent_id]] = [[parent]].[[id]]
			WHERE [[child]].[[owner]] = {:owner} AND [[child]].[[page_id]] = {:pageId}
				AND [[child]].[[id]] NOT IN (SELECT [[id]] FROM [[placed]])
			UNION
			SELECT [[moved]].[[id]]
			FROM [[deleted_blocks]] AS [[parent]]
			JOIN [[placed]] AS [[moved]] ON [[moved]].[[parent_id]] = [[parent]].[[id]]
		)
		SELECT COUNT(*)
		FROM {{leafen_database_containers}} AS [[source]]
		JOIN [[deleted_blocks]] AS [[deleted]] ON [[deleted]].[[id]] = [[source]].[[block_id]]
		JOIN {{leafen_database_container_sources}} AS [[linked]]
			ON [[linked]].[[source_container_id]] = [[source]].[[id]]
			AND [[linked]].[[owner]] = [[source]].[[owner]]
			AND [[linked]].[[workspace_id]] = [[source]].[[workspace_id]]
		LEFT JOIN [[deleted_blocks]] AS [[linked_deleted]]
			ON [[linked_deleted]].[[id]] = [[linked]].[[container_id]]
		WHERE [[source]].[[owner]] = {:owner} AND [[source]].[[workspace_id]] = {:workspaceId}
			AND [[source]].[[kind]] = 'source'
			AND [[linked_deleted]].[[id]] IS NULL
			AND NOT (
				[[linked]].[[container_id]] IN (SELECT [[value]] FROM json_each({:rewritten}))
				AND [[linked]].[[container_id]] || '|' || [[linked]].[[data_source_id]] || '|' || [[linked]].[[source_container_id]]
					NOT IN (SELECT [[value]] FROM json_each({:links}))
			)
	`).Bind(dbx.Params{
		"owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"blockIds": string(encodedBlocks), "rewritten": string(encodedRewritten), "links": string(encodedLinks),
		"placed": string(encodedPlaced),
	}).Row(&referenced)
	if err != nil {
		return err
	}
	if referenced != 0 {
		return graphInputError(
			errorCodeGraphReferenceInvalid,
			"Delete the linked database container first.",
		)
	}
	return nil
}

// rejectLinkedDataSourceDetach keeps a source in its database while another
// database links it through that database.
func rejectLinkedDataSourceDetach(app core.App, containerID, dataSourceID string) error {
	var linked int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_container_sources}}
		WHERE [[source_container_id]] = {:containerId} AND [[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{"containerId": containerID, "dataSourceId": dataSourceID}).Row(&linked); err != nil {
		return err
	}
	if linked != 0 {
		return graphInputError(errorCodeGraphReferenceInvalid, "Delete the linked database container first.")
	}
	return nil
}

// databaseContainerBlocksAmong are the blocks among those deleted that have
// a database's records to go — a database's block, or one a database's
// records name — in their order. Only they are cleaned up one by one
// (2026-11-08: five statements for each block, a 20,100-block deletion ran
// 100,000 of them, for blocks with none, while it held the one write
// connection).
func databaseContainerBlocksAmong(app core.App, blockIDs []string) ([]string, error) {
	encoded, err := json.Marshal(blockIDs)
	if err != nil {
		return nil, err
	}
	containers := []string{}
	err = app.DB().NewQuery(`
		SELECT [[gone]].[[value]] FROM json_each({:ids}) AS [[gone]]
		WHERE EXISTS (SELECT 1 FROM {{leafen_blocks}} WHERE [[id]] = [[gone]].[[value]] AND [[type]] = {:database})
			OR EXISTS (SELECT 1 FROM {{leafen_database_containers}} WHERE [[id]] = [[gone]].[[value]])
			OR EXISTS (SELECT 1 FROM {{leafen_database_container_views}} WHERE [[container_id]] = [[gone]].[[value]])
			OR EXISTS (SELECT 1 FROM {{leafen_database_container_sources}} WHERE [[container_id]] = [[gone]].[[value]])
		ORDER BY [[gone]].[[key]]
	`).Bind(dbx.Params{"ids": string(encoded), "database": graphBlockTypeInlineDatabase}).Column(&containers)
	return containers, err
}
