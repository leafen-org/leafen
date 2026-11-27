package app

import (
	"errors"
	"fmt"
	"strings"
)

// The instructions keep their room (2026-11-23, the gate's lap 9: rules
// added one at a time came to fill the 2,048 units Claude Code keeps, and
// the outside services' and apps' short lines, which "always have their
// room", fell out whole). The server checks it as it makes its first MCP
// server, as it checks its tools (checkedMCPToolArguments), and doesn't
// start when a rule added leaves too little: for each language a person's
// Leafen can be in, the longest server line — its name at the most an
// administrator may give (serverNameRunes; an alias is shorter), an address
// of mcpInstructionsLongestOrigin and another as long that the agent reaches
// it at (agentAddressNow) — the rules, and both lists' longest short lines
// leave mcpInstructionsHeadroom to spare, with the line counting the
// connection's workspaces (mcp_instructions_reach.go). A longer address takes
// from that spare first. A rule that doesn't fit goes where the rule for
// what the first message holds sends it (mcp_instructions.go), not in
// shorter words.

const (
	mcpInstructionsHeadroom = 100
	// An address as long as most are: a tunnel's or a tailnet's, a domain's.
	mcpInstructionsLongestOrigin = 64
	// The most apps a short line counts, by its digits.
	mcpInstructionsLongestAppCount = 999
)

// appAgentShortLine is the apps' short line, for a count of apps with tools.
func appAgentShortLine(apps int) string {
	return fmt.Sprintf("Leafen apps with tools: %d (leafen_app_tools).", apps)
}

// mcpInstructionsLongest is how long a language's instructions come to at
// the longest.
func mcpInstructionsLongest(language string) int {
	name := strings.Repeat("W", min(serverNameRunes, mcpServerNameRunes))
	origin := "https://" + strings.Repeat("a", mcpInstructionsLongestOrigin-len("https://"))
	reached := "https://" + strings.Repeat("b", mcpInstructionsLongestOrigin-len("https://"))
	server := mcpServerNameLineFor(name, strings.Repeat("W", serverCodeLength), origin, reached)
	services := max(utf16Length(mcpServicesNoneLine), utf16Length(mcpServicesCountLine(fmt.Sprintf("more than %d", mcpServiceInstructionServices))))
	apps := utf16Length(appAgentShortLine(mcpInstructionsLongestAppCount))
	reach := utf16Length(mcpReachShortLongest())
	return utf16Length(server) + 2 + reach + 2 + utf16Length(mcpNativeInstructionsIn(language)) + 2 + services + 2 + apps
}

// checkMCPInstructionsRoom says which languages' instructions leave too
// little room, at the longest.
func checkMCPInstructionsRoom() error {
	problems := []string{}
	for _, language := range selectableAccountLanguagePreferenceValues {
		if total := mcpInstructionsLongest(language); total+mcpInstructionsHeadroom > mcpInstructionsLimit {
			problems = append(problems, fmt.Sprintf("%s: %d of %d", language, total, mcpInstructionsLimit-mcpInstructionsHeadroom))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New("MCP instructions leave the lists no room (mcp_instructions_room.go): " + strings.Join(problems, "; "))
}
