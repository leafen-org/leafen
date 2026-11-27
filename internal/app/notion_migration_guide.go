package app

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The migration guide Leafen gives an agent (decision "설계 우선 Notion 이사"
// 4절): the flow, the plan format, the tools and the principles, as an MCP
// resource and a tool for clients that don't read resources.

//go:embed notion_migration_guide.md
var notionMigrationGuideText string

// notionMigrationGuide is the guide with the rule for the person's steps in
// Notion and the import screen (agent_outside_steps.go).
var notionMigrationGuide = withAgentOutsideSteps(notionMigrationGuideText)

const notionMigrationGuideURI = "leafen://guides/notion-migration"

type leafenMCPNotionMigrationGuideOutput struct {
	Guide string `json:"guide"`
}

func registerMCPNotionMigrationGuide(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	server.AddResource(&mcp.Resource{
		URI: notionMigrationGuideURI, Name: "notion-migration-guide", Title: "Moving a Notion workspace into Leafen",
		Description: "How to move the person's Notion pages and databases into Leafen with them: the flow, the plan format and the tools.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: notionMigrationGuideURI, MIMEType: "text/markdown", Text: notionMigrationGuideLead(ctx, app, server) + notionMigrationGuide,
		}}}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_notion_migration_guide", Title: "Read the Notion migration guide",
		Description: "Read this before moving the person's Notion workspace into Leafen: the flow, the plan format, the tools and the principles. The same text is the resource " + notionMigrationGuideURI + ".",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, leafenMCPNotionMigrationGuideOutput, error) {
		return nil, leafenMCPNotionMigrationGuideOutput{Guide: notionMigrationGuideLead(ctx, app, server) + notionMigrationGuide}, nil
	})
}

// notionMigrationImportTop is the page a migration's runs made for its first
// root that came over and shows (notionMigrationTopShows).
func notionMigrationImportTop(app core.App, stored storedNotionMigration) (string, error) {
	roots := []notionImportRoot{}
	if err := json.Unmarshal([]byte(stored.RootsJSON), &roots); err != nil {
		return "", err
	}
	for _, root := range roots {
		sourceID := root.ID
		if root.Kind == "database" {
			sourceID = notionDatabaseRootPageSourceID(root.ID)
		}
		var pageID string
		err := app.DB().NewQuery(`
			SELECT [[top]].[[native_id]] FROM {{leafen_notion_migration_ledger}} AS [[top]]
			` + notionMigrationTopShows + `
			WHERE [[top]].[[migration_id]]={:migrationId} AND [[top]].[[source_id]]={:sourceId} AND [[top]].[[kind]]='page'
		`).Bind(dbx.Params{"migrationId": stored.ID, "sourceId": sourceID}).Row(&pageID)
		if err == nil && pageID != "" {
			return pageID, nil
		}
	}
	return "", nil
}
