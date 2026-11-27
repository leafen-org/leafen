package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// `leafen mcp call` calls one of Leafen's MCP tools from the command line as
// the agent Leafen added to Claude Code (2026-11-19: Claude Code without a
// terminal — the Claude app, an SDK host — loads an MCP server added during a
// conversation only in the next one, and its /reload-plugins doesn't bring
// it, so the person had to open a new conversation between connecting and
// "move my Notion"). It takes Leafen's address and key from Claude Code's own
// `claude mcp get <entry>`, where `leafen connect --add-to claude-code` put
// them — the one entry Claude Code holds at a Leafen, each named after its
// server (agent_entry_names.go), or the one --entry names —, so the key
// passes through no conversation and no file of Leafen's,
// and asks the server at that MCP address as Claude Code would. It opens no
// data folder. Leafen's tools then show as tools from the next conversation
// on; this is for the one where they don't yet.

const (
	mcpCallWait          = 5 * time.Minute
	mcpCallClaudeCodeGet = 30 * time.Second
)

// mcpCallEntry is Leafen's entry in Claude Code: its MCP address and the
// Authorization header Claude Code sends there.
type mcpCallEntry struct{ URL, Authorization string }

func (entry mcpCallEntry) key() string {
	return strings.TrimSpace(strings.TrimPrefix(entry.Authorization, "Bearer "))
}

func mcpCallCommand() *cobra.Command {
	var schema bool
	name := ""
	command := &cobra.Command{
		Use:   "call [tool] [arguments as JSON]",
		Short: "Call one of Leafen's MCP tools as the agent Leafen added to Claude Code",
		Long: "Calls a Leafen MCP tool from the command line, as the connection `leafen connect --add-to claude-code` gave Claude Code, " +
			"for a conversation that can't load Leafen's tools yet (Claude Code without a terminal, as the Claude app, shows them from " +
			"the next conversation). Without a tool it lists Leafen's tools with its instructions; with --schema, what the tool takes. " +
			"The arguments are a JSON object, or - to read it from standard input: in Windows PowerShell, which hands a program its " +
			"argument without the JSON's double quotes, give it there, as UTF-8 (" + jsonCommandPowerShell("leafen mcp call <tool>",
			"<arguments as JSON>", "") + "). A JSON object alone with a method and a path is " +
			"leafen_call's: it runs that operation. It takes Leafen's address and key from Claude Code " +
			"(its one Leafen entry, or the entry --entry names where it holds several), so run it where Claude Code runs; the key never shows.",
		Example: "  leafen mcp call\n" +
			"  leafen mcp call --schema leafen_call\n" +
			"  leafen mcp call leafen_call '{\"method\": \"GET\", \"path\": \"/bootstrap\"}'\n" +
			"  leafen mcp call '{\"method\": \"GET\", \"path\": \"/bootstrap\"}'    # the same, leafen_call\n" +
			"  leafen mcp call --entry leafen-home-kxb leafen_search '{\"query\": \"plan\"}'\n" +
			"  " + jsonCommandPowerShell("leafen mcp call leafen_call", `{"method": "GET", "path": "/bootstrap"}`, "") + "    # in Windows PowerShell",
		Args:         cobra.MaximumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// What is called is read first: a call missing its tool says so without Claude Code.
			tool, arguments := "", ""
			if len(args) > 0 && !schema {
				var err error
				if tool, arguments, err = mcpCallToolAndArguments(args, cmd.InOrStdin()); err != nil {
					return err
				}
			}
			entry, err := claudeCodeLeafenEntry(cmd.Context(), name)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			switch {
			case len(args) == 0:
				return writeMCPCallTools(cmd.Context(), out, entry)
			case schema:
				return writeMCPCallSchema(cmd.Context(), out, entry, mcpCallToolName(args[0]))
			}
			return runMCPCall(cmd.Context(), out, entry, mcpCallToolName(tool), arguments)
		},
	}
	command.Flags().BoolVar(&schema, "schema", false, "say what the tool does and the arguments it takes, without calling it")
	command.Flags().StringVar(&name, "entry", "", agentEntryFlagUsage)
	return command
}

// mcpCallToolAndArguments reads the tool and its arguments, - taking them
// from standard input. A JSON object where the tool goes (2026-11-22: a
// polling script called `leafen mcp call '{"method":"GET","path":"/admin/
// backups/schedule"}'` and Leafen answered only `unknown tool "{…}"`) is
// leafen_call's when it alone has a method and a path, its only meaning;
// any other says the tool's name is missing, with the call's form.
func mcpCallToolAndArguments(args []string, stdin io.Reader) (string, string, error) {
	read := func() (string, error) {
		text, err := io.ReadAll(io.LimitReader(stdin, maxMCPMessageBytes+1))
		return string(text), err
	}
	tool, arguments := strings.TrimSpace(args[0]), "{}"
	if len(args) == 2 {
		arguments = args[1]
	}
	var err error
	switch {
	case tool == "-" && len(args) == 1:
		if tool, err = read(); err != nil {
			return "", "", err
		}
		tool = strings.TrimSpace(tool)
	case arguments == "-":
		if arguments, err = read(); err != nil {
			return "", "", err
		}
	}
	if !strings.HasPrefix(tool, "{") {
		return tool, arguments, nil
	}
	call := struct{ Method, Path string }{}
	if len(args) == 1 && json.Unmarshal([]byte(tool), &call) == nil && strings.TrimSpace(call.Method) != "" && strings.TrimSpace(call.Path) != "" {
		return "leafen_call", tool, nil
	}
	program := leafenTypedProgram()
	return "", "", fmt.Errorf("the tool's name is missing before its arguments: %s, such as %s; `%s mcp call` lists the tools",
		jsonCommandHere(program+" mcp call [--entry <name>] <tool>", "<arguments as JSON>", ""),
		jsonCommandHere(program+" mcp call leafen_call", `{"method": "GET", "path": "/bootstrap"}`, ""), program)
}

// mcpCallToolName takes a tool as Leafen names it, or as Claude Code shows
// it (mcp__leafen__…).
func mcpCallToolName(name string) string {
	name = strings.TrimSpace(name)
	if rest, found := strings.CutPrefix(name, "mcp__"); found {
		if _, tool, found := strings.Cut(rest, "__"); found {
			return tool
		}
	}
	return name
}

// agentEntryFlagUsage says what --entry names.
const agentEntryFlagUsage = "the name of Leafen's entry in Claude Code, where it holds several (one for each Leafen, or person); " +
	"without it, the one it holds"

// claudeCodeLeafenEntry reads Leafen's entry under name ("" for the one
// Claude Code holds at a Leafen) from Claude Code, in the folder Claude Code
// counts as this one, so one kept to this folder is found first.
func claudeCodeLeafenEntry(ctx context.Context, name string) (mcpCallEntry, error) {
	program, found := claudeCodeProgram()
	if !found {
		return mcpCallEntry{}, errors.New("Claude Code's own program isn't where this runs: run it where Claude Code runs, once " +
			"`leafen connect --approved --add-to claude-code` added Leafen to it")
	}
	ctx, cancel := context.WithTimeout(ctx, mcpCallClaudeCodeGet)
	defer cancel()
	if name == "" {
		var err error
		if name, err = claudeCodeLeafenEntryName(ctx, program); err != nil {
			return mcpCallEntry{}, err
		}
	}
	get := exec.CommandContext(ctx, program, "mcp", "get", name)
	get.Dir = claudeCodeFolder()
	output, err := get.CombinedOutput()
	entry := parseClaudeCodeMCPGet(string(output))
	if err != nil || entry.URL == "" || entry.key() == "" {
		said := appOneLine(string(output), 300)
		if entry.key() != "" {
			said = strings.ReplaceAll(said, entry.key(), "<key>")
		}
		return mcpCallEntry{}, fmt.Errorf("Claude Code has no Leafen entry with a key here (claude mcp get %s: %s): connect with "+
			"`leafen connect --name \"Claude Code\"` first", name, firstNonempty(said, "it said nothing"))
	}
	return entry, nil
}

// parseClaudeCodeMCPGet reads the address and the Authorization header from
// what `claude mcp get` prints: "URL: …" and, under "Headers:",
// "Authorization: …".
func parseClaudeCodeMCPGet(said string) mcpCallEntry {
	entry := mcpCallEntry{}
	for _, line := range strings.Split(said, "\n") {
		line = strings.TrimSpace(line)
		if value, found := strings.CutPrefix(line, "URL: "); found && entry.URL == "" {
			entry.URL = strings.TrimSpace(value)
		}
		if value, found := strings.CutPrefix(line, "Authorization: "); found && entry.Authorization == "" {
			entry.Authorization = strings.TrimSpace(value)
		}
	}
	return entry
}

// mcpCallAsk sends one JSON-RPC request to Leafen's MCP address, which
// answers each on its own (mcp_transport.go: stateless, as JSON), and gives
// its result. What Leafen says back never carries the key.
func mcpCallAsk(ctx context.Context, entry mcpCallEntry, method string, params any) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, mcpCallWait)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, entry.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", entry.Authorization)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := newMCPStdioHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("Leafen at %s didn't answer: %s", entry.URL, strings.ReplaceAll(err.Error(), entry.key(), "<key>"))
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(response.Body, maxMCPMessageBytes+1))
	if err != nil || len(answer) > maxMCPMessageBytes {
		return nil, fmt.Errorf("Leafen's answer couldn't be read")
	}
	switch {
	case response.StatusCode == http.StatusUnauthorized:
		return nil, errors.New("Leafen refused Claude Code's key (disconnected, or the server moved): connect again with " +
			"`leafen connect --name \"Claude Code\"`, which adds Leafen beside this entry and says the name it took — call " +
			"with --entry and that name then; this entry stays, never remove it yourself")
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, fmt.Errorf("Leafen answered %d: %s", response.StatusCode, appOneLine(string(answer), 300))
	}
	// An answer sent as events carries the message in its data line.
	if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		for _, line := range bytes.Split(answer, []byte("\n")) {
			if message, found := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:")); found {
				answer = message
			}
		}
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(answer, &reply); err != nil {
		return nil, fmt.Errorf("Leafen's answer wasn't MCP: %s", appOneLine(string(answer), 300))
	}
	if reply.Error != nil {
		return nil, errors.New(reply.Error.Message)
	}
	return reply.Result, nil
}

type mcpCallTool struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func mcpCallTools(ctx context.Context, entry mcpCallEntry) ([]mcpCallTool, error) {
	result, err := mcpCallAsk(ctx, entry, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	listed := struct {
		Tools []mcpCallTool `json:"tools"`
	}{}
	if err := json.Unmarshal(result, &listed); err != nil {
		return nil, err
	}
	return listed.Tools, nil
}

// writeMCPCallTools says what Leafen tells an agent as a session starts, and
// its tools, one a line.
func writeMCPCallTools(ctx context.Context, out io.Writer, entry mcpCallEntry) error {
	result, err := mcpCallAsk(ctx, entry, "initialize", map[string]any{"protocolVersion": "2025-06-18",
		"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "leafen mcp call", "version": "1"}})
	if err != nil {
		return err
	}
	started := struct {
		Instructions string `json:"instructions"`
	}{}
	_ = json.Unmarshal(result, &started)
	tools, err := mcpCallTools(ctx, entry)
	if err != nil {
		return err
	}
	lines := []string{}
	if started.Instructions != "" {
		lines = append(lines, started.Instructions, "")
	}
	lines = append(lines, fmt.Sprintf("Leafen's tools (%d), each called with %s; --schema says what one takes:", len(tools),
		jsonCommandHere(leafenTypedProgram()+" mcp call <tool>", "<arguments as JSON>", "")))
	for _, tool := range tools {
		lines = append(lines, "- "+tool.Name+": "+firstNonempty(tool.Title, appOneLine(tool.Description, 120)))
	}
	_, err = fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}

func writeMCPCallSchema(ctx context.Context, out io.Writer, entry mcpCallEntry, name string) error {
	tools, err := mcpCallTools(ctx, entry)
	if err != nil {
		return err
	}
	for _, tool := range tools {
		if tool.Name == name {
			schema := bytes.Buffer{}
			if json.Indent(&schema, tool.InputSchema, "", "  ") != nil {
				schema.Reset()
				schema.Write(tool.InputSchema)
			}
			_, err := fmt.Fprintf(out, "%s: %s\n%s\nArguments (JSON Schema):\n%s\n", tool.Name, tool.Title, tool.Description, schema.String())
			return err
		}
	}
	return fmt.Errorf("Leafen has no tool %q: `%s mcp call` lists them", name, leafenTypedProgram())
}

// runMCPCall calls the tool with the arguments and prints what it answers;
// an answer the tool marks as an error ends the command with it.
func runMCPCall(ctx context.Context, out io.Writer, entry mcpCallEntry, name, arguments string) error {
	var input map[string]any
	if err := json.Unmarshal([]byte(arguments), &input); err != nil || input == nil {
		return fmt.Errorf("the arguments are one JSON object ({} for none), such as %s",
			jsonCommandHere(leafenTypedProgram()+" mcp call "+name, `{"workspace_id": "…"}`, ""))
	}
	result, err := mcpCallAsk(ctx, entry, "tools/call", map[string]any{"name": name, "arguments": input})
	if err != nil {
		return err
	}
	answer := struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}{}
	if err := json.Unmarshal(result, &answer); err != nil {
		return err
	}
	texts := []string{}
	for _, part := range answer.Content {
		if part.Type == "text" {
			texts = append(texts, part.Text)
		}
	}
	if len(texts) == 0 && len(answer.Structured) > 0 {
		texts = append(texts, string(answer.Structured))
	}
	if answer.IsError {
		return errors.New(firstNonempty(strings.Join(texts, "\n"), "the tool answered with an error"))
	}
	_, err = fmt.Fprintln(out, strings.Join(texts, "\n"))
	return err
}
