package app

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's Private and Shared sidebar lists (seen 2026-11-13): the top pages
// and databases of the person's Private section, and those of their Shared
// section, each with its type, address, title and emoji icon, in the order
// their sidebar shows them, read on with Notion's "offset:N" cursor. Leafen
// reads the sections by the sidebar's own rule (sidebar_section_pages.go), a
// workspace after another on a connection reaching several, each section
// sorted as the person's sidebar sorts it.

func registerMCPSidebarListTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-list-private-pages", Title: "List Leafen private pages",
		Description: "List the person's top-level pages and databases in their sidebar's Private section, in the sidebar's order. Use it to browse their private pages; to find pages by words or titles, use notion-search. Follow nextCursor for the whole list.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPPageListInput) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
		return listMCPSidebarSection(ctx, app, input, sidebarSectionPrivate)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-list-shared-pages", Title: "List Leafen pages shared with you",
		Description: "List the pages and databases in the person's sidebar Shared section: those shared with them directly, in the sidebar's order. Use it to browse what was shared with them; to find pages by words or titles, use notion-search. Follow nextCursor for the whole list.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPPageListInput) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
		return listMCPSidebarSection(ctx, app, input, sidebarSectionShared)
	})
}

func listMCPSidebarSection(
	ctx context.Context, app core.App, input notionMCPPageListInput, section string,
) (*mcp.CallToolResult, notionMCPPageListOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPPageListOutput{}, err
	}
	offset, limit, err := mcpOffsetCursor(input.Cursor, input.Limit, mcpPageListDefaultLimit, mcpPageListMaximumLimit)
	if err != nil {
		return nil, notionMCPPageListOutput{}, err
	}
	heads, err := readSidebarSectionHeads(ctx, app, authority.PrincipalID)
	if err != nil {
		return nil, notionMCPPageListOutput{}, failedBecause(errMCPToolFailed, err)
	}
	items := []notionMCPPageListItem{}
	for _, workspaceID := range authority.WorkspaceIDs {
		pages := heads[workspaceID].Private
		if section == sidebarSectionShared {
			pages = heads[workspaceID].Shared
		}
		if len(pages) == 0 {
			continue
		}
		layout, err := readMCPSidebarLayout(ctx, workspaceID)
		if err != nil {
			return nil, notionMCPPageListOutput{}, err
		}
		for _, page := range sortSidebarSection(pages, sidebarLayoutSectionSort(layout, section)) {
			if !mcpAuthorityReachesPageID(app, authority, page.ID) {
				continue
			}
			// Its title reads as its backlinks name it (mcpTitleAsShown).
			title := mcpTitleAsShown(app, authority, workspaceID, page.ID, page.Title)
			item := notionMCPPageListItem{Type: "page", URL: mcpPageURL(authority, page.ID), Title: title}
			if page.Kind == pageKindDatabase {
				item.Type, item.Title = "database", mcpDatabaseShownTitle(app, authority.PrincipalID, title)
				item.URL = mcpPageAddress(app, authority, page.ID)
			}
			if page.IconType == "emoji" {
				item.Icon = page.Icon
			}
			items = append(items, item)
		}
	}
	return nil, mcpPagedList(items, offset, limit), nil
}

// readMCPSidebarLayout reads the person's sidebar in a workspace as the
// screen reads it.
func readMCPSidebarLayout(ctx context.Context, workspaceID string) (sidebarLayoutWire, error) {
	result, err := mcpNativeCall(ctx, "GET", "/workspaces/"+workspaceID+"/sidebar-layout", "", nil)
	if err != nil {
		return sidebarLayoutWire{}, err
	}
	if err := mcpNativeRefusal(result); err != nil {
		return sidebarLayoutWire{}, err
	}
	layout := sidebarLayoutWire{}
	if encoded, err := json.Marshal(result.Body); err != nil || json.Unmarshal(encoded, &layout) != nil {
		return sidebarLayoutWire{}, failedBecause(errMCPToolFailed, err)
	}
	return layout, nil
}

// sidebarLayoutSectionSort is how a person's sidebar sorts one of its
// sections, wherever they keep it; the section's first sort when no tab has
// it.
func sidebarLayoutSectionSort(layout sidebarLayoutWire, id string) string {
	sections := slices.Clone(layout.Sections)
	for _, tab := range layout.Tabs {
		sections = append(sections, tab.Sections...)
	}
	for _, section := range append(sections, sidebarSectionDefaults...) {
		if section.ID == id {
			return section.Sort
		}
	}
	return sidebarSectionSortManual
}
