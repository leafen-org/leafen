//go:build !leafen_noapps

package app

import (
	"context"
	_ "embed"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// The guide an agent follows to help a person connect Gmail to Leafen's Mail
// app: an app password first, with no setup on the server; a Google client
// the administrator makes only when needed, one step at a time — as an MCP
// resource and a tool for clients that don't read resources.

//go:embed app_platform_gmail_guide.md
var gmailGuideText string

// gmailGuide is the guide with the rule for the person's steps at Google and
// in Leafen's window (agent_outside_steps.go).
var gmailGuide = withAgentOutsideSteps(gmailGuideText)

const gmailGuideURI = "leafen://guides/gmail"

type leafenMCPGmailGuideOutput struct {
	Guide string `json:"guide"`
}

func registerMCPGmailGuide(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	server.AddResource(&mcp.Resource{
		URI: gmailGuideURI, Name: "gmail-guide", Title: "Connecting Gmail to Leafen",
		Description: "How a person connects their Gmail to Leafen's Mail app: an app password first; a Google client the administrator makes only when needed.",
		MIMEType:    "text/markdown",
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		// Its Leafen screens as the person opens them (person_links.go).
		text := withPersonLinks(ctx, app, gmailGuide)
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: gmailGuideURI, MIMEType: "text/markdown", Text: text}}}, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_gmail_guide", Title: "Read the Gmail connection guide",
		Description: "Read this before helping the person connect Gmail (or a Google client) to Leafen. The same text is the resource " + gmailGuideURI + ".",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, leafenMCPGmailGuideOutput, error) {
		return nil, leafenMCPGmailGuideOutput{Guide: withPersonLinks(ctx, app, gmailGuide)}, nil
	})
}
