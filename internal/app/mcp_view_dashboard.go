package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// A dashboard reads over MCP as Notion's fetch gives it (seen 2026-11-05): it
// has no source of its own, and names its rows of widgets, each with its view
// as fetch gives that view.

// mcpFetchedViewConfig is any view's configuration as fetch gives it, inside
// the reader's transaction.
func mcpFetchedViewConfig(
	app core.App,
	principalID string,
	view notionMCPDataSourceView,
	dataSourceID string,
	properties []notionMCPDataSourceProperty,
) (map[string]any, error) {
	if view.Type == graphViewTypeDashboard {
		return mcpDashboardFetchConfig(app, principalID, view)
	}
	return mcpViewFetchConfig(app, view, dataSourceID, properties), nil
}

func mcpDashboardFetchConfig(app core.App, principalID string, view notionMCPDataSourceView) (map[string]any, error) {
	raw, err := json.Marshal(view.Config)
	if err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for _, row := range parseDashboardViewConfig(raw).DashboardRows {
		widgets := []map[string]any{}
		for _, widgetID := range row {
			fetched, found, err := mcpDashboardWidgetFetchConfig(app, principalID, widgetID)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			widgets = append(widgets, map[string]any{
				"id": dashboardWidgetPublicID(view.ID, widgetID), "view": fetched,
				"viewUrl": "{{" + mcpViewURLPrefix + widgetID + "}}",
			})
		}
		if len(widgets) > 0 {
			rows = append(rows, map[string]any{"id": dashboardRowPublicID(view.ID, row), "widgets": widgets})
		}
	}
	return map[string]any{"name": view.Name, "rows": rows, "type": view.Type}, nil
}

// mcpDashboardWidgetFetchConfig reads a widget's view; one on a source the
// reader can't open isn't listed.
func mcpDashboardWidgetFetchConfig(app core.App, principalID, widgetID string) (map[string]any, bool, error) {
	location, err := loadMCPViewLocation(app, widgetID)
	if err != nil {
		return nil, false, err
	}
	return mcpLocatedViewFetchConfig(app, principalID, location)
}

// mcpLocatedViewFetchConfig reads a stored view as fetch gives it, false for
// one whose source the reader can't open.
func mcpLocatedViewFetchConfig(app core.App, principalID string, location mcpViewLocation) (map[string]any, bool, error) {
	source, err := loadMCPDataSourceLocation(app, location.DataSourceID)
	if err != nil {
		return nil, false, err
	}
	if _, err := authorizePageBody(app, principalID, source.PageID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	properties, _, err := loadMCPDataSourceSchema(app, source, principalID)
	if err != nil {
		return nil, false, err
	}
	config := map[string]any{}
	if err := json.Unmarshal([]byte(location.Config), &config); err != nil || config == nil {
		return nil, false, failedBecause(errMCPToolFailed, err)
	}
	fetched, err := mcpFetchedViewConfig(app, principalID, notionMCPDataSourceView{
		ID: location.ID, Name: location.Name, Type: location.Type, Config: config,
	}, location.DataSourceID, properties)
	return fetched, err == nil, err
}

// notionMCPViewOutput is the view notion-create-view and notion-update-view
// made (their answer is notionMCPViewAnswer); warnings say what the configure
// string set that fetch doesn't show (mcp_view_configure_checks.go).
type notionMCPViewOutput struct {
	Result        string         `json:"result"`
	Object        string         `json:"object"`
	ID            string         `json:"id"`
	URL           string         `json:"url"`
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	DataSourceURL string         `json:"data_source_url"`
	ParentPageID  string         `json:"parent_page_id"`
	Config        map[string]any `json:"config,omitempty"`
	Duplicate     bool           `json:"duplicate"`
	Warnings      []string       `json:"warnings,omitempty"`
}

// notionMCPViewAnswer is what notion-create-view and notion-update-view
// answer, as Notion's do (2026-11-21): the result, and warnings when there
// are any.
type notionMCPViewAnswer struct {
	Result   string   `json:"result"`
	Warnings []string `json:"warnings,omitempty"`
}

// mcpViewAnswered is the view's answer from what the tool made of it.
func mcpViewAnswered(answer *mcp.CallToolResult, output notionMCPViewOutput, err error) (*mcp.CallToolResult, notionMCPViewAnswer, error) {
	return answer, notionMCPViewAnswer{Result: output.Result, Warnings: output.Warnings}, err
}

// mcpViewWithResult adds what Notion's create and update say of a view (seen
// 2026-11-05) to the answer: a line naming it, and the view as fetch gives it,
// which its config is too — by property names, never the stored settings
// that name properties by Leafen's IDs. The line alone stands where the view
// can't be read back.
func mcpViewWithResult(
	ctx context.Context,
	app core.App,
	answer *mcp.CallToolResult,
	output notionMCPViewOutput,
	err error,
	line func(notionMCPViewOutput) string,
) (*mcp.CallToolResult, notionMCPViewOutput, error) {
	if err != nil {
		return answer, output, err
	}
	output.Config, output.Result = nil, line(output)
	authority, authorityErr := mcpAuthorityFromContext(ctx)
	if authorityErr != nil {
		return answer, output, nil
	}
	location, loadErr := loadMCPViewLocation(app, output.ID)
	if loadErr != nil {
		return answer, output, nil
	}
	config, found, readErr := mcpLocatedViewFetchConfig(app, authority.PrincipalID, location)
	if readErr != nil || !found {
		return answer, output, nil
	}
	text, textErr := mcpFetchJSON(config)
	if textErr != nil {
		return answer, output, nil
	}
	output.Config = config
	output.Result = fmt.Sprintf("%s\n\n<view url=\"%s\">\n%s\n</view>", line(output), output.URL, text)
	return answer, output, nil
}

func mcpViewCreatedLine(view notionMCPViewOutput) string {
	return fmt.Sprintf("Created view \"%s\" (%s) — %s", view.Name, view.Type, view.URL)
}

func mcpViewUpdatedLine(view notionMCPViewOutput) string {
	return fmt.Sprintf("Updated view \"%s\" — %s", view.Name, view.URL)
}
