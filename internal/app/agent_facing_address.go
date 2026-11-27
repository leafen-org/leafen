package app

import (
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The address Leafen tells an agent and its person the server is at is one:
// in the links its answers give (mcpServerURL), in the first line of its
// instructions (mcpServerNameLine) and in the lines an agent keeps in its
// instruction file (agentInstructionLines, web/src/lib/agentConnection.ts) —
// the server's own address (the one written, else a running tunnel's:
// configuredPublicOrigin), else the one the agent reaches it at
// (2026-11-22: the server moved to its Tailscale address, and an agent whose
// instruction file still named the old one wrote every link back to it).
//
// The lines themselves say to keep their address up to date, so the person's
// own words, approved as they connected, carry the change: an agent rewrites
// them when Leafen says it is at another address now, asking no one. Leafen
// says only that it is, in its instructions' first line (agentAddressNow;
// 2026-11-23: an answer telling the agent to rewrite the person's file
// "without asking" read as an injection, and the careful agents asked their
// person all the same).

// agentFacingOrigin is that address, for an agent that reaches the server at
// reached.
func agentFacingOrigin(reached string) string {
	if public := configuredPublicOrigin(); public != "" {
		return public
	}
	return reached
}

// agentConnectFacingOrigin is that address for the lines `leafen connect`
// gives an agent connected already: the address it reaches the server at
// on this computer where the server has none of its own.
func agentConnectFacingOrigin(app core.App) string {
	noteCommandTunnelChoice(app)
	return agentFacingOrigin(agentConnectOrigin(app))
}

// agentReachedOrigin is the origin of the address an agent's request came
// to (its resource), or "".
func agentReachedOrigin(resource string) string {
	parsed, err := url.Parse(strings.TrimSpace(resource))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// agentInstructionLinesNote says where the lines go: in place of the ones
// the file holds already, so it keeps one block, at this address.
const agentInstructionLinesNote = "in place of any <!-- leafen:begin --> … <!-- leafen:end --> lines it holds already, so it keeps one block, at this address"

// agentAddressNow is what the instructions' first line (mcpServerNameLine)
// adds to the address Leafen is at, facing, for an agent that reaches it at
// another, reached — an earlier one still forwarding, or this computer's —
// as facts: that it is at facing now, and that the one the agent reaches
// works too; "" when they are one. Every agent reads that line as a session
// starts, and lines that say to keep their address up to date have it
// follow; lines from before they did come again with connecting again, as
// the instructions' last rule says for a server that moved. It rode on the
// inbox's answer, with both addresses and where the links and lines point,
// until 2026-11-24, when agents stopped reading the inbox as a conversation
// starts (mcp_inbox.go); in the first line it names the agent's own address
// by what it is, which leaves the other rules their room
// (checkMCPInstructionsRoom), and the links it gives name facing.
func agentAddressNow(facing, reached string) string {
	if reached == "" || facing == "" || strings.EqualFold(reached, facing) {
		return ""
	}
	return " now (the address you reach it at works too)"
}
