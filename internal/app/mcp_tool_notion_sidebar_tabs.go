package app

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's sidebar tabs (seen 2026-11-13): get-sidebar-tabs gives the
// person's tabs, each with its shortcuts and sections — a built-in one by its
// id, or as an object with its item limit, a database view or a teamspace —
// and the catalogs of what may be added; update-sidebar-tabs takes the whole
// list back, an item kept by its id and one left out removed. Leafen's
// sidebar is one list without tabs (decision 2026-10-29), so it is given as
// one tab, "home", and taken back as one: its shortcuts, then its sections in
// order, a built-in section left out being taken off the sidebar (kept with
// its sort and count, as "섹션 제거" keeps it) and a database view or
// teamspace section left out removed. The change is the screen's, saved on
// the layout as it was read (sidebar_layout.go).

const (
	notionMCPSidebarHomeTab = sidebarHomeTabID
	notionMCPViewSection    = "database_view"
	notionMCPTeamspaceURL   = "teamspace://"
)

var (
	// Notion's names for Leafen's shortcuts, by Leafen's kind.
	notionMCPShortcutKinds = map[string]string{
		sidebarShortcutLibrary: "library", sidebarShortcutMyTasks: "tasks", sidebarShortcutNewDatabase: "new_database",
		sidebarShortcutDocuments: "documents", sidebarShortcutSettings: "settings",
	}
	notionMCPOpenModes = map[string]string{sidebarOpenSide: "side_peek", sidebarOpenCenter: "center_peek", sidebarOpenFull: "full_page"}
	// What Notion's sidebar has that Leafen's doesn't: Leafen holds no agents,
	// AI chats, meeting notes, calendar, skills or marketplace (2026-10-29).
	notionMCPSidebarAbsent = []string{"agents", "chats", "pinned_chats", "meetings_today", "myMeetings", "meeting_databases",
		"notion_apps", "workspace", "agent_companion", "new_chat", "new_meeting_note", "calendar", "skills", "meeting_notes",
		"marketplace", "agent", "chat_thread", "chat_dashboard"}
)

type notionMCPSidebarTab struct {
	ID        string `json:"id,omitempty" jsonschema:"home, the id get-sidebar-tabs gave Leafen's one list"`
	Name      string `json:"name" jsonschema:"The list's name; empty for the name the screen gives it"`
	Icon      string `json:"icon,omitempty" jsonschema:"Leafen's sidebar shows no tab icon: leave it out"`
	Shortcuts []any  `json:"shortcuts" jsonschema:"Ordered shortcuts: ids or objects from shortcutCatalog, each kept with its id"`
	Sections  []any  `json:"sections" jsonschema:"Ordered sections: ids or objects from sectionCatalog, each kept with its id"`
}

type notionMCPSidebarTabsInput struct {
	Tabs []notionMCPSidebarTab `json:"tabs" jsonschema:"The whole list as one tab, as get-sidebar-tabs gave it, with the changes"`
}

type notionMCPSidebarCatalogEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Examples    []any  `json:"examples"`
}

type notionMCPSidebarConfig struct {
	Version int                   `json:"version"`
	Tabs    []notionMCPSidebarTab `json:"tabs"`
}

type notionMCPSidebarTabsOutput struct {
	Config          notionMCPSidebarConfig         `json:"config"`
	ShortcutCatalog []notionMCPSidebarCatalogEntry `json:"shortcutCatalog"`
	SectionCatalog  []notionMCPSidebarCatalogEntry `json:"sectionCatalog"`
	Warnings        []string                       `json:"warnings,omitempty"`
}

func registerMCPSidebarTabTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-sidebar-tabs", Title: "Get the Leafen sidebar",
		Description: "Get the person's sidebar in a workspace — Leafen's is one list, given as one tab — with its shortcuts and sections in order, and catalogs of the shortcuts and sections that can be added. Catalog examples are valid notion-update-sidebar-tabs input. Call this before notion-update-sidebar-tabs.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, notionMCPSidebarTabsOutput, error) {
		workspaceID, err := mcpSidebarWorkspace(ctx, app)
		if err != nil {
			return nil, notionMCPSidebarTabsOutput{}, err
		}
		layout, err := readMCPSidebarLayout(ctx, workspaceID)
		if err != nil {
			return nil, notionMCPSidebarTabsOutput{}, err
		}
		return nil, notionMCPSidebarTabs(ctx, app, layout, nil), nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-update-sidebar-tabs", Title: "Change the Leafen sidebar",
		Description: "Replace the person's sidebar in a workspace: send back its one tab from notion-get-sidebar-tabs with every shortcut and section to keep, with its id, in the order wanted. A built-in section left out is taken off the sidebar; a database view or teamspace section or a shortcut left out is removed. Omit id only for a new shortcut or database view section. Database view sections need a view:// address or an address with v=; teamspace sections a teamspace:// address or a teamspace id from notion-get-teams. Check the returned config before saying it worked.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPSidebarTabsInput) (*mcp.CallToolResult, notionMCPSidebarTabsOutput, error) {
		return updateMCPSidebarTabs(ctx, app, input)
	})
}

// mcpSidebarWorkspace is the workspace whose sidebar a call reads: the one
// given as workspace_id (mcp_tool_arguments.go), or the connection's one;
// where it reaches several and none was given, the call names one.
func mcpSidebarWorkspace(ctx context.Context, app core.App) (string, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return "", err
	}
	if len(authority.WorkspaceIDs) != 1 {
		return "", mcpWorkspaceNeeded(authority, "")
	}
	return authority.WorkspaceIDs[0], nil
}

// notionMCPSidebarTabs writes a layout as Notion's one-tab config.
func notionMCPSidebarTabs(ctx context.Context, app core.App, layout sidebarLayoutWire, warnings []string) notionMCPSidebarTabsOutput {
	authority, _ := mcpAuthorityFromContext(ctx)
	tab := notionMCPSidebarTab{ID: notionMCPSidebarHomeTab, Name: layout.HomeName, Shortcuts: []any{}, Sections: []any{}}
	for _, shortcut := range layout.Shortcuts {
		tab.Shortcuts = append(tab.Shortcuts, notionMCPSidebarShortcut(app, authority, shortcut))
	}
	for _, section := range layout.Sections {
		if !section.Hidden {
			tab.Sections = append(tab.Sections, notionMCPSidebarSection(authority, section))
		}
	}
	return notionMCPSidebarTabsOutput{
		Config:          notionMCPSidebarConfig{Version: 1, Tabs: []notionMCPSidebarTab{tab}},
		ShortcutCatalog: notionMCPSidebarShortcutCatalog, SectionCatalog: notionMCPSidebarSectionCatalog, Warnings: warnings,
	}
}

func notionMCPSidebarSection(authority mcpAuthority, section sidebarLayoutSectionWire) any {
	switch {
	case section.ID == sidebarSectionView && section.View != nil:
		view := section.View
		out := map[string]any{"type": notionMCPViewSection, "id": view.Key, "viewUrl": mcpViewURLPrefix + view.ViewID,
			"itemLimit": section.Limit, "hidePageIcon": !view.ShowIcons}
		if view.Name != "" {
			out["customName"] = view.Name
		}
		if mode := notionMCPOpenModes[view.OpenIn]; mode != "" {
			out["openIn"] = mode
		}
		if view.TemplateID != "" {
			out["templateUrl"] = mcpPageURL(authority, view.TemplateID)
		}
		return out
	case section.ID == sidebarSectionTeamspace:
		return map[string]any{"type": "teamspace", "teamspaceUrl": notionMCPTeamspaceURL + section.TeamspaceID}
	case section.Limit != sidebarSectionDefaultLimit:
		return map[string]any{"type": section.ID, "itemLimit": section.Limit}
	}
	return section.ID
}

func notionMCPSidebarShortcut(app core.App, authority mcpAuthority, shortcut sidebarLayoutShortcutWire) any {
	out := map[string]any{"id": shortcut.Key}
	switch shortcut.Kind {
	case sidebarShortcutNewPage:
		if shortcut.PageID == "" && shortcut.Name == "" && shortcut.OpenIn == "" {
			return "new_page"
		}
		out["type"] = "new_page"
		if shortcut.PageID != "" {
			out["pageUrl"] = mcpPageURL(authority, shortcut.PageID)
		}
	case sidebarShortcutNewDatabasePage:
		out["type"], out["dataSourceUrl"], out["viewId"] = "new_page", mcpDataSourceURL(app, shortcut.DataSourceID), shortcut.ViewID
		if shortcut.TemplateID != "" {
			out["templateUrl"] = mcpPageURL(authority, shortcut.TemplateID)
		}
	case sidebarShortcutPage:
		out["type"], out["pageUrl"] = "page_link", mcpPageURL(authority, shortcut.PageID)
	case sidebarShortcutApp:
		out["type"], out["app"] = "app", shortcut.PageID
	default:
		if shortcut.Name == "" && shortcut.Kind != sidebarShortcutSettings {
			return notionMCPShortcutKinds[shortcut.Kind]
		}
		out["type"] = notionMCPShortcutKinds[shortcut.Kind]
	}
	if shortcut.Name != "" {
		out["label"] = shortcut.Name
	}
	if mode := notionMCPOpenModes[shortcut.OpenIn]; mode != "" {
		out["openIn"] = mode
	}
	return out
}

func updateMCPSidebarTabs(ctx context.Context, app core.App, input notionMCPSidebarTabsInput) (*mcp.CallToolResult, notionMCPSidebarTabsOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPSidebarTabsOutput{}, err
	}
	workspaceID, err := mcpSidebarWorkspace(ctx, app)
	if err != nil {
		return nil, notionMCPSidebarTabsOutput{}, err
	}
	if len(input.Tabs) != 1 || input.Tabs[0].ID != "" && input.Tabs[0].ID != notionMCPSidebarHomeTab {
		return nil, notionMCPSidebarTabsOutput{}, mcpInvalid("Leafen's sidebar is one list without tabs: send its one tab, id home, as notion-get-sidebar-tabs gave it, with every shortcut and section in it")
	}
	tab := input.Tabs[0]
	if !validSidebarTabName(tab.Name) {
		return nil, notionMCPSidebarTabsOutput{}, mcpInvalid("the name is at most %d characters", sidebarTabNameRunes)
	}
	current, err := readMCPSidebarLayout(ctx, workspaceID)
	if err != nil {
		return nil, notionMCPSidebarTabsOutput{}, err
	}
	reader := mcpSidebarReader{app: app, authority: authority, workspaceID: workspaceID, current: current}
	if tab.Icon != "" {
		reader.warnings = append(reader.warnings, "Leafen's sidebar shows no tab icon, so the icon was left out")
	}
	next := current
	if next.Shortcuts, err = reader.shortcuts(tab.Shortcuts); err != nil {
		return nil, notionMCPSidebarTabsOutput{}, err
	}
	if next.Sections, err = reader.sections(tab.Sections); err != nil {
		return nil, notionMCPSidebarTabsOutput{}, err
	}
	next.HomeName, next.Tabs = tab.Name, []sidebarLayoutTabWire{}
	result, err := mcpNativeCall(ctx, "PUT", "/workspaces/"+workspaceID+"/sidebar-layout", "", next)
	if err != nil {
		return nil, notionMCPSidebarTabsOutput{}, err
	}
	if err := mcpNativeRefusal(result); err != nil {
		return nil, notionMCPSidebarTabsOutput{}, err
	}
	saved := sidebarLayoutWire{}
	if encoded, err := json.Marshal(result.Body); err != nil || json.Unmarshal(encoded, &saved) != nil {
		return nil, notionMCPSidebarTabsOutput{}, failedBecause(errMCPToolFailed, err)
	}
	return nil, notionMCPSidebarTabs(ctx, app, saved, reader.warnings), nil
}

var notionMCPSidebarSectionCatalog = []notionMCPSidebarCatalogEntry{
	{ID: sidebarSectionFavorites, Description: "The person's favorite pages.", Examples: []any{"favorites"}},
	{ID: sidebarSectionTeamspaces, Description: "Teamspaces and their pages.", Examples: []any{"teamspaces"}},
	{ID: "teamspace", Description: "One teamspace the person is in, with its top pages; the teamspaces section then leaves it out. Use the object form with a teamspaceUrl.",
		Examples: []any{map[string]any{"type": "teamspace", "teamspaceUrl": "teamspace://<teamspace-id>"}}},
	{ID: sidebarSectionShared, Description: "Pages shared with the person.", Examples: []any{"shared"}},
	{ID: sidebarSectionPrivate, Description: "The person's private pages.", Examples: []any{"private"}},
	{ID: sidebarSectionRecents, Description: "Recently visited pages.", Examples: []any{"recents"}},
	{ID: sidebarSectionDeveloper, Description: "Developer tools and links.", Examples: []any{"developer"}},
	{ID: notionMCPViewSection, Description: "Rows from a database view. Use the object form with a viewUrl, and optionally itemLimit (5, 10, 15, 20, 50 or 100), customName, hidePageIcon, openIn and templateUrl. The sidebar holds up to 10.",
		Examples: []any{map[string]any{"type": notionMCPViewSection, "viewUrl": "view://<view-id>", "itemLimit": 5}}},
}

var notionMCPSidebarShortcutCatalog = []notionMCPSidebarCatalogEntry{
	{ID: "new_page", Description: "Make a new page: the string form at the top of the private pages, or the object form in a page (pageUrl) or a database (dataSourceUrl, with viewId and templateUrl), with a label and openIn.",
		Examples: []any{"new_page", map[string]any{"type": "new_page", "dataSourceUrl": "collection://<data-source-id>", "label": "New task"},
			map[string]any{"type": "new_page", "pageUrl": "<page address>", "openIn": "side_peek"}}},
	{ID: "page_link", Description: "Open a page or database. Use the object form with its pageUrl.",
		Examples: []any{map[string]any{"type": "page_link", "pageUrl": "<page address>"}}},
	{ID: "new_database", Description: "Make a new database.", Examples: []any{"new_database"}},
	{ID: "library", Description: "Open the library.", Examples: []any{"library"}},
	{ID: "tasks", Description: "Open the person's tasks.", Examples: []any{"tasks"}},
	{ID: "documents", Description: "Open the documents.", Examples: []any{"documents"}},
	{ID: "settings", Description: "Open the settings.", Examples: []any{map[string]any{"type": "settings"}}},
}
