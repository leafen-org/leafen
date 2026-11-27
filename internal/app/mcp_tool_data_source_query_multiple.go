package app

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's query across data sources (seen 2026-11-13) is the SQL that
// notion-query-data-sources runs over the data sources it names, which
// Leafen's runs for one or many alike (queryMCPDataSourceSQL).

type notionMCPQueryMultipleDataSourcesInput struct {
	DataSourceURLs []string `json:"data_source_urls" jsonschema:"2 to 10 collection:// data source URLs from notion-fetch, each a table the query names by its URL"`
	Query          string   `json:"query" jsonschema:"Read-only SQLite query using the quoted collection:// URLs as table names and the columns fetch's <sqlite-table> lists: JOINs, UNIONs and aggregations across them"`
	Params         []any    `json:"params,omitempty" jsonschema:"Values bound to the query's ? placeholders, in order: each a string, a number, true or false, or null"`
	Mode           string   `json:"mode,omitempty" jsonschema:"sql, the only mode"`
}

func registerMCPDataSourceQueryMultipleTool(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-query-multiple-data-sources", Title: "Query several Leafen data sources",
		Description: "Run read-only SQLite SQL across 2 to 10 data sources — JOINs, UNIONs, comparisons and aggregations — as notion-query-data-sources runs SQL. Fetch each data source first for its collection:// URL and columns; bind outside values with ? and params; put every filter in WHERE, since a view's filters and sorts don't apply. For one data source or a saved view, use notion-query-data-sources.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPQueryMultipleDataSourcesInput) (*mcp.CallToolResult, notionMCPQueryDataSourcesOutput, error) {
		if mode := strings.TrimSpace(input.Mode); mode != "" && mode != mcpDataSourceQueryModeSQL {
			return nil, notionMCPQueryDataSourcesOutput{}, mcpInvalid("mode is sql, the only mode this tool takes")
		}
		if len(input.DataSourceURLs) < 2 || len(input.DataSourceURLs) > 10 {
			return nil, notionMCPQueryDataSourcesOutput{}, mcpInvalid("data_source_urls names 2 to 10 data sources; for one, use notion-query-data-sources")
		}
		result, output, err := queryMCPDataSourceSQL(ctx, app, notionMCPQueryDataSourcesData{
			Mode: mcpDataSourceQueryModeSQL, DataSourceURLs: input.DataSourceURLs, Query: input.Query, Params: input.Params,
		})
		if result != nil || err != nil {
			return result, output, err
		}
		text, err := mcpFetchJSON(output)
		if err != nil {
			return nil, notionMCPQueryDataSourcesOutput{}, failedBecause(errMCPToolFailed, err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, output, nil
	})
}
