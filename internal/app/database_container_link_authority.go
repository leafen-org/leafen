package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type linkedDatabaseReferenceRow struct {
	SourceContainerID string `db:"source_container_id"`
	OwnerID           string `db:"owner"`
	WorkspaceID       string `db:"workspace_id"`
	SourcePageID      string `db:"source_page_id"`
	DataSourceID      string `db:"data_source_id"`
}

type linkedDatabaseReadScope struct {
	sourceContainerPages map[string]string
	// relationSourcePages are the readable other-page sources this page's
	// relation properties, and its linked views' sources', target, by source.
	relationSourcePages map[string]string
	// trashed is an inline database alone in the trash the read was asked to carry (content_graph_trashed_database.go).
	trashed *graphTrashedDatabase
}

type linkedDatabaseReadCandidate struct {
	SourceContainerID string `db:"source_container_id"`
	SourcePageID      string `db:"source_page_id"`
}

func authorizeLinkedDatabaseReadScope(
	app core.App,
	principalID string,
	current pageAuthorization,
	pageID string,
) (linkedDatabaseReadScope, error) {
	candidates := []linkedDatabaseReadCandidate{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[membership]].[[source_container_id]], [[source]].[[page_id]] AS [[source_page_id]]
		FROM {{leafen_database_container_sources}} AS [[membership]]
		JOIN {{leafen_database_containers}} AS [[linked]] ON [[linked]].[[id]] = [[membership]].[[container_id]]
		JOIN {{leafen_database_containers}} AS [[source]]
			ON [[source]].[[id]] = [[membership]].[[source_container_id]]
			AND [[source]].[[owner]] = [[linked]].[[owner]]
			AND [[source]].[[workspace_id]] = [[linked]].[[workspace_id]]
			AND [[source]].[[kind]] = 'source'
		WHERE [[linked]].[[owner]] = {:owner} AND [[linked]].[[workspace_id]] = {:workspaceId}
			AND [[linked]].[[page_id]] = {:pageId} AND [[membership]].[[source_container_id]] != ''
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[linked_trash]]
				WHERE [[linked_trash]].[[container_id]] = [[linked]].[[id]]
			)
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[source_trash]]
				WHERE [[source_trash]].[[container_id]] = [[source]].[[id]]
			)
		ORDER BY [[membership]].[[source_container_id]]
	`).Bind(dbx.Params{
		"owner": current.Workspace.OwnerID, "workspaceId": current.Workspace.WorkspaceID,
		"pageId": pageID,
	}).All(&candidates); err != nil {
		return linkedDatabaseReadScope{}, err
	}
	scope := linkedDatabaseReadScope{sourceContainerPages: map[string]string{}}
	pageAccess := map[string]bool{pageID: true}
	for _, candidate := range candidates {
		allowed, checked := pageAccess[candidate.SourcePageID]
		if !checked {
			authorized, err := authorizePageBody(
				app, principalID, candidate.SourcePageID, permissionWorkspaceRead,
			)
			allowed = err == nil && authorized.Workspace.OwnerID == current.Workspace.OwnerID &&
				authorized.Workspace.WorkspaceID == current.Workspace.WorkspaceID
			pageAccess[candidate.SourcePageID] = allowed
		}
		if allowed {
			scope.sourceContainerPages[candidate.SourceContainerID] = candidate.SourcePageID
		}
	}
	relationTargets := []rollupReachedSource{}
	// So do the other-page sources the page layouts' tabs show.
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[data_source_id]], [[source_page_id]] FROM (
			SELECT [[target]].[[target_source_id]] AS [[data_source_id]], [[source]].[[page_id]] AS [[source_page_id]]
			FROM {{leafen_database_relation_property_targets}} AS [[target]]
			JOIN {{leafen_data_sources}} AS [[source]]
				ON [[source]].[[id]] = [[target]].[[target_source_id]] AND [[source]].[[owner]] = [[target]].[[owner]]
			WHERE [[target]].[[owner]] = {:owner} AND [[target]].[[page_id]] = {:pageId}
				AND [[source]].[[workspace_id]] = {:workspaceId} AND [[source]].[[page_id]] != {:pageId}
			UNION ALL
			-- A linked view's source keeps its relations' targets too, as its
			-- own page shows them; a target the page links itself is read already.
			SELECT [[target]].[[target_source_id]], [[source]].[[page_id]]
			FROM {{leafen_database_container_sources}} AS [[member]]
			JOIN {{leafen_database_relation_property_targets}} AS [[target]]
				ON [[target]].[[owner]] = [[member]].[[owner]] AND [[target]].[[source_id]] = [[member]].[[data_source_id]]
			JOIN {{leafen_data_sources}} AS [[source]]
				ON [[source]].[[id]] = [[target]].[[target_source_id]] AND [[source]].[[owner]] = [[target]].[[owner]]
			WHERE [[member]].[[owner]] = {:owner} AND [[member]].[[page_id]] = {:pageId} AND [[member]].[[source_container_id]] != ''
				AND [[source]].[[workspace_id]] = {:workspaceId} AND [[source]].[[page_id]] != {:pageId}
				AND [[target]].[[target_source_id]] NOT IN (
					SELECT [[linked_source]].[[data_source_id]] FROM {{leafen_database_container_sources}} AS [[linked_source]]
					WHERE [[linked_source]].[[owner]] = {:owner} AND [[linked_source]].[[page_id]] = {:pageId}
				)
			UNION ALL
			-- A tab's source is found by its id, its owner and workspace read
			-- after (+), or each tab reads the workspace's every source.
			SELECT [[tab_source]].[[id]], [[tab_source]].[[page_id]]
			FROM {{leafen_database_page_layouts}} AS [[layout]]
			JOIN {{leafen_data_sources}} AS [[own]] ON [[own]].[[id]] = [[layout]].[[data_source_id]]
			JOIN json_each([[layout]].[[layout]], '$.tabs') AS [[tab]]
			JOIN {{leafen_data_sources}} AS [[tab_source]] ON [[tab_source]].[[id]] = json_extract([[tab]].[[value]], '$.dataSourceId')
				AND +[[tab_source]].[[owner]] = [[own]].[[owner]]
			WHERE [[own]].[[owner]] = {:owner} AND [[own]].[[page_id]] = {:pageId}
				AND +[[tab_source]].[[workspace_id]] = {:workspaceId} AND [[tab_source]].[[page_id]] != {:pageId}
		)
		ORDER BY [[data_source_id]]
	`).Bind(dbx.Params{
		"owner": current.Workspace.OwnerID, "workspaceId": current.Workspace.WorkspaceID, "pageId": pageID,
	}).All(&relationTargets); err != nil {
		return linkedDatabaseReadScope{}, err
	}
	reached, err := loadRollupReachedSources(app, current.Workspace.OwnerID, current.Workspace.WorkspaceID, pageID)
	if err != nil {
		return linkedDatabaseReadScope{}, err
	}
	relationTargets = append(relationTargets, reached...)
	access := newRelationTargetPageAccess(app, principalID, current, pageID)
	for page, allowed := range pageAccess {
		access.allowed[page] = allowed
	}
	scope.relationSourcePages = map[string]string{}
	for _, target := range relationTargets {
		// Only a signed-in reader's own access reveals another page.
		if principalID != "" && access.readable(target.SourcePageID) {
			scope.relationSourcePages[target.DataSourceID] = target.SourcePageID
		}
	}
	return scope, nil
}

// authorizeLinkedDatabaseReferences admits the other-page source a linked
// database shows, and the source properties its own views reference, once the
// actor can read the source page.
func authorizeLinkedDatabaseReferences(
	app core.App,
	principalID string,
	current pageAuthorization,
	pageID string,
	operations []graphOperation,
) (map[string]graphEntityReference, map[string]graphEntityReference, error) {
	sources := map[string]graphEntityReference{}
	properties := map[string]graphEntityReference{}
	authorizedPages := map[string]struct{}{pageID: {}}
	// A database in this mutation that takes a source as its own owns it once
	// the mutation applies; its own sources are on this page.
	batchOwners := map[string]map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertBlock || operation.Block == nil ||
			operation.Block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		content, err := parseDatabaseContainerBlockContent(operation.Block.Content)
		if err != nil {
			continue
		}
		owned := map[string]struct{}{}
		for _, id := range content.ownDataSourceIDs() {
			owned[id] = struct{}{}
		}
		batchOwners[operation.Block.ID] = owned
	}
	authorize := func(sourceContainerID, dataSourceID string) (linkedDatabaseReferenceRow, error) {
		if owners, inBatch := batchOwners[sourceContainerID]; inBatch {
			if _, owned := owners[dataSourceID]; !owned {
				return linkedDatabaseReferenceRow{}, graphInputError(
					errorCodeGraphReferenceInvalid, "The database to link was not found.",
				)
			}
			return linkedDatabaseReferenceRow{
				SourceContainerID: sourceContainerID, OwnerID: current.Workspace.OwnerID,
				WorkspaceID: current.Workspace.WorkspaceID, SourcePageID: pageID, DataSourceID: dataSourceID,
			}, nil
		}
		row, err := loadLinkedDatabaseReference(
			app, current.Workspace.OwnerID, current.Workspace.WorkspaceID, sourceContainerID, dataSourceID,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return row, linkedDatabaseNotFound(app, principalID, current, dataSourceID)
		}
		if err != nil {
			return row, err
		}
		if _, ok := authorizedPages[row.SourcePageID]; !ok {
			if principalID == "" {
				return row, graphInputError(errorCodeGraphReferenceInvalid, "The database to link was not found.")
			}
			authorized, authErr := authorizePageBody(app, principalID, row.SourcePageID, permissionWorkspaceRead)
			if authErr != nil || authorized.Workspace.OwnerID != current.Workspace.OwnerID ||
				authorized.Workspace.WorkspaceID != current.Workspace.WorkspaceID {
				// An import builds its pages out of sight and links them as it
				// goes: a page it is building may show a database another page
				// of the same import holds.
				staged, err := pagesStagedTogether(app, current, pageID, row.SourcePageID)
				if err != nil {
					return row, err
				}
				if !staged {
					return row, graphInputError(errorCodeGraphReferenceInvalid, "The database to link was not found.")
				}
			}
			authorizedPages[row.SourcePageID] = struct{}{}
		}
		sources[row.DataSourceID] = graphEntityReference{
			ID: row.DataSourceID, Owner: row.OwnerID, PageID: row.SourcePageID,
		}
		return row, nil
	}
	containers, err := loadGraphViewContainers(app, current.Workspace.OwnerID, pageID, operations)
	if err != nil {
		return nil, nil, err
	}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil &&
			operation.Block.Type == graphBlockTypeInlineDatabase:
			content, err := parseDatabaseContainerBlockContent(operation.Block.Content)
			if err != nil {
				continue
			}
			for dataSourceID, sourceContainerID := range content.linkedSourceContainers() {
				if _, err := authorize(sourceContainerID, dataSourceID); err != nil {
					return nil, nil, err
				}
			}
		case operation.Kind == graphOperationKindUpsertView && operation.View != nil &&
			operation.View.ContainerID != "":
			content, known := containers[operation.View.ContainerID]
			sourceContainerID, linked := content.linkedSourceContainers()[operation.View.DataSourceID]
			if !known || !linked {
				continue
			}
			row, err := authorize(sourceContainerID, operation.View.DataSourceID)
			if err != nil {
				return nil, nil, err
			}
			if row.SourcePageID == pageID {
				continue
			}
			referenced, err := loadGraphReferences(
				app, "leafen_database_properties", linkedGraphViewPropertyIDs(operation.View),
			)
			if err != nil {
				return nil, nil, err
			}
			for id, property := range referenced {
				if property.DataSourceID == row.DataSourceID && property.PageID == row.SourcePageID {
					properties[id] = property
				}
			}
		}
	}
	return sources, properties, nil
}

func linkedGraphViewPropertyIDs(view *graphDatabaseView) map[string]struct{} {
	ids := map[string]struct{}{}
	var config struct {
		GroupBy string `json:"groupBy"`
	}
	if view.Type == graphViewTypeBoard && json.Unmarshal(view.Config, &config) == nil && config.GroupBy != "" {
		ids[config.GroupBy] = struct{}{}
	}
	for _, id := range graphViewPropertyReferences(view.Config) {
		ids[id] = struct{}{}
	}
	return ids
}

// pagesStagedTogether reports whether a page and another page whose database
// it names — the one a linked view shows, or one a relation reaches — are
// both still being built by an import into the same workspace. An import
// builds its pages out of sight and joins them as it goes, so its pages reach
// each other's databases as they will once it publishes them. A database
// page of a page it builds is one it builds too (workspaceImportStagedRowPage):
// its body comes back as a page's, with the databases it shows (2026-11-17:
// a linked database in a database page's body failed a whole archive import).
func pagesStagedTogether(app core.App, current pageAuthorization, pageID, sourcePageID string) (bool, error) {
	if pageID == sourcePageID {
		return false, nil
	}
	pages := []struct {
		ID      string `db:"id"`
		Staging bool   `db:"notion_import_staging"`
	}{}
	// The two pages are found by id; owner and workspace are read after (+),
	// or SQLite reads the workspace's every page.
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[notion_import_staging]] FROM {{leafen_pages}}
		WHERE [[id]] IN ({:pageId}, {:sourcePageId})
			AND +[[owner]] = {:owner} AND +[[workspace]] = {:workspaceId}
	`).Bind(dbx.Params{
		"pageId": pageID, "sourcePageId": sourcePageID,
		"owner": current.Workspace.OwnerID, "workspaceId": current.Workspace.WorkspaceID,
	}).All(&pages); err != nil {
		return false, err
	}
	if len(pages) != 2 {
		return false, nil
	}
	for _, page := range pages {
		if !page.Staging && !workspaceImportStagedRowPage(app, page.ID) {
			return false, nil
		}
	}
	return true, nil
}

// pageStagedByImport reports whether a page is one an import is still
// building: a page it stages or a database page of one.
func pageStagedByImport(app core.App, page *core.Record) bool {
	return page != nil && (page.GetBool("notion_import_staging") || workspaceImportStagedRowPage(app, page.Id))
}

func loadLinkedDatabaseReference(
	app core.App,
	owner, workspaceID, sourceContainerID, dataSourceID string,
) (linkedDatabaseReferenceRow, error) {
	row := linkedDatabaseReferenceRow{}
	query := `
		SELECT [[container]].[[id]] AS [[source_container_id]], [[container]].[[owner]],
			[[container]].[[workspace_id]], [[source]].[[page_id]] AS [[source_page_id]],
			[[source]].[[id]] AS [[data_source_id]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[container]].[[id]]
		JOIN {{leafen_data_sources}} AS [[source]]
			ON [[source]].[[id]] = [[membership]].[[data_source_id]]
			AND [[source]].[[owner]] = [[container]].[[owner]]
			AND [[source]].[[workspace_id]] = [[container]].[[workspace_id]]
		WHERE [[container]].[[kind]] = 'source' AND [[membership]].[[source_container_id]] = ''
			AND [[container]].[[owner]] = {:owner}
			AND [[container]].[[workspace_id]] = {:workspaceId}
			AND [[source]].[[id]] = {:dataSourceId}`
	query += ` AND NOT EXISTS (
		SELECT 1 FROM {{leafen_database_container_trash}} AS [[container_trash]]
		WHERE [[container_trash]].[[container_id]] = [[container]].[[id]]
	)`
	params := dbx.Params{
		"owner": owner, "workspaceId": workspaceID, "dataSourceId": dataSourceID,
	}
	if sourceContainerID != "" {
		query += " AND [[container]].[[id]] = {:sourceContainerId}"
		params["sourceContainerId"] = sourceContainerID
	}
	query += " ORDER BY [[container]].[[page_id]], [[container]].[[position]], [[container]].[[id]] LIMIT 1"
	err := app.DB().NewQuery(query).Bind(params).One(&row)
	return row, err
}

// linkedDatabaseNotFound refuses a linked view of a database this page's
// workspace doesn't hold: one of another workspace the actor reads as such
// (other_workspace_refusal.go).
func linkedDatabaseNotFound(app core.App, principalID string, current pageAuthorization, dataSourceID string) error {
	workspaceID, sourcePage := "", ""
	if principalID != "" {
		workspaceID, sourcePage = dataSourceInOtherWorkspace(app, principalID, dataSourceID, current.Workspace.WorkspaceID)
	}
	if workspaceID == "" {
		return graphInputError(errorCodeGraphReferenceInvalid, "The database to link was not found.")
	}
	return otherWorkspaceRefused(app, "dataSourceId", workspaceCrossing{
		subject: "a linked view made here", from: current.Workspace.WorkspaceID, place: "its data source " + dataSourceID,
		to: workspaceID, link: true, whole: sourcePage,
	})
}
