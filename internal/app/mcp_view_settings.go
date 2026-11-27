package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// mcpNewViewSettings are a new view's settings: what its layout starts with,
// then what its configure string says, what that says of a form's responses,
// and its warnings.
func mcpNewViewSettings(
	app core.App, authority mcpAuthority, source mcpDataSourceLocation, viewType, configure string,
) (map[string]any, notionViewFormSettingsInput, []string, error) {
	properties, views, err := loadMCPDataSourceSchema(app, source, authority.PrincipalID)
	if err != nil {
		return nil, notionViewFormSettingsInput{}, nil, mcpDataSourceQueryError(err)
	}
	// It picks its first property of a kind in the order the API lists them, as Notion's.
	ordered, err := notionPublicOrderedProperties(app, source.ID, properties)
	if err != nil {
		return nil, notionViewFormSettingsInput{}, nil, mcpDataSourceQueryError(err)
	}
	config := mcpNewViewConfig(viewType, ordered, views)
	if strings.TrimSpace(configure) == "" {
		if viewType == graphViewTypeChart {
			return nil, notionViewFormSettingsInput{}, nil, mcpInvalid(notionNewChartDirectiveMessage)
		}
		return config, notionViewFormSettingsInput{}, nil, nil
	}
	scope, err := mcpViewReaderScope(app, source, authority.PrincipalID)
	if err != nil {
		return nil, notionViewFormSettingsInput{}, nil, mcpDataSourceQueryError(err)
	}
	grouped, _ := config["groupBy"].(string)
	subGrouped, _ := config["subGroupBy"].(string)
	form, warnings, err := applyMCPViewConfigure(app, scope, viewType, config, configure, false, authority.clock())
	if err != nil {
		return config, form, nil, mcpViewConfigureError(err)
	}
	mcpRegroupedView(viewType, config, grouped, subGrouped, properties)
	if viewType == graphViewTypeChart {
		if refusal := mcpNewChartRefusal(config); refusal != "" {
			return nil, form, nil, mcpInvalid("%s", refusal)
		}
	}
	return config, form, warnings, nil
}

// mcpNewChartRefusal refuses, as Notion does, a new chart that doesn't say
// what it charts (seen 2026-11-05): its type, and what groups it unless it is
// a single number.
func mcpNewChartRefusal(config map[string]any) string {
	stored, _ := config["chartType"].(string)
	if stored == "" {
		return notionNewChartDirectiveMessage
	}
	for official, chartType := range notionViewChartTypes {
		if chartType == stored && official != "number" {
			if grouped, _ := config["chartGroupBy"].(string); grouped == "" {
				return fmt.Sprintf("A \"%s\" chart needs a GROUP BY directive for its x-axis.", official)
			}
		}
	}
	return ""
}

// mcpConfiguredViewSettings are a view's settings with a configure string's
// changes, what that says of a form's responses, and its warnings.
func mcpConfiguredViewSettings(
	app core.App, authority mcpAuthority, source mcpDataSourceLocation, view mcpViewLocation, configure string,
) (json.RawMessage, notionViewFormSettingsInput, []string, error) {
	scope, err := mcpViewReaderScope(app, source, authority.PrincipalID)
	if err != nil {
		return nil, notionViewFormSettingsInput{}, nil, mcpDataSourceQueryError(err)
	}
	config := map[string]any{}
	if err := json.Unmarshal([]byte(view.Config), &config); err != nil || config == nil {
		return nil, notionViewFormSettingsInput{}, nil, failedBecause(errMCPToolFailed, err)
	}
	grouped, _ := config["groupBy"].(string)
	subGrouped, _ := config["subGroupBy"].(string)
	form, warnings, err := applyMCPViewConfigure(app, scope, view.Type, config, configure, true, authority.clock())
	if err != nil {
		return nil, form, nil, mcpViewConfigureError(err)
	}
	regrouped, _ := config["groupBy"].(string)
	subRegrouped, _ := config["subGroupBy"].(string)
	if regrouped != grouped || subRegrouped != subGrouped {
		properties, _, err := loadMCPDataSourceSchema(app, source, authority.PrincipalID)
		if err != nil {
			return nil, form, nil, mcpDataSourceQueryError(err)
		}
		mcpRegroupedView(view.Type, config, grouped, subGrouped, properties)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, form, nil, failedBecause(errMCPToolFailed, err)
	}
	if !form.present() {
		return encoded, form, warnings, nil
	}
	link, found, err := loadStoredFormLinkByView(app, view.OwnerID, view.PageID, view.ID)
	if err != nil {
		return nil, form, nil, failedBecause(errMCPToolFailed, err)
	}
	audience := nextNotionViewFormAudience(link, found, form)
	formLocation := notionViewLocation{
		ID: view.ID, OwnerID: view.OwnerID, WorkspaceID: view.WorkspaceID, PageID: view.PageID,
		DataSourceID: view.DataSourceID, Type: view.Type, Config: string(encoded),
	}
	if err := checkNotionViewFormAudience(app, formLocation, audience); err != nil {
		return nil, form, nil, mcpViewConfigureError(err)
	}
	encoded, err = withNotionViewFormAudience(encoded, audience, form.access)
	return encoded, form, warnings, err
}

// mcpSaveViewFormSettings writes a form's new audience on its link, in the
// change that made it.
func mcpSaveViewFormSettings(
	app core.App, authority mcpAuthority, view mcpViewLocation, form notionViewFormSettingsInput, current pageAuthorization,
) error {
	link, found, err := loadStoredFormLinkByView(app, view.OwnerID, view.PageID, view.ID)
	if err != nil {
		return err
	}
	return saveNotionViewFormAudience(app, current.Workspace, authority.PrincipalID, notionViewLocation{
		ID: view.ID, OwnerID: view.OwnerID, WorkspaceID: view.WorkspaceID, PageID: view.PageID,
		DataSourceID: view.DataSourceID, Type: view.Type,
	}, nextNotionViewFormAudience(link, found, form), form.access)
}

func mcpViewReaderScope(app core.App, source mcpDataSourceLocation, principalID string) (notionViewFilterScope, error) {
	return notionViewReaderScope(app, notionViewLocation{
		DataSourceID: source.ID, SourcePageID: source.PageID, WorkspaceID: source.WorkspaceID, OwnerID: source.OwnerID,
	}, principalID)
}

// mcpViewConfigureError says what a view's settings refused in the API's words.
func mcpViewConfigureError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errMCPToolInvalid) || errors.Is(err, errMCPScopeDenied) {
		return err
	}
	return mcpInvalid("configure: %s", err.Error())
}

// createMCPLinkedView adds an inline linked view of a data source at the end
// of a page, as the screen's linked view does: its own copy of the view, so
// changing it never changes the database it shows.
func createMCPLinkedView(
	ctx context.Context,
	app core.App,
	authority mcpAuthority,
	dataSourceID, pageID, name, viewType string,
	input notionMCPCreateViewInput,
) (*mcp.CallToolResult, notionMCPViewOutput, error) {
	if viewType == graphViewTypeForm {
		return nil, notionMCPViewOutput{}, mcpInvalid("a form view goes in the database that holds its data source, not one that links it")
	}
	location, err := loadMCPDataSourceLocation(app, dataSourceID)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpDataSourceQueryError(err)
	}
	workspace, err := authorizeMCPViewSource(app, authority, location, true)
	if err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	sourceContainerID := ""
	err = app.DB().NewQuery(`
		SELECT [[container]].[[id]] FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]]=[[container]].[[id]]
		WHERE [[membership]].[[data_source_id]]={:dataSourceId} AND [[container]].[[kind]]='source'
		ORDER BY [[container]].[[id]] LIMIT 1
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&sourceContainerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notionMCPViewOutput{}, errMCPGoneOrUnreached
	}
	if err != nil {
		return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	authorize := func(txApp core.App) (pageAuthorization, error) {
		page, err := authorizePageBody(txApp, authority.PrincipalID, pageID, permissionWorkspaceWrite)
		if err != nil || page.Workspace.WorkspaceID != workspace.WorkspaceID || page.Workspace.OwnerID != location.OwnerID {
			return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
		}
		return page, nil
	}
	// A page of another workspace than the source's is told as such (mcp_other_workspace.go).
	if page, err := authorizePageBody(app, authority.PrincipalID, pageID, permissionWorkspaceRead); err == nil && page.Workspace.WorkspaceID != workspace.WorkspaceID {
		return nil, notionMCPViewOutput{}, mcpLinkedAcross(app, "a linked view", input.DataSourceID, workspace.WorkspaceID, page.Workspace.WorkspaceID)
	}
	if _, err := authorize(app); err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	config, _, warnings, err := mcpNewViewSettings(app, authority, location, viewType, input.Configure)
	if err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	if input.ShowDataSourceTitle != nil && !*input.ShowDataSourceTitle {
		config["showDataSourceTitle"] = false
	}
	if input.Position != nil {
		return nil, notionMCPViewOutput{}, mcpInvalid("position is for a view tab made with database_id")
	}
	encodedConfig, err := json.Marshal(config)
	if err != nil {
		return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, notionMCPViewOutput{}, errMCPToolFailed
	}
	seed := mcpCallSeed()
	blockID := mcpViewEntityID(authority.ConnectionID, seed, "linked-block")
	viewID := mcpViewEntityID(authority.ConnectionID, seed, "view")
	content, err := json.Marshal(map[string]any{
		"dataSourceId": dataSourceID, "viewId": viewID, "linkedDatabaseSource": true, "sourceContainerId": sourceContainerID,
	})
	if err != nil {
		return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	requestHash, err := mcpViewRequestHash("create-linked", viewID, sourceContainerID, dataSourceID, pageID, name+"\x00"+input.Configure, viewType)
	if err != nil {
		return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	receipt, err := persistence.applyContentGraphMutationContext(
		ctx, authority.PrincipalID, pageID,
		graphMutationInput{MutationID: mcpViewMutationID(authority.ConnectionID, seed, "create-mutation")},
		contentGraphMutationHooks{
			StructuralAppend: true,
			Authorize:        authorize,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return requestHash, nil
			},
			Prepare: func(_ core.App, _ pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				order := doc.GetArray(blockOrderRoot(""))
				var afterID *string
				if order.Len() > 0 {
					tail, ok := order.Get(order.Len() - 1).(string)
					if !ok || tail == "" {
						return contentGraphMutationPreparation{}, graphStructureAuthorityError()
					}
					afterID = &tail
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{
					{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
						ID: blockID, AfterID: afterID, Position: "p0000000000",
						Type: graphBlockTypeInlineDatabase, Content: content,
					}},
					{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
						ID: viewID, DataSourceID: dataSourceID, ContainerID: blockID,
						Type: viewType, Name: name, Position: "p0000000000", Config: encodedConfig,
					}},
				}}, nil
			},
		},
	)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpDatabaseCreateError(err)
	}
	if receipt.Counts.Views != 1 {
		return nil, notionMCPViewOutput{}, errMCPToolFailed
	}
	return nil, notionMCPViewOutput{
		Object: "view", ID: viewID, URL: mcpViewURLPrefix + viewID,
		Name: name, Type: viewType, DataSourceURL: mcpDataSourceURL(app, dataSourceID),
		ParentPageID: pageID, Config: config, Duplicate: receipt.Duplicate, Warnings: warnings,
	}, nil
}

// mcpViewFetchSettings are a view's settings as fetch reads them, its
// properties by name: what the configure language sets can be read back, its
// filter in Notion's words (mcp_view_fetch_vocabulary.go).
func mcpViewFetchSettings(
	config map[string]any, byID map[string]notionMCPDataSourceProperty,
) map[string]any {
	name := func(raw any) (string, bool) {
		id, _ := raw.(string)
		property, found := byID[id]
		return property.Name, found
	}
	settings := map[string]any{}
	if group, ok := config["filterGroup"].(map[string]any); ok {
		// A view with no condition has no filter, as Notion's has none.
		if filter := mcpViewAdvancedFilter(group, byID); filter != nil {
			if terms, _ := filter["filters"].([]any); len(terms) > 0 {
				settings["advancedFilter"] = filter
			}
		}
	}
	if quick, ok := config["quickFilters"].([]any); ok && len(quick) > 0 {
		terms := make([]any, 0, len(quick))
		for _, item := range quick {
			encoded, _ := json.Marshal(item)
			filter := databaseQueryFilter{}
			if json.Unmarshal(encoded, &filter) == nil {
				if condition := mcpFilterCondition(filter, byID); condition != nil {
					terms = append(terms, condition)
				}
			}
		}
		settings["quickFilters"] = terms
	}
	if sorts, ok := config["sorts"].([]any); ok && len(sorts) > 0 {
		named := make([]any, 0, len(sorts))
		for _, item := range sorts {
			entry, _ := item.(map[string]any)
			property, _ := name(entry["propertyId"])
			named = append(named, map[string]any{"property": property, "direction": entry["direction"]})
		}
		settings["sorts"] = named
	}
	for key, label := range map[string]string{
		"groupBy": "groupBy", "subGroupBy": "subGroupBy", "calendarBy": "calendarBy", "timelineBy": "timelineBy",
		"timelineEndBy": "timelineEndBy", "mapBy": "mapBy", "coverProperty": "cover",
	} {
		if property, found := name(config[key]); found {
			settings[label] = property
		}
	}
	for _, key := range []string{
		"groupByMode", "hideEmptyGroups", "wrap", "frozenColumns", "cardLayout", "cardSize", "fitImage", "icon",
	} {
		if value, set := config[key]; set {
			settings[key] = value
		}
	}
	if config["coverProperty"] == notionViewCardPreviews["page_content"] {
		settings["cover"] = "page_content"
	}
	if widths, ok := config["propertyWidths"].(map[string]any); ok && len(widths) > 0 {
		named := map[string]any{}
		for id, width := range widths {
			if property, found := name(id); found {
				named[property] = width
			}
		}
		settings["widths"] = named
	}
	return settings
}

func mcpViewListed(config map[string]any, id string, properties map[string]notionMCPDataSourceProperty) bool {
	raw, listing := config["visibleProperties"].([]any)
	return !listing || slices.Contains(raw, any(id)) || properties[id].Type == graphValueTypeTitle
}
