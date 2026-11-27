package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type graphReferenceCache struct {
	references  map[string]graphEntityReference
	completeIDs map[string]struct{}
}

func (cache *graphReferenceCache) completeMissing(id string) bool {
	if cache == nil {
		return false
	}
	if _, complete := cache.completeIDs[id]; !complete {
		return false
	}
	_, found := cache.references[id]
	return !found
}

func (cache *graphReferenceCache) store(reference graphEntityReference) {
	if cache == nil {
		return
	}
	if cache.references == nil {
		cache.references = map[string]graphEntityReference{}
	}
	if cache.completeIDs == nil {
		cache.completeIDs = map[string]struct{}{}
	}
	cache.references[reference.ID] = reference
	cache.completeIDs[reference.ID] = struct{}{}
}

func loadGraphProjection(app core.App, owner, pageID string, operations []graphOperation) (graphProjection, error) {
	return loadGraphProjectionWithReferenceCaches(app, owner, pageID, operations, nil, nil, nil)
}

func loadGraphProjectionWithSourceReferences(
	app core.App,
	owner, pageID string,
	operations []graphOperation,
	sourceReferences map[string]graphEntityReference,
) (graphProjection, error) {
	return loadGraphProjectionWithReferenceCaches(app, owner, pageID, operations, sourceReferences, nil, nil)
}

func loadGraphProjectionWithReferences(
	app core.App,
	owner, pageID string,
	operations []graphOperation,
	sourceReferences map[string]graphEntityReference,
	propertyReferences map[string]graphEntityReference,
) (graphProjection, error) {
	return loadGraphProjectionWithReferenceCaches(
		app, owner, pageID, operations, sourceReferences, propertyReferences, nil,
	)
}

func loadGraphProjectionWithReferenceCaches(
	app core.App,
	owner, pageID string,
	operations []graphOperation,
	sourceReferences map[string]graphEntityReference,
	propertyReferences map[string]graphEntityReference,
	rowReferences *graphReferenceCache,
) (graphProjection, error) {
	return loadGraphProjectionWithAllReferenceCaches(
		app, owner, pageID, operations, sourceReferences, nil, propertyReferences, rowReferences, nil,
	)
}

// externalRows are other-page rows the mutation may name as relation
// targets; loadGraphProjectionWithAllReferenceCaches refuses any other
// other-page row.
func loadGraphProjectionWithAllReferenceCaches(
	app core.App,
	owner, pageID string,
	operations []graphOperation,
	sourceReferences map[string]graphEntityReference,
	viewReferences map[string]graphEntityReference,
	propertyReferences map[string]graphEntityReference,
	rowReferences *graphReferenceCache,
	externalRows map[string]graphEntityReference,
) (graphProjection, error) {
	blockIDs := map[string]struct{}{}
	sourceIDs := map[string]struct{}{}
	viewIDs := map[string]struct{}{}
	propertyIDs := map[string]struct{}{}
	rowIDs := map[string]struct{}{}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			blockIDs[operation.Block.ID] = struct{}{}
			if operation.Block.ParentID != nil {
				blockIDs[*operation.Block.ParentID] = struct{}{}
			}
			if operation.Block.Type == graphBlockTypeInlineDatabase {
				content, err := parseDatabaseContainerBlockContent(operation.Block.Content)
				if err != nil {
					return graphProjection{}, graphInputError(errorCodeGraphInvalid, "Check the inline database content.")
				}
				content.DataSourceID = strings.TrimSpace(content.DataSourceID)
				content.ViewID = strings.TrimSpace(content.ViewID)
				if content.DataSourceID == "" || content.ViewID == "" {
					return graphProjection{}, graphInputError(errorCodeGraphReferenceInvalid, "Check the inline database link.")
				}
				// One its page took away, unable to show it, names what the
				// page doesn't hold, as it did (database_unavailable_blocks.go).
				returning, err := unavailableDatabaseBlockReturns(app, pageID, operation.Block.ID, content)
				if err != nil {
					return graphProjection{}, err
				}
				if !returning {
					for _, id := range content.memberDataSourceIDs() {
						sourceIDs[id] = struct{}{}
					}
					viewIDs[content.ViewID] = struct{}{}
				}
			}
			if operation.Block.AfterID != nil {
				blockIDs[*operation.Block.AfterID] = struct{}{}
			}
		case graphOperationKindMoveBlock:
			blockIDs[operation.Move.BlockID] = struct{}{}
			if operation.Move.ParentID != nil {
				blockIDs[*operation.Move.ParentID] = struct{}{}
			}
			if operation.Move.AfterID != nil {
				blockIDs[*operation.Move.AfterID] = struct{}{}
			}
		case graphOperationKindDeleteBlock:
			blockIDs[operation.Deletion.BlockID] = struct{}{}
		case graphOperationKindUpsertDataSource:
			sourceIDs[operation.DataSource.ID] = struct{}{}
		case graphOperationKindDeleteDataSource:
			sourceIDs[operation.DataSourceDeletion.ID] = struct{}{}
		case graphOperationKindUpsertView:
			viewIDs[operation.View.ID] = struct{}{}
			sourceIDs[operation.View.DataSourceID] = struct{}{}
			if dashboardID := dashboardWidgetOwner(operation.View.Config); dashboardID != "" {
				viewIDs[dashboardID] = struct{}{}
			}
			if operation.View.ContainerID != "" {
				blockIDs[operation.View.ContainerID] = struct{}{}
			}
			if operation.View.Type == graphViewTypeBoard {
				var config struct {
					GroupBy string `json:"groupBy"`
				}
				if json.Unmarshal(operation.View.Config, &config) != nil {
					return graphProjection{}, graphInputError(errorCodeGraphInvalid, "Check the board view configuration.")
				}
				if config.GroupBy != "" {
					propertyIDs[config.GroupBy] = struct{}{}
				}
			}
			for _, propertyID := range graphViewPropertyReferences(operation.View.Config) {
				propertyIDs[propertyID] = struct{}{}
			}
		case graphOperationKindDeleteView:
			viewIDs[operation.ViewDeletion.ID] = struct{}{}
		case graphOperationKindUpsertProperty:
			propertyIDs[operation.Property.ID] = struct{}{}
			sourceIDs[operation.Property.DataSourceID] = struct{}{}
			if target := graphRelationTarget(operation.Property.Type, operation.Property.Config); target != "" {
				sourceIDs[target] = struct{}{}
			}
		case graphOperationKindDeleteProperty:
			propertyIDs[operation.PropertyDeletion.ID] = struct{}{}
		case graphOperationKindUpsertRow:
			rowIDs[operation.Row.ID] = struct{}{}
			sourceIDs[operation.Row.DataSourceID] = struct{}{}
			if operation.Row.SubitemParentID != nil {
				rowIDs[*operation.Row.SubitemParentID] = struct{}{}
			}
			for propertyID, value := range operation.Row.Values {
				propertyIDs[propertyID] = struct{}{}
				if value.Type == graphValueTypeRelation {
					for _, targetID := range graphRelationRowIDs(value.Value) {
						rowIDs[targetID] = struct{}{}
					}
				}
			}
		case graphOperationKindDeleteRow:
			rowIDs[operation.RowDeletion.ID] = struct{}{}
		case graphOperationKindConfigureDependency:
			sourceIDs[operation.DependencyBinding.DataSourceID] = struct{}{}
			propertyIDs[operation.DependencyBinding.PredecessorPropertyID] = struct{}{}
			propertyIDs[operation.DependencyBinding.SuccessorPropertyID] = struct{}{}
		case graphOperationKindUpsertDependencyEdge:
			sourceIDs[operation.DependencyEdge.DataSourceID] = struct{}{}
			rowIDs[operation.DependencyEdge.PredecessorRowID] = struct{}{}
			rowIDs[operation.DependencyEdge.SuccessorRowID] = struct{}{}
		case graphOperationKindDeleteDependencyEdge:
			sourceIDs[operation.DependencyEdgeDeletion.DataSourceID] = struct{}{}
		}
	}

	blocks, err := loadGraphReferences(app, "leafen_blocks", blockIDs)
	if err != nil {
		return graphProjection{}, err
	}
	sources, err := loadGraphReferencesWithCache(app, "leafen_data_sources", sourceIDs, sourceReferences)
	if err != nil {
		return graphProjection{}, err
	}
	views, err := loadGraphReferencesWithCache(app, "leafen_database_views", viewIDs, viewReferences)
	if err != nil {
		return graphProjection{}, err
	}
	properties, err := loadGraphReferencesWithCache(app, "leafen_database_properties", propertyIDs, propertyReferences)
	if err != nil {
		return graphProjection{}, err
	}
	rows, err := loadGraphReferencesWithCompleteCache(app, "leafen_database_rows", rowIDs, rowReferences)
	if err != nil {
		return graphProjection{}, err
	}
	containers, err := loadGraphViewContainers(app, owner, pageID, operations)
	if err != nil {
		return graphProjection{}, err
	}
	projection := graphProjection{
		blocks: blocks, sources: sources, views: views, properties: properties, rows: rows,
		containers: containers,
	}
	written := graphWrittenIDsOf(operations)
	if err := validateGraphReferencePage(blocks, nil, owner, pageID, written.blocks); err != nil {
		return graphProjection{}, err
	}
	if err := validateGraphReferencePage(rows, externalRows, owner, pageID, written.rows); err != nil {
		return graphProjection{}, err
	}
	if err := validateGraphReferencePage(properties, propertyReferences, owner, pageID, written.properties); err != nil {
		return graphProjection{}, err
	}
	if err := validateGraphReferencePage(sources, sourceReferences, owner, pageID, written.sources); err != nil {
		return graphProjection{}, err
	}
	if err := validateGraphReferencePage(views, viewReferences, owner, pageID, written.views); err != nil {
		return graphProjection{}, err
	}
	if err := refuseTakenGraphDependencyEdgeIDs(app, owner, pageID, written); err != nil {
		return graphProjection{}, err
	}

	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			parentID := ""
			if operation.Block.ParentID != nil {
				parentID = *operation.Block.ParentID
			}
			projection.blocks[operation.Block.ID] = graphEntityReference{ID: operation.Block.ID, Owner: owner, PageID: pageID, ParentID: parentID, Type: operation.Block.Type}
		case graphOperationKindUpsertDataSource:
			projection.sources[operation.DataSource.ID] = graphEntityReference{ID: operation.DataSource.ID, Owner: owner, PageID: pageID}
		case graphOperationKindUpsertView:
			projection.views[operation.View.ID] = graphEntityReference{ID: operation.View.ID, Owner: owner, PageID: pageID, DataSourceID: operation.View.DataSourceID, ContainerID: operation.View.ContainerID, Type: operation.View.Type, Config: string(operation.View.Config)}
		case graphOperationKindUpsertProperty:
			projection.properties[operation.Property.ID] = graphEntityReference{ID: operation.Property.ID, Owner: owner, PageID: pageID, DataSourceID: operation.Property.DataSourceID, Type: operation.Property.Type, Config: string(operation.Property.Config)}
		case graphOperationKindUpsertRow:
			parentID := ""
			if operation.Row.SubitemParentID != nil {
				parentID = *operation.Row.SubitemParentID
			}
			projection.rows[operation.Row.ID] = graphEntityReference{ID: operation.Row.ID, Owner: owner, PageID: pageID, ParentID: parentID, DataSourceID: operation.Row.DataSourceID}
		}
	}
	return projection, nil
}

// validateGraphReferencePage refuses what a change names on another page,
// unless authorized there; one it would write under an ID that content
// elsewhere holds is refused as a taken ID (content_graph_written_ids.go).
func validateGraphReferencePage(
	references map[string]graphEntityReference,
	authorizedExternal map[string]graphEntityReference,
	owner, pageID string,
	written map[string]struct{},
) error {
	for id, reference := range references {
		if reference.Owner == owner && reference.PageID == pageID {
			continue
		}
		authorized, ok := authorizedExternal[id]
		if reference.Owner == owner && ok && authorized.Owner == reference.Owner && authorized.PageID == reference.PageID {
			continue
		}
		if _, own := written[id]; own {
			return graphIDTakenError()
		}
		return graphInputError(errorCodeGraphReferenceInvalid, "Content on another page can't be changed.")
	}
	return nil
}

func loadGraphReferencesWithCache(
	app core.App,
	table string,
	ids map[string]struct{},
	cached map[string]graphEntityReference,
) (map[string]graphEntityReference, error) {
	completeIDs := make(map[string]struct{}, len(cached))
	for id := range cached {
		completeIDs[id] = struct{}{}
	}
	return loadGraphReferencesWithCompleteCache(app, table, ids, &graphReferenceCache{
		references: cached, completeIDs: completeIDs,
	})
}

func loadGraphReferencesWithCompleteCache(
	app core.App,
	table string,
	ids map[string]struct{},
	cache *graphReferenceCache,
) (map[string]graphEntityReference, error) {
	result := make(map[string]graphEntityReference, len(ids))
	missing := make(map[string]struct{}, len(ids))
	for id := range ids {
		if cache != nil {
			if _, complete := cache.completeIDs[id]; complete {
				if reference, found := cache.references[id]; found {
					result[id] = reference
				}
				continue
			}
		}
		missing[id] = struct{}{}
	}
	loaded, err := loadGraphReferences(app, table, missing)
	if err != nil {
		return nil, err
	}
	for id, reference := range loaded {
		result[id] = reference
	}
	return result, nil
}

func loadGraphReferences(app core.App, table string, ids map[string]struct{}) (map[string]graphEntityReference, error) {
	result := make(map[string]graphEntityReference, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	columns := "[[id]], [[owner]], [[page_id]], '' AS [[parent_id]], '' AS [[data_source_id]], '' AS [[type]], '' AS [[config]]"
	switch table {
	case "leafen_blocks":
		columns = "[[id]], [[owner]], [[page_id]], [[parent_id]], '' AS [[data_source_id]], [[type]], '' AS [[config]]"
	case "leafen_database_views":
		columns = "[[id]], [[owner]], [[page_id]], '' AS [[parent_id]], [[data_source_id]], [[container_id]], [[type]], [[config]]"
	case "leafen_database_properties":
		columns = "[[id]], [[owner]], [[page_id]], '' AS [[parent_id]], [[data_source_id]], [[type]], [[config]]"
	case "leafen_database_rows", "leafen_database_dependency_edges":
		columns = "[[id]], [[owner]], [[page_id]], '' AS [[parent_id]], [[data_source_id]], '' AS [[type]], '' AS [[config]]"
	case "leafen_data_sources":
	default:
		return nil, fmt.Errorf("unsupported graph reference table %q", table)
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	encoded, err := json.Marshal(ordered)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		"SELECT %s FROM {{%s}} WHERE [[id]] IN (SELECT value FROM json_each({:ids}))",
		columns,
		table,
	)
	var stored []graphEntityReference
	if err := app.DB().NewQuery(query).Bind(dbx.Params{"ids": string(encoded)}).All(&stored); err != nil {
		return nil, err
	}
	for _, reference := range stored {
		result[reference.ID] = reference
	}
	return result, nil
}
