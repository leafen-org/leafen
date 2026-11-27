package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A status condition may name a whole group, as Notion's does (seen
// 2026-11-11: picking "완료" with all its options reads "상태 속성이
// [Complete] 값으로 설정되었습니다" and is kept as the group). Such a condition
// is met by whatever options are in the group as it runs: Notion's ran for a
// row set to an option added to "Complete" after the automation was saved,
// for a row moved from "Done" to "Archived" inside it, and not for a row moved
// out of it to "In progress". A condition's list holds option IDs and the IDs
// of the groups ticked whole; an option's ID wins over a group's, as a view's
// status filter reads them (databaseQueryOptionTargets).

// databaseAutomationStatusGroupsAsOptions puts in place of each whole group a
// status condition names the options in that group now.
func databaseAutomationStatusGroupsAsOptions(
	app core.App, state *databaseAutomationEvaluationState, candidate databaseAutomationEvaluationCandidate,
	trigger *databaseAutomationTrigger,
) {
	for index, condition := range trigger.Conditions {
		if condition.Type != databaseAutomationTriggerPropertyEdited || condition.PropertyID == "" || len(condition.Value) == 0 {
			continue
		}
		var wanted []any
		if json.Unmarshal(condition.Value, &wanted) != nil || len(wanted) == 0 {
			continue
		}
		property, found := state.statusProperty(app, candidate, condition.PropertyID)
		if !found {
			continue
		}
		config := databaseQueryParsedPropertyConfig(property)
		options := map[string]struct{}{}
		for _, option := range config.Options {
			options[option.ID] = struct{}{}
		}
		expanded := make([]any, 0, len(wanted))
		changed := false
		for _, item := range wanted {
			id, text := item.(string)
			if _, option := options[id]; !text || option {
				expanded = append(expanded, item)
				continue
			}
			group, named := databaseAutomationStatusGroup(config, id)
			if !named {
				expanded = append(expanded, item)
				continue
			}
			changed = true
			for _, optionID := range group {
				expanded = append(expanded, optionID)
			}
		}
		if !changed {
			continue
		}
		// A group with no options left names none of them, which no row holds.
		if value, err := json.Marshal(expanded); err == nil {
			trigger.Conditions[index].Value = value
		}
	}
}

// databaseAutomationStatusGroup is the options in a status's group: those it
// lists and those that name it as theirs.
func databaseAutomationStatusGroup(config databaseQueryPropertyConfig, groupID string) ([]string, bool) {
	for _, group := range config.StatusGroups {
		if group.ID != groupID {
			continue
		}
		seen := map[string]struct{}{}
		members := []string{}
		add := func(id string) {
			if _, done := seen[id]; id != "" && !done {
				seen[id] = struct{}{}
				members = append(members, id)
			}
		}
		for _, id := range group.OptionIDs {
			add(id)
		}
		for _, option := range config.Options {
			if option.Group != "" && option.Group == group.Name {
				add(option.ID)
			}
		}
		return members, true
	}
	return nil, false
}

// statusProperty is a database's status property as it is now, read once a
// chunk; a property of another type, or gone, is not found.
func (state *databaseAutomationEvaluationState) statusProperty(
	app core.App, candidate databaseAutomationEvaluationCandidate, propertyID string,
) (storedDatabaseQueryProperty, bool) {
	key := candidate.OwnerID + "\x00" + candidate.PageID + "\x00" + candidate.DatabaseID + "\x00" + propertyID
	if property, cached := state.statusProperties[key]; cached {
		return property, property.Type == graphValueTypeStatus
	}
	property := storedDatabaseQueryProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[data_source_id]] = {:databaseId} AND [[id]] = {:id}
	`).Bind(dbx.Params{
		"owner": candidate.OwnerID, "pageId": candidate.PageID,
		"databaseId": candidate.DatabaseID, "id": propertyID,
	}).One(&property)
	if err != nil {
		property = storedDatabaseQueryProperty{}
	}
	state.statusProperties[key] = property
	return property, property.Type == graphValueTypeStatus
}
