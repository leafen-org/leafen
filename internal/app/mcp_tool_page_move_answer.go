package app

import (
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// notionMCPMoveAnswer is what notion-move-pages answers, as Notion's does
// (2026-11-21: "Successfully moved 1 item: <id>"): a line naming the pages
// moved, and warnings when a page's move left something behind.
type notionMCPMoveAnswer struct {
	Result   string   `json:"result"`
	Warnings []string `json:"warnings,omitempty"`
}

// mcpMoveAnswered is the move's answer from the pages it moved.
func mcpMoveAnswered(answer *mcp.CallToolResult, output notionMCPMovePagesOutput, err error) (*mcp.CallToolResult, notionMCPMoveAnswer, error) {
	if err != nil {
		return answer, notionMCPMoveAnswer{}, err
	}
	ids, warnings := make([]string, 0, len(output.Moved)), []string{}
	for _, page := range output.Moved {
		ids = append(ids, page.ID)
		if page.Warning != "" {
			warnings = append(warnings, page.Warning)
		}
	}
	items := "items"
	if len(ids) == 1 {
		items = "item"
	}
	moved := notionMCPMoveAnswer{Result: fmt.Sprintf("Successfully moved %d %s: %s", len(ids), items, strings.Join(ids, ", "))}
	if len(warnings) > 0 {
		moved.Warnings = warnings
	}
	return answer, moved, nil
}
