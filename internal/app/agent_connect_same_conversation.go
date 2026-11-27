package app

import (
	"cmp"
	"os"
	"strings"
)

// Once Leafen is in Claude Code's settings, the conversation that connected
// it goes on with Leafen's tools, so its person never opens a new one between
// connecting and their first request, such as moving their Notion (user
// 2026-11-19: "the first ten minutes must be very smooth"). Measured with
// Claude Code 2.1.280 and 2.1.325 on 2026-11-19 (a throwaway settings
// folder, with the model's answers scripted on this computer): a server
// added during a conversation isn't in it, not on the next message nor
// through /mcp; in a terminal, the person's /reload-plugins brings it into
// the same conversation at once, tools and Leafen's instructions, and an
// entry put in place of an earlier one with its new key; the agent can't run
// /reload-plugins itself (its Skill tool says to ask the person). Without a
// terminal — the Claude app, an SDK host — /reload-plugins brings no server,
// so there the agent goes on through `leafen mcp call` (mcp_call_command.go)
// beside Leafen's program, or calls Leafen's MCP over HTTP itself where the
// program isn't (Docker, another computer: agent_connect_by_code.go), and
// Leafen's tools show from the next conversation; it never sends its person
// to one (user 2026-11-19). /reload-plugins itself gives the agent no turn
// (2026-11-21, the real Claude Code 2.1.330 on the Windows test machine: the
// tools came in, and nothing went on for 25 seconds in three conversations,
// while the agent had said it would go on once they typed it): so the person
// is asked to type it and then send a word, ‘continue’, and the agent says so
// and waits for that word. The web's connection text asks in the same words
// (agentPrompt sameConversation).

// claudeCodeHost is where the Claude Code that runs a command is: in a
// terminal, without one, or not known (a person ran it, or it runs where
// Claude Code isn't, as in Docker).
type claudeCodeHost int

const (
	claudeCodeHostUnknown claudeCodeHost = iota
	claudeCodeHostTerminal
	claudeCodeHostNoTerminal
)

// claudeCodeHostHere reads it from what Claude Code sets in the commands it
// runs: CLAUDE_CODE_ENTRYPOINT is cli in a terminal, and names its host
// otherwise (claude-desktop, sdk-cli, …).
func claudeCodeHostHere() (claudeCodeHost, string) {
	entry := strings.TrimSpace(os.Getenv("CLAUDE_CODE_ENTRYPOINT"))
	switch {
	case !runByClaudeCode() || entry == "":
		return claudeCodeHostUnknown, ""
	case entry == "cli":
		return claudeCodeHostTerminal, entry
	}
	return claudeCodeHostNoTerminal, entry
}

// leafenTypedProgram is this program as a command an agent types to run it
// again: the desktop app's own, else leafen.
func leafenTypedProgram() string {
	if setupApp != nil {
		return cmp.Or(setupApp.Program, "leafen-desktop")
	}
	return "leafen"
}

var claudeCodeReloadTell = mcpWords{"이 대화에서 바로 이어 가려면 입력창에 /reload-plugins를 입력한 뒤 ‘계속’이라고 보내 주세요.",
	"To go on in this conversation, type /reload-plugins in the message box, then send ‘continue’."}

const claudeCodeReloadWhy = "Claude Code loads an MCP server added during a conversation only when its person types /reload-plugins, " +
	"which you can't run yourself: Leafen's tools (leafen_…) then show in this same conversation. /reload-plugins doesn't give you a " +
	"turn, so you go on only when they send the word after it: the line asks for both, and it isn't said that you go on as soon " +
	"as they type /reload-plugins."

// claudeCodeSameConversation is what the agent tells its person, with tell
// first, and how it goes on in this conversation, by where Claude Code runs;
// local says Leafen's program runs beside it, for `leafen mcp call`, and
// address is Leafen's MCP, which it calls over HTTP otherwise, as the entry
// Claude Code keeps under name does ("" when the shell chose it: first, the
// server's own name for it, or the name it took beside an earlier one).
func claudeCodeSameConversation(language, tell, address, name, first string, local bool) []string {
	reload := tell + " " + claudeCodeReloadTell.in(language)
	host, entry := claudeCodeHostHere()
	httpCall := claudeCodeHTTPCallHow(cmp.Or(name, first, claudeCodeServerName), address)
	if name == "" {
		httpCall += " (If Claude Code added Leafen beside an earlier entry, or kept one, the name it said takes " +
			cmp.Or(first, claudeCodeServerName) + "'s place in that line.)"
	}
	call := leafenTypedProgram() + " mcp call"
	if name != "" {
		call += " --entry " + name
	}
	switch {
	case host == claudeCodeHostTerminal:
		return []string{"Left to do: a line to your person, in their language, carrying all that this says (/reload-plugins as written): “" + reload + "”",
			claudeCodeReloadWhy + " Once they send it, go on here: don't send them to a new conversation."}
	case host == claudeCodeHostNoTerminal && local:
		return []string{"Left to do: a line to your person, in their language, carrying all that this says (/reload-plugins as written): “" + tell + "”",
			"This Claude Code runs without a terminal (CLAUDE_CODE_ENTRYPOINT=" + entry + ", as in the Claude app): it shows an MCP " +
				"server added during a conversation from the next one on, and /reload-plugins doesn't bring it into this one. Don't " +
				"send your person to a new conversation for it: go on in this one through Leafen's command line — `" + call +
				"` lists Leafen's tools with its instructions, `" + call + " <tool> --schema` says what one takes, and " +
				jsonCommandHere(call+" <tool>", "<arguments as JSON>", "") +
				" calls it as you, with the key Leafen gave Claude Code, which never shows."}
	case host == claudeCodeHostNoTerminal:
		return []string{"Left to do: a line to your person, in their language, carrying all that this says (/reload-plugins as written): “" + tell + "”",
			"This Claude Code runs without a terminal (CLAUDE_CODE_ENTRYPOINT=" + entry + ", as in the Claude app): it shows an MCP " +
				"server added during a conversation from the next one on, and /reload-plugins doesn't bring it into this one. Don't " +
				"send your person to a new conversation for it: " + httpCall}
	}
	return []string{"Left to do: a line to your person, in their language, carrying all that this says (/reload-plugins as written): “" + reload + "” — in Claude Code without a terminal (CLAUDE_CODE_ENTRYPOINT " +
		"isn't cli, as in the Claude app) “" + tell + "” alone.",
		claudeCodeReloadWhy + " Once they send it, go on here: don't send them to a new conversation. Without a terminal, " +
			"/reload-plugins doesn't bring it, so there " + httpCall}
}
