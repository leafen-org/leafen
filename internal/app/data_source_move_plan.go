package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// These operations move a data source between databases on one page: the original database lets it go, the destination takes it, and
// databases that linked it through the original follow it. The public API,
// the editor and the cross-page move job share them.

type dataSourceMoveLinker struct {
	ContainerID string `db:"container_id"`
	PageID      string `db:"page_id"`
}

const maxDataSourceMoveLinkers = 100

// dataSourceMoveLinkers lists the other live databases that link the source
// through its original database; the move points them at the destination so
// no link runs through a database that no longer owns the source.
func dataSourceMoveLinkers(app core.App, originID, sourceID, targetID string) ([]dataSourceMoveLinker, error) {
	linkers := []dataSourceMoveLinker{}
	if err := app.DB().NewQuery(`
		SELECT [[membership]].[[container_id]], [[container]].[[page_id]]
		FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_database_containers}} AS [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
		WHERE [[membership]].[[source_container_id]] = {:originId} AND [[membership]].[[data_source_id]] = {:sourceId}
			AND [[membership]].[[container_id]] != {:targetId}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]]
			)
		ORDER BY [[container]].[[page_id]], [[membership]].[[container_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"originId": originID, "sourceId": sourceID, "targetId": targetID, "limit": maxDataSourceMoveLinkers + 1,
	}).All(&linkers); err != nil {
		return nil, err
	}
	if len(linkers) > maxDataSourceMoveLinkers {
		return nil, notionDataSourceMoveInvalid("The data source is linked from too many databases to move.")
	}
	return linkers, nil
}

// dataSourceMoveLinkerOperations points the given databases' links to the
// source at its new database.
func dataSourceMoveLinkerOperations(
	app core.App, doc *crdt.Doc, pageID, ownerID string, linkers []dataSourceMoveLinker, sourceID, targetID string,
) ([]graphOperation, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, err
	}
	operations := []graphOperation{}
	for _, linker := range linkers {
		if linker.PageID != pageID {
			continue
		}
		entry, content, err := notionDataSourceMoveContainer(metadata, linker.ContainerID)
		if err != nil {
			return nil, err
		}
		linked := content.linkedMemberList()
		for index := range linked {
			if linked[index].DataSourceID == sourceID {
				linked[index].SourceContainerID = targetID
			}
		}
		block, err := notionDataSourceMoveBlock(app, entry, linker.ContainerID, pageID, ownerID,
			withDatabaseContainerMembers(entry.Content, content.ownDataSourceIDs(), linked))
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block})
	}
	return operations, nil
}

// dataSourceMoveSpec says where a source goes and whether its views go too.
type dataSourceMoveSpec struct {
	// TargetID is the destination database, or the block of a new database.
	TargetID string
	// NewDatabase makes the destination a new database at the end of its page.
	NewDatabase bool
	// MoveViews takes the source's views along instead of leaving them with
	// the original database as linked views.
	MoveViews bool
	// ViewID is the destination's new table view when the views stay behind.
	ViewID string
	// ViewName names that view: a move made in the app names it after the
	// source, as Notion's "다음으로 이동:" does, and one made through the API
	// leaves it never named, as Notion's PATCH does (both seen 2026-11-17: the
	// app's move read back as the source's title, or "Untitled" for a source
	// with none; the API's as "Untitled").
	ViewName string
	// Back takes the source back to TargetID, undoing a move of it alone.
	Back *dataSourceMoveBack
}

// dataSourceMoveOriginOperations lets the source leave the original
// database. When its views stay, the database keeps them as linked views of
// the moved source. It returns the positions of every view of the source on
// this page.
func dataSourceMoveOriginOperations(
	app core.App, doc *crdt.Doc, pageID, ownerID, originID, sourceID string, spec dataSourceMoveSpec,
) ([]graphOperation, []string, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, nil, err
	}
	entry, origin, err := notionDataSourceMoveContainer(metadata, originID)
	if err != nil {
		return nil, nil, err
	}
	own := origin.ownDataSourceIDs()
	if !slices.Contains(own, sourceID) {
		return nil, nil, errDatabaseRowPageConflict
	}
	remaining := slices.DeleteFunc(slices.Clone(own), func(id string) bool { return id == sourceID })
	linked := origin.linkedMemberList()
	if !spec.MoveViews {
		linked = append(linked, databaseContainerLinkedMember{DataSourceID: sourceID, SourceContainerID: spec.TargetID})
	} else if len(remaining)+len(linked) == 0 {
		return nil, nil, notionDataSourceMoveInvalid(
			"This is the database's only data source; move the database itself to take its views along.",
		)
	}
	content := withDatabaseContainerMembers(entry.Content, remaining, linked)
	views := []graphOperation{}
	positions := []string{}
	shown := []databaseViewAuthority{}
	shownIDs := map[string]string{}
	pageViews, err := loadDatabaseViewRecords(app, pageID)
	if err != nil {
		return nil, nil, err
	}
	for id, stored := range pageViews {
		if stored.DataSourceID != sourceID {
			if stored.ContainerID == originID || stored.ContainerID == "" && slices.Contains(remaining, stored.DataSourceID) {
				shown = append(shown, stored)
				shownIDs[stored.Position+"\x00"+id] = id
			}
			continue
		}
		positions = append(positions, stored.Position)
		if stored.ContainerID != "" || spec.MoveViews {
			continue
		}
		config, err := json.Marshal(stored.Config)
		if err != nil {
			return nil, nil, err
		}
		views = append(views, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: id, DataSourceID: sourceID, ContainerID: originID,
			Type: stored.Type, Name: stored.Name, Position: stored.Position, Config: config,
		}})
	}
	// A database whose selected view leaves with the source selects its first
	// remaining view.
	if selected, _ := content["viewId"].(string); spec.MoveViews && selected != "" {
		if pageViews[selected].DataSourceID == sourceID {
			keys := make([]string, 0, len(shownIDs))
			for key := range shownIDs {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			if len(keys) == 0 {
				return nil, nil, errDatabaseRowPageConflict
			}
			content["viewId"] = shownIDs[keys[0]]
		}
	}
	block, err := notionDataSourceMoveBlock(app, entry, originID, pageID, ownerID, content)
	if err != nil {
		return nil, nil, err
	}
	// Keep the order the same on every retry of this plan.
	slices.SortFunc(views, func(left, right graphOperation) int {
		return strings.Compare(left.View.ID, right.View.ID)
	})
	return append([]graphOperation{{Kind: graphOperationKindUpsertBlock, Block: block}}, views...), positions, nil
}

// dataSourceMoveTargetOperations makes the source the destination database's
// last own source. When the views stay behind it gets a new table view after
// the given view positions.
func dataSourceMoveTargetOperations(
	app core.App, doc *crdt.Doc, pageID, ownerID, sourceID string, spec dataSourceMoveSpec, positions []string,
) ([]graphOperation, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, err
	}
	entry, destination, err := notionDataSourceMoveContainer(metadata, spec.TargetID)
	if err != nil {
		return nil, err
	}
	if slices.Contains(destination.ownDataSourceIDs(), sourceID) {
		return nil, errDatabaseRowPageConflict
	}
	// A destination that already linked the source now owns it, so the views
	// it owned for the linked source become the source's own views.
	linked := slices.DeleteFunc(destination.linkedMemberList(), func(member databaseContainerLinkedMember) bool {
		return member.DataSourceID == sourceID
	})
	block, err := notionDataSourceMoveBlock(app, entry, spec.TargetID, pageID, ownerID,
		withDatabaseContainerMembers(entry.Content, append(destination.ownDataSourceIDs(), sourceID), linked))
	if err != nil {
		return nil, err
	}
	operations := []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: block}}
	released := []graphOperation{}
	positions = slices.Clone(positions)
	pageViews, err := loadDatabaseViewRecords(app, pageID)
	if err != nil {
		return nil, err
	}
	for id, stored := range pageViews {
		if stored.DataSourceID != sourceID {
			continue
		}
		positions = append(positions, stored.Position)
		if stored.ContainerID != spec.TargetID {
			continue
		}
		config, err := json.Marshal(stored.Config)
		if err != nil {
			return nil, err
		}
		released = append(released, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: id, DataSourceID: sourceID, Type: stored.Type, Name: stored.Name, Position: stored.Position, Config: config,
		}})
	}
	slices.SortFunc(released, func(left, right graphOperation) int {
		return strings.Compare(left.View.ID, right.View.ID)
	})
	operations = append(operations, released...)
	if spec.MoveViews {
		return operations, nil
	}
	position, err := notionDataSourceMoveViewPosition(positions)
	if err != nil {
		return nil, err
	}
	return append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
		ID: spec.ViewID, DataSourceID: sourceID, Type: graphViewTypeTable,
		Name: spec.ViewName, Position: position, Config: json.RawMessage(`{}`),
	}}), nil
}

// dataSourceMoveNewDatabaseOperations makes a new database at the end of the
// page that owns the source, showing its first moved view or a new table view.
// The caller places the block last in the page body once it is applied.
func dataSourceMoveNewDatabaseOperations(
	app core.App, pageID, ownerID, workspaceID, sourceID string, spec dataSourceMoveSpec, positions []string,
) ([]graphOperation, error) {
	afterID, err := lastTopLevelBlock(app, ownerID, pageID)
	if err != nil {
		return nil, err
	}
	operations := []graphOperation{}
	viewID := spec.ViewID
	if spec.MoveViews {
		first := ""
		pageViews, err := loadDatabaseViewRecords(app, pageID)
		if err != nil {
			return nil, err
		}
		for id, stored := range pageViews {
			key := stored.Position + "\x00" + id
			if stored.DataSourceID == sourceID && stored.ContainerID == "" && (first == "" || key < first) {
				first, viewID = key, id
			}
		}
		if first == "" {
			return nil, errDatabaseRowPageConflict
		}
	} else {
		position, err := notionDataSourceMoveViewPosition(positions)
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: viewID, DataSourceID: sourceID, Type: graphViewTypeTable,
			Name: spec.ViewName, Position: position, Config: json.RawMessage(`{}`),
		}})
	}
	content, err := json.Marshal(map[string]any{"dataSourceId": sourceID, "viewId": viewID})
	if err != nil {
		return nil, err
	}
	return append([]graphOperation{{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
		ID: spec.TargetID, AfterID: afterID, Position: "notion-data-source-database",
		Type: graphBlockTypeInlineDatabase, Content: content,
	}}}, operations...), nil
}

func notionDataSourceMoveContainer(
	metadata map[string]structuralBlockMetadata, containerID string,
) (structuralBlockMetadata, databaseContainerBlockContent, error) {
	entry, exists := metadata[containerID]
	if !exists || entry.Type != graphBlockTypeInlineDatabase {
		return structuralBlockMetadata{}, databaseContainerBlockContent{}, errDatabaseRowPageConflict
	}
	encoded, err := json.Marshal(entry.Content)
	if err != nil {
		return structuralBlockMetadata{}, databaseContainerBlockContent{}, err
	}
	content, err := parseDatabaseContainerBlockContent(encoded)
	if err != nil {
		return structuralBlockMetadata{}, databaseContainerBlockContent{}, errDatabaseRowPageConflict
	}
	return entry, content, nil
}

func notionDataSourceMoveBlock(
	app core.App, entry structuralBlockMetadata, containerID, pageID, ownerID string, content map[string]any,
) (*graphBlock, error) {
	var position string
	if err := app.DB().NewQuery(`
		SELECT [[position]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:id}
	`).Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "id": containerID}).Row(&position); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	block := &graphBlock{ID: containerID, Position: position, Type: graphBlockTypeInlineDatabase, Content: encoded}
	if entry.ParentID != "" {
		parentID := entry.ParentID
		block.ParentID = &parentID
	}
	return block, nil
}

// notionDataSourceMoveViewPosition places the new table view after every
// existing view of the source.
func notionDataSourceMoveViewPosition(positions []string) (string, error) {
	if len(positions) == 0 {
		return "a00000000", nil
	}
	return databasePositionAfter(slices.Max(positions))
}
