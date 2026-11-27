package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type notionMCPUnsupportedResult struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Reason      string `json:"reason"`
	Alternative string `json:"alternative"`
	Retryable   bool   `json:"retryable"`
}

// notionMCPUnsupportedToolSpec is one of Notion's MCP tools Leafen doesn't
// have. Called by its Notion name, it answers why and the alternative. A
// token lists it only when its page turns Notion's names on
// (mcp_notion_only_tools.go), in one line (Brief) with no arguments, last
// (2026-11-12: an agent connecting found 16 of 69 tools were these, read for
// nothing).
type notionMCPUnsupportedToolSpec struct {
	Name        string
	Brief       string
	Reason      string
	Alternative string
}

func registerApprovedUnsupportedNotionMCPTools(server *mcp.Server) {
	for _, spec := range notionMCPUnsupportedTools {
		registerApprovedUnsupportedNotionMCPTool(server, spec)
	}
}

var notionMCPUnsupportedTools = []notionMCPUnsupportedToolSpec{
	{
		Name:        "notion-ai-search",
		Brief:       "Not in Leafen; use notion-search instead.",
		Reason:      "Leafen does not own an AI or connected-source search runtime, provider credentials, privacy boundary, or billing authority.",
		Alternative: "Use notion-search for bounded keyword search over authorized Leafen pages.",
	},
	{
		// Notion's search over the apps connected to it, such as Slack or
		// Google Drive (its Search and read information page, 2026-11-13).
		Name:        "notion-connector-search",
		Brief:       "Not in Leafen (no connected-app search); use notion-search instead.",
		Reason:      "Leafen keeps no search over connected apps such as Slack or Google Drive; a service connected to Leafen is reached only through its own tools.",
		Alternative: "Use notion-search for authorized Leafen pages. On Leafen's native endpoint, search a service the person connected with that service's own tool: leafen_services lists them and leafen_service_call calls one.",
	},
	{
		Name:        "notion-download-skill",
		Brief:       "Not in Leafen (no Skills); use notion-fetch and the attachment tools instead.",
		Reason:      "Leafen does not own versioned Skill artifacts, supporting-file archives, or signed Skill downloads.",
		Alternative: "Use notion-fetch and the ordinary attachment tools for authorized page and file content.",
	},
	{
		Name:        "notion-convert-page-to-skill",
		Brief:       "Not in Leafen (no Skills); keep it an ordinary page.",
		Reason:      "Leafen does not own persistent page-to-Skill identity, version, artifact, or lifecycle authority.",
		Alternative: "Keep the content as an ordinary page and use notion-fetch or notion-update-page.",
	},
	{
		Name:        "notion-create-folder",
		Brief:       "Not in Leafen (no Folders); use notion-create-pages and notion-move-pages instead.",
		Reason:      "Leafen has no distinct Folder identity, lifecycle, permission, or import/export authority.",
		Alternative: "Use notion-create-pages and notion-move-pages to organize content with the page hierarchy.",
	},
	{
		Name:        "notion-list-agents",
		Brief:       "Not in Leafen (no Custom Agents); use pages and comments instead.",
		Reason:      "Leafen has governance records for agents but no Custom Agent execution directory or runtime.",
		Alternative: "Use ordinary pages and comments for collaborative records; governance records are not executable agents.",
	},
	{
		Name:        "notion-search-agents",
		Brief:       "Not in Leafen (no Custom Agents); use pages and comments instead.",
		Reason:      "Leafen has governance records for agents but no Custom Agent execution directory or runtime.",
		Alternative: "Use ordinary pages and comments for collaborative records; governance records are not executable agents.",
	},
	{
		Name:        "notion-query-sessions",
		Brief:       "Not in Leafen (no agent sessions); use pages and comments instead.",
		Reason:      "Leafen does not own a durable Custom Agent session, run, or event runtime.",
		Alternative: "Use ordinary pages and comments for durable collaborative records.",
	},
	{
		Name:        "notion-search-sessions",
		Brief:       "Not in Leafen (no agent sessions); use pages and comments instead.",
		Reason:      "Leafen does not own a durable Custom Agent session, run, or event runtime.",
		Alternative: "Use ordinary pages and comments for durable collaborative records.",
	},
	{
		Name:        "notion-spawn-session",
		Brief:       "Not in Leafen (no agent sessions); use pages and comments instead.",
		Reason:      "Leafen has no execution provider, session admission, credential, cost, or recovery authority for Custom Agents.",
		Alternative: "No agent-execution alternative is available; use ordinary page and comment tools for collaborative work.",
	},
	{
		Name:        "notion-get-session-status",
		Brief:       "Not in Leafen (no agent sessions); use pages and comments instead.",
		Reason:      "Leafen does not own a durable Custom Agent session, run, or event runtime.",
		Alternative: "No agent-session alternative is available; use ordinary pages and comments for durable records.",
	},
	{
		Name:        "notion-wait-session",
		Brief:       "Not in Leafen (no agent sessions); use pages and comments instead.",
		Reason:      "Leafen does not own a durable Custom Agent session, streaming, or wait runtime.",
		Alternative: "No agent-session alternative is available; use ordinary pages and comments for durable records.",
	},
	{
		Name:        "notion-stop-session",
		Brief:       "Not in Leafen (no agent sessions); use pages and comments instead.",
		Reason:      "Leafen does not own a Custom Agent run or cancellation authority.",
		Alternative: "No agent-session alternative is available; use ordinary pages and comments for durable records.",
	},
	{
		Name:        "notion-send-message-to-session",
		Brief:       "Not in Leafen (no agent sessions); use notion-create-comment instead.",
		Reason:      "Leafen does not own a Custom Agent session, follow-up message, or execution authority.",
		Alternative: "Use notion-create-comment for durable human collaboration on an authorized page.",
	},
	{
		Name:        "notion-list-session-events",
		Brief:       "Not in Leafen (no agent sessions); use notion-get-comments instead.",
		Reason:      "Leafen does not own a durable Custom Agent session-event stream or retention authority.",
		Alternative: "Use notion-get-comments for durable page discussion records.",
	},
	{
		Name:        "notion-read-session-event",
		Brief:       "Not in Leafen (no agent sessions); use notion-get-comments instead.",
		Reason:      "Leafen does not own a durable Custom Agent session-event body or retention authority.",
		Alternative: "Use notion-get-comments for durable page discussion records.",
	},
	{
		Name:        "notion-calendar-list-calendars",
		Brief:       "Not in Leafen (no calendars); keep dates in a database with a calendar view.",
		Reason:      "Leafen keeps no calendar accounts or events: it doesn't connect to Notion Calendar or to a calendar service.",
		Alternative: "Keep dated work in a database with a date property and a calendar view (notion-create-database, notion-create-view with type calendar), and read it with notion-query-data-sources.",
	},
	{
		Name:        "notion-calendar-list-events",
		Brief:       "Not in Leafen (no calendar events); keep dates in a database with a calendar view.",
		Reason:      "Leafen keeps no calendar accounts or events: it doesn't connect to Notion Calendar or to a calendar service.",
		Alternative: "Keep dated work in a database with a date property and a calendar view (notion-create-database, notion-create-view with type calendar), and read it with notion-query-data-sources.",
	},
	{
		Name:        "notion-calendar-list-coworkers-events",
		Brief:       "Not in Leafen (no coworkers' calendars); keep dates in a database with a calendar view.",
		Reason:      "Leafen keeps no calendar accounts or events: it doesn't connect to Notion Calendar or to a calendar service.",
		Alternative: "Keep dated work in a database with a date property and a calendar view (notion-create-database, notion-create-view with type calendar), and read it with notion-query-data-sources.",
	},
	{
		Name:        "notion-calendar-list-calendar-resources",
		Brief:       "Not in Leafen (no calendar rooms or resources); keep dates in a database with a calendar view.",
		Reason:      "Leafen keeps no calendar accounts or events: it doesn't connect to Notion Calendar or to a calendar service.",
		Alternative: "Keep dated work in a database with a date property and a calendar view (notion-create-database, notion-create-view with type calendar), and read it with notion-query-data-sources.",
	},
	{
		Name:        "notion-calendar-list-contacts",
		Brief:       "Not in Leafen (no calendar contacts); keep dates in a database with a calendar view.",
		Reason:      "Leafen keeps no calendar accounts or events: it doesn't connect to Notion Calendar or to a calendar service.",
		Alternative: "Keep dated work in a database with a date property and a calendar view (notion-create-database, notion-create-view with type calendar), and read it with notion-query-data-sources.",
	},
	{
		Name:        "notion-calendar-create-events",
		Brief:       "Not in Leafen (no calendar events to create); keep dates in a database with a calendar view.",
		Reason:      "Leafen keeps no calendar accounts or events: it doesn't connect to Notion Calendar or to a calendar service.",
		Alternative: "Keep dated work in a database with a date property and a calendar view (notion-create-database, notion-create-view with type calendar), and read it with notion-query-data-sources.",
	},
	{
		Name:        "notion-calendar-update-events",
		Brief:       "Not in Leafen (no calendar events to change); keep dates in a database with a calendar view.",
		Reason:      "Leafen keeps no calendar accounts or events: it doesn't connect to Notion Calendar or to a calendar service.",
		Alternative: "Keep dated work in a database with a date property and a calendar view (notion-create-database, notion-create-view with type calendar), and read it with notion-query-data-sources.",
	},
	{
		Name:        "notion-calendar-cancel-events",
		Brief:       "Not in Leafen (no calendar events to cancel); keep dates in a database with a calendar view.",
		Reason:      "Leafen keeps no calendar accounts or events: it doesn't connect to Notion Calendar or to a calendar service.",
		Alternative: "Keep dated work in a database with a date property and a calendar view (notion-create-database, notion-create-view with type calendar), and read it with notion-query-data-sources.",
	},
	{
		Name:        "notion-create-meeting-note",
		Brief:       "Not in Leafen (no AI meeting notes); add a row to a meeting notes database with notion-create-pages.",
		Reason:      "Leafen has meeting notes databases but no AI meeting notes: no recording, transcript, Notes tab or calendar event behind a note.",
		Alternative: "Use notion-create-pages with a meeting notes database's data source as the parent: the row is a meeting in that database, and its content the meeting's notes. With none yet, make one from the meetings template as notion-convert-to-meetings-database says.",
	},
	{
		Name:        "notion-convert-to-meetings-database",
		Brief:       "Not in Leafen (no converting a database into a meetings database); make one from the meetings template instead.",
		Reason:      "Leafen makes a meeting notes database only from its meetings template; an existing database doesn't turn into one.",
		Alternative: "Make a meeting notes database from the meetings template yourself, as the screen does: make an empty page where it goes (notion-create-pages, no content) and turn it into one with leafen_call POST /pages/{that page's ID}/database-starter, templateId \"meetings\", views [\"table\"], the person's language and the page's cursor — leafen_operations describes it with the screen's other templates. Or keep meetings in the existing database as ordinary rows.",
	},
	{
		Name:        "notion-link-meeting-note-to-calendar-event",
		Brief:       "Not in Leafen (no calendar events); link the event in the page with notion-update-page.",
		Reason:      "Leafen keeps no calendar events to link a meeting note to.",
		Alternative: "Put the event's date or address in the note with notion-update-page.",
	},
	{
		Name:        "notion-get-enabled-skills",
		Brief:       "Not in Leafen (no Skills); use notion-search and notion-fetch for instruction pages.",
		Reason:      "Leafen does not own Skill artifacts, enablement or connected-agent Skill settings.",
		Alternative: "Keep reusable instructions as ordinary pages and read them with notion-search and notion-fetch.",
	},
	{
		// Turns a person's own Skills on or off in Notion (its Reuse
		// instructions page, 2026-11-13).
		Name:        "notion-update-enabled-skills",
		Brief:       "Not in Leafen (no Skills to turn on or off); keep instructions as ordinary pages.",
		Reason:      "Leafen does not own Skill artifacts or a person's Skill enablement.",
		Alternative: "Keep reusable instructions as ordinary pages; nothing needs turning on. Read them with notion-search and notion-fetch.",
	},
	{
		Name:        "notion-update-skill-settings",
		Brief:       "Not in Leafen (no Skills); keep the page an ordinary page.",
		Reason:      "Leafen does not own Skill enablement, placement or connected-agent Skill settings.",
		Alternative: "Keep reusable instructions as ordinary pages; nothing needs turning on.",
	},
	{
		Name:        "notion-upload-skill",
		Brief:       "Not in Leafen (no Skills); write SKILL.md into a page with notion-create-pages and files with notion-create-attachment.",
		Reason:      "Leafen does not own Skill archives, Skills databases or Skill imports.",
		Alternative: "Write the instructions into an ordinary page with notion-create-pages or notion-update-page, and attach supporting files with notion-create-file-upload and notion-create-attachment.",
	},
	{
		Name:        "notion-update-folder",
		Brief:       "Not in Leafen (no Folders); use pages, sub-pages and notion-move-pages instead.",
		Reason:      "Leafen has no distinct Folder identity, lifecycle, permission, or import/export authority.",
		Alternative: "Use notion-create-pages, notion-move-pages and notion-create-attachment to organize files with the page hierarchy.",
	},
	{
		Name:        "notion-complete-mcp-setup",
		Brief:       "Not in Leafen (no setup to record); notion-get-setup-guide gives the guide.",
		Reason:      "Leafen keeps no record of a guided MCP setup: a connection is ready once the person connects it.",
		Alternative: "Nothing to record; read notion-get-setup-guide for how Leafen is used.",
	},
}

// addUnsupportedNotionMCPTool lists a tool Leafen doesn't have in one line,
// taking any arguments, and answers each call with text.
func addUnsupportedNotionMCPTool(server *mcp.Server, name, brief string, answer func() (string, error)) {
	server.AddTool(&mcp.Tool{
		Name: name, Description: brief, InputSchema: map[string]any{"type": "object"},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text, err := answer()
		if _, missing := mcpAuthorityFromContext(ctx); missing != nil {
			text = missing.Error()
		} else if err != nil {
			text = errMCPToolFailed.Error()
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: true}, nil
	})
}

func registerApprovedUnsupportedNotionMCPTool(server *mcp.Server, spec notionMCPUnsupportedToolSpec) {
	addUnsupportedNotionMCPTool(server, spec.Name, spec.Brief, func() (string, error) {
		body, err := json.Marshal(notionMCPUnsupportedResult{
			Code:        notionMeetingNotesUnsupportedCode,
			Message:     "This official Notion MCP capability is not enabled in Leafen.",
			Reason:      spec.Reason,
			Alternative: spec.Alternative,
			Retryable:   false,
		})
		return string(body), err
	})
}

// notionMCPToolUnsupported says whether a tool is one of Notion's Leafen
// doesn't have.
func notionMCPToolUnsupported(name string) bool {
	return name == notionMeetingNotesMCPToolName || slices.ContainsFunc(notionMCPUnsupportedTools,
		func(spec notionMCPUnsupportedToolSpec) bool { return spec.Name == name })
}

// mcpUnsupportedToolsLast lists the tools Leafen doesn't have after the ones
// it does, in their order, so an agent reads the tools it can use first.
func mcpUnsupportedToolsLast() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			listed, ok := result.(*mcp.ListToolsResult)
			if err != nil || method != "tools/list" || !ok || listed == nil {
				return result, err
			}
			ordered := *listed
			ordered.Tools = make([]*mcp.Tool, 0, len(listed.Tools))
			for _, last := range []bool{false, true} {
				for _, tool := range listed.Tools {
					if notionMCPToolUnsupported(tool.Name) == last {
						ordered.Tools = append(ordered.Tools, tool)
					}
				}
			}
			return &ordered, nil
		}
	}
}

// notionMCPUnsupportedOption answers an official tool option Leafen doesn't
// take as its unsupported tools answer: a tool error carrying the reason and
// what to do instead.
func notionMCPUnsupportedOption(reason, alternative string) error {
	body, err := json.Marshal(notionMCPUnsupportedResult{
		Code:        notionMeetingNotesUnsupportedCode,
		Message:     "This official Notion MCP capability is not enabled in Leafen.",
		Reason:      reason,
		Alternative: alternative,
		Retryable:   false,
	})
	if err != nil {
		return failedBecause(errMCPToolFailed, err)
	}
	return errors.New(string(body))
}

// mcpSkillPageUnsupported answers a page tool asked to make its page a Skill.
func mcpSkillPageUnsupported() error {
	return notionMCPUnsupportedOption(
		"Leafen does not own Skill identity, version, artifact, or lifecycle authority.",
		"Leave is_skill out to write the page as an ordinary page.",
	)
}
