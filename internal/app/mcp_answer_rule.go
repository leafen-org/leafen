package app

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// What an answer to an agent may hold (2026-11-21, the person: "낭비는 저거만의
// 문제가 아닐수도있는거 아니야?" — a move's status listed 310 items, and other
// answers carried more than the agent asked for). Every tool keeps one rule:
//
//   - A tool Notion's MCP has answers as Notion's does: with the fields
//     Notion's answer to the same call has (mcpNotionAnswerFields, read from
//     Notion's answers on 2026-11-21), and no more. Where Leafen has to say
//     how it differs it may add a field that Notion's answer has not, only
//     when there is something to say (left out otherwise), and named here
//     with why. Where Leafen's answer is Notion's, it stays so and is never cut
//     below it: an agent works with it as it did on Notion.
//   - A tool only Leafen has says a list that grows by its groups and counts,
//     and its items only when the call asks for them or a page at a time
//     (mcp_answer_lists.go): a list of records in its answer needs the tool
//     to take list and start_cursor, or start_cursor for the pages of what it
//     finds, unless the list is bounded (mcpLeafenAnswerBoundedLists says
//     by what).
//   - An answer is no longer than Notion's: a list Notion's answer stops at
//     its length (mcpNotionAnswerLength) stops there too, and says how to
//     read on as Notion's says it.
//   - What Leafen says of the call beside its result — the workspaces it
//     worked in, a key that ends soon — is a field of the answer's own,
//     never a line after it (mcpAnswerNoteFields, mcp_answer_notes.go).
//
// The server checks its own tools against this as it makes them, with the
// arguments' rule (mcp_tool_arguments.go), and doesn't start with one that
// breaks it — a new tool keeps the rule too.
//
// And from one version to the next an answer only adds fields: a field it
// gives keeps its name, its meaning and its type and is not dropped; one that
// has to change is added under a new name beside the old, which stays. An
// agent checks answers against the tool list it read when it connected and
// keeps that list across the server's upgrades; the list says what a tool
// gives open to fields it doesn't name (mcp_tool_schemas.go, 2026-11-23), so
// only a renamed, retyped or dropped field breaks it.
//
// And an answer leads only to Leafen's own tools: it says what is so — where
// Leafen is, what a call did, what is left to do in Leafen — and never asks
// an agent to change the person's files, its own settings or another service
// for Leafen's sake, nor tells it not to ask them (2026-11-23: the inbox note
// told agents to rewrite the person's instruction file "without asking", and
// careful agents took it for an injection and asked their person anyway).
// Such a change rides on what the person approved: the instruction lines
// they agreed to as they connected say to follow Leafen's address
// (agent_facing_address.go), and a step outside Leafen that a task the
// person asked for needs is that task's own, taken with the person as
// agent_outside_steps.go has it.

// mcpNotionAnswer is what Notion's answer to a tool holds: its fields, and
// those Leafen adds when it has something to say, each with why.
type mcpNotionAnswer struct {
	Fields []string
	Leafen map[string]string
}

var (
	mcpLeafenWarnings = map[string]string{"warnings": "what Leafen did otherwise than asked, or keeps otherwise than Notion, and names it gave that its answer doesn't show"}
	mcpNotionResult   = mcpNotionAnswer{Fields: []string{"result"}}
)

// mcpNotionAnswerFields are the fields of Notion's answers, tool by tool, as
// Notion's MCP gave them to the same calls on synthetic pages (2026-11-21).
// The tools Notion wasn't asked about — they read the workspace's people or
// all its pages, which would have put a real workspace's in the answers, or
// Notion refuses them below its Business plan — keep the fields their
// descriptions give Notion's answer.
var mcpNotionAnswerFields = map[string]mcpNotionAnswer{
	"notion-search": {Fields: []string{"results", "type", "has_more", "next_cursor"}, Leafen: map[string]string{
		"notice": "that Leafen's index is still being built and the search goes on", "backlog": "how much of the index is still to build",
	}},
	"notion-fetch": {Fields: []string{"metadata", "title", "url", "text", "references"}, Leafen: map[string]string{
		"workspace_id": "the workspace, on a connection reaching several", "path": "where the page is",
		"page_last_edited_at": "when the page was last edited, for a later notion-update-page to name",
		"verification":        "a wiki page's verification", "icon": "the page's icon", "cover": "the page's cover",
		"font": "the page's font", "full_width": "the page's full width", "small_text": "the page's small text",
		"truncated": "that the text stops where its blocks go on", "blocks": "the page's blocks, when include_blocks asks",
		"blocks_next_cursor": "where the page's blocks go on",
		"file_versions":      "each stored file's base_version, which leafen_replace_file takes, so replacing one needs no leafen_get_file first (2026-11-26)",
	}},
	"notion-create-pages":                {Fields: []string{"pages"}, Leafen: mcpLeafenWarnings},
	"notion-update-page":                 {Fields: []string{"page_id"}, Leafen: mcpLeafenWarnings},
	"notion-duplicate-page":              {Fields: []string{"page_id", "page_url"}},
	"notion-move-pages":                  {Fields: []string{"result"}, Leafen: mcpLeafenWarnings},
	"notion-delete-pages":                {Fields: []string{"results", "note"}},
	"notion-restore-pages":               {Fields: []string{"results", "note"}},
	"notion-delete-databases":            {Fields: []string{"results", "note"}},
	"notion-create-database":             {Fields: []string{"result"}, Leafen: mcpLeafenWarnings},
	"notion-update-data-source":          {Fields: []string{"result"}, Leafen: mcpLeafenWarnings},
	"notion-create-view":                 {Fields: []string{"result"}, Leafen: mcpLeafenWarnings},
	"notion-update-view":                 {Fields: []string{"result"}, Leafen: mcpLeafenWarnings},
	"notion-query-data-sources":          mcpNotionQueryAnswer,
	"notion-query-multiple-data-sources": mcpNotionQueryAnswer,
	"notion-create-comment":              {Fields: []string{"result"}, Leafen: mcpLeafenWarnings},
	"notion-update-comment":              {Fields: []string{"result"}, Leafen: mcpLeafenWarnings},
	"notion-delete-comment":              mcpNotionResult,
	"notion-set-comment-reaction":        mcpNotionResult,
	"notion-set-discussion-resolved":     mcpNotionResult,
	"notion-get-comments": {Fields: []string{"suggested_edits_status", "text"}, Leafen: map[string]string{
		"next_cursor": "where the discussions go on when not all of them fit in one answer",
	}},
	"notion-create-suggested-edit":  mcpNotionSuggestedEdit,
	"notion-get-suggested-edit":     mcpNotionSuggestedEdit,
	"notion-resolve-suggested-edit": mcpNotionSuggestedEdit,
	"notion-create-attachment": {Fields: []string{"file_upload_id", "filename", "content_type", "content_length", "status",
		"markdown_source", "suggested_markdown"}},
	"notion-create-file-upload": {Fields: []string{"upload_url", "upload_headers", "file_upload_id", "expires_at"}, Leafen: map[string]string{
		"upload_request": "how the file goes to upload_url — method, body, form field and a curl line — so no agent has to guess it (2026-11-26: one sent it another way first)",
	}},
	"notion-download-attachment":    {Fields: []string{"file_upload_id", "filename", "content_type", "content"}},
	"notion-get-file-download-urls": {Fields: []string{"files", "unresolved"}},
	"notion-get-async-task": {Fields: []string{"object", "id", "status", "created_time", "poll_after_seconds", "operation",
		"phase", "result", "error"}},
	"notion-get-version-changes": {Fields: []string{"type", "url", "start_time", "end_time", "authors", "changes",
		"older_changes_end_time", "message"},
		Leafen: map[string]string{"inline_databases": "the inline databases whose changes a call names their url to read"}},
	"notion-view-version": {Fields: []string{"type", "url", "content", "configuration", "properties", "rows", "total_rows",
		"next_cursor", "truncated"}},
	"notion-restore-version":        {Fields: []string{"status", "started_at", "message"}},
	"notion-list-page-backlinks":    {Fields: []string{"backlinks", "next_cursor"}},
	"notion-page-view":              {Fields: []string{"url", "title"}},
	"notion-get-users":              {Fields: []string{"object", "type", "results", "has_more", "next_cursor", "user"}},
	"notion-get-self":               {Fields: []string{"user", "workspace", "workspaces"}},
	"notion-get-teams":              {Fields: []string{"results", "joined", "available", "has_more", "workspaces"}},
	"notion-get-groups":             {Fields: []string{"results", "hasMore"}},
	"notion-get-tool-access":        {Fields: []string{"current_tool_access"}},
	"notion-get-setup-guide":        {Fields: []string{"guide"}},
	"notion-list-private-pages":     mcpNotionPageList,
	"notion-list-shared-pages":      mcpNotionPageList,
	"notion-list-recent-pages":      mcpNotionPageList,
	"notion-list-favorite-pages":    mcpNotionPageList,
	"notion-update-page-favorites":  mcpNotionPageList,
	"notion-get-sidebar-tabs":       mcpNotionSidebarTabs,
	"notion-update-sidebar-tabs":    mcpNotionSidebarTabs,
	"notion-memory-search":          {Fields: []string{"results", "type", "has_more", "next_cursor", "notice", "backlog"}},
	"notion-flavored-markdown-spec": {Fields: []string{"spec"}},
}

var (
	mcpNotionQueryAnswer = mcpNotionAnswer{Fields: []string{"results", "has_more", "next_cursor"}, Leafen: map[string]string{
		"data_source_ids": "the data sources a SQL query read", "status": "that a long query goes on",
		"next_action": "what to call while a long query goes on", "retry_after_seconds": "when to call again while it goes on",
		"warnings": "what Leafen read otherwise than Notion",
	}}
	mcpNotionSuggestedEdit = mcpNotionAnswer{Fields: []string{"suggested_edit_url", "status", "page_url", "author", "before", "after",
		"allowed_actions"}}
	mcpNotionPageList    = mcpNotionAnswer{Fields: []string{"results", "nextCursor"}}
	mcpNotionSidebarTabs = mcpNotionAnswer{Fields: []string{"config", "sectionCatalog", "shortcutCatalog"}, Leafen: mcpLeafenWarnings}
)

// mcpNotionAnswerLength is how long an answer grows before Notion's stops a
// list in it and says where the list goes on: get-version-changes gave 80
// of a database's changes in 65,267 characters, where the next would have
// passed 65,536, with older_changes_end_time and a call for the older ones
// (2026-11-22, seen once — the length is read from that one answer).
const mcpNotionAnswerLength = 1 << 16

// mcpNotionAnswerKept is how many of a list's items, in order, an answer
// holds within mcpNotionAnswerLength: around is the answer with the list
// empty and what it says when the list stops, written at their length.
func mcpNotionAnswerKept[Item any](around any, items []Item) (int, error) {
	length, err := mcpAnswerLength(around)
	if err != nil {
		return 0, err
	}
	for index, item := range items {
		size, err := mcpAnswerLength(item)
		if err != nil {
			return 0, err
		}
		if index > 0 {
			size++ // the comma before it
		}
		if length+size > mcpNotionAnswerLength {
			return index, nil
		}
		length += size
	}
	return len(items), nil
}

// mcpAnswerLength is an answer's length as Notion's is counted: the UTF-16
// code units of its JSON text.
func mcpAnswerLength(value any) (int, error) {
	text, err := mcpFetchJSON(value)
	if err != nil {
		return 0, err
	}
	length := 0
	for _, character := range text {
		length += utf16.RuneLen(character)
	}
	return length, nil
}

// mcpLeafenAnswerBoundedLists are the lists of records Leafen's own tools
// give whole, by what keeps each short: "tool field.path", a path standing
// for the lists under it too.
var mcpLeafenAnswerBoundedLists = map[string]string{
	"leafen_list_workspaces workspaces": "the workspaces fixed on the connection",
	"leafen_bug_report similar":         "the few open reports the report's words find",
	"leafen_inbox_item thread":          "one notice's discussion, as notion-get-comments gives one discussion",
	"leafen_remote_access reachedFrom":  "the addresses the server was reached from, a handful",
	"leafen_server_version changes":     "the newest changes, as many as one answer lists",
	"leafen_workday skipped":            "the weekend days and holidays inside the days counted",
	"leafen_update_page page":           "the page's own title and warnings",
	"leafen_update_page warnings":       "what the update did otherwise than asked",
	"leafen_fetch file_versions":        "the stored files the read's content names and the page's files properties hold",
	"leafen_workspace_move receiving":   "who to invite and which public pages to turn on again, which the agent goes through one by one",
	// A move's record (notionMigrationAnswered): the roots the person chose,
	// what stays at defaults by kind, the reported properties without their
	// row values, and of the check only what its next asks to fix.
	"leafen_notion_migration_start migration": "a move's record as notionMigrationAnswered gives it",
	"leafen_notion_migration_scan migration":  "a move's record as notionMigrationAnswered gives it",
	"leafen_notion_migration_run migration":   "a move's record as notionMigrationAnswered gives it",
	"leafen_notion_migration_run":             "the status's answer, whose item lists come only with leafen_notion_migration_status's list",
	// The plan's check and its one question: what the agent settles with the
	// person one by one before the run. The plan itself comes only when
	// include_draft or include_plan asks, as one document the agent edits.
	"leafen_notion_migration_plan":     "the plan's check, its one question, and the plan only when include_draft or include_plan asks",
	"leafen_notion_migration_set_plan": "the plan's check, its one question, and the plan only when include_draft or include_plan asks",
	"leafen_notion_migration_verify":   "what differs by group, and of it only what its next asks to fix",
}

// mcpLeafenAnswerBounded says why a Leafen tool's list at path is short.
func mcpLeafenAnswerBounded(tool, path string) bool {
	for at := path; ; {
		if _, bounded := mcpLeafenAnswerBoundedLists[tool+" "+at]; bounded {
			return true
		}
		cut := strings.LastIndex(at, ".")
		if cut < 0 {
			_, bounded := mcpLeafenAnswerBoundedLists[tool]
			return bounded
		}
		at = at[:cut]
	}
}

// checkMCPAnswerRule says where a server's tools break the answers' rule.
func checkMCPAnswerRule(tools []*mcp.Tool) error {
	problems := []string{}
	for _, tool := range tools {
		output := mcpAnswerSchemaOf(tool.OutputSchema)
		switch {
		case output == nil:
		case strings.HasPrefix(tool.Name, "notion-"):
			problems = append(problems, mcpNotionAnswerProblems(tool.Name, output)...)
		case strings.HasPrefix(tool.Name, "leafen_"):
			problems = append(problems, mcpLeafenAnswerProblems(tool, output)...)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	slices.Sort(problems)
	return fmt.Errorf("MCP answers break the answers' rule (mcp_answer_rule.go): %s", strings.Join(problems, "; "))
}

// mcpAnswerSchema is the part of an output schema the rule reads.
type mcpAnswerSchema struct {
	Type       any                         `json:"type"`
	Properties map[string]*mcpAnswerSchema `json:"properties"`
	Items      *mcpAnswerSchema            `json:"items"`
	Required   []string                    `json:"required"`
}

func mcpAnswerSchemaOf(schema any) *mcpAnswerSchema {
	if schema == nil {
		return nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	read := &mcpAnswerSchema{}
	if json.Unmarshal(data, read) != nil || len(read.Properties) == 0 {
		return nil
	}
	return read
}

func (schema *mcpAnswerSchema) is(kind string) bool {
	switch typed := schema.Type.(type) {
	case string:
		return typed == kind
	case []any:
		return slices.Contains(typed, any(kind))
	}
	return false
}

// mcpNotionAnswerProblems names a Notion tool's fields Notion's answer has
// not, and Leafen's own ones an answer always carries.
func mcpNotionAnswerProblems(name string, output *mcpAnswerSchema) []string {
	notion, known := mcpNotionAnswerFields[name]
	if !known {
		return []string{name + " answers fields no one compared with Notion's answer: add its Notion answer's to mcpNotionAnswerFields"}
	}
	problems := []string{}
	for field := range output.Properties {
		_, leafens := notion.Leafen[field]
		switch {
		case slices.Contains(notion.Fields, field):
		case !leafens:
			problems = append(problems, fmt.Sprintf("%s answers %s, which Notion's answer has not", name, field))
		case slices.Contains(output.Required, field):
			problems = append(problems, fmt.Sprintf("%s always answers %s: Leafen's own field goes only where it has something to say", name, field))
		}
	}
	return problems
}

// mcpLeafenAnswerProblems names the lists of records a Leafen tool answers
// whole, with no list to ask for them by nor pages to go through.
func mcpLeafenAnswerProblems(tool *mcp.Tool, output *mcpAnswerSchema) []string {
	takes, _ := mcpToolSchemaArguments(tool)
	_, cursor := takes["start_cursor"]
	_, list := takes["list"]
	if cursor {
		return nil
	}
	problems := []string{}
	for _, path := range mcpAnswerRecordLists(output, "") {
		if list || mcpLeafenAnswerBounded(tool.Name, path) {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s answers %s whole: take list and start_cursor (mcp_answer_lists.go), or say what bounds it in mcpLeafenAnswerBoundedLists", tool.Name, path))
	}
	return problems
}

// mcpAnswerRecordLists are the paths of an output's lists of records, a
// list's own records' lists after it.
func mcpAnswerRecordLists(schema *mcpAnswerSchema, at string) []string {
	if schema == nil {
		return nil
	}
	paths := []string{}
	if schema.is("array") && schema.Items != nil && len(schema.Items.Properties) > 0 {
		paths = append(paths, at)
		schema = schema.Items
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Properties)) {
		paths = append(paths, mcpAnswerRecordLists(schema.Properties[name], strings.TrimPrefix(at+"."+name, "."))...)
	}
	return paths
}
