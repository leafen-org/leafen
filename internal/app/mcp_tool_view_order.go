package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Where a view's tab stands among its database's tabs, whether it shows its
// data source's title, and taking a tab away: what a person does to a
// database's tabs, as the API's view position and view deletion do
// (notion_view_mutation.go). The database's fetch lists its views in their
// tab order.

func checkMCPViewPosition(position *notionMCPViewPosition) error {
	if position == nil {
		return nil
	}
	switch position.Type {
	case "start", "end":
		if position.ViewID == "" {
			return nil
		}
	case "after_view":
		if _, ok := parseMCPViewIdentifier(position.ViewID); ok {
			return nil
		}
	}
	return mcpInvalid("position is {\"type\": \"start\"}, {\"type\": \"end\"} or {\"type\": \"after_view\", \"view_id\": …}")
}

// placedMCPView adds a new view where its position puts it among all its
// database's tabs, as the API does (notionPlacedTabOperations): last when it
// names none.
func placedMCPView(
	app core.App, container mcpDatabaseContainerLocation, created graphDatabaseView, position *notionMCPViewPosition,
) ([]graphOperation, error) {
	placement := notionTabPlacement{kind: "end"}
	anchorID := ""
	if position != nil {
		placement.kind = position.Type
	}
	if placement.kind == "after_view" {
		anchorID, _ = parseMCPViewIdentifier(position.ViewID)
		if anchor, err := loadMCPViewLocation(app, mcpLocalID(app, anchorID)); err == nil {
			placement.anchorID = anchor.ID
		}
	}
	operations, err := notionPlacedTabOperations(app, container.WorkspaceID, container.ID, created, placement)
	if errors.Is(err, errNotionTabAnchorMissing) {
		return nil, mcpInvalid("%s", notionViewNotInDatabase(anchorID).message)
	}
	return operations, err
}

// mcpViewWithTitleShown is a view's settings showing its data source's title
// or not; shown is how a view starts, so it is left unset.
func mcpViewWithTitleShown(raw json.RawMessage, shown bool) (json.RawMessage, error) {
	config := map[string]any{}
	if err := json.Unmarshal(raw, &config); err != nil || config == nil {
		return nil, failedBecause(errMCPToolFailed, err)
	}
	delete(config, "showDataSourceTitle")
	if !shown {
		config["showDataSourceTitle"] = false
	}
	return json.Marshal(config)
}

type leafenMCPDeleteViewInput struct {
	ViewID string `json:"view_id" jsonschema:"The view:// URL or ID of the view to take away, as fetch gives it"`
}

type leafenMCPDeleteViewOutput struct {
	Result string `json:"result"`
	ID     string `json:"id"`
}

// registerMCPViewDeleteTool takes a view's tab away, as a person's "삭제" in
// the tab's menu does: a database keeps one view of each source, and shows
// the next when the one it showed goes.
func registerMCPViewDeleteTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_delete_view", Title: "Delete a Leafen data source view",
		Description: "Take a view's tab away from its database, or a widget from its dashboard. A database keeps at least one view of each " +
			"data source; fetch the database to see its views in tab order. notion-create-view's position puts a new tab first or after another.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPDeleteViewInput) (*mcp.CallToolResult, leafenMCPDeleteViewOutput, error) {
		return deleteMCPView(ctx, app, input)
	})
}

func deleteMCPView(ctx context.Context, app core.App, input leafenMCPDeleteViewInput) (*mcp.CallToolResult, leafenMCPDeleteViewOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, leafenMCPDeleteViewOutput{}, err
	}
	viewID, ok := parseMCPViewIdentifier(input.ViewID)
	if !ok {
		return nil, leafenMCPDeleteViewOutput{}, mcpInvalid("view_id as notion-fetch gives it")
	}
	location, err := loadMCPViewLocation(app, mcpLocalID(app, viewID))
	if err != nil {
		return nil, leafenMCPDeleteViewOutput{}, mcpDataSourceQueryError(err)
	}
	source, err := loadMCPDataSourceLocation(app, location.DataSourceID)
	if err != nil || !viewBelongsToMCPDataSource(location, source) {
		return nil, leafenMCPDeleteViewOutput{}, mcpRefusedUnlessStorage(errMCPGoneOrUnreached, err)
	}
	linked := location.ContainerID != ""
	workspace, err := authorizeMCPViewSource(app, authority, source, linked)
	if err != nil {
		return nil, leafenMCPDeleteViewOutput{}, err
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, leafenMCPDeleteViewOutput{}, errMCPToolFailed
	}
	requestHash, err := mcpViewRequestHash("delete", location.ID, "", source.ID, location.PageID, "", "")
	if err != nil {
		return nil, leafenMCPDeleteViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	_, err = persistence.applyContentGraphMutationContext(
		ctx, authority.PrincipalID, location.PageID,
		graphMutationInput{MutationID: mcpViewMutationID(authority.ConnectionID, mcpCallSeed(), "delete-mutation")},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := loadMCPViewLocation(txApp, location.ID)
				if err != nil || !sameMCPViewLocation(current, location) {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
				}
				if _, err := authorizeMCPViewSource(txApp, authority, source, linked); err != nil {
					return pageAuthorization{}, err
				}
				authorized, err := authorizePageBody(txApp, authority.PrincipalID, location.PageID, permissionWorkspaceWrite)
				if err != nil || authorized.Workspace.WorkspaceID != workspace.WorkspaceID || authorized.Workspace.OwnerID != source.OwnerID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
				}
				return authorized, nil
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return requestHash, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				tab := notionViewLocation{}
				if err := txApp.DB().NewQuery(notionViewTabSelect + ` AND [[view]].[[id]] = {:id}`).
					Bind(dbx.Params{"workspaceId": location.WorkspaceID, "id": location.ID}).One(&tab); err != nil {
					return contentGraphMutationPreparation{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
				}
				operations, err := notionViewDeleteOperations(txApp, tab)
				if errors.Is(err, errNotionViewKeepsOne) {
					return contentGraphMutationPreparation{}, mcpInvalid("%s", notionViewKeepsOneMessage)
				}
				return contentGraphMutationPreparation{Operations: operations}, err
			},
		},
	)
	if err != nil {
		return nil, leafenMCPDeleteViewOutput{}, mcpDatabaseCreateError(err)
	}
	return nil, leafenMCPDeleteViewOutput{
		Result: fmt.Sprintf("Deleted view \"%s\" — %s%s", location.Name, mcpViewURLPrefix, location.ID), ID: location.ID,
	}, nil
}
