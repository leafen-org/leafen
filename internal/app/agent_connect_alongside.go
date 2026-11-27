package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Leafen goes into an agent's settings beside any entry there already, never
// in place of one (user 2026-11-19: told by the connection text to remove a
// leafen entry of the same address and add it again, an agent removed its
// person's earlier connection — "connect in parallel; cutting the earlier
// one off is for the person, in settings"). Leafen's entry is named
// leafen-<server>-<code>, or, where that is taken — another person's
// connection, an earlier one — leafen-<server>-<code>-<person>
// (agent_entry_names.go);
// nothing an agent runs from Leafen removes or overwrites an entry, and one
// at this Leafen that works here already as the person connecting is kept
// instead of another added (agent_connect_reuse.go). An earlier one
// Leafen refuses the key of, or one to this Leafen the agent won't use any
// more, it asks its person once whether to remove, and only on their yes
// disconnects it with its own key and takes it out of its settings (user
// 2026-11-20: the agent had ended with "disconnect it yourself in Settings";
// agentEntryTidyAsk); another person's stays as it is. The web's text takes
// the same names and says the same (web/src/lib/agentEntryNames.ts).

// agentEntryTidyAsk is what the agent does with an earlier entry Leafen went
// in beside: never removed unasked; asked about once when it no longer
// serves; on a yes, disconnected here with its own key (DELETE
// /mcp/connections/current, mcp_connection_self_disconnect.go) and taken
// out of the agent's settings.
const agentEntryTidyAsk = "Look at the earlier one: if Leafen refuses its key, or it is an earlier connection to this Leafen you won't use any more, " +
	"ask your person once whether to remove it, and only on their yes remove it — while its key still works, first disconnect it with that key " +
	"(DELETE its address's origin + " + apiRoot + mcpOwnConnectionPath + ", its header read from your settings and never printed), " +
	"then take it out of your settings (claude mcp remove <its name> --scope user; Codex: its [mcp_servers.<its name>] table). " +
	"Another person's connection, or another Leafen's, that works stays as it is: it is not your person's to remove."

// freeClaudeCodeEntryName is the first of names Claude Code holds no server
// under, in any scope it reads in folder (`claude mcp get` finds one by
// name in the folder's, the project's and the user's settings alike), with
// the names before it, which are taken. Every name taken is an error, before
// anything is made.
func freeClaudeCodeEntryName(program, folder string, names []string) (string, []string, error) {
	for at, name := range names {
		ctx, cancel := context.WithTimeout(context.Background(), claudeCodeCommandWait)
		command := exec.CommandContext(ctx, program, "mcp", "get", name)
		command.Dir = folder
		err := command.Run()
		timedOut := ctx.Err() != nil
		cancel()
		var exit *exec.ExitError
		switch {
		case err == nil:
			continue
		case errors.As(err, &exit) && !timedOut:
			return name, names[:at], nil
		}
		return "", nil, fmt.Errorf("Claude Code didn't say whether it holds a server named %s (%s); nothing was connected", name, err)
	}
	return "", nil, fmt.Errorf("Claude Code holds servers named %s already, and Leafen removes or overwrites none of them: nothing "+
		"was connected. Don't remove one on your own: which of them goes is your person's to decide. %s Then run this again",
		strings.Join(names, ", "), agentEntryTidyAsk)
}

// claudeCodeAddLine is the shell line that adds Leafen's entry, which source
// prints, to Claude Code in scope under the first name it doesn't hold yet,
// as the web's text does — unless an entry it holds at this Leafen works
// already (agent_connect_reuse.go), which it keeps: source runs only once
// none works and a name is free, so otherwise nothing is made. Once it
// added Leafen, it names the entries at this Leafen whose key Leafen refuses.
func claudeCodeAddLine(scope string, names []string, reach agentEntryReach, source, kept string) string {
	return `C="${CLAUDE_CODE_EXECPATH:-claude}"; S=` + scope + `; W=""; R=""; ` + claudeCodeHeldLine(reach) +
		`if [ -n "$W" ]; then ` + kept + `echo "Claude Code is connected to this Leafen already as $W, which works: nothing was made"; ` +
		`else for N in ` + strings.Join(names, " ") + ` ""; do [ -z "$N" ] || ! "$C" mcp get "$N" >/dev/null 2>&1 && break; done; ` +
		`if [ -z "$N" ]; then echo "Claude Code has ` + strings.Join(names, ", ") + ` already: nothing was made"; ` +
		`else "$C" mcp add-json --scope "$S" "$N" "$(` + source + `)" && { [ -z "$R" ] || echo "Leafen refuses the key of:$R"; }; fi; fi`
}

// agentEntryBesideTell is what the agent does once Leafen went in beside
// entries taken: names, the one Leafen's has, for what follows, and asks
// about an earlier one that no longer serves (agentEntryTidyAsk).
func agentEntryBesideTell(taken, refused []string, name string) string {
	held, them := "a server named "+taken[0]+" already, which stays as it was and doesn't reach this Leafen as this connection does", "it"
	if len(taken) > 1 {
		held, them = "servers named "+strings.Join(taken, " and ")+" already, which stay as they were and don't reach this Leafen "+
			"as this connection does", "them"
	}
	refuses := ""
	if len(refused) > 0 {
		refuses = " Leafen refuses the key of " + strings.Join(refused, " and ") + "."
	}
	return fmt.Sprintf("Claude Code held %s — Leafen never removes or overwrites one — so Leafen is beside %s as %s, the name "+
		"of Leafen's entry in what follows. Left to do: a line to your person that Leafen went in beside an earlier connection.%s %s",
		held, them, name, refuses, agentEntryTidyAsk)
}

// agentEntryBesideLine says, with the lines that add Leafen's entry, to add
// it beside one there already rather than in its place.
func agentEntryBesideLine(names []string) string {
	if len(names) < 2 {
		return "Name it " + names[0] + ". If your settings hold a server of that name already, leave it as it is and ask your person. " +
			agentEntryTidyAsk
	}
	return "Name it " + names[0] + ". If your settings hold a server of that name already — another person's connection, or an " +
		"earlier one — leave it as it is: add this one beside it as " + names[1] + " in place of " + names[0] +
		"; left to do then: a line to your person that it went in beside. " + agentEntryTidyAsk
}
