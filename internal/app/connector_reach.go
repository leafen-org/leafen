package app

import (
	"net/url"
	"strings"
)

// Whether a Claude connector can connect to an address — the one rule every
// answer that hands an agent the server's address for a custom connector
// follows (2026-11-22: a server whose public address carried :25781 got
// "the server didn't respond" at Claude's check). Claude connects to a
// connector from its own cloud, and only on the standard HTTPS port: "Claude
// can't connect to a URL that specifies another port" (support.claude.com,
// article 11175166). Only the connector is stopped there — browsers, Leafen's
// apps and agents such as Claude Code work at the address as it is — so an
// answer says so beside what it says of the address, never as a fault of the
// address. OpenAI's own pages for ChatGPT's custom MCP servers name no such
// limit, so nothing here is said of ChatGPT. The web says the same
// (web/src/lib/claudeConnector.ts).

// claudeConnectorBlockingPort is the port that keeps a Claude connector from
// the address: "" when it can connect (https, with no port or 443), and ""
// for an address that isn't https, which agents on the internet don't take
// at all.
func claudeConnectorBlockingPort(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Scheme != "https" {
		return ""
	}
	if port := parsed.Port(); port != "443" {
		return port
	}
	return ""
}

// claudeConnectorPortNote is what an agent's answer adds for an address a
// Claude connector can't connect to, so the agent tells the person exactly
// that — "" for one it can.
func claudeConnectorPortNote(address string) string {
	port := claudeConnectorBlockingPort(address)
	if port == "" {
		return ""
	}
	return "Claude's custom connectors connect only to an https address without a port (443), and " + address + " is on port " + port +
		": a Claude chat (Claude.ai, the Claude app's chat) can't add Leafen there as a custom connector — Claude says the server didn't respond. " +
		"Only the connector fails: browsers, Leafen's apps and agents such as Claude Code connect at this address as it is; for a connector " +
		"they use Leafen's Tailscale, their own Cloudflare tunnel, or an address opened on port 443. Left to do, where the person would use " +
		"one: exactly that, to them."
}
