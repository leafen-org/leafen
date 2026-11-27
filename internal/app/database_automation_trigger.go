package app

import "strings"

func databaseAutomationTriggerMatches(
	trigger databaseAutomationTrigger, eventType, changedPropertyIDs string, values map[string]graphRowValue,
) bool {
	if eventType == databaseAutomationScheduleEvent {
		return trigger.Type == databaseAutomationTriggerSchedule
	}
	if trigger.Type != databaseAutomationTriggerEvents {
		return false
	}
	added := eventType == databaseAutomationTriggerRowAdded
	changed := map[string]struct{}{}
	for _, propertyID := range strings.Split(changedPropertyIDs, "\x00") {
		// A page added with a property's value edits it, as Notion's do (seen
		// 2026-10-24: a page duplicated with Stage Won runs "Stage = Won" and
		// "편집된 모든 속성", and with "모든" page added and Stage = Won only
		// such a page does); one added empty edits none.
		if value, present := values[propertyID]; propertyID != "" && (!added || !databaseAutomationRowValueEmpty(value, present)) {
			changed[propertyID] = struct{}{}
		}
	}
	matches := 0
	for _, condition := range trigger.Conditions {
		matched := false
		switch condition.Type {
		case databaseAutomationTriggerRowAdded:
			matched = added
		case databaseAutomationTriggerPropertyEdited:
			if eventType == databaseAutomationTriggerPropertyEdited || added {
				if condition.PropertyID == "" {
					matched = len(changed) > 0
				} else if _, edited := changed[condition.PropertyID]; edited {
					// What the edit left in it, when the condition names some.
					value, present := values[condition.PropertyID]
					matched = databaseAutomationConditionValueMatches(condition, value, present)
				}
			}
		}
		if matched {
			matches++
		}
	}
	if trigger.Mode == "all" {
		return matches == len(trigger.Conditions)
	}
	return matches > 0
}
