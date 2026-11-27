package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type notionDataSourceCreateRequest struct {
	Parent     json.RawMessage            `json:"parent"`
	Properties map[string]json.RawMessage `json:"properties"`
	Title      json.RawMessage            `json:"title,omitempty"`
	Icon       json.RawMessage            `json:"icon,omitempty"`
}

type notionDataSourceCreateContainer struct {
	ID          string `db:"id"`
	OwnerID     string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
}

// serveNotionDataSourceCreate appends one new own data source to an existing
// source database through the same graph mutation that owns its membership.
func serveNotionDataSourceCreate(event *core.RequestEvent, ygo *pocketBaseYgoPersistence) error {
	if ygo == nil {
		return notionRequestNotCompleted(event, nil)
	}
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:write", permissionWorkspaceWrite)
	if err != nil || !ok {
		return err
	}
	input := notionDataSourceCreateRequest{}
	if err := bindNotionStrictJSON(event, &input); err != nil {
		return notionBindRefused(event, err)
	}
	if input.Parent == nil || input.Properties == nil {
		return notionPageValidationError(event, "body.parent and body.properties are needed.")
	}
	publicDatabaseID, ok := notionDataSourceCreateParent(input.Parent)
	if !ok {
		return notionPageValidationError(event, "The data source parent must be a database_id.")
	}
	if len(input.Icon) != 0 {
		if _, err := notionPagePresentation(input.Icon, nil); err != nil {
			return notionPageValidationError(event, "The data source icon is invalid.")
		}
	}
	encoded, encodeErr := json.Marshal(input.Properties)
	if encodeErr != nil || len(encoded) > maxNotionDataSourceSchemaBytes ||
		len(input.Properties) > maxNotionDataSourceSchemaProperties {
		return notionPageValidationError(event, "The data source schema exceeds the supported size.")
	}
	noteNotionAskedObject(event, "database", publicDatabaseID)
	identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityDatabase, publicDatabaseID)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionPageNotFound(event)
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	container, err := authorizeNotionDataSourceCreateContainer(
		event.App, workspace.Identity, identity.InternalID,
	)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	// Its title's mentions are named for the database's workspace; none, or
	// only spaces, leaves it unnamed (notionDataSourceName).
	title, titleSpans := "", []graphTextSpan(nil)
	if len(input.Title) != 0 && string(input.Title) != "[]" {
		if title, titleSpans, err = notionDataSourceName(
			input.Title, notionNameMentions(event.App, workspace.Identity, container.Page),
		); err != nil {
			return notionDataSourceMutationError(event, err)
		}
	}
	dataSourceID := newDatabaseEntityID()
	viewID := newDatabaseEntityID()
	order := notionRequestKeyOrder(event, "properties")
	_, err = ygo.applyContentGraphMutationContext(
		event.Request.Context(), workspace.Identity.PrincipalID, container.Container.PageID,
		graphMutationInput{MutationID: "notion-public-data-source-create:" + newDiagnosticReference()},
		contentGraphMutationHooks{
			LinkVerificationProperties: true,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := authorizeNotionDataSourceCreateContainer(
					txApp, workspace.Identity, container.Container.ContainerID,
				)
				if err != nil || current.Container.PageID != container.Container.PageID {
					return pageAuthorization{}, refusedUnlessStorage(errDatabaseRowPageNotFound, err)
				}
				return current.Page, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, err := authorizeNotionDataSourceCreateContainer(
					txApp, workspace.Identity, container.Container.ContainerID,
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				operations, err := planNotionDataSourceCreate(
					txApp, doc, current, input.Properties, order, input.Icon, title, titleSpans, dataSourceID, viewID,
				)
				return contentGraphMutationPreparation{Operations: operations}, err
			},
		},
	)
	if err != nil {
		return notionDataSourceMutationError(event, err)
	}
	response, err := loadNotionDataSourceResponse(
		event.App, event.Request, workspace.Identity, dataSourceID, permissionWorkspaceRead,
	)
	if err != nil {
		return notionDataSourceReadError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func notionDataSourceCreateParent(raw json.RawMessage) (string, bool) {
	parent := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &parent) != nil || parent == nil {
		return "", false
	}
	for field := range parent {
		if field != "type" && field != "database_id" {
			return "", false
		}
	}
	if encoded, present := parent["type"]; present {
		var parentType string
		if json.Unmarshal(encoded, &parentType) != nil || parentType != "database_id" {
			return "", false
		}
	}
	var databaseID string
	if json.Unmarshal(parent["database_id"], &databaseID) != nil || strings.TrimSpace(databaseID) == "" {
		return "", false
	}
	return strings.TrimSpace(databaseID), true
}

// authorizeNotionDataSourceCreateContainer resolves a live source database the
// integration may edit. Linked databases never own new sources.
func authorizeNotionDataSourceCreateContainer(
	app core.App, identity notionOAuthBearerIdentity, containerID string,
) (notionDatabaseRowPageAuthorization, error) {
	container := notionDataSourceCreateContainer{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[owner]], [[container]].[[workspace_id]],
			[[container]].[[page_id]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_blocks}} AS [[block]]
			ON [[block]].[[id]] = [[container]].[[block_id]]
			AND [[block]].[[owner]] = [[container]].[[owner]]
			AND [[block]].[[page_id]] = [[container]].[[page_id]]
			AND [[block]].[[type]] = 'inline_database'
		WHERE [[container]].[[id]] = {:id} AND [[container]].[[workspace_id]] = {:workspaceId}
			AND [[container]].[[kind]] = 'source'
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]] = [[container]].[[id]]
			)
	`).Bind(dbx.Params{"id": containerID, "workspaceId": identity.WorkspaceID}).One(&container)
	if errors.Is(err, sql.ErrNoRows) {
		return notionDatabaseRowPageAuthorization{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	page, err := authorizePageBody(app, identity.PrincipalID, container.PageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) || err == nil && (page.Page.GetBool("in_trash") ||
		page.Workspace.WorkspaceID != identity.WorkspaceID ||
		page.Workspace.OwnerID != container.OwnerID ||
		!notionOAuthIdentityAllowsPage(app, identity, page)) {
		return notionDatabaseRowPageAuthorization{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	return notionDatabaseRowPageAuthorization{
		Identity: identity, Page: page,
		Container: notionDataSourceParentAuthority{
			ContainerID: container.ID, OwnerID: container.OwnerID,
			WorkspaceID: container.WorkspaceID, PageID: container.PageID,
		},
	}, nil
}

func planNotionDataSourceCreate(
	app core.App,
	doc *crdt.Doc,
	authorization notionDatabaseRowPageAuthorization,
	properties map[string]json.RawMessage,
	order []string,
	icon json.RawMessage,
	title string, titleSpans []graphTextSpan, dataSourceID, viewID string,
) ([]graphOperation, error) {
	metadata, err := structuralMetadata(doc)
	if err != nil {
		return nil, err
	}
	containerID := authorization.Container.ContainerID
	entry, exists := metadata[containerID]
	if !exists || entry.Type != graphBlockTypeInlineDatabase {
		return nil, errDatabaseRowPageNotFound
	}
	current, err := json.Marshal(entry.Content)
	if err != nil {
		return nil, err
	}
	membership, err := parseDatabaseContainerBlockContent(current)
	if err != nil || membership.LinkedDatabaseSource || membership.DataSourceID == "" {
		return nil, errDatabaseRowPageNotFound
	}
	var position string
	if err := app.DB().NewQuery(`
		SELECT [[position]] FROM {{leafen_blocks}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] = {:id}
	`).Bind(dbx.Params{
		"owner": authorization.Container.OwnerID, "pageId": authorization.Container.PageID, "id": containerID,
	}).Row(&position); err != nil {
		return nil, err
	}
	authorization.Container.DataSource, authorization.Container.Title = dataSourceID, title
	source := &graphDataSource{ID: dataSourceID, Title: title, TitleSpans: titleSpans}
	if len(icon) != 0 {
		if source.Icon, source.IconType, err = notionDataSourceIcon(app, authorization.Identity, icon); err != nil {
			return nil, err
		}
	}
	keys := notionNamedPropertyKeys(properties)
	schema, err := planNotionDataSourceSchema(app, authorization, dataSourceID, keys, order, properties)
	if err != nil {
		return nil, err
	}
	operations := append([]graphOperation{{Kind: graphOperationKindUpsertDataSource, DataSource: source}}, schema...)
	// Its view is named after it, as Notion names it (seen 2026-10-10); one
	// added without a name has its view named "New database", not the
	// "Default view" a new database's first view takes (seen 2026-11-18 in
	// two tabs, the source added to an inline and a full-page database).
	viewName := strings.TrimSpace(title)
	if viewName == "" {
		viewName = notionUnnamedSourceViewName
	}
	// It is the database's last tab, as Notion adds it (seen 2026-11-12).
	view, err := notionPlacedTabOperations(app, authorization.Container.WorkspaceID, containerID, graphDatabaseView{
		ID: viewID, DataSourceID: dataSourceID, Type: graphViewTypeTable,
		Name: viewName, Config: notionCreatedViewConfig(),
	}, notionTabPlacement{kind: "end"})
	if err != nil {
		return nil, err
	}
	operations = append(operations, view...)
	additional := slices.Clone(membership.AdditionalDataSourceIDs)
	entry.Content["additionalDataSourceIds"] = append(additional, dataSourceID)
	content, err := json.Marshal(entry.Content)
	if err != nil {
		return nil, err
	}
	block := &graphBlock{
		ID: containerID, Position: position, Type: graphBlockTypeInlineDatabase, Content: content,
	}
	if entry.ParentID != "" {
		parentID := entry.ParentID
		block.ParentID = &parentID
	}
	return append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: block}), nil
}

// planNotionDataSourceSchema plans a new source's properties in the given key
// order; exactly one of them is the title. Its formulas are checked in the
// order the request writes them (order, notion_formula_request_order.go).
func planNotionDataSourceSchema(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	dataSourceID string,
	keys, order []string,
	properties map[string]json.RawMessage,
) ([]graphOperation, error) {
	operations := make([]graphOperation, 0, len(keys))
	ids := map[string]string{}
	names := map[string]struct{}{}
	titles := 0
	for index, requestKey := range keys {
		key := strings.TrimSpace(requestKey)
		if key == "" && !notionDataSourceTitleRequest(properties[requestKey]) || string(properties[requestKey]) == "null" {
			return nil, notionDatabaseRowPageRequestInvalid("database property %q configuration is invalid", requestKey)
		}
		planned, err := planNotionDataSourceProperty(
			app, authorization, key, properties[requestKey], storedNotionDataSourceProperty{}, false, fmt.Sprintf("p%010d", index), true,
		)
		if errors.Is(err, errNotionDataSourcePropertyLeft) {
			continue
		}
		if err != nil {
			return nil, err
		}
		// A name another property has takes a number, as in Notion.
		if _, duplicate := names[planned.Name]; duplicate {
			taken := make(map[string]bool, len(names))
			for name := range names {
				taken[name] = true
			}
			planned.Name = distinctDatabasePropertyName(planned.Name, taken)
		}
		names[planned.Name] = struct{}{}
		ids[requestKey] = planned.ID
		if planned.Type == graphValueTypeTitle {
			titles++
		}
		operations = append(operations, mcpDataSourcePropertyOperation(dataSourceID, storedMCPDataSourceProperty{
			ID: planned.ID, Name: planned.Name, Type: planned.Type,
			Position: planned.Position, Config: planned.Config,
		}))
	}
	if titles != 1 {
		return nil, notionDatabaseRowPageRequestInvalid("a data source requires exactly one title property")
	}
	if err := resolveNotionRollupRequests(app, authorization, dataSourceID, operations, nil); err != nil {
		return nil, err
	}
	if err := checkNewNotionSourceFormulaTypes(operations, order, ids, dataSourceID, notionFormulaRelatedLoader(app)); err != nil {
		return nil, err
	}
	// A new source's properties have no IDs a reference could name yet.
	if err := storeNotionFormulaReferences(operations, func() (notionFormulaScope, error) {
		return notionFormulaScope{}, nil
	}); err != nil {
		return nil, err
	}
	written := map[string]bool{}
	for _, property := range notionFormulaNewSourceScope(operations) {
		written[property.ID] = true
	}
	if err := notionFormulaCycleRefusal(notionFormulaNewSourceScope(operations), written); err != nil {
		return nil, err
	}
	return operations, nil
}
