package app

import (
	"encoding/json"
	"sync"
)

// databaseStatusConfigFacts is what an option property's config says of the
// values its rows read: the options it has, and a status's default
// (databaseStatusDefaultOptionID).
type databaseStatusConfigFacts struct {
	fallback string
	known    map[string]struct{}
	// listed tells a config that lists its options, none or more, from one
	// that keeps none, whose values are taken as they are.
	listed bool
}

// databaseStatusDefaults keeps the facts of the status configs read lately:
// a query reads one for each row, so its config is worked out once, not once
// a row.
var databaseStatusDefaults = struct {
	sync.Mutex
	byConfig map[string]databaseStatusConfigFacts
}{byConfig: map[string]databaseStatusConfigFacts{}}

const maxDatabaseStatusDefaults = 256

// databaseStatusDefaultOptionID is the option a status reads as while a row
// sets none, as Notion's (seen 2026-10-06, 2026-11-08): the one its config
// names, else the first of its first group's ("To-do"), else its first
// option. A config naming none ("") has no default, as a status made from a
// select has in Notion (default_option null): its rows that set none read
// empty. The web reads the same (statusDefaultOptionId).
func databaseStatusDefaultOptionID(config string) string {
	return databaseStatusConfigFactsOf(config).fallback
}

func databaseStatusConfigFactsOf(config string) databaseStatusConfigFacts {
	databaseStatusDefaults.Lock()
	facts, known := databaseStatusDefaults.byConfig[config]
	databaseStatusDefaults.Unlock()
	if known {
		return facts
	}
	facts = databaseStatusConfigFacts{fallback: databaseStatusConfigDefault(config), known: map[string]struct{}{}}
	var parsed struct {
		Options []databaseQueryPropertyOption `json:"options"`
	}
	if json.Unmarshal([]byte(config), &parsed) == nil && parsed.Options != nil {
		facts.listed = true
		for _, option := range parsed.Options {
			facts.known[option.ID] = struct{}{}
		}
	}
	databaseStatusDefaults.Lock()
	if len(databaseStatusDefaults.byConfig) >= maxDatabaseStatusDefaults {
		databaseStatusDefaults.byConfig = map[string]databaseStatusConfigFacts{}
	}
	databaseStatusDefaults.byConfig[config] = facts
	databaseStatusDefaults.Unlock()
	return facts
}

func databaseStatusConfigDefault(config string) string {
	var parsed struct {
		DefaultOptionID *string                       `json:"defaultOptionId"`
		Options         []databaseQueryPropertyOption `json:"options"`
		StatusGroups    []databaseQueryStatusGroup    `json:"statusGroups"`
	}
	if json.Unmarshal([]byte(config), &parsed) != nil || len(parsed.Options) == 0 {
		return ""
	}
	known := make(map[string]bool, len(parsed.Options))
	for _, option := range parsed.Options {
		known[option.ID] = true
	}
	if parsed.DefaultOptionID != nil {
		if *parsed.DefaultOptionID == "" || known[*parsed.DefaultOptionID] {
			return *parsed.DefaultOptionID
		}
	}
	if len(parsed.StatusGroups) > 0 {
		for _, id := range parsed.StatusGroups[0].OptionIDs {
			if known[id] {
				return id
			}
		}
	}
	return parsed.Options[0].ID
}

// databaseStatusValueOrDefault is a row's stored status as it reads: the
// option it sets, else the status's default. Notion keeps no default in a
// row (seen 2026-11-08): rows that set none, made before the status or
// after it (by hand, by the API or from a template), read the default and
// switch with it, a filter or a sort reads it, and a change of type leaves
// them empty; so Leafen writes none either. A row whose option the status no
// longer has sets none, as Notion's reads it (seen 2026-11-08: the rows of
// options its API took away read the default, or empty with no options).
func databaseStatusValueOrDefault(config string, value json.RawMessage) json.RawMessage {
	var option string
	if len(value) > 0 && string(value) != "null" && json.Unmarshal(value, &option) != nil {
		return value
	}
	facts := databaseStatusConfigFactsOf(config)
	if option != "" {
		if _, known := facts.known[option]; known || !facts.listed {
			return value
		}
		value = json.RawMessage("null")
	}
	if facts.fallback == "" {
		return value
	}
	encoded, err := json.Marshal(facts.fallback)
	if err != nil {
		return value
	}
	return encoded
}
