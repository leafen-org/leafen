package app

import (
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
)

// A linked database owns its views, and the archive carries them with it.

type workspaceImportArchiveLinkedView struct {
	SourceID string
	Value    archiveViewValue
}

// workspaceImportArchiveLinkedViewSources returns the archived views a linked
// database block owns.
func workspaceImportArchiveLinkedViewSources(db *sql.DB, blockID string) ([]workspaceImportArchiveLinkedView, error) {
	rows, err := db.Query(`
		SELECT source_id, value_json FROM entities
		WHERE kind = ? AND `+workspaceImportArchiveViewContainer+` = ?
		ORDER BY json_extract(value_json, '$.position'), source_id
	`, workspaceExportItemView, blockID)
	if err != nil {
		return nil, workspaceImportStorageError(err)
	}
	defer rows.Close()
	views := []workspaceImportArchiveLinkedView{}
	for rows.Next() {
		var sourceID, encoded string
		view := workspaceImportArchiveLinkedView{}
		if rows.Scan(&sourceID, &encoded) != nil || json.Unmarshal([]byte(encoded), &view.Value) != nil {
			return nil, workspaceImportInputError()
		}
		view.SourceID = sourceID
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, workspaceImportStorageError(err)
	}
	return views, nil
}

// workspaceImportArchiveLinkedViewSelection returns the imported view a
// linked database block selects among the views it owns.
func workspaceImportArchiveLinkedViewSelection(
	db *sql.DB, block archiveBlockValue, mappedBlockID string,
) ([]graphOperation, string, error) {
	operations, err := workspaceImportArchiveLinkedViewOperations(db, block, mappedBlockID)
	if err != nil || len(operations) == 0 {
		return nil, "", err
	}
	return operations, operations[0].View.ID, nil
}

// workspaceImportArchiveLinkedViewOperations maps a linked database block's
// views for import with the view the block selects first, so the block and
// that view land in one change.
func workspaceImportArchiveLinkedViewOperations(
	db *sql.DB, block archiveBlockValue, mappedBlockID string,
) ([]graphOperation, error) {
	if block.Type != graphBlockTypeInlineDatabase {
		return nil, nil
	}
	content, err := parseDatabaseContainerBlockContent(block.Content)
	if err != nil || !content.LinkedDatabaseSource {
		return nil, err
	}
	sources, err := workspaceImportArchiveLinkedViewSources(db, block.ID)
	if err != nil {
		return nil, err
	}
	dataSourceID, err := workspaceImportArchiveReference(db, content.DataSourceID, workspaceExportItemDataSource)
	if err != nil {
		return nil, err
	}
	operations := make([]graphOperation, 0, len(sources))
	selected := -1
	for _, source := range sources {
		id, err := workspaceImportArchiveReference(db, source.SourceID, workspaceExportItemView)
		if err != nil {
			return nil, err
		}
		config, err := rewriteWorkspaceImportArchiveJSON(db, source.Value.Config)
		if err != nil {
			return nil, err
		}
		if source.SourceID == content.ViewID {
			selected = len(operations)
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: id, DataSourceID: dataSourceID, ContainerID: mappedBlockID, Type: source.Value.Type,
			Name: source.Value.Name, Position: source.Value.Position, Config: config,
		}})
	}
	if len(operations) == 0 {
		return nil, workspaceImportInputError()
	}
	if selected < 0 {
		selected = 0
	}
	operations[0], operations[selected] = operations[selected], operations[0]
	return operations, nil
}

// workspaceImportArchiveLinkedViewItems are the views a linked database
// block brings, to come right after it on its page or its database page's
// body (2026-11-17: a database page's linked database came without them).
func workspaceImportArchiveLinkedViewItems(
	db *sql.DB, encoded string, block workspaceImportStagedItem,
) ([]workspaceImportStagedItem, error) {
	value := archiveBlockValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil || block.Operation == nil || block.Operation.Block == nil {
		return nil, workspaceImportInputError()
	}
	views, err := workspaceImportArchiveLinkedViewOperations(db, value, block.Operation.Block.ID)
	if err != nil {
		return nil, err
	}
	items := make([]workspaceImportStagedItem, 0, len(views))
	for index := range views {
		items = append(items, workspaceImportStagedItem{PageID: block.PageID, Operation: &views[index]})
	}
	return items, nil
}

// workspaceImportArchiveLinkedBlockContent points an imported linked block at
// the view it selects among the views it owns.
func workspaceImportArchiveLinkedBlockContent(content json.RawMessage, viewID string) (json.RawMessage, error) {
	value := map[string]any{}
	if json.Unmarshal(content, &value) != nil {
		return nil, workspaceImportInputError()
	}
	value["viewId"] = viewID
	return json.Marshal(value)
}

// workspaceImportArchiveLinkedPair keeps a linked database block and the view
// it selects in one chunk and one change.
func workspaceImportArchiveLinkedPair(block, view *graphOperation) bool {
	return block != nil && view != nil && block.Kind == graphOperationKindUpsertBlock && block.Block != nil &&
		block.Block.Type == graphBlockTypeInlineDatabase && view.Kind == graphOperationKindUpsertView &&
		view.View != nil && view.View.ContainerID == block.Block.ID
}

// workspaceImportArchiveTrashLinkedViews maps the views that travel with
// trashed linked databases.
func workspaceImportArchiveTrashLinkedViews(
	db *sql.DB, source recoverableBlockTrashSnapshot, mapped *recoverableBlockTrashSnapshot, blockIDs map[string]string,
) error {
	carried := map[string][]graphDatabaseView{}
	for _, view := range source.Views {
		carried[view.ContainerID] = append(carried[view.ContainerID], view)
	}
	for index, block := range source.Blocks {
		content, linked := recoverableLinkedDatabaseContainer(block)
		if !linked {
			if err := workspaceImportArchiveTrashOwnedViews(db, block, index, carried[block.ID], mapped, blockIDs); err != nil {
				return err
			}
			continue
		}
		mappedBlockID := blockIDs[block.ID]
		dataSourceID, err := workspaceImportArchiveReference(db, content.DataSourceID, workspaceExportItemDataSource)
		if err != nil {
			return err
		}
		selected := ""
		for _, view := range carried[block.ID] {
			config, err := rewriteWorkspaceImportArchiveJSON(db, view.Config)
			if err != nil {
				return err
			}
			id := nativeLinkedContainerViewID(mappedBlockID, view.ID)
			if view.ID == content.ViewID || selected == "" {
				selected = id
			}
			mapped.Views = append(mapped.Views, graphDatabaseView{
				ID: id, DataSourceID: dataSourceID, ContainerID: mappedBlockID, Type: view.Type,
				Name: view.Name, Position: view.Position, Config: config,
			})
		}
		if selected == "" {
			return workspaceImportInputError()
		}
		rewritten, err := workspaceImportArchiveLinkedBlockContent(mapped.Blocks[index].Content, selected)
		if err != nil {
			return err
		}
		mapped.Blocks[index].Content = rewritten
	}
	return nil
}

// workspaceImportArchiveTrashOwnedViews maps the views a trashed database owns
// for the sources it links, keeping its selection when it chose one of them.
func workspaceImportArchiveTrashOwnedViews(
	db *sql.DB, block recoverableBlockTrashBlock, index int, views []graphDatabaseView,
	mapped *recoverableBlockTrashSnapshot, blockIDs map[string]string,
) error {
	content, _, database := recoverableDatabaseContainer(block)
	if !database || len(views) == 0 {
		return nil
	}
	mappedBlockID := blockIDs[block.ID]
	selected := ""
	for _, view := range views {
		dataSourceID, err := workspaceImportArchiveReference(db, view.DataSourceID, workspaceExportItemDataSource)
		if err != nil {
			return err
		}
		config, err := rewriteWorkspaceImportArchiveJSON(db, view.Config)
		if err != nil {
			return err
		}
		id := nativeLinkedContainerViewID(mappedBlockID, view.ID)
		if view.ID == content.ViewID {
			selected = id
		}
		mapped.Views = append(mapped.Views, graphDatabaseView{
			ID: id, DataSourceID: dataSourceID, ContainerID: mappedBlockID, Type: view.Type,
			Name: view.Name, Position: view.Position, Config: config,
		})
	}
	if selected == "" {
		return nil
	}
	rewritten, err := workspaceImportArchiveLinkedBlockContent(mapped.Blocks[index].Content, selected)
	if err != nil {
		return err
	}
	mapped.Blocks[index].Content = rewritten
	return nil
}

// nativeLinkedContainerViewID is the id a linked database's view takes, the
// same for the same database and view each time.
func nativeLinkedContainerViewID(containerID, viewID string) string {
	return uuid.NewSHA1(
		uuid.NameSpaceOID, []byte("leafen:linked-container-view:v1\x00"+containerID+"\x00"+viewID),
	).String()
}
