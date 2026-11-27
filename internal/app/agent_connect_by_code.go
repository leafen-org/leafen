package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// An agent that runs commands connects with the one-time code of Leafen's
// connection text in one command, which trades the code and puts what it
// trades for straight into the agent's own settings, so the key shows in no
// conversation and no output (user 2026-11-19: the code traded for a key was
// the intended road, ahead of a sign-in through the browser; the text had
// sent Claude Code to /mcp and a browser instead, and the Claude app's Code
// tab, which can't load a server added mid-conversation, then asked for a new
// session before the Notion move ever started). The exchange answers with
// the key as JSON, or, asked by "answer", with the entry alone as that
// program's settings take it:
//
//   - "claude-code": the JSON `claude mcp add-json` takes (claudeCodeEntry),
//     for `claude mcp add-json --scope user leafen-<server>-<code> "$(curl … exchange …)"`.
//   - "codex": the lines of a table in Codex's config.toml, the name of the
//     table left to the command that appends them, which names it
//     leafen-<server>-<code> or, beside one of that name there already,
//     leafen-<server>-<code>-<person> (agentEntryNames).
//
// A refused trade answers as before, with the code kept for another try.

const codeAnswerCodex = "codex"

// connectionCodeAnswerKnown says whether the exchange can answer so.
func connectionCodeAnswerKnown(answer string) bool {
	return answer == "" || answer == agentAddToClaudeCode || answer == codeAnswerCodex
}

// writeConnectionCodeAnswer answers a trade made with Leafen's entry for the
// program the agent named, for its command to hand straight to its settings.
func writeConnectionCodeAnswer(event *core.RequestEvent, answer, address, key string) error {
	if answer == codeAnswerCodex {
		return event.Blob(http.StatusCreated, "text/plain; charset=utf-8", []byte(codexEntryLines(address, key)))
	}
	entry, err := claudeCodeEntry(address, key)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPConnectionFailed, "Couldn't create the MCP connection.", err)
	}
	return event.Blob(http.StatusCreated, "application/json", []byte(entry+"\n"))
}

// codexEntryLines is Leafen's MCP as a table of Codex's config.toml takes it,
// without the table's head: its address and the header with its key.
func codexEntryLines(address, key string) string {
	return "url = " + tomlBasicString(address) + "\nhttp_headers = { \"Authorization\" = " + tomlBasicString("Bearer "+key) + " }\n"
}

// tomlBasicString writes value as a TOML basic string.
func tomlBasicString(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

// claudeCodeHTTPCall is the line that calls Leafen's MCP at address as the
// entry Claude Code keeps under name does, for a conversation that doesn't
// show Leafen's tools yet and has no Leafen program beside it (Leafen in
// Docker, on another computer): the header comes from Claude Code's own
// `claude mcp get` straight into curl's standard input, so the key is in no
// command, output or conversation. The same line is in the web's connection
// text (web/src/lib/agentConnection.ts).
func claudeCodeHTTPCall(name, address string) string {
	return `"${CLAUDE_CODE_EXECPATH:-claude}" mcp get ` + name + ` | sed -n 's/^ *\(Authorization: .*\)$/\1/p' | curl -sS ` +
		shellQuoted(address) + ` -H @- -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' ` +
		`-d '{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "<tool>", "arguments": {…}}}'`
}

// claudeCodeHTTPCallHow says how the agent goes on with that line: what
// Leafen tells it first, its tools, and each call, one request at a time.
func claudeCodeHTTPCallHow(name, address string) string {
	return "go on in this one by calling Leafen's MCP over HTTP yourself, one JSON-RPC request a call, with the key Claude Code " +
		"keeps, which never shows: " + claudeCodeHTTPCall(name, address) + " — in Windows PowerShell: " +
		claudeCodeHTTPCallPowerShell(name, address) + " — first \"method\": \"initialize\" (params " +
		`{"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "Claude Code", "version": "1"}}) for what ` +
		"Leafen tells you, then \"tools/list\" for its tools, then \"tools/call\" for each. Leafen's tools show as your own from " +
		"the next conversation on."
}
