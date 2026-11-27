package app

import "github.com/pocketbase/pocketbase/core"

// An organization could once have its app tools take a person's answer
// only from their agent's program's own form (MCP elicitation). Leafen asks
// no form any more — every question goes through the agent's conversation
// (person_ask.go, decided 2026-11-21) — so the setting has nothing to hold
// to, and migration 839 takes its table away (schemaBaselineRetired).

const appAgentFormAnswersTable = "leafen_app_agent_form_answers"

func dropAppAgentFormAnswers(app core.App) error {
	return dropRetiredTable(app, appAgentFormAnswersTable)
}

func validateAppAgentFormAnswersDropped(app core.App) error {
	return validateRetiredTableDropped(app, appAgentFormAnswersTable)
}
