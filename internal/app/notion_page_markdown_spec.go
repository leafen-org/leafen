package app

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The Markdown a page's body reads and writes (notion_page_markdown_spec.md),
// pointed to as Notion's MCP points to its own (seen 2026-10-28): each tool
// that takes page Markdown says, in Leafen's words, to read the resource
// notion://docs/notion-flavored-markdown-spec first, or to fetch that URI,
// rather than guess. Leafen serves its own syntax under the same URI, so an
// agent that learned Notion's tools finds it where it looks.

//go:embed notion_page_markdown_spec.md
var notionPageMarkdownSpec string

const notionPageMarkdownSpecURI = "notion://docs/notion-flavored-markdown-spec"

// notionPageMarkdownSpecRule opens the description of each tool that takes
// page Markdown.
const notionPageMarkdownSpecRule = "Page content is Leafen page Markdown, set out in the MCP resource `" + notionPageMarkdownSpecURI +
	"`: read it before writing — as a resource, or with notion-fetch given that URI where the client can't read resources. " +
	"Don't guess at syntax it doesn't set out: that is kept as plain text."

func registerMCPPageMarkdownSpec(server *mcp.Server) {
	server.AddResource(&mcp.Resource{
		URI: mcpViewDSLSpecURI, Name: "view-dsl-spec", Title: "Leafen view configuration",
		Description: "The language notion-create-view and notion-update-view take a view's filters, sorts, grouping and display in.",
		MIMEType:    "text/markdown",
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: mcpViewDSLSpecURI, MIMEType: "text/markdown", Text: mcpViewDSLSpec,
		}}}, nil
	})
	server.AddResource(&mcp.Resource{
		URI: notionPageMarkdownSpecURI, Name: "notion-flavored-markdown-spec", Title: "Leafen page Markdown",
		Description: "The Markdown a Leafen page's body reads and writes: CommonMark with Notion's tags for callouts, toggles, equations, columns, tables, colors and mentions.",
		MIMEType:    "text/markdown",
	}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
			URI: notionPageMarkdownSpecURI, MIMEType: "text/markdown", Text: notionPageMarkdownSpec,
		}}}, nil
	})
}

// fetchMCPPageMarkdownSpec is the specification as fetch gives it, as
// Notion's fetch gives a notion://docs/ resource.
func fetchMCPPageMarkdownSpec() notionMCPFetchOutput {
	return notionMCPFetchOutput{Metadata: notionMCPFetchMetadata{Type: "documentation"}, Title: "Leafen page Markdown",
		URL: notionPageMarkdownSpecURI, Text: notionPageMarkdownSpec}
}

// fetchMCPViewDSLSpec is the view language's specification as fetch gives it.
func fetchMCPViewDSLSpec() notionMCPFetchOutput {
	return notionMCPFetchOutput{Metadata: notionMCPFetchMetadata{Type: "documentation"}, Title: "Leafen view configuration",
		URL: mcpViewDSLSpecURI, Text: mcpViewDSLSpec}
}
