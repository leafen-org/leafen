package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// An agent reading a row's page — GET /database-rows/{rowId}/page with its
// key, by REST or leafen_call — takes each select's, multi-select's and
// status's options short: the first databaseRowPageAgentOptionsShown and
// those the row holds, its config saying how many there are and how to read
// them all (2026-11-26, an agent's real use: a row of a database whose
// select had 60 options answered 12,067 characters, every option, on every
// row read; a row page's notion-fetch gives its table short the same way,
// mcp_parent_schema_text.go). The caller is told apart as a refusal tells it
// (apiErrorAgentRequest): the screen's own session and an app's key — the
// app SDK's table rows and an app's own reads go through this route — read
// them whole, as a column's menu is drawn from them; ?options=all reads them
// whole for anyone. A config read cut short and written back keeps the
// options it didn't show (databaseOptionsCutKept): one is deleted from it
// only by "delete": true.

// databaseRowPageAgentOptionsShown is how many of a column's options an
// agent's read shows beside those the row holds.
const databaseRowPageAgentOptionsShown = 10

// databaseOptionsCutKey marks a column's config read with its options cut
// short, saying how many it shows, how many there are and how to read all.
const databaseOptionsCutKey = "optionsCut"

// databaseRowPageOptionsForAgent cuts the options of an agent's read of a
// row's page short, unless it asked for them all.
func databaseRowPageOptionsForAgent(event *core.RequestEvent, rowID string, all bool, page *databaseRowPageSnapshot) {
	if all || !apiErrorAgentRequest(event) {
		return
	}
	values := map[string]struct {
		Value json.RawMessage `json:"value"`
	}{}
	_ = json.Unmarshal(page.Row.Values, &values)
	for index, property := range page.Properties {
		if _, options := databasePropertyConversionOptionTypes[property.Type]; !options {
			continue
		}
		held := map[string]bool{}
		var one string
		var many []string
		if json.Unmarshal(values[property.ID].Value, &one) == nil {
			held[one] = true
		} else if json.Unmarshal(values[property.ID].Value, &many) == nil {
			for _, id := range many {
				held[id] = true
			}
		}
		if cut, changed := databaseOptionsCut(property.Config, held, rowID); changed {
			page.Properties[index].Config = cut
		}
	}
}

// databaseOptionsCut is a column's config with its options cut to the first
// shown and those held, and the mark saying so; unchanged when it has no
// more than those.
func databaseOptionsCut(raw json.RawMessage, held map[string]bool, rowID string) (json.RawMessage, bool) {
	config := map[string]any{}
	if json.Unmarshal(raw, &config) != nil {
		return raw, false
	}
	options, _ := config["options"].([]any)
	if len(options) <= databaseRowPageAgentOptionsShown {
		return raw, false
	}
	shown := make([]any, 0, databaseRowPageAgentOptionsShown+len(held))
	for index, option := range options {
		if index < databaseRowPageAgentOptionsShown || held[databaseOptionID(option)] {
			shown = append(shown, option)
		}
	}
	config["options"] = shown
	config[databaseOptionsCutKey] = map[string]any{
		"shown": len(shown), "total": len(options), "all": "GET /database-rows/" + rowID + "/page?options=all",
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

// databaseOptionsCutKept is a column's config a change writes, read cut
// short and given back: its options as the column has them (stored), each
// the change names as it names it, those it names that are new after them;
// none of the others left out. Its mark goes. Any other config is as given.
func databaseOptionsCutKept(stored, sent json.RawMessage) json.RawMessage {
	if !strings.Contains(string(sent), `"`+databaseOptionsCutKey+`"`) {
		return sent
	}
	config := map[string]any{}
	if json.Unmarshal(sent, &config) != nil {
		return sent
	}
	if _, cut := config[databaseOptionsCutKey]; !cut {
		return sent
	}
	delete(config, databaseOptionsCutKey)
	had := struct {
		Options []map[string]any `json:"options"`
	}{}
	_ = json.Unmarshal(stored, &had)
	given, _ := config["options"].([]any)
	byID := map[string]any{}
	for _, option := range given {
		if id := databaseOptionID(option); id != "" {
			byID[id] = option
		}
	}
	kept, placed := make([]any, 0, len(had.Options)+len(given)), map[string]bool{}
	for _, option := range had.Options {
		id := databaseOptionID(option)
		if sentOption, named := byID[id]; named {
			kept = append(kept, sentOption)
		} else {
			kept = append(kept, option)
		}
		placed[id] = true
	}
	for _, option := range given {
		if !placed[databaseOptionID(option)] {
			kept = append(kept, option)
		}
	}
	config["options"] = kept
	encoded, err := json.Marshal(config)
	if err != nil {
		return sent
	}
	return encoded
}

// databaseOptionID is an option's ID as a config holds it, "" for none.
func databaseOptionID(option any) string {
	held, _ := option.(map[string]any)
	id, _ := held["id"].(string)
	return strings.TrimSpace(id)
}
