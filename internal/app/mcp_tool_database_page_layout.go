package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_database_page_layout reads and sets a database's page layout
// (database_page_layout.go), Notion's "레이아웃 사용자 지정", which Notion's
// API doesn't give: an agent matching a moved database to what Notion shows
// reads it, changes what differs and gives it back (the migration guide's
// "Match what Notion didn't give", 2026-11-12).

type leafenMCPDatabasePageLayoutInput struct {
	DataSourceID string         `json:"data_source_id" jsonschema:"The database's data source as notion-fetch gives it (collection://…)"`
	Layout       map[string]any `json:"layout,omitempty" jsonschema:"The layout to apply, in the shape this tool answers; left out, the tool only answers the current one"`
	Reset        bool           `json:"reset,omitzero" jsonschema:"true puts the database's pages back to Notion's original page"`
	Revision     *int           `json:"revision,omitempty" jsonschema:"The revision this tool answered; given, a change over a newer layout is refused"`
}

type leafenMCPDatabasePageLayoutProperty struct {
	ID   string `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
	Type string `json:"type" db:"type"`
}

type leafenMCPDatabasePageLayoutOutput struct {
	DataSourceID string `json:"data_source_id"`
	Revision     int    `json:"revision"`
	// Applied is false while the pages show Notion's original page, the
	// layout answered.
	Applied bool `json:"applied"`
	// Layout is a databasePageLayout, its tabs' view settings as they are.
	Layout     any                                   `json:"layout"`
	Properties []leafenMCPDatabasePageLayoutProperty `json:"properties"`
}

func registerMCPDatabasePageLayoutTool(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_database_page_layout", Title: "Read or set a database’s page layout",
		Description: "A database's page layout, Notion's \"Customize layout\" for all its pages, which Notion's API doesn't give. " +
			"Without layout it answers the current one (Notion’s original page until one is applied) and the properties’ IDs. " +
			"heading.pinned: property IDs under the title (up to 15); page and panel: modules in order, kind properties (the one property group), " +
			"property (propertyId), relations (relations: [{propertyId, display minimized or section}], the display options \"Minimal\" and \"As page section\"), " +
			"comments or content (one, on the page); propertyGroup: {kind:section,id,name} and " +
			"{kind:property,propertyId} in order; structure simple or tabbed (tabs); options: inlineComments default|minimal, pageComments, " +
			"propertyIcons, fullWidth, templates. Give layout back changed, with its revision, to apply it; reset goes back to the original. " +
			"A property's visibility on the pages (\"Property visibility\") is its own config's hideWhenEmpty and hideInPagePanel, not the layout's: " +
			"notion-update-data-source’s page_layout sets the layout and the visibility in Notion’s form.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPDatabasePageLayoutInput) (*mcp.CallToolResult, leafenMCPDatabasePageLayoutOutput, error) {
		return mcpDatabasePageLayout(ctx, app, input)
	})
}

func mcpDatabasePageLayout(
	ctx context.Context, app core.App, input leafenMCPDatabasePageLayoutInput,
) (*mcp.CallToolResult, leafenMCPDatabasePageLayoutOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, leafenMCPDatabasePageLayoutOutput{}, err
	}
	dataSourceID, ok := mcpLocalDataSourceID(app, input.DataSourceID)
	writes := input.Layout != nil
	raw, err := json.Marshal(input.Layout)
	if !ok || err != nil || writes && input.Reset {
		return nil, leafenMCPDatabasePageLayoutOutput{}, mcpInvalid("data_source_id as notion-fetch gives it (collection://…), and a layout to apply or reset, not both")
	}
	location, err := loadMCPDataSourceLocation(app, dataSourceID)
	if err != nil {
		return nil, leafenMCPDatabasePageLayoutOutput{}, mcpDataSourceQueryError(err)
	}
	permission, role, refused := permissionWorkspaceRead, pageAccessRoleView, errMCPCantRead
	if writes || input.Reset {
		permission, role, refused = permissionWorkspaceWrite, pageAccessRoleEdit, errMCPCantChange
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: location.WorkspaceID}, permission)
	if err != nil || workspace.WorkspaceID != location.WorkspaceID {
		return nil, leafenMCPDatabasePageLayoutOutput{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	output, revision := leafenMCPDatabasePageLayoutOutput{}, 0
	err = app.RunInTransaction(func(txApp core.App) error {
		if _, err := authorizeDatabasePageLayout(txApp, authority.PrincipalID, location.PageID, location.ID, role); err != nil {
			return refused
		}
		current, err := loadDatabasePageLayout(txApp, location.ID)
		if err != nil {
			return err
		}
		revision = current.Revision
		if (writes || input.Reset) && input.Revision != nil && *input.Revision != current.Revision {
			return errDatabasePageLayoutConflict
		}
		switch {
		case input.Reset:
			if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_database_page_layouts}} WHERE [[data_source_id]] = {:dataSourceId}`).
				Bind(dbx.Params{"dataSourceId": location.ID}).Execute(); err != nil {
				return err
			}
			current = databasePageLayoutResponse{DataSourceID: location.ID}
		case writes:
			parsed, err := parseDatabasePageLayout(txApp, raw, location.PageID, location.ID, location.WorkspaceID)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(parsed)
			if err != nil {
				return err
			}
			current.Revision++
			if err := storeDatabasePageLayout(txApp, location.ID, location.WorkspaceID, encoded, current.Revision,
				authority.PrincipalID, time.Now().UTC().UnixMilli()); err != nil {
				return err
			}
			current.Layout = &parsed
		}
		output = leafenMCPDatabasePageLayoutOutput{DataSourceID: mcpDataSourceURL(txApp, location.ID), Revision: current.Revision}
		output.Applied = current.Layout != nil
		output.Layout = defaultDatabasePageLayout()
		if current.Layout != nil {
			output.Layout = *current.Layout
		}
		output.Properties = []leafenMCPDatabasePageLayoutProperty{}
		return txApp.DB().NewQuery(`SELECT [[id]], [[name]], [[type]] FROM {{leafen_database_properties}}
			WHERE [[data_source_id]] = {:source} AND [[page_id]] = {:page} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
			ORDER BY [[position]], [[id]]`).
			Bind(dbx.Params{"source": location.ID, "page": location.PageID}).All(&output.Properties)
	})
	switch {
	case errors.Is(err, errDatabasePageLayoutInvalid):
		return nil, leafenMCPDatabasePageLayoutOutput{}, mcpInvalid("the layout doesn't read: each property once (not the title) among heading.pinned, " +
			"the modules and propertyGroup by an ID in properties, one properties and one content module (content on the page), " +
			"structure simple or tabbed, inlineComments default or minimal, and no field this tool doesn't answer")
	case errors.Is(err, errDatabasePageLayoutConflict):
		return nil, leafenMCPDatabasePageLayoutOutput{}, mcpInvalid("the layout is at revision %d now: read it again (without layout) "+
			"and make the change on that one", revision)
	case errors.Is(err, errMCPCantRead), errors.Is(err, errMCPCantChange):
		return nil, leafenMCPDatabasePageLayoutOutput{}, err
	case err != nil:
		return nil, leafenMCPDatabasePageLayoutOutput{}, failedBecause(errMCPToolFailed, err)
	}
	return nil, output, nil
}

// defaultDatabasePageLayout is the layout a database's pages have before one
// is applied, Notion's original page, as the screen lays it out
// (web/src/lib/databasePageLayout.ts).
func defaultDatabasePageLayout() databasePageLayout {
	return databasePageLayout{
		Heading: databasePageLayoutHeading{Pinned: []string{}, Labels: true},
		Page: []databasePageLayoutModule{
			{ID: "properties", Kind: "properties"}, {ID: "comments", Kind: "comments"}, {ID: "content", Kind: "content"},
		},
		Panel: []databasePageLayoutModule{}, PropertyGroup: []databasePageLayoutGroupItem{},
		Structure: "simple", Tabs: []databasePageLayoutTab{},
		Options: databasePageLayoutOptions{InlineComments: "default", PageComments: true, PropertyIcons: true, Templates: true},
	}
}
