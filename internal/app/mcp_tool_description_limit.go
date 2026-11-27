package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A tool's description keeps within what Claude Code keeps of it, as the
// instructions do: 2,048 characters, counted in UTF-16 units, the rest cut
// (mcpInstructionsLimit; 2026-11-26, the gate's lap 13: leafen_operations'
// had come to 2,646, and its last 598 — how a call marked confirm is carried
// out after 428 — never reached an agent). The server checks every tool's at
// the longest a connection lists it — with the line a connection reaching
// several workspaces adds to a tool that takes no workspace_id
// (mcpListedArguments) — as it makes an endpoint's first MCP server, beside
// their arguments (checkedMCPToolArguments), and an installed app's tool,
// written from its maker's words, at the longest its manifest allows
// (appAgentToolDescriptionsRoom); it doesn't start when one is past it. A
// description that doesn't fit says less, or leaves to the tool's answers or
// to its input's own descriptions what applies only once it is called.

// mcpToolDescriptionLimit is the most of a tool's description an agent
// reads.
const mcpToolDescriptionLimit = mcpInstructionsLimit

// mcpListedDescriptionLongest is the longest a tool's description comes to
// on a connection's list.
func mcpListedDescriptionLongest(tool *mcp.Tool) int {
	properties, _ := mcpToolSchemaArguments(tool)
	_, takesWorkspace := properties["workspace_id"]
	length := utf16Length(tool.Description)
	if !takesWorkspace && !mcpWorkspaceChoosingTools[tool.Name] {
		length += utf16Length(mcpHeldWorkspaceLine)
	}
	return length
}

// checkMCPToolDescriptions says which of a server's tools, and of the apps'
// at the longest, have a description past what an agent reads.
func checkMCPToolDescriptions(tools []*mcp.Tool) error {
	problems := []string{}
	for _, tool := range tools {
		if length := mcpListedDescriptionLongest(tool); length > mcpToolDescriptionLimit {
			problems = append(problems, fmt.Sprintf("%s: %d of %d", tool.Name, length, mcpToolDescriptionLimit))
		}
	}
	if err := appAgentToolDescriptionsRoom(); err != nil {
		problems = append(problems, err.Error())
	}
	if len(problems) == 0 {
		return nil
	}
	slices.Sort(problems)
	return errors.New("MCP tool descriptions run past what an agent reads (mcp_tool_description_limit.go): " + strings.Join(problems, "; "))
}
