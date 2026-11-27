package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Views of a linked database belong to that database: they live in the page
// that holds the linked block, name it in containerId and show only its
// source, so editing them never changes the original database. A view without
// containerId belongs to the own database that holds its source on its page.

// loadGraphViewContainers returns the database block content each view
// operation names, from the same operation batch or the stored page.
func loadGraphViewContainers(
	app core.App, owner, pageID string, operations []graphOperation,
) (map[string]databaseContainerBlockContent, error) {
	containers := map[string]databaseContainerBlockContent{}
	wanted := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertView && operation.View != nil && operation.View.ContainerID != "" {
			wanted[operation.View.ContainerID] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return containers, nil
	}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil ||
			operation.Block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		if _, needed := wanted[operation.Block.ID]; !needed {
			continue
		}
		content, err := parseDatabaseContainerBlockContent(operation.Block.Content)
		if err != nil {
			return nil, graphInputError(errorCodeGraphInvalid, "Check the inline database content.")
		}
		containers[operation.Block.ID] = content
		delete(wanted, operation.Block.ID)
	}
	if len(wanted) == 0 {
		return containers, nil
	}
	ids := make([]string, 0, len(wanted))
	for id := range wanted {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	params := dbx.Params{"owner": owner, "pageId": pageID, "type": graphBlockTypeInlineDatabase}
	placeholders := make([]string, len(ids))
	for index, id := range ids {
		key := fmt.Sprintf("container%d", index)
		params[key] = id
		placeholders[index] = "{:" + key + "}"
	}
	rows := []struct {
		ID      string `db:"id"`
		Content string `db:"content"`
	}{}
	if err := app.DB().NewQuery(fmt.Sprintf(`
		SELECT [[id]], [[content]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[type]] = {:type}
			AND [[id]] IN (%s)
	`, strings.Join(placeholders, ", "))).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		content, err := parseDatabaseContainerBlockContent(json.RawMessage(row.Content))
		if err != nil {
			return nil, err
		}
		containers[row.ID] = content
	}
	return containers, nil
}

// validateGraphViewContainer checks where a view may live: a view owned by a
// linked database shows that database's source from its own page; any other
// view stays with its source on the source's page.
func validateGraphViewContainer(projection graphProjection, view *graphDatabaseView, pageID string) error {
	source, sourceExists := projection.sources[view.DataSourceID]
	if view.ContainerID == "" {
		if !sourceExists || source.PageID != pageID {
			return graphInputError(errorCodeGraphReferenceInvalid, "The view's data source was not found.")
		}
		return nil
	}
	block, blockExists := projection.blocks[view.ContainerID]
	content, contentExists := projection.containers[view.ContainerID]
	_, linkedMember := content.linkedSourceContainers()[view.DataSourceID]
	if !sourceExists || !blockExists || !contentExists || block.PageID != pageID ||
		block.Type != graphBlockTypeInlineDatabase || !linkedMember {
		return graphInputError(errorCodeGraphReferenceInvalid, "Check the linked database's views.")
	}
	return nil
}

// graphViewShownByContainer reports whether a database block may select the
// view: linked databases select their own views, own databases the views of
// their member sources on the same page.
func graphViewShownByContainer(
	view graphEntityReference, blockID, pageID string, content databaseContainerBlockContent,
) bool {
	// A dashboard widget shows inside its dashboard, never as a tab.
	if view.PageID != pageID || dashboardWidgetOwner([]byte(view.Config)) != "" {
		return false
	}
	if view.ContainerID != "" {
		// A database owns the views of the sources it links.
		_, linked := content.linkedSourceContainers()[view.DataSourceID]
		return view.ContainerID == blockID && linked
	}
	if content.LinkedDatabaseSource {
		return false
	}
	for _, id := range content.dataSourceIDs() {
		if id == view.DataSourceID {
			return true
		}
	}
	return false
}

// linkedDatabaseViewsOwnedBy reads the stored views that the given linked
// database blocks own on this page.
func linkedDatabaseViewsOwnedBy(app core.App, pageID string, owners map[string]struct{}) ([]graphDatabaseView, error) {
	views := []graphDatabaseView{}
	if len(owners) == 0 {
		return views, nil
	}
	rows := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
		ContainerID  string `db:"container_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[container_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_views}} WHERE [[page_id]] = {:pageId} AND [[container_id]] != ''
		ORDER BY [[container_id]], [[position]], [[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, owned := owners[row.ContainerID]; !owned {
			continue
		}
		config, err := databaseRecordConfig(row.Config)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			return nil, err
		}
		views = append(views, graphDatabaseView{
			ID: row.ID, DataSourceID: row.DataSourceID, ContainerID: row.ContainerID,
			Type: row.Type, Name: row.Name, Position: row.Position, Config: encoded,
		})
	}
	return views, nil
}

// recoverableLinkedDatabaseViews captures the views databases in a trashed
// block tree own for the sources they link, so a restore brings them back with
// their blocks.
func recoverableLinkedDatabaseViews(
	app core.App, pageID string, blocks []recoverableBlockTrashBlock,
) ([]graphDatabaseView, error) {
	owners := map[string]struct{}{}
	for _, block := range blocks {
		if _, _, database := recoverableDatabaseContainer(block); database {
			owners[block.ID] = struct{}{}
		}
	}
	views, err := linkedDatabaseViewsOwnedBy(app, pageID, owners)
	if err != nil || len(views) == 0 {
		return nil, err
	}
	return views, nil
}

// restoreRecoverableLinkedDatabaseViews returns the captured views after the
// blocks that own them, named as the trash entry kept them.
func restoreRecoverableLinkedDatabaseViews(snapshot recoverableBlockTrashSnapshot) []graphOperation {
	operations := make([]graphOperation, 0, len(snapshot.Views))
	for index := range snapshot.Views {
		view := snapshot.Views[index]
		view.Config = append(json.RawMessage(nil), view.Config...)
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &view})
	}
	return operations
}

// deleteOrphanedLinkedDatabaseViews removes the views whose linked database
// block left the page in this mutation.
func deleteOrphanedLinkedDatabaseViews(
	app core.App, doc *crdt.Doc, owner, workspaceID, pageID, actorID string, operations []graphOperation,
) error {
	deletesBlocks := false
	for _, operation := range operations {
		if operation.Kind == graphOperationKindDeleteBlock {
			deletesBlocks = true
			break
		}
	}
	if !deletesBlocks {
		return nil
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return err
	}
	owned := []struct {
		ID          string `db:"id"`
		ContainerID string `db:"container_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[container_id]] FROM {{leafen_database_views}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[container_id]] != ''
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID}).All(&owned); err != nil {
		return err
	}
	orphaned := []graphOperation{}
	for _, view := range owned {
		if _, live := metadata[view.ContainerID]; live {
			continue
		}
		orphaned = append(orphaned, graphOperation{
			Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: view.ID},
		})
	}
	if len(orphaned) == 0 {
		return nil
	}
	sort.Slice(orphaned, func(left, right int) bool {
		return orphaned[left].ViewDeletion.ID < orphaned[right].ViewDeletion.ID
	})
	return applyDatabaseGraphMutation(app, doc, owner, workspaceID, pageID, actorID, orphaned)
}

// moveLinkedDatabaseViews carries the views a linked database owns to the
// page its block moves to.
func moveLinkedDatabaseViews(
	app core.App, owner, workspaceID, fromPageID, toPageID, blockID string,
) error {
	views, err := linkedDatabaseViewsOwnedBy(app, fromPageID, map[string]struct{}{blockID: {}})
	if err != nil || len(views) == 0 {
		return err
	}
	sources := map[string]struct{}{}
	for _, view := range views {
		for _, table := range []string{"leafen_database_views", "leafen_database_view_property_refs"} {
			column := "[[id]]"
			if table == "leafen_database_view_property_refs" {
				column = "[[view_id]]"
			}
			if _, err := app.DB().NewQuery(`
				UPDATE {{` + table + `}} SET [[page_id]]={:toPageId}
				WHERE [[owner]]={:owner} AND [[page_id]]={:fromPageId} AND ` + column + `={:id}
			`).Bind(dbx.Params{
				"toPageId": toPageID, "fromPageId": fromPageID, "owner": owner, "id": view.ID,
			}).Execute(); err != nil {
				return err
			}
		}
		sources[view.DataSourceID] = struct{}{}
	}
	sourceIDs := make([]string, 0, len(sources))
	for sourceID := range sources {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Strings(sourceIDs)
	for _, sourceID := range sourceIDs {
		if err := refreshDatabaseContainerViewsForDataSource(app, owner, workspaceID, sourceID); err != nil {
			return err
		}
	}
	return nil
}

// retainedDatabaseViewOwner is the owner a view written without one keeps:
// the database block that owns it while that block still links the view's
// source, as when the source moves into that database, and none otherwise.
func retainedDatabaseViewOwner(app core.App, doc *crdt.Doc, pageID, viewID, dataSourceID string) (string, error) {
	stored, exists, err := loadDatabaseViewRecord(app, pageID, viewID)
	if err != nil || !exists || stored.ContainerID == "" {
		return "", err
	}
	if doc == nil {
		// A change made without the page's blocks keeps the owner.
		return stored.ContainerID, nil
	}
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return "", err
	}
	entry, present := metadata[stored.ContainerID]
	if !present || entry.Type != graphBlockTypeInlineDatabase {
		return "", nil
	}
	encoded, err := json.Marshal(entry.Content)
	if err != nil {
		return "", err
	}
	if content, err := parseDatabaseContainerBlockContent(encoded); err == nil {
		if _, linked := content.linkedSourceContainers()[dataSourceID]; linked {
			return stored.ContainerID, nil
		}
	}
	return "", nil
}
