package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Before a connect command takes a key, it looks at the connections the
// agent holds to Leafen already — mechanically, in the command, not left to
// the agent (user 2026-11-20: a pasted text added leafen-maple-2 beside a
// leafen-maple that worked, to the same server: "shouldn't it check first
// whether it is connected already?"). Each entry the agent holds at an
// address of this Leafen, whatever its name (one made before the server
// was renamed too), is checked with its own key: GET /mcp/connections/current with the
// reach the new key would have — all its person's workspaces, as the person
// connecting now (checkOwnMCPConnection) — which never shows the key. Another
// person's entry in the same settings (2026-11-27: one computer account, two
// people) answers 403 and is never kept: Leafen goes in beside it, and the
// agent works as its own person. One that answers 204 — and, in Claude Code, is
// in the settings the agent adds to (every folder, or one folder) — is
// kept: nothing is traded or made, and a one-time code stays unused. Only
// when none works does Leafen go in beside them (agent_connect_alongside.go),
// and the command names those whose key Leafen refuses (401) for the agent
// to ask its person once whether to remove. The web's text does the same in
// its commands (agentConnectCommands in web/src/lib/agentConnection.ts).

const agentEntryCheckWait = 10 * time.Second

// agentEntryReach is what an entry must reach to be kept: an MCP address of
// this Leafen, and the check's query (all its person's workspaces, as the
// person connecting).
type agentEntryReach struct {
	Addresses []string
	Query     string
}

// agentEntryReachFor is the reach of an agent connection for the person
// ownerID at the addresses (each an MCP address; "" left out): all their
// workspaces (connection_person_rights.go), which an MCP token kept to one
// doesn't pass for, as them — another person's key doesn't.
func agentEntryReachFor(addresses []string, ownerID string) agentEntryReach {
	values := url.Values{}
	values.Set("all", "1")
	if ownerID != "" {
		values.Set("person", ownerID)
	}
	reach := agentEntryReach{Query: values.Encode()}
	for _, address := range addresses {
		if address = strings.TrimRight(address, "/"); address != "" && !slices.Contains(reach.Addresses, address) {
			reach.Addresses = append(reach.Addresses, address)
		}
	}
	return reach
}

// agentEntryReachHere is that reach at the addresses this Leafen is reached
// at: on this computer (as the server last said it), and from anywhere.
func agentEntryReachHere(app core.App, ownerID string, also ...string) agentEntryReach {
	facts := readSetupState(app.DataDir()).Facts
	addresses := []string{}
	for _, origin := range append([]string{facts.Here, facts.Outside, configuredPublicOrigin()}, also...) {
		if origin = strings.TrimRight(origin, "/"); origin != "" {
			addresses = append(addresses, origin+nativeMCPPath)
		}
	}
	return agentEntryReachFor(addresses, ownerID)
}

// agentEntryCheckLine is the shell of the check, for one entry at "$A" named
// "$M" whose header header prints: kept in W when Leafen takes its key for
// the reach and kept holds, its name added to R when Leafen refuses it.
func agentEntryCheckLine(reach agentEntryReach, header, kept string) string {
	patterns := []string{}
	for _, address := range reach.Addresses {
		patterns = append(patterns, shellQuoted(address))
	}
	check := `"${A%` + nativeMCPPath + `}` + apiRoot + mcpOwnConnectionPath
	if reach.Query != "" {
		check += "?" + reach.Query
	}
	condition := `[ "$H" = 204 ]`
	if kept != "" {
		condition += " && " + kept
	}
	return `case "$A" in ` + strings.Join(patterns, "|") + `) K="$(` + header + `)"; if [ -n "$K" ]; then ` +
		`H="$(printf '%s\n' "$K" | curl -s -o /dev/null -w '%{http_code}' ` + check + `" -H @-)"; if ` + condition +
		`; then W="$M"; break; fi; [ "$H" != 401 ] || R="$R $M"; fi;; esac; `
}

// claudeCodeHeldLine is the shell that checks each entry Claude Code lists
// at an address of the reach (`claude mcp list`: "<name>: <url> (HTTP) - …"),
// reading its URL and header from `claude mcp get` and keeping one in the
// settings S names (its Scope line: "User config" for user, "Local config"
// for local).
func claudeCodeHeldLine(reach agentEntryReach) string {
	if len(reach.Addresses) == 0 {
		return ""
	}
	listed := []string{}
	for _, address := range reach.Addresses {
		listed = append(listed, "-e "+shellQuoted(": "+address+" (HTTP)"))
	}
	return `for M in $("$C" mcp list 2>/dev/null | grep -F ` + strings.Join(listed, " ") + ` | sed -n -E 's/^ *(.*): [^ ]+ \(HTTP\).*$/\1/p'); ` +
		`do G="$("$C" mcp get "$M" 2>/dev/null)" || continue; A="$(printf '%s\n' "$G" | sed -n 's/^ *URL: *//p')"; ` +
		agentEntryCheckLine(reach, `printf '%s\n' "$G" | sed -n 's/^ *\(Authorization: .*\)$/\1/p'`,
			`printf '%s\n' "$G" | grep -qi "^ *Scope: $S "`) + `done; `
}

// claudeCodeEntriesHere are the entries Claude Code holds in folder at an
// address of the reach, as the shell line reads them: one whose key Leafen
// still takes for it in scope (kept, the rest unread), and those whose key
// Leafen refuses. A list Claude Code doesn't give leaves none.
func claudeCodeEntriesHere(program, folder, scope string, reach agentEntryReach) (string, []string) {
	refused := []string{}
	listed, err := claudeCodeSays(program, folder, "mcp", "list")
	if err != nil {
		return "", refused
	}
	for _, name := range claudeCodeListedAt(listed, reach.Addresses) {
		said, err := claudeCodeSays(program, folder, "mcp", "get", name)
		if err != nil {
			continue
		}
		switch status, inScope := checkClaudeCodeEntry(said, reach, scope); {
		case status == http.StatusNoContent && inScope:
			return name, refused
		case status == http.StatusUnauthorized:
			refused = append(refused, name)
		}
	}
	return "", refused
}

// claudeCodeSays runs Claude Code's program in folder and gives what it said.
func claudeCodeSays(program, folder string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), claudeCodeCommandWait)
	defer cancel()
	command := exec.CommandContext(ctx, program, args...)
	command.Dir = folder
	output, err := command.Output()
	return string(output), err
}

// claudeCodeListedAt are the names `claude mcp list` lists at one of the
// addresses.
func claudeCodeListedAt(listed string, addresses []string) []string {
	names := []string{}
	for _, line := range strings.Split(listed, "\n") {
		for _, address := range addresses {
			if at := strings.Index(line, ": "+address+" (HTTP)"); at > 0 {
				names = append(names, strings.TrimSpace(line[:at]))
				break
			}
		}
	}
	return names
}

// checkClaudeCodeEntry checks the entry `claude mcp get` said, by its URL
// and header lines, and reads whether it is in scope by its Scope line
// ("Scope: User config …" for user, "Local config" for local): the check's
// status, 0 for an entry at no address of this Leafen or without a header.
func checkClaudeCodeEntry(said string, reach agentEntryReach, scope string) (int, bool) {
	address, header, inScope := "", "", false
	for _, line := range strings.Split(said, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "URL:"):
			address = strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(line, "URL:")), "/")
		case strings.HasPrefix(line, "Authorization:"):
			header = strings.TrimSpace(strings.TrimPrefix(line, "Authorization:"))
		case strings.HasPrefix(line, "Scope:"):
			inScope = strings.HasPrefix(strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Scope:"))), scope+" ")
		}
	}
	if header == "" || !slices.Contains(reach.Addresses, address) {
		return 0, false
	}
	return checkAgentEntry(address, header, reach.Query), inScope
}

// checkAgentEntry asks the Leafen at the MCP address whether the key in
// header still reaches what query names: its answer's status, 0 when none came.
func checkAgentEntry(address, header, query string) int {
	target := strings.TrimSuffix(address, nativeMCPPath) + apiRoot + mcpOwnConnectionPath
	if query != "" {
		target += "?" + query
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentEntryCheckWait)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0
	}
	request.Header.Set("Authorization", header)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0
	}
	response.Body.Close()
	return response.StatusCode
}

// agentEntryKeptLines say that Claude Code holds a connection to this Leafen
// that works already, so nothing was asked or made; the instruction file's
// lines as they read now, which came with that connection — an agent whose
// lines are from before Leafen's address changed, or before the lines kept
// their address up to date, gets them by connecting again
// (agent_facing_address.go) —; and what comes next: the start, where it is
// offered.
func agentEntryKeptLines(app core.App, plan agentConnectPlan, name string) []string {
	return append([]string{strings.TrimSpace(fmt.Sprintf("Claude Code is connected to this Leafen already as %s, and Leafen still "+
		"takes its key for what this connection would reach, as your person: nothing was asked or made. Left to do: a line to your "+
		"person that it was connected already, then Leafen's tools (%s is Leafen's entry in them).", name, name)),
		agentKeptInstructionLinesSay, agentInstructionLines(agentConnectFacingOrigin(app))},
		agentStartNext(app, plan.OwnerID, agentPlanWorkspaceIDs(plan))...)
}

// agentKeptInstructionLinesSay says what the lines are to an agent connected
// already: those it holds, as they read now.
const agentKeptInstructionLinesSay = "The <!-- leafen:begin --> … <!-- leafen:end --> lines that came with this connection, which hold " +
	"no key, read now as below; where your instruction file holds them as they read before, these go in their place:"

// agentPlanWorkspaceIDs are the workspaces a plan's connection reaches.
func agentPlanWorkspaceIDs(plan agentConnectPlan) []string {
	ids := []string{}
	for _, row := range plan.Workspaces {
		ids = append(ids, row.ID)
	}
	return ids
}

// agentEntryCheckWords say the same check to an agent Leafen runs no
// command for: an entry at this Leafen it holds already that works is kept.
func agentEntryCheckWords(reach agentEntryReach) string {
	if len(reach.Addresses) == 0 {
		return ""
	}
	check := apiRoot + mcpOwnConnectionPath
	if reach.Query != "" {
		check += "?" + reach.Query
	}
	return "First, if your settings hold a server at " + strings.Join(reach.Addresses, " or ") + " already, check it with its own " +
		"header, never printed: GET its origin + " + check + " answers 204 when Leafen still takes its key for this, as your " +
		"person, and then you keep it and take no other (403: it reaches less, or works as another person — leave it as it is " +
		"and go in beside it; 401: Leafen refuses its key; ask your person once whether to remove it)."
}

// keptClaudeCodeEntry is the entry Claude Code here holds to this Leafen
// that works already, for a plan connecting Claude Code here; "" with none.
func keptClaudeCodeEntry(plan agentConnectPlan, way agentConnectWay) string {
	if way.ClaudeCodeJSON || (way.AddTo != agentAddToClaudeCode && !runByClaudeCode()) {
		return ""
	}
	program, found := claudeCodeProgram()
	if !found {
		return ""
	}
	working, _ := claudeCodeEntriesHere(program, claudeCodeFolder(), way.claudeCodeScope(), plan.Reach)
	return working
}

// writeAgentEntryKept says that the connection Claude Code holds as name is
// kept, and nothing asked or made.
func writeAgentEntryKept(out io.Writer, app core.App, plan agentConnectPlan, name string, asJSON bool) error {
	lines := agentEntryKeptLines(app, plan, name)
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"status": "connected_already", "name": plan.Name, "entryName": name,
			"instructionLines": agentInstructionLines(agentConnectFacingOrigin(app)), "message": strings.Join(lines, "\n")})
	}
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}
