package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A select's, multi-select's or status's options follow Notion's when the
// same plan runs again, as the property's name does (decision "설계 우선
// Notion 이사" 3절 원칙 4; 2026-11-24: an option renamed or recoloured in
// Notion kept its old name and colour, one Notion deleted stayed, and one
// Notion added came into no property, its rows holding an option the
// property didn't have). An option is the same one by its ID, so the rows,
// filters and groups holding it hold it still under its new name. The ledger
// keeps each property's options as a run gave them — after the plan's values
// (notion_migration_values.go) — and a run again takes, option by option:
//
//   - a name or colour Notion changed since, while Leafen still has the one
//     the run gave: one changed in Leafen since stays, as a page or property
//     renamed in Leafen keeps its name (notionMigrationNameFollows);
//   - an option Notion added since, unless an option of its name is there
//     already (one the plan's values joined it with);
//   - an option Notion deleted since goes, while Leafen still has it as the
//     run gave it; Notion took it out of its rows, which the run empties as it
//     does any cell Notion emptied (notion_migration_rerun_emptied.go);
//   - an option taken away in Leafen since stays away.
//
// A status's option comes into its group, and leaves it, with it. A property
// a run before kept no options of brings only the options it lacks.

// notionMigrationLedgerOptions keeps, by a property's source, its options as
// the run gave them.
const notionMigrationLedgerOptions = "options"

// keepNotionMigrationOptions keeps in the ledger a property's options as the
// run gives them (its staged source, the plan's values taken).
func keepNotionMigrationOptions(app core.App, migrationID string, source storedNotionSource, propertyID string) error {
	plan := notionDatabasePropertyPlan{}
	if migrationID == "" || json.Unmarshal([]byte(source.Text), &plan) != nil || !notionMigrationOptionTypes[plan.Type] {
		return nil
	}
	options, err := json.Marshal(plan.Config["options"])
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_migration_ledger}}
			([[migration_id]],[[workspace_id]],[[source_id]],[[kind]],[[native_id]],[[page_id]],[[state]])
		SELECT [[id]], [[workspace_id]], {:sourceId}, {:kind}, {:nativeId}, '', {:options}
		FROM {{leafen_notion_migrations}} WHERE [[id]]={:migrationId}
		ON CONFLICT ([[migration_id]],[[source_id]],[[kind]]) DO UPDATE SET
			[[native_id]]=excluded.[[native_id]], [[state]]=excluded.[[state]]
	`).Bind(dbx.Params{
		"migrationId": migrationID, "sourceId": source.SourceID, "kind": notionMigrationLedgerOptions,
		"nativeId": propertyID, "options": string(options),
	}).Execute()
	return err
}

// followedNotionMigrationOptions is a property's config with its options as
// they follow Notion's since the run before, and whether any did.
func followedNotionMigrationOptions(
	app core.App, payload notionImportJobPayload, source storedNotionSource, propertyType, config string,
) (string, bool, error) {
	plan := notionDatabasePropertyPlan{}
	current := map[string]any{}
	if payload.MigrationRerunSince <= 0 || !notionMigrationOptionTypes[propertyType] || json.Unmarshal([]byte(source.Text), &plan) != nil ||
		!notionMigrationOptionTypes[plan.Type] || json.Unmarshal([]byte(config), &current) != nil {
		return config, false, nil
	}
	kept, err := notionMigrationLedgerState(app, payload.MigrationID, source.SourceID, notionMigrationLedgerOptions)
	if err != nil {
		return config, false, err
	}
	known := map[string]map[string]any{}
	knownList := []any{}
	if kept != "" && json.Unmarshal([]byte(kept), &knownList) == nil {
		known = notionMigrationOptionsByID(knownList)
	}
	given, _ := plan.Config["options"].([]any)
	now := notionMigrationOptionsByID(given)
	held, _ := current["options"].([]any)
	next, names, changed := make([]any, 0, len(held)+len(given)), map[string]bool{}, false
	for _, raw := range held {
		option, _ := raw.(map[string]any)
		id, _ := option["id"].(string)
		gave, wasGiven := known[id]
		notion, isGiven := now[id]
		switch {
		case wasGiven && !isGiven && notionMigrationOptionAs(option, gave, "name") && notionMigrationOptionAs(option, gave, "color"):
			// Notion deleted it, and it is as the run gave it.
			changed = true
			notionMigrationStatusGroupLeave(current, id)
			continue
		case wasGiven && isGiven:
			for _, field := range []string{"name", "color"} {
				if !notionMigrationOptionAs(notion, gave, field) && notionMigrationOptionAs(option, gave, field) {
					option[field], changed = notion[field], true
				}
			}
		}
		name, _ := option["name"].(string)
		names[strings.TrimSpace(name)] = true
		next = append(next, option)
	}
	for _, raw := range given {
		option, _ := raw.(map[string]any)
		id, _ := option["id"].(string)
		name, _ := option["name"].(string)
		if _, wasGiven := known[id]; wasGiven || names[strings.TrimSpace(name)] || notionMigrationOptionHeld(held, id) {
			continue
		}
		names[strings.TrimSpace(name)], changed = true, true
		next = append(next, option)
		notionMigrationStatusGroupJoin(current, plan.Config, id)
	}
	if !changed {
		return config, false, nil
	}
	current["options"] = next
	encoded, err := json.Marshal(current)
	return string(encoded), err == nil, err
}

// notionMigrationOptionsByID are options by their IDs.
func notionMigrationOptionsByID(options []any) map[string]map[string]any {
	byID := map[string]map[string]any{}
	for _, raw := range options {
		if option, ok := raw.(map[string]any); ok {
			if id, _ := option["id"].(string); id != "" {
				byID[id] = option
			}
		}
	}
	return byID
}

// notionMigrationOptionAs says whether two options have a field alike.
func notionMigrationOptionAs(one, other map[string]any, field string) bool {
	left, _ := one[field].(string)
	right, _ := other[field].(string)
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

// notionMigrationOptionHeld says whether options hold one of an ID.
func notionMigrationOptionHeld(options []any, id string) bool {
	for _, raw := range options {
		if option, ok := raw.(map[string]any); ok && option["id"] == id {
			return true
		}
	}
	return false
}

// notionMigrationStatusGroupLeave takes an option out of a status's groups.
func notionMigrationStatusGroupLeave(config map[string]any, id string) {
	groups, _ := config["statusGroups"].([]any)
	for _, raw := range groups {
		group, _ := raw.(map[string]any)
		members, _ := group["optionIds"].([]any)
		left := make([]any, 0, len(members))
		for _, member := range members {
			if member != id {
				left = append(left, member)
			}
		}
		if group != nil {
			group["optionIds"] = left
		}
	}
}

// notionMigrationStatusGroupJoin puts a new option of a status in the group
// Notion has it in, by the group's ID or else its name.
func notionMigrationStatusGroupJoin(config, staged map[string]any, id string) {
	groups, _ := config["statusGroups"].([]any)
	stagedGroups, _ := staged["statusGroups"].([]any)
	for _, raw := range stagedGroups {
		stagedGroup, _ := raw.(map[string]any)
		members, _ := stagedGroup["optionIds"].([]any)
		if !notionMigrationIDsHold(members, id) {
			continue
		}
		for _, held := range groups {
			group, _ := held.(map[string]any)
			if group == nil || group["id"] != stagedGroup["id"] && !notionMigrationOptionAs(group, stagedGroup, "name") {
				continue
			}
			list, _ := group["optionIds"].([]any)
			group["optionIds"] = append(list, id)
			return
		}
	}
}

// notionMigrationIDsHold says whether a list of IDs holds one.
func notionMigrationIDsHold(ids []any, id string) bool {
	for _, held := range ids {
		if held == id {
			return true
		}
	}
	return false
}
