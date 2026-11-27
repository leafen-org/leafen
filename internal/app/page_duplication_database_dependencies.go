package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	pageDuplicationDependencyBindingCursor = "binding:"
	pageDuplicationDependencyEdgeCursor    = "edge:"
)

type pageDuplicationSourceDependencyBinding struct {
	DataSourceID              string `db:"data_source_id"`
	TargetDataSourceID        string `db:"target_data_source_id"`
	TargetPredecessorProperty string `db:"target_predecessor_property_id"`
	TargetSuccessorProperty   string `db:"target_successor_property_id"`
	DataKey                   string `db:"data_key"`
	TargetPredecessorExisting string `db:"target_predecessor_existing"`
	TargetSuccessorExisting   string `db:"target_successor_existing"`
	TargetDataKeyExisting     string `db:"target_data_key_existing"`
	TargetRevisionExisting    int    `db:"target_revision_existing"`
}

type pageDuplicationSourceDependencyEdge struct {
	ID                     string `db:"id"`
	TargetID               string `db:"-"`
	TargetDataSourceID     string `db:"target_data_source_id"`
	TargetPredecessorRowID string `db:"target_predecessor_row_id"`
	TargetSuccessorRowID   string `db:"target_successor_row_id"`
}

func loadPageDuplicationDatabaseDependencyOperations(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) ([]graphOperation, *graphReferenceCache, string, bool, error) {
	if state.CopyCursor == "" || strings.HasPrefix(state.CopyCursor, pageDuplicationDependencyBindingCursor) {
		return loadPageDuplicationDatabaseDependencyBindings(app, job, state)
	}
	if strings.HasPrefix(state.CopyCursor, pageDuplicationDependencyEdgeCursor) {
		return loadPageDuplicationDatabaseDependencyEdges(app, job, state)
	}
	return nil, nil, state.CopyCursor, false, errPageDuplicationConflict
}

func loadPageDuplicationDatabaseDependencyBindings(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) ([]graphOperation, *graphReferenceCache, string, bool, error) {
	cursor := strings.TrimPrefix(state.CopyCursor, pageDuplicationDependencyBindingCursor)
	bindings := []pageDuplicationSourceDependencyBinding{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[data_source_id]],[[dataSource]].[[target_id]] AS [[target_data_source_id]],
			[[predecessor]].[[target_id]] AS [[target_predecessor_property_id]],
			[[successor]].[[target_id]] AS [[target_successor_property_id]],[[source]].[[data_key]],
			COALESCE([[target]].[[predecessor_property_id]],'') AS [[target_predecessor_existing]],
			COALESCE([[target]].[[successor_property_id]],'') AS [[target_successor_existing]],
			COALESCE([[target]].[[data_key]],'') AS [[target_data_key_existing]],
			COALESCE([[target]].[[revision]],0) AS [[target_revision_existing]]
		FROM {{leafen_database_dependency_bindings}} [[source]]
		JOIN {{leafen_page_duplication_identity_map}} [[dataSource]]
			ON [[dataSource]].[[job_id]]={:jobId} AND [[dataSource]].[[kind]]='data_source'
			AND [[dataSource]].[[source_id]]=[[source]].[[data_source_id]]
		JOIN {{leafen_page_duplication_identity_map}} [[predecessor]]
			ON [[predecessor]].[[job_id]]={:jobId} AND [[predecessor]].[[kind]]='property'
			AND [[predecessor]].[[source_id]]=[[source]].[[predecessor_property_id]]
		JOIN {{leafen_page_duplication_identity_map}} [[successor]]
			ON [[successor]].[[job_id]]={:jobId} AND [[successor]].[[kind]]='property'
			AND [[successor]].[[source_id]]=[[source]].[[successor_property_id]]
		LEFT JOIN {{leafen_database_dependency_bindings}} [[target]]
			ON [[target]].[[data_source_id]]=[[dataSource]].[[target_id]]
			AND [[target]].[[owner]]={:ownerId} AND [[target]].[[workspace_id]]={:workspaceId}
			AND [[target]].[[page_id]]={:targetPageId}
		WHERE [[source]].[[owner]]={:sourceOwnerId} AND [[source]].[[workspace_id]]={:sourceWorkspaceId}
			AND [[source]].[[page_id]]={:sourcePageId} AND [[source]].[[data_source_id]]>{:cursor}
		ORDER BY [[source]].[[data_source_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "ownerId": job.OwnerID, "workspaceId": job.WorkspaceID,
		"sourceOwnerId": job.sourceOwner(), "sourceWorkspaceId": job.sourceWorkspace(),
		"sourcePageId": state.SourcePageID, "targetPageId": state.TargetPageID,
		"cursor": cursor, "limit": maxPageDuplicationItemsPerTurn + 1,
	}).All(&bindings)
	if err != nil {
		return nil, nil, state.CopyCursor, false, err
	}
	hasMore := trimPageDuplicationWindow(&bindings)
	operations := make([]graphOperation, 0, len(bindings))
	for _, binding := range bindings {
		if binding.TargetRevisionExisting > 0 {
			if binding.TargetRevisionExisting != 1 ||
				binding.TargetPredecessorExisting != binding.TargetPredecessorProperty ||
				binding.TargetSuccessorExisting != binding.TargetSuccessorProperty ||
				binding.TargetDataKeyExisting != binding.DataKey {
				return nil, nil, state.CopyCursor, false, errPageDuplicationConflict
			}
			continue
		}
		operations = append(operations, graphOperation{
			Kind: graphOperationKindConfigureDependency,
			DependencyBinding: &graphDependencyBinding{
				DataSourceID:          binding.TargetDataSourceID,
				PredecessorPropertyID: binding.TargetPredecessorProperty,
				SuccessorPropertyID:   binding.TargetSuccessorProperty,
				DataKey:               binding.DataKey, ExpectedRevision: 0,
			},
		})
	}
	nextCursor := pageDuplicationDependencyEdgeCursor
	if hasMore {
		nextCursor = pageDuplicationDependencyBindingCursor + bindings[len(bindings)-1].DataSourceID
	}
	return operations, nil, nextCursor, false, nil
}

func loadPageDuplicationDatabaseDependencyEdges(
	app core.App,
	job storedPageDuplicationJob,
	state storedPageDuplicationEntityState,
) ([]graphOperation, *graphReferenceCache, string, bool, error) {
	cursor := strings.TrimPrefix(state.CopyCursor, pageDuplicationDependencyEdgeCursor)
	edges := []pageDuplicationSourceDependencyEdge{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]],[[dataSource]].[[target_id]] AS [[target_data_source_id]],
			[[predecessor]].[[target_id]] AS [[target_predecessor_row_id]],
			[[successor]].[[target_id]] AS [[target_successor_row_id]]
		FROM {{leafen_database_dependency_edges}} [[source]]
		JOIN {{leafen_page_duplication_identity_map}} [[dataSource]]
			ON [[dataSource]].[[job_id]]={:jobId} AND [[dataSource]].[[kind]]='data_source'
			AND [[dataSource]].[[source_id]]=[[source]].[[data_source_id]]
		JOIN {{leafen_page_duplication_identity_map}} [[predecessor]]
			ON [[predecessor]].[[job_id]]={:jobId} AND [[predecessor]].[[kind]]='row'
			AND [[predecessor]].[[source_id]]=[[source]].[[predecessor_row_id]]
		JOIN {{leafen_page_duplication_identity_map}} [[successor]]
			ON [[successor]].[[job_id]]={:jobId} AND [[successor]].[[kind]]='row'
			AND [[successor]].[[source_id]]=[[source]].[[successor_row_id]]
		WHERE [[source]].[[owner]]={:ownerId} AND [[source]].[[workspace_id]]={:workspaceId}
			AND [[source]].[[page_id]]={:sourcePageId} AND [[source]].[[id]]>{:cursor}
		ORDER BY [[source]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "ownerId": job.sourceOwner(), "workspaceId": job.sourceWorkspace(),
		"sourcePageId": state.SourcePageID, "cursor": cursor,
		"limit": maxPageDuplicationItemsPerTurn + 1,
	}).All(&edges)
	if err != nil {
		return nil, nil, state.CopyCursor, false, err
	}
	hasMore := trimPageDuplicationWindow(&edges)
	sourceIDs := make([]string, len(edges))
	for index := range edges {
		sourceIDs[index] = edges[index].ID
	}
	identityMappings, err := pageDuplicationDatabaseReferenceMappings(
		app, job.ID, "dependency_edge", sourceIDs,
	)
	if err != nil {
		return nil, nil, state.CopyCursor, false, err
	}
	operations := make([]graphOperation, len(edges))
	cache := &graphReferenceCache{
		references: map[string]graphEntityReference{}, completeIDs: map[string]struct{}{},
	}
	for index := range edges {
		edges[index].TargetID = identityMappings[edges[index].ID]
		if edges[index].TargetID == "" {
			return nil, nil, state.CopyCursor, false, errPageDuplicationConflict
		}
		operations[index] = graphOperation{
			Kind: graphOperationKindUpsertDependencyEdge,
			DependencyEdge: &graphDependencyEdge{
				ID: edges[index].TargetID, DataSourceID: edges[index].TargetDataSourceID,
				PredecessorRowID: edges[index].TargetPredecessorRowID,
				SuccessorRowID:   edges[index].TargetSuccessorRowID, ExpectedRevision: 1,
			},
		}
		cache.completeIDs[edges[index].TargetPredecessorRowID] = struct{}{}
		cache.completeIDs[edges[index].TargetSuccessorRowID] = struct{}{}
	}
	nextCursor := state.CopyCursor
	if len(edges) > 0 {
		nextCursor = pageDuplicationDependencyEdgeCursor + edges[len(edges)-1].ID
	}
	return operations, cache, nextCursor, !hasMore, nil
}
