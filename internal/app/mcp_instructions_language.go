package app

import (
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// The first rule of the instructions names the person's language where
// their Leafen is in one other than English (2026-11-23: isolated agents
// asked in Korean said a line between tool calls, once a whole answer, in
// English, after Leafen's English answers — the rule said "in their
// language" and left the agent to judge it from one short request among
// English instructions, answers and the agent's own files). Their Leafen's
// language is a fact Leafen holds, named by its own name (한국어, 日本語),
// which is short and is itself in that language: the rule stays within
// what Claude Code keeps of the instructions (mcpInstructionsLimit).

// mcpPersonLanguageWords is what the rule adds for a person whose Leafen is
// in code: "" for English, the system's or an unknown code, else its name
// in itself in brackets.
func mcpPersonLanguageWords(code string) string {
	if code == "" || code == accountLanguageEn || code == accountLanguageSystem {
		return ""
	}
	tag, err := language.Parse(code)
	if err != nil {
		return ""
	}
	if name := display.Self.Name(tag); name != "" {
		return " (" + name + ")"
	}
	return ""
}
