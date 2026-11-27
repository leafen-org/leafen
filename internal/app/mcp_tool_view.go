package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const mcpViewURLPrefix = "view://"

type notionMCPCreateViewInput struct {
	DatabaseID          string `json:"database_id,omitempty" jsonschema:"The database to add a view tab to: its address, database:// URL or ID. Give this or parent_page_id"`
	DataSourceID        string `json:"data_source_id" jsonschema:"collection:// data source URL or bare Leafen data source ID"`
	Name                string `json:"name" jsonschema:"Non-empty view name"`
	Type                string `json:"type" jsonschema:"View type: table, board, list, calendar, timeline, gallery, feed, form, chart, map, or dashboard"`
	ParentPageID        string `json:"parent_page_id,omitempty" jsonschema:"A page to add an inline linked view of the data source to, at its end. Give this or database_id"`
	Configure           string `json:"configure,omitempty" jsonschema:"The view's settings in the view language: FILTER, QUICK FILTER, SORT BY, GROUP BY, SUB GROUP BY, CARD LAYOUT, CARD SIZE, CALENDAR BY, TIMELINE BY, MAP BY, SHOW, HIDE, COVER, WRAP CELLS, WRAP PROPERTIES, FREEZE COLUMNS, WIDTH, ICON, CHART and FORM. Read notion://docs/view-dsl-spec, or pass it to notion-fetch; don't guess the syntax"`
	ShowDataSourceTitle *bool  `json:"show_data_source_title,omitempty" jsonschema:"Whether the view shows its data source's title above it, as fetch's showDataSourceTitle reads it; true when left out"`
	// Position is where the new tab goes among its database's tabs, as the API's view position is.
	Position *notionMCPViewPosition `json:"position,omitempty" jsonschema:"With database_id, where the new view's tab goes among the database's tabs, whatever their data source: type start, end (when left out) or after_view with view_id"`
}

// notionMCPViewPosition is where a view goes among its database's tabs.
type notionMCPViewPosition struct {
	Type   string `json:"type" jsonschema:"start, end or after_view"`
	ViewID string `json:"view_id,omitempty" jsonschema:"With after_view, the view:// URL or ID of the tab it goes after"`
}

type notionMCPUpdateViewInput struct {
	ViewID              string `json:"view_id" jsonschema:"view:// URL or bare Leafen view ID"`
	Name                string `json:"name,omitempty" jsonschema:"New name for the view"`
	Configure           string `json:"configure,omitempty" jsonschema:"Settings to change in the view language, as notion-create-view takes them, and CLEAR FILTER, CLEAR QUICK FILTER, CLEAR SORT, CLEAR GROUP BY, CLEAR SUB GROUP BY, CLEAR ICON, CLEAR REFERENCE LINES and CLEAR COLOR RULES. Read notion://docs/view-dsl-spec, or pass it to notion-fetch; don't guess the syntax"`
	ShowDataSourceTitle *bool  `json:"show_data_source_title,omitempty" jsonschema:"Whether the view shows its data source's title above it, as fetch's showDataSourceTitle reads it"`
}

type mcpViewLocation struct {
	ID           string `db:"id"`
	OwnerID      string `db:"owner"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	ContainerID  string `db:"container_id"`
	Type         string `db:"type"`
	Name         string `db:"name"`
	Position     string `db:"position"`
	Config       string `db:"config"`
}

var mcpCreatableViewTypes = map[string]struct{}{
	graphViewTypeTable: {}, graphViewTypeBoard: {}, graphViewTypeList: {},
	graphViewTypeCalendar: {}, graphViewTypeTimeline: {}, graphViewTypeGallery: {},
	graphViewTypeForm: {}, graphViewTypeChart: {}, graphViewTypeMap: {},
	graphViewTypeDashboard: {}, graphViewTypeFeed: {},
}

func registerMCPViewTools(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-create-view", Title: "Create a Leafen data source view",
		Description: "Create a view on a data source. Give database_id to add a tab to that database, or parent_page_id to add an inline linked view at the end of that page. The view starts as a person's new view of its layout does; configure sets its filters, sorts, grouping and display in the view language: read " + mcpViewDSLSpecURI + ", or pass it to notion-fetch, and don't guess the syntax.",
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPCreateViewInput) (*mcp.CallToolResult, notionMCPViewAnswer, error) {
		answer, output, err := createMCPView(ctx, request, app, input)
		return mcpViewAnswered(mcpViewWithResult(ctx, app, answer, output, err, mcpViewCreatedLine))
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-update-view", Title: "Update a Leafen data source view",
		Description: "Update a view's name, filters, sorts, grouping or display; only what is given changes. configure uses the language notion-create-view does (" + mcpViewDSLSpecURI + "), and CLEAR removes settings.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, request *mcp.CallToolRequest, input notionMCPUpdateViewInput) (*mcp.CallToolResult, notionMCPViewAnswer, error) {
		answer, output, err := updateMCPView(ctx, request, app, input)
		return mcpViewAnswered(mcpViewWithResult(ctx, app, answer, output, err, mcpViewUpdatedLine))
	})
}

func createMCPView(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPCreateViewInput,
) (*mcp.CallToolResult, notionMCPViewOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	dataSourceID, sourceOK := mcpLocalDataSourceID(app, input.DataSourceID)
	parentPageID := mcpLocalID(app, input.ParentPageID)
	name := strings.TrimSpace(input.Name)
	viewType := strings.TrimSpace(input.Type)
	_, typeOK := mcpCreatableViewTypes[viewType]
	if !sourceOK || len(parentPageID) > maxGraphIdentifierLength ||
		name == "" || utf8.RuneCountInString(name) > maxGraphIdentifierLength || !typeOK {
		return nil, notionMCPViewOutput{}, mcpInvalid("data_source_id as notion-fetch gives it, a name of at most %d characters, and type one of table, board, list, calendar, timeline, gallery, feed, form, chart, map or dashboard", maxGraphIdentifierLength)
	}
	if strings.TrimSpace(input.DatabaseID) == "" {
		if parentPageID == "" {
			return nil, notionMCPViewOutput{}, mcpInvalid("give database_id for a view tab, or parent_page_id for an inline linked view")
		}
		return createMCPLinkedView(ctx, app, authority, dataSourceID, parentPageID, name, viewType, input)
	}
	databaseID, databaseOK := resolveMCPDatabaseID(app, mcpLocalID(app, input.DatabaseID))
	if !databaseOK {
		return nil, notionMCPViewOutput{}, mcpDenied("database_id names no database this connection can open: " +
			"give the database's address or ID as notion-fetch gave it")
	}
	container, err := loadMCPDatabaseContainerLocation(app, databaseID)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpDataSourceQueryError(err)
	}
	sourceContainer, err := resolveMCPDatabaseContainerSource(app, container, dataSourceID)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpViewSourceError(app, err, container.WorkspaceID, dataSourceID, input.DataSourceID)
	}
	location, err := loadMCPDataSourceLocation(app, dataSourceID)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpDataSourceQueryError(err)
	}
	if location.OwnerID != sourceContainer.OwnerID ||
		location.WorkspaceID != sourceContainer.WorkspaceID || location.PageID != sourceContainer.PageID {
		return nil, notionMCPViewOutput{}, mcpDenied("data_source_id isn't one of that database's sources")
	}
	// A database owns the views of the sources it links, and forms stay in the
	// database that holds their source.
	linked := sourceContainer.ID != container.ID
	if linked && viewType == graphViewTypeForm {
		return nil, notionMCPViewOutput{}, mcpInvalid("a form view goes in the database that holds its data source, not one that links it")
	}
	target := mcpDatabaseViewTarget(container, location, linked)
	workspace, err := authorizeMCPViewSource(app, authority, location, linked)
	if err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	if parentPageID != "" && parentPageID != container.PageID {
		return nil, notionMCPViewOutput{}, mcpInvalid("with database_id, parent_page_id is the page the database is on")
	}
	parentPageID = container.PageID
	config, form, warnings, err := mcpNewViewSettings(app, authority, location, viewType, input.Configure)
	if err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	if input.ShowDataSourceTitle != nil && !*input.ShowDataSourceTitle {
		config["showDataSourceTitle"] = false
	}
	if err := checkMCPViewPosition(input.Position); err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	encodedConfig, err := json.Marshal(config)
	if err != nil {
		return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	formAudience := ""
	if form.present() {
		formAudience = nextNotionViewFormAudience(storedFormLink{}, false, form)
		if encodedConfig, err = withNotionViewFormAudience(encodedConfig, formAudience, form.access); err != nil ||
			json.Unmarshal(encodedConfig, &config) != nil {
			return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
		}
	}
	if target, err := authorizePageBody(
		app, authority.PrincipalID, container.PageID, permissionWorkspaceWrite,
	); err != nil || target.Workspace.OwnerID != container.OwnerID ||
		target.Workspace.WorkspaceID != container.WorkspaceID {
		return nil, notionMCPViewOutput{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, notionMCPViewOutput{}, errMCPToolFailed
	}
	seed := mcpCallSeed()
	mutationID := mcpViewMutationID(authority.ConnectionID, seed, "create-mutation")
	viewID := mcpViewEntityID(authority.ConnectionID, seed, "view")
	requestHash, err := mcpViewRequestHash(
		"create", viewID, databaseID, dataSourceID, parentPageID, name+"\x00"+input.Configure, viewType,
	)
	if err != nil {
		return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	receipt, err := persistence.applyContentGraphMutationContext(
		ctx, authority.PrincipalID, target.PageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{StepsKeepGraph: true, // its step saves only the form's link, outside the graph
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				currentContainer, err := loadMCPDatabaseContainerLocation(txApp, databaseID)
				if err != nil || currentContainer != container {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
				}
				currentSource, err := resolveMCPDatabaseContainerSource(
					txApp, currentContainer, dataSourceID,
				)
				if err != nil || currentSource != sourceContainer {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
				}
				current, err := loadMCPDataSourceLocation(txApp, dataSourceID)
				if err != nil || !sameMCPDataSourceLocation(current, location) {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
				}
				sourcePermission := permissionWorkspaceWrite
				if linked {
					sourcePermission = permissionWorkspaceRead
				}
				authorized, err := authorizePageBody(
					txApp, authority.PrincipalID, location.PageID, sourcePermission,
				)
				if err != nil || authorized.Workspace.WorkspaceID != workspace.WorkspaceID ||
					authorized.Workspace.OwnerID != location.OwnerID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
				}
				containerPage, err := authorizePageBody(
					txApp, authority.PrincipalID, currentContainer.PageID, permissionWorkspaceWrite,
				)
				if err != nil || containerPage.Workspace.WorkspaceID != workspace.WorkspaceID ||
					containerPage.Workspace.OwnerID != location.OwnerID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
				}
				currentWorkspace, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{
					WorkspaceID: location.WorkspaceID,
				}, permissionWorkspaceWrite)
				if err != nil || currentWorkspace.WorkspaceID != workspace.WorkspaceID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
				}
				if linked {
					return containerPage, nil
				}
				return authorized, nil
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return requestHash, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				position, err := nextMCPViewPosition(txApp, location)
				if linked {
					position, err = nextMCPLinkedViewPosition(txApp, container)
				}
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				created := graphDatabaseView{
					ID: viewID, DataSourceID: dataSourceID, ContainerID: target.ContainerID,
					Type: viewType, Name: name, Position: position, Config: encodedConfig,
				}
				operations, err := placedMCPView(txApp, container, created, input.Position)
				return contentGraphMutationPreparation{Operations: operations}, err
			},
			AfterApply: func(txApp core.App, current pageAuthorization, doc *crdt.Doc) error {
				save := notionViewFormAudienceSaver(authority.PrincipalID, notionViewLocation{
					ID: viewID, OwnerID: location.OwnerID, WorkspaceID: location.WorkspaceID, PageID: target.PageID,
					DataSourceID: dataSourceID, Type: viewType,
				}, formAudience, form.access)
				if save == nil {
					return nil
				}
				return save(txApp, current)
			},
		},
	)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpDatabaseCreateError(err)
	}
	// A view placed before others renumbers them in the same change.
	if receipt.Counts.Views < 1 {
		return nil, notionMCPViewOutput{}, errMCPToolFailed
	}
	return nil, notionMCPViewOutput{
		Object: "view", ID: viewID, URL: mcpViewURLPrefix + viewID,
		Name: name, Type: viewType, DataSourceURL: mcpDataSourceURL(app, dataSourceID),
		ParentPageID: parentPageID, Config: config, Duplicate: receipt.Duplicate, Warnings: warnings,
	}, nil
}

func updateMCPView(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	input notionMCPUpdateViewInput,
) (*mcp.CallToolResult, notionMCPViewOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	viewID, idOK := parseMCPViewIdentifier(input.ViewID)
	name := strings.TrimSpace(input.Name)
	if !idOK || name == "" && strings.TrimSpace(input.Configure) == "" && input.ShowDataSourceTitle == nil ||
		utf8.RuneCountInString(name) > maxGraphIdentifierLength {
		return nil, notionMCPViewOutput{}, mcpInvalid("view_id as notion-fetch gives it, and a name of at most %d characters, configure or show_data_source_title to change", maxGraphIdentifierLength)
	}
	viewID = mcpLocalID(app, viewID)
	location, err := loadMCPViewLocation(app, viewID)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpDataSourceQueryError(err)
	}
	source, err := loadMCPDataSourceLocation(app, location.DataSourceID)
	if err != nil || !viewBelongsToMCPDataSource(location, source) {
		return nil, notionMCPViewOutput{}, mcpRefusedUnlessStorage(errMCPGoneOrUnreached, err)
	}
	linked := location.ContainerID != ""
	workspace, err := authorizeMCPViewSource(app, authority, source, linked)
	if err != nil {
		return nil, notionMCPViewOutput{}, err
	}
	// A configure string that doesn't read says so before anything changes.
	if strings.TrimSpace(input.Configure) != "" {
		if _, _, _, err := mcpConfiguredViewSettings(app, authority, source, location, input.Configure); err != nil {
			return nil, notionMCPViewOutput{}, err
		}
	}
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, notionMCPViewOutput{}, errMCPToolFailed
	}
	seed := mcpCallSeed()
	mutationID := mcpViewMutationID(authority.ConnectionID, seed, "update-mutation")
	form, warnings := notionViewFormSettingsInput{}, []string(nil)
	requestHash, err := mcpViewRequestHash("update", viewID, "", source.ID, location.PageID, name+"\x00"+input.Configure, "")
	if err != nil {
		return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	receipt, err := persistence.applyContentGraphMutationContext(
		ctx, authority.PrincipalID, location.PageID,
		graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{StepsKeepGraph: true, // its step saves only the form's link, outside the graph
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, err := loadMCPViewLocation(txApp, viewID)
				if err != nil || !sameMCPViewLocation(current, location) {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
				}
				if _, err := authorizeMCPViewSource(txApp, authority, source, linked); err != nil {
					return pageAuthorization{}, err
				}
				authorized, err := authorizePageBody(
					txApp, authority.PrincipalID, location.PageID, permissionWorkspaceWrite,
				)
				if err != nil || authorized.Workspace.WorkspaceID != workspace.WorkspaceID ||
					authorized.Workspace.OwnerID != source.OwnerID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
				}
				currentWorkspace, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{
					WorkspaceID: source.WorkspaceID,
				}, permissionWorkspaceWrite)
				if err != nil || currentWorkspace.WorkspaceID != workspace.WorkspaceID {
					return pageAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
				}
				return authorized, nil
			},
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return requestHash, nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, err := loadMCPViewLocation(txApp, viewID)
				if err != nil || !sameMCPViewLocation(current, location) {
					return contentGraphMutationPreparation{}, mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
				}
				viewName, config := name, json.RawMessage(current.Config)
				if viewName == "" {
					viewName = current.Name
				}
				if strings.TrimSpace(input.Configure) != "" {
					if config, form, warnings, err = mcpConfiguredViewSettings(txApp, authority, source, current, input.Configure); err != nil {
						return contentGraphMutationPreparation{}, err
					}
				}
				if input.ShowDataSourceTitle != nil {
					if config, err = mcpViewWithTitleShown(config, *input.ShowDataSourceTitle); err != nil {
						return contentGraphMutationPreparation{}, err
					}
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertView,
					View: &graphDatabaseView{
						ID: current.ID, DataSourceID: current.DataSourceID, ContainerID: current.ContainerID,
						Type: current.Type, Name: viewName, Position: current.Position,
						Config: config,
					},
				}}}, nil
			},
			AfterApply: func(txApp core.App, current pageAuthorization, doc *crdt.Doc) error {
				if !form.present() {
					return nil
				}
				return mcpSaveViewFormSettings(txApp, authority, location, form, current)
			},
		},
	)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpDatabaseCreateError(err)
	}
	if receipt.Counts.Views != 1 {
		return nil, notionMCPViewOutput{}, errMCPToolFailed
	}
	current, err := loadMCPViewLocation(app, viewID)
	if err != nil {
		return nil, notionMCPViewOutput{}, mcpDataSourceQueryError(err)
	}
	output, err := mcpViewOutput(app, current, receipt.Duplicate)
	if err != nil {
		return nil, notionMCPViewOutput{}, failedBecause(errMCPToolFailed, err)
	}
	output.Warnings = warnings
	return nil, output, nil
}

func authorizeMCPViewSourceWrite(
	app core.App,
	authority mcpAuthority,
	location mcpDataSourceLocation,
) (workspaceAuthorization, error) {
	page, err := authorizePageBody(app, authority.PrincipalID, location.PageID, permissionWorkspaceWrite)
	if err != nil || page.Workspace.WorkspaceID != location.WorkspaceID ||
		page.Workspace.OwnerID != location.OwnerID {
		return workspaceAuthorization{}, mcpRefusedUnlessStorage(errMCPCantChange, err)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: location.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != location.WorkspaceID {
		return workspaceAuthorization{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	return workspace, nil
}

func loadMCPViewLocation(app core.App, viewID string) (mcpViewLocation, error) {
	return loadMCPViewLocationWhere(app, viewID, databaseSourceNotTrashedSQL("[[source]].[[id]]"))
}

// loadMCPViewLocationToRead also finds a view of a data source in the trash,
// whose rows a view's query reads (mcp_fetch_trash.go).
func loadMCPViewLocationToRead(app core.App, viewID string) (mcpViewLocation, error) {
	return loadMCPViewLocationWhere(app, viewID, "TRUE")
}

func loadMCPViewLocationWhere(app core.App, viewID, sourceShown string) (mcpViewLocation, error) {
	// An ID as the API gives it names the same view, as Notion's one ID does.
	if identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityView, viewID); err == nil {
		viewID = identity.InternalID
	}
	location := mcpViewLocation{}
	err := app.DB().NewQuery(`
		SELECT [[view]].[[id]], [[view]].[[owner]], [[view]].[[workspace_id]],
			[[view]].[[page_id]], [[view]].[[data_source_id]], [[view]].[[container_id]], [[view]].[[type]],
			[[view]].[[name]], [[view]].[[position]], [[view]].[[config]]
		FROM {{leafen_database_views}} AS [[view]]
		JOIN {{leafen_data_sources}} AS [[source]]
			ON [[source]].[[id]] = [[view]].[[data_source_id]]
			AND [[source]].[[owner]] = [[view]].[[owner]]
			AND [[source]].[[workspace_id]] = [[view]].[[workspace_id]]
			AND ([[source]].[[page_id]] = [[view]].[[page_id]] OR [[view]].[[container_id]] != '')
		WHERE [[view]].[[id]] = {:id} AND ` + sourceShown + `
	`).Bind(dbx.Params{"id": viewID}).One(&location)
	if errors.Is(err, sql.ErrNoRows) {
		return mcpViewLocation{}, mcpDenied("that view is gone")
	}
	return location, err
}

func nextMCPViewPosition(app core.App, location mcpDataSourceLocation) (string, error) {
	current, err := loadMCPDataSourceLocation(app, location.ID)
	if err != nil || !sameMCPDataSourceLocation(current, location) {
		return "", mcpRefusedUnlessStorage(errMCPChangedMeanwhile, err)
	}
	var tail string
	err = app.DB().NewQuery(`
		SELECT [[position]] FROM {{leafen_database_views}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
		ORDER BY [[position]] DESC, [[id]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"owner": location.OwnerID, "workspaceId": location.WorkspaceID,
		"pageId": location.PageID, "dataSourceId": location.ID,
	}).Row(&tail)
	if errors.Is(err, sql.ErrNoRows) {
		return "p0000000000", nil
	}
	if err != nil {
		return "", err
	}
	return databasePositionAfter(tail)
}

func parseMCPViewIdentifier(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, mcpViewURLPrefix) {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, mcpViewURLPrefix))
	}
	return raw, raw != "" && len(raw) <= maxGraphIdentifierLength &&
		!strings.ContainsAny(raw, "/?#")
}

func mcpViewMutationID(connectionID, seed, kind string) string {
	digest := sha256.Sum256([]byte("leafen:mcp-view:v1\x00" + connectionID + "\x00" + seed + "\x00" + kind))
	return "mcp-view-" + kind + "-" + hex.EncodeToString(digest[:16])
}

// mcpEntityID names something an agent makes that people see — a view, a
// block, a data source, a property — the same for the same request, in the
// shape the screen gives its own: a UUID (2026-11-06: an agent's view showed
// in its link as "mcp-view-view-<hex>", its properties as
// "mcp-ds-update-property-<hex>").
func mcpEntityID(name string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(name)).String()
}

func mcpViewEntityID(connectionID, seed, kind string) string {
	return mcpEntityID("leafen:mcp-view:v1\x00" + connectionID + "\x00" + seed + "\x00" + kind)
}

func mcpViewRequestHash(
	operation, viewID, databaseID, sourceID, pageID, name, viewType string,
) (string, error) {
	payload, err := json.Marshal(struct {
		Operation  string `json:"operation"`
		ViewID     string `json:"view_id"`
		DatabaseID string `json:"database_id,omitempty"`
		SourceID   string `json:"source_id"`
		PageID     string `json:"page_id"`
		Name       string `json:"name"`
		Type       string `json:"type,omitempty"`
	}{operation, viewID, databaseID, sourceID, pageID, name, viewType})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func sameMCPDataSourceLocation(left, right mcpDataSourceLocation) bool {
	return left.ID == right.ID && left.OwnerID == right.OwnerID &&
		left.WorkspaceID == right.WorkspaceID && left.PageID == right.PageID
}

func viewBelongsToMCPDataSource(view mcpViewLocation, source mcpDataSourceLocation) bool {
	return view.DataSourceID == source.ID && view.OwnerID == source.OwnerID &&
		view.WorkspaceID == source.WorkspaceID && (view.PageID == source.PageID || view.ContainerID != "")
}

func sameMCPViewLocation(left, right mcpViewLocation) bool {
	return left.ID == right.ID && left.OwnerID == right.OwnerID &&
		left.WorkspaceID == right.WorkspaceID && left.PageID == right.PageID &&
		left.DataSourceID == right.DataSourceID && left.ContainerID == right.ContainerID
}

func mcpViewOutput(app core.App, location mcpViewLocation, duplicate bool) (notionMCPViewOutput, error) {
	config := map[string]any{}
	if err := json.Unmarshal([]byte(location.Config), &config); err != nil || config == nil {
		return notionMCPViewOutput{}, errors.New(errorCodeGraphReadFailed)
	}
	return notionMCPViewOutput{
		Object: "view", ID: location.ID, URL: mcpViewURLPrefix + location.ID,
		Name: location.Name, Type: location.Type,
		DataSourceURL: mcpDataSourceURL(app, location.DataSourceID),
		ParentPageID:  location.PageID, Config: config, Duplicate: duplicate,
	}, nil
}

func fetchMCPView(
	ctx context.Context,
	app core.App,
	viewID string,
) (*mcp.CallToolResult, notionMCPFetchOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPFetchOutput{}, err
	}
	output := notionMCPFetchOutput{}
	err = runNotionReadSnapshot(ctx, app, func(txApp core.App) error {
		location, err := loadMCPViewLocation(txApp, viewID)
		if err != nil {
			return err
		}
		page, err := authorizePageBody(
			txApp, authority.PrincipalID, location.PageID, permissionWorkspaceRead,
		)
		if err != nil || page.Workspace.WorkspaceID != location.WorkspaceID ||
			page.Workspace.OwnerID != location.OwnerID {
			return mcpRefusedUnlessStorage(errMCPCantRead, err)
		}
		if !mcpAuthorityReachesPage(txApp, authority, page.Page) {
			return errMCPOutsideTokenPages
		}
		workspace, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{
			WorkspaceID: location.WorkspaceID,
		}, permissionWorkspaceRead)
		if err != nil || workspace.WorkspaceID != location.WorkspaceID {
			return mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
		}
		view, err := mcpViewOutput(txApp, location, false)
		if err != nil {
			return err
		}
		source, err := loadMCPDataSourceLocation(txApp, location.DataSourceID)
		if err != nil {
			return err
		}
		// A linked view's database lies outside a narrowed token's pages
		// when its own page does.
		if !mcpAuthorityReachesPageID(txApp, authority, source.PageID) {
			return errMCPOutsideTokenPages
		}
		properties, _, err := loadMCPDataSourceSchema(txApp, source, authority.PrincipalID)
		if err != nil {
			return err
		}
		config, err := mcpFetchedViewConfig(txApp, authority.PrincipalID, notionMCPDataSourceView{
			ID: view.ID, Name: view.Name, Type: view.Type, Config: view.Config,
		}, location.DataSourceID, properties)
		if err != nil {
			return err
		}
		text, err := mcpViewFetchText(mcpFetchedView{URL: view.URL, Config: config})
		if err != nil {
			return err
		}
		output = notionMCPFetchOutput{
			Metadata: notionMCPFetchMetadata{Type: "view"}, Title: view.Name, URL: view.URL, Text: text,
		}
		return nil
	})
	if err != nil {
		return nil, notionMCPFetchOutput{}, mcpDataSourceQueryError(err)
	}
	return nil, output, nil
}
