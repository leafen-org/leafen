package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func validateGraphMutation(
	app core.App,
	owner, workspaceID, pageID string,
	operations []graphOperation,
	runtimeWritablePropertyIDs map[string]struct{},
) (graphMutationCounts, error) {
	return validateGraphMutationWithSourceReferences(
		app, owner, workspaceID, pageID, operations, runtimeWritablePropertyIDs, nil,
	)
}

func validateGraphMutationWithSourceReferences(
	app core.App,
	owner, workspaceID, pageID string,
	operations []graphOperation,
	runtimeWritablePropertyIDs map[string]struct{},
	sourceReferences map[string]graphEntityReference,
) (graphMutationCounts, error) {
	return validateGraphMutationWithReferences(
		app, owner, workspaceID, pageID, operations, runtimeWritablePropertyIDs,
		sourceReferences, nil,
	)
}

func validateGraphMutationWithReferences(
	app core.App,
	owner, workspaceID, pageID string,
	operations []graphOperation,
	runtimeWritablePropertyIDs map[string]struct{},
	sourceReferences map[string]graphEntityReference,
	propertyReferences map[string]graphEntityReference,
) (graphMutationCounts, error) {
	return validateGraphMutationWithReferenceCaches(
		app, owner, workspaceID, pageID, operations, runtimeWritablePropertyIDs,
		sourceReferences, propertyReferences, nil,
	)
}

func validateGraphMutationWithReferenceCaches(
	app core.App,
	owner, workspaceID, pageID string,
	operations []graphOperation,
	runtimeWritablePropertyIDs map[string]struct{},
	sourceReferences map[string]graphEntityReference,
	propertyReferences map[string]graphEntityReference,
	rowReferences *graphReferenceCache,
) (graphMutationCounts, error) {
	return validateGraphMutationWithAllReferenceCaches(
		app, owner, workspaceID, pageID, operations, runtimeWritablePropertyIDs,
		sourceReferences, nil, propertyReferences, rowReferences,
	)
}

func validateGraphMutationWithAllReferenceCaches(
	app core.App,
	owner, workspaceID, pageID string,
	operations []graphOperation,
	runtimeWritablePropertyIDs map[string]struct{},
	sourceReferences map[string]graphEntityReference,
	viewReferences map[string]graphEntityReference,
	propertyReferences map[string]graphEntityReference,
	rowReferences *graphReferenceCache,
) (graphMutationCounts, error) {
	return validateGraphMutationWithAllReferenceCachesMode(
		app, owner, workspaceID, pageID, operations, runtimeWritablePropertyIDs,
		sourceReferences, viewReferences, propertyReferences, rowReferences, nil, false, false,
	)
}

func validateGraphMutationWithAllReferenceCachesMode(
	app core.App,
	owner, workspaceID, pageID string,
	operations []graphOperation,
	runtimeWritablePropertyIDs map[string]struct{},
	sourceReferences map[string]graphEntityReference,
	viewReferences map[string]graphEntityReference,
	propertyReferences map[string]graphEntityReference,
	rowReferences *graphReferenceCache,
	externalRows map[string]graphEntityReference,
	allowBlockUpdateBeforeDelete, storedBlocks bool,
) (graphMutationCounts, error) {
	counts := graphMutationCounts{}
	blockIDs := map[string]struct{}{}
	upsertedBlockIDs := map[string]struct{}{}
	sourceIDs := map[string]struct{}{}
	viewIDs := map[string]struct{}{}
	propertyIDs := map[string]struct{}{}
	rowIDs := map[string]struct{}{}
	dependencyBindingIDs := map[string]struct{}{}
	dependencyEdgeIDs := map[string]struct{}{}

	for index := range operations {
		operation := &operations[index]
		operation.Kind = strings.TrimSpace(operation.Kind)
		if graphOperationPayloadCount(*operation) != 1 {
			return counts, graphOperationShapeRefusal(index, *operation)
		}
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			if operation.Block == nil || operation.Move != nil || operation.Deletion != nil || operation.DataSource != nil || operation.View != nil || operation.Property != nil || operation.Row != nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphBlock(operation.Block); err != nil {
				return counts, graphOperationRefusalAt(err, index, "block")
			}
			if !addGraphID(blockIDs, operation.Block.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't save the same block twice.")
			}
			upsertedBlockIDs[operation.Block.ID] = struct{}{}
			counts.Blocks++
		case graphOperationKindMoveBlock:
			if operation.Move == nil || operation.Block != nil || operation.Deletion != nil || operation.DataSource != nil || operation.View != nil || operation.Property != nil || operation.Row != nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphBlockMove(operation.Move); err != nil {
				return counts, graphOperationRefusalAt(err, index, "move")
			}
			if !addGraphID(blockIDs, operation.Move.BlockID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same block twice.")
			}
			counts.Blocks++
		case graphOperationKindDeleteBlock:
			if operation.Deletion == nil || operation.Block != nil || operation.Move != nil || operation.DataSource != nil || operation.View != nil || operation.Property != nil || operation.Row != nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphBlockDelete(operation.Deletion); err != nil {
				return counts, graphOperationRefusalAt(err, index, "deletion")
			}
			if !addGraphID(blockIDs, operation.Deletion.BlockID) {
				_, updatedFirst := upsertedBlockIDs[operation.Deletion.BlockID]
				if !allowBlockUpdateBeforeDelete || !updatedFirst {
					return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same block twice.")
				}
				delete(upsertedBlockIDs, operation.Deletion.BlockID)
			}
		case graphOperationKindUpsertDataSource:
			if operation.DataSource == nil || operation.Block != nil || operation.Move != nil || operation.Deletion != nil || operation.View != nil || operation.Property != nil || operation.Row != nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphDataSource(operation.DataSource); err != nil {
				return counts, graphOperationRefusalAt(err, index, "dataSource")
			}
			if !addGraphID(sourceIDs, operation.DataSource.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't save the same data source twice.")
			}
			counts.DataSources++
		case graphOperationKindDeleteDataSource:
			if operation.DataSourceDeletion == nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphDatabaseDelete(operation.DataSourceDeletion); err != nil {
				return counts, graphOperationRefusalAt(err, index, "dataSourceDeletion")
			}
			if !addGraphID(sourceIDs, operation.DataSourceDeletion.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same data source twice.")
			}
			counts.DataSources++
		case graphOperationKindUpsertView:
			if operation.View == nil || operation.Block != nil || operation.Move != nil || operation.Deletion != nil || operation.DataSource != nil || operation.Property != nil || operation.Row != nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphView(operation.View); err != nil {
				return counts, graphOperationRefusalAt(err, index, "view")
			}
			if !addGraphID(viewIDs, operation.View.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't save the same view twice.")
			}
			counts.Views++
		case graphOperationKindDeleteView:
			if operation.ViewDeletion == nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphDatabaseDelete(operation.ViewDeletion); err != nil {
				return counts, graphOperationRefusalAt(err, index, "viewDeletion")
			}
			if !addGraphID(viewIDs, operation.ViewDeletion.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same view twice.")
			}
			counts.Views++
		case graphOperationKindUpsertProperty:
			if operation.Property == nil || operation.Block != nil || operation.Move != nil || operation.Deletion != nil || operation.DataSource != nil || operation.View != nil || operation.Row != nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphProperty(operation.Property); err != nil {
				return counts, graphOperationRefusalAt(err, index, "property")
			}
			if !addGraphID(propertyIDs, operation.Property.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't save the same property twice.")
			}
			counts.Properties++
		case graphOperationKindDeleteProperty:
			if operation.PropertyDeletion == nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphDatabaseDelete(operation.PropertyDeletion); err != nil {
				return counts, graphOperationRefusalAt(err, index, "propertyDeletion")
			}
			if !addGraphID(propertyIDs, operation.PropertyDeletion.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same property twice.")
			}
			counts.Properties++
		case graphOperationKindUpsertRow:
			if operation.Row == nil || operation.Block != nil || operation.Move != nil || operation.Deletion != nil || operation.DataSource != nil || operation.View != nil || operation.Property != nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphRow(operation.Row, runtimeWritablePropertyIDs); err != nil {
				return counts, graphOperationRefusalAt(err, index, "row")
			}
			if !addGraphID(rowIDs, operation.Row.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't save the same row twice.")
			}
			counts.Rows++
			counts.RowValues += len(operation.Row.Values)
		case graphOperationKindDeleteRow:
			if operation.RowDeletion == nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphDatabaseDelete(operation.RowDeletion); err != nil {
				return counts, graphOperationRefusalAt(err, index, "rowDeletion")
			}
			if !addGraphID(rowIDs, operation.RowDeletion.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same row twice.")
			}
			counts.Rows++
		case graphOperationKindConfigureDependency:
			if operation.DependencyBinding == nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphDependencyBinding(operation.DependencyBinding); err != nil {
				return counts, graphOperationRefusalAt(err, index, "dependencyBinding")
			}
			if !addGraphID(dependencyBindingIDs, operation.DependencyBinding.DataSourceID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same dependency configuration twice.")
			}
			counts.DependencyBindings++
		case graphOperationKindUpsertDependencyEdge:
			if operation.DependencyEdge == nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphDependencyEdge(operation.DependencyEdge); err != nil {
				return counts, graphOperationRefusalAt(err, index, "dependencyEdge")
			}
			if !addGraphID(dependencyEdgeIDs, operation.DependencyEdge.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same dependency link twice.")
			}
			counts.DependencyEdges++
		case graphOperationKindDeleteDependencyEdge:
			if operation.DependencyEdgeDeletion == nil {
				return counts, graphOperationShapeRefusal(index, *operation)
			}
			if err := normalizeGraphDependencyEdgeDelete(operation.DependencyEdgeDeletion); err != nil {
				return counts, graphOperationRefusalAt(err, index, "dependencyEdgeDeletion")
			}
			if !addGraphID(dependencyEdgeIDs, operation.DependencyEdgeDeletion.ID) {
				return counts, graphInputError(errorCodeGraphInvalid, "One change can't change the same dependency link twice.")
			}
			counts.DependencyEdges++
		default:
			return counts, graphOperationShapeRefusal(index, *operation)
		}
	}

	if err := validateAndCanonicalizeGraphFileReferences(app, workspaceID, operations); err != nil {
		return counts, err
	}
	projection, err := loadGraphProjectionWithAllReferenceCaches(
		app, owner, pageID, operations, sourceReferences, viewReferences, propertyReferences, rowReferences,
		externalRows,
	)
	if err != nil {
		return counts, err
	}
	if err := repairDatabaseContainerViews(app, owner, pageID, projection, operations); err != nil {
		return counts, err
	}
	if projection.returningDatabases, err = loadReturningUnavailableDatabaseBlocks(app, pageID, operations); err != nil {
		return counts, err
	}
	if err := validateGraphReferences(projection, operations, pageID); err != nil {
		return counts, err
	}
	if err := validateStructuralGraphPlaces(app, owner, pageID, projection, operations, storedBlocks); err != nil {
		return counts, err
	}
	if err := validateNativeTableBlockOperations(app, owner, pageID, operations); err != nil {
		return counts, err
	}
	if err := validateGraphBlockCycles(app, owner, pageID, structuralCycleOperations(operations)); err != nil {
		return counts, err
	}
	if err := validateDatabaseContainerDeletionReferences(
		app, owner, workspaceID, pageID, operations,
	); err != nil {
		return counts, err
	}
	return counts, nil
}

func graphOperationPayloadCount(operation graphOperation) int {
	count := 0
	for _, present := range []bool{
		operation.Block != nil, operation.Move != nil, operation.Deletion != nil,
		operation.DataSource != nil, operation.DataSourceDeletion != nil,
		operation.View != nil, operation.ViewDeletion != nil,
		operation.Property != nil, operation.PropertyDeletion != nil,
		operation.Row != nil, operation.RowDeletion != nil, operation.DateRestore != nil,
		operation.DependencyBinding != nil, operation.DependencyEdge != nil,
		operation.DependencyEdgeDeletion != nil,
	} {
		if present {
			count++
		}
	}
	return count
}

func addGraphID(ids map[string]struct{}, id string) bool {
	if _, exists := ids[id]; exists {
		return false
	}
	ids[id] = struct{}{}
	return true
}

func normalizeGraphID(value string) (string, error) {
	return graphIDField("an id", value)
}

func normalizeGraphPosition(value string) (string, error) {
	return graphPositionField("position", value, false)
}

func normalizeGraphBlock(block *graphBlock) error {
	var err error
	block.ID, err = graphIDField("id", block.ID)
	if err != nil {
		return err
	}
	if block.ParentID != nil {
		parentID, err := graphIDField("parentId", *block.ParentID)
		if err != nil {
			return err
		}
		block.ParentID = &parentID
		if parentID == block.ID {
			return graphInputError(errorCodeGraphReferenceInvalid, "A block can't be its own parent.")
		}
	}
	if err := graphOptionalIDField("afterId", &block.AfterID); err != nil {
		return err
	}
	if block.AfterID != nil && *block.AfterID == block.ID {
		return graphInputError(errorCodeGraphReferenceInvalid, "A block can't be placed after itself.")
	}
	// A block's position may be left out: it goes after afterId, or last
	// under its parent when afterId is null (content_graph_structure.go),
	// as Notion's API places a block by "after" alone.
	block.Position, err = graphPositionField("position", block.Position, true)
	if err != nil {
		return err
	}
	block.Type = strings.TrimSpace(block.Type)
	if _, supported := graphBlockTypes[block.Type]; !supported {
		return graphRefused(errorCodeGraphInvalid, "Unsupported block type.",
			"type %q isn't a block type: leafen_operations' content-graph mutations list each type's content in block_contents", block.Type)
	}
	block.Content, err = normalizeJSONObject(block.Content)
	if err != nil {
		return graphValueAt(err, "content")
	}
	if len(block.BaseContent) != 0 {
		base := *block
		base.Content, base.BaseContent = block.BaseContent, nil
		if err := normalizeGraphBlock(&base); err != nil {
			return err
		}
		block.BaseContent = base.Content
	}
	// Only accepting or rejecting a suggested block or break settles it, and
	// only the suggestion routes suggest one.
	block.Content, err = withGraphBlockSuggestion(block.Content, "")
	if err != nil {
		return err
	}
	if block.Content, err = withoutGraphBlockSplit(block.Content); err != nil {
		return err
	}
	if block.Type != graphBlockTypeInlineDatabase && block.Type != graphBlockTypeFile && block.Type != graphBlockTypeButton {
		var content map[string]any
		if json.Unmarshal(block.Content, &content) != nil {
			return graphRefused(errorCodeGraphInvalid, "Check the paragraph content.", "content isn't a JSON object")
		}
		if _, containsText := content["text"]; containsText {
			return graphInputError(errorCodeGraphTextAuthorityConflict, "Paragraph text can only change through the live collaboration connection.")
		}
		if block.Type == graphBlockTypeNumberedListItem {
			if err := validateNumberedListContent(content); err != nil {
				return err
			}
		}
		if err := validateBlockMarkerContent(block.Type, content); err != nil {
			return err
		}
		if block.Type == graphBlockTypeTab {
			if _, hasLabel := content["label"]; hasLabel {
				return graphInputError(errorCodeGraphInvalid, "Store the tab name in its direct paragraph block.")
			}
		}
	}
	return normalizeGraphBlockTypedContent(block)
}

func validateGraphReferences(projection graphProjection, operations []graphOperation, pageID string) error {
	for index, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock:
			if operation.Block.ParentID != nil {
				if _, exists := projection.blocks[*operation.Block.ParentID]; !exists {
					return graphRefused(errorCodeGraphReferenceInvalid, "Parent block not found.",
						"operations[%d].block.parentId %q names no block of this page or of this change (null for the page itself)", index, *operation.Block.ParentID)
				}
			}
			if operation.Block.Type == graphBlockTypeInlineDatabase {
				content, _ := parseDatabaseContainerBlockContent(operation.Block.Content)
				// One its page took away, unable to show it, comes back as it was.
				if _, returning := projection.returningDatabases[operation.Block.ID]; returning {
					continue
				}
				if err := validateDatabaseContainerMemberReferences(projection, operation.Block.ID, content, pageID); err != nil {
					return err
				}
			}
		case graphOperationKindUpsertView:
			if err := validateGraphViewContainer(projection, operation.View, pageID); err != nil {
				return err
			}
			if err := validateGraphDashboardView(projection, operation.View, pageID); err != nil {
				return err
			}
			if operation.View.Type == graphViewTypeBoard {
				var config struct {
					GroupBy string `json:"groupBy"`
				}
				_ = json.Unmarshal(operation.View.Config, &config)
				if config.GroupBy != "" {
					property, exists := projection.properties[config.GroupBy]
					if !exists || property.DataSourceID != operation.View.DataSourceID {
						return graphInputError(errorCodeGraphReferenceInvalid, "Board group property not found.")
					}
				}
			}
			for _, propertyID := range graphViewPropertyReferences(operation.View.Config) {
				property, exists := projection.properties[propertyID]
				if !exists || property.DataSourceID != operation.View.DataSourceID {
					return graphInputError(errorCodeGraphReferenceInvalid, "View property link not found.")
				}
			}
		case graphOperationKindUpsertProperty:
			source, exists := projection.sources[operation.Property.DataSourceID]
			if !exists || source.PageID != pageID {
				return graphRefused(errorCodeGraphReferenceInvalid, "The property's data source was not found.",
					"operations[%d].property.dataSourceId %q names no data source of this page", index, operation.Property.DataSourceID)
			}
			if operation.Property.Type == graphValueTypeRelation {
				target := graphRelationTarget(operation.Property.Type, operation.Property.Config)
				if _, exists := projection.sources[target]; target == "" || !exists {
					return graphInputError(errorCodeGraphReferenceInvalid, "The relation property's target data source was not found.")
				}
			}
		case graphOperationKindUpsertRow:
			source, exists := projection.sources[operation.Row.DataSourceID]
			if !exists || source.PageID != pageID {
				return graphRefused(errorCodeGraphReferenceInvalid, "The row's data source was not found.",
					"operations[%d].row.dataSourceId %q names no data source of this page", index, operation.Row.DataSourceID)
			}
			if operation.Row.SubitemParentID != nil {
				parent, exists := projection.rows[*operation.Row.SubitemParentID]
				if !exists || parent.DataSourceID != operation.Row.DataSourceID {
					return graphInputError(errorCodeGraphReferenceInvalid, "The parent item was not found in the same data source.")
				}
			}
			for propertyID, value := range operation.Row.Values {
				property, exists := projection.properties[propertyID]
				switch {
				case !exists || property.DataSourceID != operation.Row.DataSourceID:
					return graphRefused(errorCodeGraphReferenceInvalid, "Check the row property link.",
						"operations[%d].row.values.%s names no property of the row's data source", index, propertyID)
				case property.Type != value.Type:
					return graphRefused(errorCodeGraphReferenceInvalid, "Check the row property link.",
						"operations[%d].row.values.%s.type is %q, the property's type", index, propertyID, property.Type)
				}
				if value.Type == graphValueTypeRelation {
					targetSourceID := graphRelationTarget(property.Type, json.RawMessage(property.Config))
					if targetSourceID == "" {
						return graphInputError(errorCodeGraphReferenceInvalid, "The relation property's target data source was not found.")
					}
					for _, targetID := range graphRelationRowIDs(value.Value) {
						target, exists := projection.rows[targetID]
						if !exists || target.DataSourceID != targetSourceID {
							return graphInputError(errorCodeGraphReferenceInvalid, "Relation row link not found.")
						}
					}
				}
			}
		case graphOperationKindDeleteDataSource:
			source, exists := projection.sources[operation.DataSourceDeletion.ID]
			if !exists || source.PageID != pageID {
				return graphInputError(errorCodeGraphReferenceInvalid, "The data source to delete was not found.")
			}
		case graphOperationKindDeleteView:
			view, exists := projection.views[operation.ViewDeletion.ID]
			if !exists || view.PageID != pageID {
				return graphInputError(errorCodeGraphReferenceInvalid, "The view to delete was not found.")
			}
		case graphOperationKindDeleteProperty:
			property, exists := projection.properties[operation.PropertyDeletion.ID]
			if !exists || property.PageID != pageID {
				return graphInputError(errorCodeGraphReferenceInvalid, "The property to delete was not found.")
			}
		case graphOperationKindDeleteRow:
			row, exists := projection.rows[operation.RowDeletion.ID]
			if !exists || row.PageID != pageID {
				return graphInputError(errorCodeGraphReferenceInvalid, "The row to delete was not found.")
			}
		case graphOperationKindConfigureDependency:
			binding := operation.DependencyBinding
			if _, exists := projection.sources[binding.DataSourceID]; !exists {
				return graphInputError(errorCodeGraphReferenceInvalid, "Dependency data source not found.")
			}
			predecessor, predecessorExists := projection.properties[binding.PredecessorPropertyID]
			successor, successorExists := projection.properties[binding.SuccessorPropertyID]
			if !predecessorExists || !successorExists ||
				predecessor.DataSourceID != binding.DataSourceID || successor.DataSourceID != binding.DataSourceID ||
				predecessor.Type != graphValueTypeRelation || successor.Type != graphValueTypeRelation {
				return graphInputError(errorCodeGraphReferenceInvalid, "Dependency relation property not found.")
			}
		case graphOperationKindUpsertDependencyEdge:
			edge := operation.DependencyEdge
			predecessor, predecessorExists := projection.rows[edge.PredecessorRowID]
			successor, successorExists := projection.rows[edge.SuccessorRowID]
			if _, sourceExists := projection.sources[edge.DataSourceID]; !sourceExists ||
				!predecessorExists || !successorExists ||
				predecessor.DataSourceID != edge.DataSourceID || successor.DataSourceID != edge.DataSourceID {
				return graphInputError(errorCodeGraphReferenceInvalid, "Dependency row link not found.")
			}
		case graphOperationKindDeleteDependencyEdge:
			if _, exists := projection.sources[operation.DependencyEdgeDeletion.DataSourceID]; !exists {
				return graphInputError(errorCodeGraphReferenceInvalid, "Dependency data source not found.")
			}
		}
	}
	return nil
}
