package app

import (
	"fmt"
	"strings"
)

// The instructions say where this connection reaches (the user, 2026-11-26:
// an agent at work in a folder whose work is kept outside Leafen, its
// connection reaching one personal workspace, searched Leafen before every
// answer — the rule now sends it to Leafen when a question concerns what is
// kept there, and this line names where that is): its workspaces by name,
// whole where they fit after the rules and the other lists' short lines,
// else how many there are, which leafen_list_workspaces names — the short
// line, whose room the server checks as it starts (checkMCPInstructionsRoom).
// It is the person's reach or less: the rule naming whom the agent works as
// no longer says so itself.

// mcpReachWholeUnits bounds the line naming the workspaces.
const mcpReachWholeUnits = 240

// mcpReachLines are the line naming the connection's workspaces and the
// short one counting them.
func mcpReachLines(authority mcpAuthority) (whole, short string) {
	names := make([]string, 0, len(authority.ApprovedWorkspaces))
	for _, workspace := range authority.ApprovedWorkspaces {
		names = append(names, fmt.Sprintf("%q", workspace.Name))
	}
	short = mcpReachShortLine(len(names))
	if len(names) == 0 {
		return short, short
	}
	whole = "This connection's workspaces: " + strings.Join(names, ", ") + "."
	if utf16Length(whole) > mcpReachWholeUnits {
		whole = short
	}
	return whole, short
}

// mcpReachShortLine counts the connection's workspaces.
func mcpReachShortLine(workspaces int) string {
	return fmt.Sprintf("This connection's workspaces: %d.", workspaces)
}

// mcpReachShortLongest is the short line at its longest.
func mcpReachShortLongest() string {
	return mcpReachShortLine(mcpInstructionsLongestAppCount)
}
