package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/pocketbase/pocketbase/core"
)

type notionViewNewDatabaseRequest struct {
	Parent struct {
		Type   string `json:"type"`
		PageID string `json:"page_id"`
	} `json:"parent"`
	Position *struct {
		Type    string `json:"type"`
		BlockID string `json:"block_id"`
	} `json:"position"`
}

// serveNotionViewCreateDatabase adds a linked database block to a page that
// shows the data source through the new view, like choosing a linked view of
// an existing data source in the editor.
func serveNotionViewCreateDatabase(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	input notionViewCreateRequest, name string,
) error {
	request := notionViewNewDatabaseRequest{}
	decoder := json.NewDecoder(bytes.NewReader(input.CreateDatabase))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || request.Parent.Type != "page_id" ||
		request.Position != nil && (request.Position.Type != "after_block" || request.Position.BlockID == "") {
		return notionPageValidationError(event, "create_database needs a page_id parent and an optional after_block position.")
	}
	// The new database links the source, so it can't hold a form.
	if input.Type == graphViewTypeForm {
		return notionPageValidationError(event, notionLinkedFormMessage)
	}
	pageID, err := resolveNotionParentPageRecordID(event.App, request.Parent.PageID)
	source, sourceErr := resolveNotionObjectIdentity(event.App, notionObjectIdentityDataSource, input.DataSourceID)
	if err != nil || sourceErr != nil || source.Workspace != identity.WorkspaceID {
		return notionViewReadError(event, errDatabaseRowPageNotFound)
	}
	page, err := authorizeNotionViewPages(event.App, identity, pageID, pageID, permissionWorkspaceWrite)
	if err != nil {
		return notionViewReadError(event, err)
	}
	owner, workspaceID := page.Workspace.OwnerID, identity.WorkspaceID
	reference, err := loadLinkedDatabaseReference(event.App, owner, workspaceID, "", source.InternalID)
	if errors.Is(err, sql.ErrNoRows) {
		err = errDatabaseRowPageNotFound
	}
	if err == nil {
		_, err = authorizeNotionViewPages(event.App, identity, pageID, reference.SourcePageID, permissionWorkspaceWrite)
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	var afterID *string
	if request.Position != nil {
		afterID, err = notionBodyAnchor(event.App, identity, pageID, "", request.Position.BlockID)
		if errors.Is(err, errNotionBodyAnchorMissing) || errors.Is(err, errNotionBodyAnchorOutside) ||
			errors.Is(err, errNotionBodyAnchorTrashed) {
			return notionPageValidationError(event, "position.block_id must be a block of the parent page.")
		}
	} else {
		afterID, err = lastTopLevelBlock(event.App, owner, pageID)
	}
	if err != nil {
		return notionViewReadError(event, err)
	}
	blockID := uuid.NewString()
	created := notionViewLocation{
		ID: uuid.NewString(), WorkspaceID: workspaceID, PageID: pageID, DataSourceID: source.InternalID,
		OwnContainer: blockID, ContainerID: blockID, SourcePageID: reference.SourcePageID,
		Type: input.Type, Name: name, Config: "{}",
	}
	// Its own row order from the start, as a view made in a database (notion_view_mutation.go).
	config, err := notionViewUpdatedConfig(event.App, identity.PrincipalID, created, string(notionCreatedViewConfig()), input.Filter, input.Sorts, input.QuickFilters, input.Configuration)
	if err == nil {
		config, err = notionAPICreatedViewConfig(event.App, identity.PrincipalID, created, config)
	}
	if err == nil {
		config, err = withNotionViewAPIFields(created.Type, config, input.Configuration, true)
	}
	if err == nil {
		config, err = withViewNameGiven(config, name)
	}
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	content, err := json.Marshal(map[string]any{
		"dataSourceId": source.InternalID, "viewId": created.ID,
		"linkedDatabaseSource": true, "sourceContainerId": reference.SourceContainerID,
	})
	if err != nil {
		return notionViewReadError(event, err)
	}
	_, err = ygo.applyContentGraphMutationContext(
		event.Request.Context(), identity.PrincipalID, pageID,
		graphMutationInput{
			MutationID: "notion-public-view-create-database:" + newDiagnosticReference(),
			Operations: []graphOperation{
				{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
					ID: blockID, AfterID: afterID, Position: "notion-view-database",
					Type: graphBlockTypeInlineDatabase, Content: content,
				}},
				notionViewOperation(created, name, "p0000000000", config),
			},
		},
		contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizeNotionViewPages(txApp, identity, pageID, reference.SourcePageID, permissionWorkspaceWrite)
			},
		},
	)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	publicID, err := ensureNotionObjectIdentity(event.App, notionObjectIdentityView, workspaceID, created.ID)
	if err != nil {
		return notionViewReadError(event, err)
	}
	return serveNotionViewRetrieve(event, publicID)
}
