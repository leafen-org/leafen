package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

type leafenMCPOperationsInput struct {
	Group     string `json:"group,omitempty" jsonschema:"A group's name from the answer without arguments: its operations and Leafen's tools in it, those this connection can run and those only its person does."`
	Query     string `json:"query,omitempty" jsonschema:"English words, such as \"empty trash\" or \"invite member\": the operations and tools whose names, summaries or keywords hold them, best first, ten an answer."`
	Operation string `json:"operation,omitempty" jsonschema:"An operation id (\"METHOD /path\") to describe: what its body and query take, which fields it needs and the values they take, and the record it answers with; or a tool's name for its input."`
	All       bool   `json:"all,omitzero" jsonschema:"With group or query: also what this connection can't run, each with what it lacks."`
	Detail    string `json:"detail,omitempty" jsonschema:"With operation: full for every field of the records it holds and every record of its contract, its answers' too; left out, what a call sends. Alone: list for every operation's id, name and scope, full with method, path and contract too."`
	// StartCursor goes through what was found, a window at a time
	// (mcp_tool_operations_window.go).
	StartCursor string `json:"start_cursor,omitempty" jsonschema:"The next_cursor of the last answer to the same query or detail, to go on from there."`
}

type leafenMCPOperationRecordField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Required is a field the operation refuses the call without.
	Required bool `json:"required,omitzero"`
	// ZeroMeans is what 0 means where it isn't none, as a period of 0 days
	// that keeps everything.
	ZeroMeans string `json:"zeroMeans,omitempty"`
	// Deprecated is an old name kept on the wire: what it was, and its new one.
	Deprecated string `json:"deprecated,omitempty"`
	// Values are all a field takes, where its kind lists them: a list's
	// every entry, for a list.
	Values []string `json:"values,omitempty"`
	// Keys are all a map takes as keys, and RequiredKeys those a call needs.
	Keys         []string `json:"keys,omitempty"`
	RequiredKeys []string `json:"requiredKeys,omitempty"`
}

type leafenMCPOperationsOutput struct {
	// Groups are every group, for a call that names none and asks no words
	// (native_operation_groups.go); Group and About the one named, and
	// Hidden how many of it, or of what words found, this connection can't
	// run.
	Groups     []leafenMCPOperationGroup `json:"groups,omitempty"`
	Group      string                    `json:"group,omitempty"`
	About      string                    `json:"about,omitempty"`
	Operations []nativeOperation         `json:"operations"`
	Hidden     string                    `json:"hidden,omitempty"`
	// Connection is the grant of the connection asking, whose
	// this_connection each operation says (native_operation_grant.go).
	Connection *nativeOperationConnection `json:"connection,omitempty"`
	// Areas are how many operations each area of the API has, past one
	// window of every operation's list.
	Areas   map[string]int                             `json:"areas,omitempty"`
	Records map[string][]leafenMCPOperationRecordField `json:"records,omitempty"`
	// MoreFields are, for a record beside the body's, its actions' and the
	// answer that has many fields, how many it left out: detail "full" gives
	// them (native_operation_describe.go).
	MoreFields map[string]int `json:"more_fields,omitempty"`
	// Input is the record of the request's body where the operation has one,
	// and says so where it takes none.
	Input string `json:"input,omitempty"`
	// Answer is the record it answers with when it does what was asked.
	Answer string `json:"answer,omitempty"`
	// Query are the query names the operation reads, those it needs marked.
	Query []leafenMCPOperationRecordField `json:"query,omitempty"`
	// Kinds are all the values each named kind in the records takes.
	Kinds map[string][]string `json:"kinds,omitempty"`
	// Actions are the record of the body for each value of its action, where
	// that value decides what else it takes.
	Actions map[string]string `json:"actions,omitempty"`
	// BlockContents are what each kind of block keeps in its content.
	BlockContents map[string]string `json:"block_contents,omitempty"`
	// PropertyConfigs are what each type of property keeps in its config
	// beside the keys every one takes (native_operation_field_shapes.go).
	PropertyConfigs map[string]string `json:"property_configs,omitempty"`
	// FieldShapes are the keys the records' free-form fields hold, by
	// record.field.
	FieldShapes map[string]string `json:"field_shapes,omitempty"`
	// Bytes is what an operation that takes its body as bytes, not JSON,
	// takes: leafen_call sends them from body_base64 with the headers named
	// (native_operation_byte_inputs.go).
	Bytes *nativeOperationByteInput `json:"bytes,omitempty"`
	// Tools are Leafen's tools from the connection's list: those of the
	// group named, those words find, those of one operation's group its name
	// names, or the one named, with its input (native_operation_group_answers.go).
	Tools []leafenMCPFoundTool `json:"tools,omitempty"`
	// Usage is how to find and run operations: over MCP it is the tool's
	// description, which a client reads once, and no answer repeats it
	// (2026-11-16: it was 1.2 KB of every operation's description, three
	// quarters of the median); over REST, which has no tool description, the
	// answers that aren't one operation's description carry it.
	Usage string `json:"usage,omitempty"`
	// NextCursor and Next go on through what was found
	// (mcp_tool_operations_window.go).
	NextCursor string `json:"next_cursor,omitempty"`
	Next       string `json:"next,omitempty"`
}

type leafenMCPCallInput struct {
	Method     string `json:"method,omitempty" jsonschema:"The operation's HTTP method."`
	Path       string `json:"path,omitempty" jsonschema:"The operation's path below /api/leafen/v1 with its values filled in, such as /pages/abc123."`
	Query      string `json:"query,omitempty" jsonschema:"A query string, without the leading ?."`
	Body       any    `json:"body,omitempty" jsonschema:"The JSON request body, as the operation's contract records describe it."`
	BodyBase64 string `json:"body_base64,omitempty" jsonschema:"Instead of body, for an operation whose input is bytes (leafen_operations describes it with bytes): those bytes in standard base64."`
	// Headers are what such an operation names beside its bytes, and no others.
	Headers      map[string]string `json:"headers,omitempty" jsonschema:"For an operation whose input is bytes: the headers its bytes description names, such as X-Leafen-Part-SHA256; no others are sent."`
	Confirmation string            `json:"confirmation,omitempty" jsonschema:"For an operation marked confirm: the confirmation the first call returned, or the approvalId of the request you asked your person about in the conversation, once they said yes (with :hour after it where they said yes for an hour)."`
	// Tool and Arguments run one of Leafen's tools by name instead
	// (mcp_named_tools.go answers them before this tool's handler).
	Tool      string `json:"tool,omitempty" jsonschema:"Instead of an operation: a tool's name from leafen_operations."`
	Arguments any    `json:"arguments,omitempty" jsonschema:"The tool's input, as its input_schema says."`
	// WaitSeconds waits on a long job's state (mcp_call_wait.go).
	WaitSeconds int `json:"wait_seconds,omitzero" jsonschema:"For a GET of a long job's state — a scheduled backup or restore (/admin/backups/schedule), a backup file, an export, an import, a job read by its id: wait up to this many seconds (at most 45) for it to end, answering as soon as it has; remote access (/admin/tunnel): for its next stage; any other call answers at once."`
}

// leafenMCPOperationsUsage is how to run what leafen_operations finds, at
// the end of its description: what a call marked confirm answers comes
// first, as an agent can't carry one out without it, and the description
// stays within what Claude Code keeps (mcp_tool_description_limit.go;
// 2026-11-26, the gate's lap 13: it had come to 2,646 and the confirm part,
// its last, never reached an agent). Waiting on a long job is leafen_call's
// own (its description and wait_seconds).
const leafenMCPOperationsUsage = "Run one with leafen_call: the method, the path with its values filled in and the JSON body. " +
	"An operation marked confirm (deleting for good, sharing, permissions, members, administration) first answers 428: with a confirmation, " +
	"call again with the same request and that confirmation within five minutes; with an approval, ask your person in the conversation in the words it gives, " +
	"and once they say yes call again with the same request and confirmation set to its approvalId. " +
	"operation describes one: its body's and query's fields, those it needs marked required, and what it answers with (answer); detail: full adds the rest of its contract. " +
	"all: true adds what this connection can't run, each with what it lacks. " +
	"tools are Leafen's tools: give a tool's name as operation for its input, and run it with leafen_call's tool and arguments. " +
	"An operation whose input is bytes says so (bytes): send them as body_base64 with the headers it names; " +
	"an answer of bytes comes as body_base64 up to 200 KiB, a larger one as its size and a download_url."

// nativeOperationEnumValues are what a field of a named kind takes.
func nativeOperationEnumValues(enums map[string][]string, fieldType string) []string {
	for _, word := range strings.FieldsFunc(fieldType, func(r rune) bool { return r == '|' || r == ' ' || r == '[' || r == ']' || r == '<' || r == '>' }) {
		if values, named := enums[word]; named {
			return values
		}
	}
	return nil
}

// graphBlockContentShapes say what each kind of block keeps in its content, as
// the screen writes it, for a content graph change that makes one.
var graphBlockContentShapes = map[string]string{
	graphBlockTypeParagraph:       `{} — its text comes in textUpdates, as for every text block`,
	graphBlockTypeToggle:          `{"collapsed": true when closed}`,
	graphBlockTypeToDo:            `{"checked": boolean, "assignees": the user IDs it is assigned to, "due": {"date": "YYYY-MM-DD" or with a time, "dateEnd", "dateTimeZone", "dateReminder": minutes before}}`,
	graphBlockTypeCode:            `{"language": a language name}`,
	graphBlockTypeCallout:         `{"icon": an emoji or image address}`,
	graphBlockTypeEquation:        `{"expression": the expression}`,
	graphBlockTypeBookmark:        `{"url": the address, "caption": text spans}`,
	graphBlockTypeEmbed:           `{"url": the link, which the screen draws in its service's frame, "service": a service as the "/" menu names it, such as figma, or html for an HTML file, "caption": text spans}`,
	graphBlockTypeImage:           `{"url": the address, or "fileId": an uploaded file, "caption": text spans, "imageLink": where it opens}`,
	graphBlockTypeBreadcrumb:      `{}`,
	graphBlockTypeTableOfContents: `{}`,
	graphBlockTypeLinkToPage:      `{"childPageId": the page, or "databaseContainerId": a database}`,
	graphBlockTypeSyncedBlock:     `{} for an original, holding its blocks; {"syncedBlockId": the original's block ID} for a copy, which holds none: it shows the original's, and blocks go under the original; {"syncedBlockId": a page's ID} shows that page's whole body, edited on the page`,
	graphBlockTypeButton:          `{"label": its name, "buttonActionDocument": {"version": 1, "label": its name, "actions": its steps, as POST /pages/{pageId}/databases/{databaseId}/automations' actionDocument sets them out, with a button's own kinds}}; pressed with POST /pages/{pageId}/buttons/{blockId}/execute`,
	graphBlockTypeInlineDatabase:  `{"dataSourceId": its source, "viewId": its view; "linkedDatabaseSource": true and "sourceContainerId": the database it shows for a linked one; "locked": true locks its views and properties for everyone, as the app's "데이터베이스 잠금" does (left out, unlocked; a full-page database locks with its page, PATCH /pages/{id}/metadata isLocked)}`,
	graphBlockTypeChildPage:       `{"childPageId": the page, whose ID is the block's}`,
}

func registerMCPOperationTools(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_operations", Title: "Find what Leafen can do",
		Description: "Find what this connection can run with leafen_call beyond your list's tools: every native API operation — everything a person saves on screen — " +
			"and Leafen's tools your list leaves out. Common work goes with your list's tools, Notion's among them: pages and rows, a database's columns, moves, " +
			"comments, search and the trash. For anything else, call this without arguments for the groups of what Leafen does, a line and examples each; " +
			"give the task's one as group for its operations and tools, those this connection can run and those only its person does, then one as operation " +
			"for what a call sends it. query takes English words as a shortcut over every group. " +
			"Look here before you say Leafen can't do something or hand it to your person, and do what this connection can in the task. " +
			leafenMCPOperationsUsage,
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPOperationsInput) (*mcp.CallToolResult, leafenMCPOperationsOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, leafenMCPOperationsOutput{}, err
		}
		grant, err := nativeOperationGrantOf(app, mcpNativeKey(authority))
		if err != nil {
			return nil, leafenMCPOperationsOutput{}, err
		}
		output, err := describeNativeOperations(input, grant, mcpOperationsToolsOf(ctx))
		if err == nil {
			output, err = mcpOperationsWindowed(output, input.StartCursor, strings.TrimSpace(input.Query) != "")
		}
		if err != nil {
			return nil, leafenMCPOperationsOutput{}, err
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_call", Title: "Run a Leafen operation or tool",
		Description: "Run one native API operation from leafen_operations as this connection's person, in its workspaces: " +
			"the same checks, saving and history as the screen. Or run one of Leafen's tools it found, or one an answer names that your list leaves out: " +
			"give tool and arguments instead of method and path. A secret it takes or answers (a key, a password, a code): if you can run commands, " +
			"put it in or keep it by `leafen mcp secret` (a file, an environment variable or the keychain), out of this conversation; else this call " +
			"takes and answers it as usual. Following a long job (a backup, a restore, an export, an import), give wait_seconds (at most 45) " +
			"with its GET: the call answers as soon as it ends, instead of asking again and again; remote access's GET /admin/tunnel the same, at its next stage. This connection's own switches: " + mcpConnectionSwitchesUsage,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPCallInput) (*mcp.CallToolResult, nativeOperationResult, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, nativeOperationResult{}, err
		}
		path := strings.TrimSpace(input.Path)
		if !strings.HasPrefix(path, "/") || strings.Contains(path, "?") || strings.Contains(path, "..") {
			return nil, nativeOperationResult{}, errors.New("give the path below /api/leafen/v1, starting with /, without a query")
		}
		// What no call runs is said, with the way that does it (native_operation_not_calls.go).
		if why, notCall := nativeOperationNotCallFor(input.Method, path); notCall {
			return nil, nativeOperationResult{}, mcpInvalid("%s %s isn't a call: it is %s", strings.ToUpper(strings.TrimSpace(input.Method)), path, why)
		}
		var body []byte
		switch {
		case input.Body != nil && input.BodyBase64 != "":
			return nil, nativeOperationResult{}, mcpInvalid("give body or body_base64, not both")
		case input.BodyBase64 != "":
			if body, err = base64.StdEncoding.DecodeString(strings.TrimSpace(input.BodyBase64)); err != nil {
				return nil, nativeOperationResult{}, mcpInvalid("body_base64 is the bytes in standard base64 (A–Z, a–z, 0–9, + and /, padded with =)")
			}
		case input.Body != nil:
			if body, err = json.Marshal(input.Body); err != nil {
				return nil, nativeOperationResult{}, err
			}
		}
		body = mcpBringOperationUploads(app, authority, strings.ToUpper(strings.TrimSpace(input.Method)), path, body) // mcp_file_attach.go
		header, err := leafenCallRequestHeaders(input.Method, path, input.BodyBase64 != "", input.Headers)
		if err != nil {
			return nil, nativeOperationResult{}, err
		}
		if len(body) == 0 && input.BodyBase64 == "" {
			header.Del("Content-Type")
		}
		// An action the key rules confirm may wait for the person, whom the
		// agent asks in the conversation (mcp_action_approvals.go).
		result, err := callNativeOperationWaiting(ctx, input.WaitSeconds, func() (nativeOperationResult, error) {
			return callNativeOperationWith(withOperationAnswerDownloads(ctx, app), mcpNativeKey(authority),
				input.Method, path, input.Query, body, header, input.Confirmation)
		}, input.Method, path)
		// A large answer's download address is under the server the agent
		// connected to.
		if err == nil && result.DownloadURL != "" {
			if result.DownloadURL, err = mcpServerURL(authority.Resource, result.DownloadURL); err != nil {
				return nil, nativeOperationResult{}, failedBecause(errMCPToolFailed, err)
			}
		}
		if err == nil && result.Note == "" {
			result.Note = agentSecretAnswerNote(input.Method, path, result.Status)
		}
		// A backup place's failure says its common cause in words beside its key.
		if cause := backupFailureCauseNote(result.Body); err == nil && cause != "" {
			result.Note = strings.TrimSpace(result.Note + " " + cause)
		}
		return nil, result, err
	})
}
