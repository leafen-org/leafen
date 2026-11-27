package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// In the first run an agent connects itself only after its person said yes
// to Leafen's question, and runs nothing to connect before (2026-11-21, the
// real Claude Code 2.1.330 on the Windows test machine, in its default auto
// mode: a first `leafen connect` the person hadn't asked for was refused as
// undermining oversight — with --add-to claude-code, and again with its name
// alone chained after the step the person had answered — while the same run
// went through once the person had said to go on, and --approved
// --add-to claude-code went through on their yes to Leafen's question). So
// `leafen setup status`, at the agents step, gives that question itself, in
// the person's language, and what to run on their yes; the one question is
// the only one asked (user 2026-11-19: no choices the person didn't ask
// for). docs/install.md step 4 says the same.

// agentConnectNamePlaceholder stands for the agent's name in the question
// setup status gives, where the agent that runs it isn't known.
const agentConnectNamePlaceholder = "<your program's name>"

// agentConnectFirstRun is how an agent runs `leafen connect` to be asked:
// with its name alone, which changes no settings (connect's help says it so,
// and setup status where it has no question to give).
const agentConnectFirstRun = "run leafen connect --name \"" + agentConnectNamePlaceholder + "\" as it is, with nothing added (no " +
	"--approved, no --add-to), once the person asked you to connect or said to go on: it connects nothing and changes no " +
	"settings, it only says the one question to ask the person in this conversation and what to run on their yes — for " +
	"Claude Code, the same command with --approved --add-to claude-code"

// setupAgentAsk is the question an agent asks its person before connecting
// itself in the first run, as `leafen connect` would word it for the
// account's workspaces, with the agent's name left as the placeholder.
func setupAgentAsk(app core.App, owner string) string {
	workspaces, err := userKeyChoosableWorkspaces(app, owner)
	if err != nil || len(workspaces) == 0 {
		return ""
	}
	ask := agentConnectAsk(app, agentConnectPlan{OwnerID: owner, Name: agentConnectNamePlaceholder, Workspaces: workspaces})
	return ask.Question + " " + ask.Effect
}

// setupConnectNext is setup status's next step at the agents step: the
// question, asked before anything runs, and what to run on its yes.
func setupConnectNext(ask string) string {
	skip := " If they'd rather not now: leafen setup skip agents"
	if ask == "" {
		return "connect yourself: " + agentConnectFirstRun + "." + skip
	}
	name, run := agentConnectNamePlaceholder, "leafen connect --name \""+agentConnectNamePlaceholder+"\" --approved (Claude Code: "+
		"leafen connect --name \"Claude Code\" --approved --add-to claude-code, which adds Leafen to its settings for every folder "+
		"with the key never showing)"
	if runByClaudeCode() {
		name, run = "Claude Code", "leafen connect --name \"Claude Code\" --approved --add-to claude-code (Leafen adds itself to Claude "+
			"Code's settings for every folder, and the key never shows)"
		ask = strings.ReplaceAll(ask, agentConnectNamePlaceholder, name)
	}
	words := "connect yourself, asking first: before anything runs to connect, left to do is one question to the person in this " +
		"conversation, in their language, carrying all that this says"
	if name == agentConnectNamePlaceholder {
		words += " (your program's name in place of " + agentConnectNamePlaceholder + ")"
	}
	return words + ": “" + ask + "” Nothing else goes with it: every folder, all their workspaces and everything they may do are " +
		"Leafen's defaults, not choices to offer. The answer is theirs alone. Only on their yes: " + run + ", and what it says." + skip
}
