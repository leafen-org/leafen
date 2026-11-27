package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// applyDatabaseStarterInPlace puts the template's database where a new,
// still empty inline database stands, as Notion's start panel does with a
// suggestion picked for a database just made (seen 2026-10-30): the empty
// database's data source and the views its block owned go and the block
// shows the template's, keeping its ID so one opened alone or in a side peek
// stays where it is, while the page keeps its title and kind. With a data
// source named, only that new empty source of the database is replaced by
// the template's (the panel after "데이터 소스 추가"), and the database shows
// its first view. A source with rows or more than its title property is
// refused, so nothing made by hand is replaced.
func (p *pocketBaseYgoPersistence) applyDatabaseStarterInPlace(
	ctx context.Context, userID, pageID string, starter databaseStarterTemplate,
	input databaseStarterApplyInput, hash string, labels map[string]string,
) (databaseStarterApplyResponse, string, error) {
	prepared, err := prepareDatabaseStarter(starter, pageID, input, labels, time.Now().In(personLocation(p.app, userID)))
	if err != nil {
		return databaseStarterApplyResponse{}, "starter-prepare", err
	}
	mutation := graphMutationInput{MutationID: input.MutationID, Operations: prepared.Operations, BasePageCursor: &input.BasePageCursor, BaseGraphRevision: &input.BaseGraphRevision, AsHeld: input.AsHeld}
	receipt, err := p.applyContentGraphMutationContext(ctx, userID, pageID, mutation, contentGraphMutationHooks{
		EnforcePageLock: true, PersonChange: true, SuppressNotifications: true, SuppressAutomationEvents: true,
		ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) { return hash, nil },
		Prepare: func(app core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
			target := struct {
				Type     string `db:"type"`
				ParentID string `db:"parent_id"`
				Position string `db:"position"`
				Content  string `db:"content"`
			}{}
			if app.DB().NewQuery(`SELECT [[type]],[[parent_id]],[[position]],[[content]] FROM {{leafen_blocks}} WHERE [[id]]={:id} AND [[page_id]]={:pageId}`).
				Bind(dbx.Params{"id": input.ReplaceBlockID, "pageId": pageID}).One(&target) != nil {
				return contentGraphMutationPreparation{}, databaseStarterRefused("replaceBlockId names no block of this page")
			}
			content := map[string]any{}
			if target.Type != graphBlockTypeInlineDatabase {
				return contentGraphMutationPreparation{}, databaseStarterRefused("replaceBlockId names a %s block, not an inline database", target.Type)
			}
			if json.Unmarshal([]byte(target.Content), &content) != nil {
				return contentGraphMutationPreparation{}, databaseStarterInvalid()
			}
			// A linked view chose a template after all ("←" from its new view):
			// the block goes, its source is someone's own and stays.
			linked := content["linkedDatabaseSource"] == true
			if linked && input.ReplaceDataSourceID != "" {
				return contentGraphMutationPreparation{}, databaseStarterRefused("replaceDataSourceId goes with a database of its own, not a linked view, whose source is someone's own and stays")
			}
			emptyID, _ := content["dataSourceId"].(string)
			if input.ReplaceDataSourceID != "" && !replaceDatabaseStarterMember(content, input.ReplaceDataSourceID, prepared) {
				return contentGraphMutationPreparation{}, databaseStarterRefused("replaceDataSourceId names no data source of the database replaceBlockId names")
			} else if input.ReplaceDataSourceID != "" {
				emptyID = input.ReplaceDataSourceID
			}
			if !linked {
				if emptyID == "" {
					return contentGraphMutationPreparation{}, databaseStarterRefused("the database replaceBlockId names shows no data source to replace")
				}
				properties, err := databaseEntityIDsForSource(doc, databasePropertyMetaRoot, emptyID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				rows, err := databaseEntityIDsForSource(doc, databaseRowMetaRoot, emptyID)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				if len(rows) > 0 || len(properties) > 1 {
					return contentGraphMutationPreparation{}, databaseStarterRefused("the data source to replace has %d rows and %d properties: a template replaces only a new, empty one with its title property alone, so nothing made by hand is replaced",
						len(rows), len(properties))
				}
			}
			var parentID *string
			if target.ParentID != "" {
				parentID = &target.ParentID
			}
			operations := make([]graphOperation, 0, len(prepared.Operations)+2)
			for _, operation := range prepared.Operations {
				if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil {
					if input.ReplaceDataSourceID != "" {
						// The database stays; only its new source is the template's.
						continue
					}
					// The same block in its place (prepareDatabaseStarter gave it the block's ID).
					block := *operation.Block
					block.ParentID, block.AfterID, block.Position = parentID, nil, target.Position
					operation.Block = &block
				}
				operations = append(operations, operation)
			}
			if input.ReplaceDataSourceID != "" {
				encoded, err := json.Marshal(content)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				block := graphBlock{ID: input.ReplaceBlockID, ParentID: parentID, Position: target.Position, Type: graphBlockTypeInlineDatabase, Content: encoded}
				operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &block})
			} else {
				// The views the block owned, a linked view's "새 보기", go with what it showed.
				owned := []string{}
				if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_views}} WHERE [[container_id]]={:id} AND [[page_id]]={:pageId} ORDER BY [[id]]`).
					Bind(dbx.Params{"id": input.ReplaceBlockID, "pageId": pageID}).Column(&owned); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				for _, id := range owned {
					operations = append(operations, graphOperation{Kind: graphOperationKindDeleteView, ViewDeletion: &graphDatabaseDelete{ID: id}})
				}
			}
			if !linked {
				operations = append(operations, graphOperation{Kind: graphOperationKindDeleteDataSource, DataSourceDeletion: &graphDatabaseDelete{ID: emptyID}})
			}
			return contentGraphMutationPreparation{Operations: operations}, nil
		},
		AfterApply: func(app core.App, auth pageAuthorization, _ *crdt.Doc) error {
			return finishDatabaseStarter(app, auth, userID, pageID, prepared)
		},
	})
	if err != nil {
		return databaseStarterApplyResponse{}, "starter-apply", err
	}
	return databaseStarterApplyResponse{Receipt: receipt, DataSourceID: prepared.DataSourceID, ViewID: prepared.ViewID}, "", nil
}

// replaceDatabaseStarterMember swaps one of the database block's own sources
// for the template's in the block's content and shows the template's first
// view; it reports false when the source isn't the block's own.
func replaceDatabaseStarterMember(content map[string]any, sourceID string, prepared preparedDatabaseStarter) bool {
	found := false
	if first, _ := content["dataSourceId"].(string); first == sourceID {
		content["dataSourceId"], found = prepared.DataSourceID, true
	}
	if additional, ok := content["additionalDataSourceIds"].([]any); ok {
		for index, id := range additional {
			if id == sourceID {
				additional[index], found = prepared.DataSourceID, true
			}
		}
	}
	if found {
		content["viewId"] = prepared.ViewID
	}
	return found
}
