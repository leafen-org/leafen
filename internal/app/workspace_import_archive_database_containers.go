package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

func validateWorkspaceImportArchiveDatabaseContainers(db workspaceImportArchiveReferenceValidator) error {
	var containers, inlineDatabases int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM entities WHERE kind = ?`, workspaceExportItemDatabaseContainer,
	).Scan(&containers); err != nil {
		return workspaceImportStorageError(err)
	}
	// A database page's body holds databases as a page's does, in its trash
	// too.
	if err := db.QueryRow(`
		SELECT (
			SELECT COUNT(*) FROM entities
			WHERE kind IN (?, ?) AND json_extract(value_json, '$.type') = 'inline_database'
				AND COALESCE(json_extract(value_json, '$.content.dataSourceId'), '') != ''
		) + (
			SELECT COUNT(*) FROM entities AS trash,
				json_each(trash.value_json, '$.snapshot.blocks') AS block
			WHERE trash.kind IN (?, ?) AND json_extract(block.value, '$.type') = 'inline_database'
				AND COALESCE(json_extract(block.value, '$.content.dataSourceId'), '') != ''
		)
	`, workspaceExportItemBlock, workspaceExportItemRowPageBlock,
		workspaceExportItemBlockTrash, workspaceExportItemRowBlockTrash).Scan(&inlineDatabases); err != nil {
		return workspaceImportStorageError(err)
	}
	if containers != inlineDatabases {
		return workspaceImportInputError()
	}
	var distinctContainers int
	if err := db.QueryRow(`
		SELECT COUNT(DISTINCT source_id) FROM entities WHERE kind = ?
	`, workspaceExportItemDatabaseContainer).Scan(&distinctContainers); err != nil {
		return workspaceImportStorageError(err)
	}
	if distinctContainers != containers {
		return workspaceImportInputError()
	}
	rows, err := db.Query(
		`SELECT value_json FROM entities WHERE kind = ? ORDER BY sequence`,
		workspaceExportItemDatabaseContainer,
	)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return workspaceImportInputError()
		}
		value := archiveDatabaseContainerValue{}
		if json.Unmarshal([]byte(encoded), &value) != nil ||
			!validWorkspaceImportArchiveDatabaseContainerIdentity(value) {
			return workspaceImportInputError()
		}
		if err := validateWorkspaceImportArchiveDatabaseContainer(db, value); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return workspaceImportStorageError(err)
	}
	return nil
}

func validWorkspaceImportArchiveDatabaseContainerIdentity(value archiveDatabaseContainerValue) bool {
	validID := func(id string) bool {
		return id != "" && strings.TrimSpace(id) == id && len(id) <= maxGraphIdentifierLength
	}
	if !validID(value.ID) || value.BlockID != value.ID || !validID(value.PageID) ||
		value.Position == "" || (value.Kind != databaseContainerKindSource && value.Kind != databaseContainerKindLinked) ||
		len(value.DataSourceIDs) == 0 {
		return false
	}
	seenSources := map[string]struct{}{}
	for _, id := range value.DataSourceIDs {
		if !validID(id) {
			return false
		}
		if _, exists := seenSources[id]; exists {
			return false
		}
		seenSources[id] = struct{}{}
	}
	if value.ParentBlockID != nil && !validID(*value.ParentBlockID) {
		return false
	}
	if value.Kind == databaseContainerKindSource {
		if value.SourceContainerID != nil && *value.SourceContainerID != "" {
			return false
		}
	} else if value.SourceContainerID == nil || !validID(*value.SourceContainerID) || *value.SourceContainerID == value.ID {
		return false
	}
	seenViews := map[string]struct{}{}
	for _, id := range value.ViewIDs {
		if !validID(id) {
			return false
		}
		if _, exists := seenViews[id]; exists {
			return false
		}
		seenViews[id] = struct{}{}
	}
	return true
}

func validateWorkspaceImportArchiveDatabaseContainer(
	db workspaceImportArchiveReferenceValidator,
	value archiveDatabaseContainerValue,
) error {
	if _, err := workspaceImportArchiveHostReference(db, value.PageID); err != nil {
		return err
	}
	for _, dataSourceID := range value.DataSourceIDs {
		if _, err := workspaceImportArchiveReference(db, dataSourceID, workspaceExportItemDataSource); err != nil {
			return err
		}
	}
	if value.ParentBlockID != nil {
		if _, err := workspaceImportArchiveOptionalLiveOrTrashBlockReference(db, *value.ParentBlockID); err != nil {
			return err
		}
	}
	if value.SourceContainerID != nil {
		if _, err := workspaceImportArchiveOptionalLiveOrTrashBlockReference(db, *value.SourceContainerID); err != nil {
			return err
		}
	}
	for _, viewID := range value.ViewIDs {
		if _, err := workspaceImportArchiveReference(db, viewID, workspaceExportItemView); err != nil {
			return err
		}
	}

	block, trashed, err := workspaceImportArchiveDatabaseContainerBlock(db, value.BlockID, value.PageID)
	if err != nil || block.ID != value.ID || block.Type != "inline_database" ||
		(!trashed && block.Position != value.Position) ||
		!sameWorkspaceImportArchiveOptionalID(block.ParentID, value.ParentBlockID) {
		return workspaceImportInputError()
	}
	if validateDatabaseContainerBlockShape(block.Content) != nil {
		return workspaceImportInputError()
	}
	content, err := parseDatabaseContainerBlockContent(block.Content)
	if err != nil || !slices.Equal(content.memberDataSourceIDs(), value.DataSourceIDs) ||
		content.LinkedDatabaseSource != (value.Kind == databaseContainerKindLinked) {
		return workspaceImportInputError()
	}
	expectedSourceContainerID := ""
	if value.SourceContainerID != nil {
		expectedSourceContainerID = *value.SourceContainerID
	}
	if content.SourceContainerID != expectedSourceContainerID {
		return workspaceImportInputError()
	}
	// Every linked member must come from a source database in the archive.
	for dataSourceID, sourceContainerID := range content.linkedSourceContainers() {
		var sourceJSON string
		if err := db.QueryRow(`
			SELECT value_json FROM entities WHERE kind = ? AND source_id = ?
		`, workspaceExportItemDatabaseContainer, sourceContainerID).Scan(&sourceJSON); err != nil {
			return workspaceImportInputError()
		}
		source := archiveDatabaseContainerValue{}
		if json.Unmarshal([]byte(sourceJSON), &source) != nil || source.Kind != databaseContainerKindSource ||
			!slices.Contains(source.DataSourceIDs, dataSourceID) {
			return workspaceImportInputError()
		}
	}
	return validateWorkspaceImportArchiveDatabaseContainerViews(db, value, content, trashed)
}

func workspaceImportArchiveDatabaseContainerBlock(
	db workspaceImportArchiveReferenceValidator,
	blockID, pageID string,
) (archiveBlockValue, bool, error) {
	var blockJSON string
	err := db.QueryRow(`
		SELECT value_json FROM entities
		WHERE kind = ? AND source_id = ? AND page_id = ?
	`, workspaceExportItemBlock, blockID, pageID).Scan(&blockJSON)
	if err == nil {
		block := archiveBlockValue{}
		if json.Unmarshal([]byte(blockJSON), &block) != nil {
			return archiveBlockValue{}, false, workspaceImportInputError()
		}
		return block, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return archiveBlockValue{}, false, workspaceImportStorageError(err)
	}
	// A database in a database page's body is a block of that page's body.
	err = db.QueryRow(`
		SELECT value_json FROM entities
		WHERE kind = ? AND source_id = ? AND json_extract(value_json, '$.rowId') = ?
	`, workspaceExportItemRowPageBlock, blockID, pageID).Scan(&blockJSON)
	if err == nil {
		row := archiveRowPageBlockValue{}
		if json.Unmarshal([]byte(blockJSON), &row) != nil {
			return archiveBlockValue{}, false, workspaceImportInputError()
		}
		var parentID *string
		if row.ParentID != "" {
			parent := row.ParentID
			parentID = &parent
		}
		return archiveBlockValue{
			ID: row.ID, PageID: pageID, ParentID: parentID, Position: row.Position,
			Type: row.Type, Content: row.Content,
		}, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return archiveBlockValue{}, false, workspaceImportStorageError(err)
	}
	// A database in the trash is a block of a trash entry, a page's or a
	// database page's, whose database page is the database's.
	var trashPageID, storedJSON string
	err = db.QueryRow(`
		SELECT json_extract(trash.value_json, CASE WHEN trash.kind = ? THEN '$.rowId' ELSE '$.pageId' END),
			json(block.value)
		FROM entities AS trash, json_each(trash.value_json, '$.snapshot.blocks') AS block
		WHERE trash.kind IN (?, ?) AND json_extract(block.value, '$.id') = ?
		LIMIT 2
	`, workspaceExportItemRowBlockTrash, workspaceExportItemBlockTrash, workspaceExportItemRowBlockTrash,
		blockID).Scan(&trashPageID, &storedJSON)
	if err != nil || trashPageID != pageID {
		return archiveBlockValue{}, false, workspaceImportInputError()
	}
	stored := recoverableBlockTrashBlock{}
	if json.Unmarshal([]byte(storedJSON), &stored) != nil {
		return archiveBlockValue{}, false, workspaceImportInputError()
	}
	var parentID *string
	if stored.ParentID != "" {
		parent := stored.ParentID
		parentID = &parent
	}
	return archiveBlockValue{
		ID: stored.ID, PageID: pageID, ParentID: parentID, Position: stored.Position,
		Type: stored.Type, Content: stored.Content,
	}, true, nil
}

func sameWorkspaceImportArchiveOptionalID(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// validateWorkspaceImportArchiveDatabaseContainerViews accepts, member by
// member, the views of an own source and the views the database owns for a
// linked source; a linked database from an archive written before databases
// owned views shows the views of its source instead. A database whose block
// is in the trash took the views it owns there: they travel in its trash
// entry (the blockTrash or rowBlockTrash snapshot's views, as block_trash.go
// keeps them, and come back with its block), so its viewIds list none of
// them, as its export writes it (workspaceExportDatabaseContainerItemSQL),
// and those carried views make it one that owns its views.
func validateWorkspaceImportArchiveDatabaseContainerViews(
	db workspaceImportArchiveReferenceValidator,
	value archiveDatabaseContainerValue,
	content databaseContainerBlockContent,
	trashed bool,
) error {
	owned, err := workspaceImportArchiveViewIDs(db, workspaceImportArchiveViewContainer+` = ?`, value.ID)
	if err != nil {
		return err
	}
	ownsViews := len(owned) > 0
	if trashed && !ownsViews {
		if ownsViews, err = workspaceImportArchiveTrashCarriesViews(db, value.ID); err != nil {
			return err
		}
	}
	legacyLinked := value.Kind == databaseContainerKindLinked && !ownsViews
	linked := content.linkedSourceContainers()
	expected := []string{}
	for _, dataSourceID := range value.DataSourceIDs {
		predicate := `json_extract(value_json, '$.dataSourceId') = ? AND ` + workspaceImportArchiveViewContainer + ` = ?`
		containerID := value.ID
		if _, isLinked := linked[dataSourceID]; !isLinked || legacyLinked {
			containerID = ""
		}
		views, err := workspaceImportArchiveViewIDs(db, predicate, dataSourceID, containerID)
		if err != nil {
			return err
		}
		expected = append(expected, views...)
	}
	if !slices.Equal(expected, value.ViewIDs) {
		return workspaceImportInputError()
	}
	return nil
}

func workspaceImportArchiveViewIDs(
	db workspaceImportArchiveReferenceValidator, predicate string, arguments ...any,
) ([]string, error) {
	rows, err := db.Query(`
		SELECT source_id FROM entities
		WHERE kind = ? AND `+predicate+`
		ORDER BY json_extract(value_json, '$.position'), source_id
	`, append([]any{workspaceExportItemView}, arguments...)...)
	if err != nil {
		return nil, workspaceImportStorageError(err)
	}
	defer rows.Close()
	views := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return nil, workspaceImportInputError()
		}
		views = append(views, id)
	}
	if err := rows.Err(); err != nil {
		return nil, workspaceImportStorageError(err)
	}
	return views, nil
}

// workspaceImportArchiveTrashCarriesViews reports whether the trash entry of
// a database in the trash carries views it owns.
func workspaceImportArchiveTrashCarriesViews(
	db workspaceImportArchiveReferenceValidator, containerID string,
) (bool, error) {
	var carries bool
	if err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM entities AS trash, json_each(trash.value_json, '$.snapshot.views') AS carried
			WHERE trash.kind IN (?, ?) AND json_extract(carried.value, '$.containerId') = ?
		)
	`, workspaceExportItemBlockTrash, workspaceExportItemRowBlockTrash, containerID).Scan(&carries); err != nil {
		return false, workspaceImportStorageError(err)
	}
	return carries, nil
}
