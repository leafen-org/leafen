package app

import (
	"fmt"
	"strings"
)

// A Notion migration's plan is saved only with the values its fields take at
// all: a person's action one of the four the plan's people take, a
// property's type a Leafen property type or none (its source's). Whether a
// value fits what the scan found — a type the source converts to, an
// invitation for a person with an email — is the plan's check's, as before
// (notion_migration_plan_check.go); a value no plan takes is refused when
// the plan is sent, naming the field and what it takes (2026-11-26: a plan
// with a person's action misspelled was saved, and only its check said the
// value was invalid, the action's choices said nowhere).

// notionMigrationPlanValueProblem names the first value of a plan its field
// doesn't take, none for none.
func notionMigrationPlanValueProblem(plan notionMigrationPlan) string {
	for index, person := range plan.People {
		switch action := strings.TrimSpace(person.Action); action {
		case "", notionMigrationChoiceMember, notionMigrationChoiceKeep, notionMigrationChoiceInvite, notionMigrationChoiceClear:
		default:
			return fmt.Sprintf("people[%d].action is member, keep, invite or clear, not %q", index, action)
		}
	}
	propertyProblem := func(at string, planned []notionMigrationPlanProperty) string {
		for index, property := range planned {
			if kind := strings.TrimSpace(property.Type); kind != "" {
				if _, known := graphValueTypes[kind]; !known {
					return fmt.Sprintf("%s.properties[%d].type is a Leafen property type, or left out for its source's, not %q", at, index, kind)
				}
			}
		}
		return ""
	}
	for index, database := range plan.Databases {
		at := fmt.Sprintf("databases[%d]", index)
		if problem := propertyProblem(at, database.Properties); problem != "" {
			return problem
		}
		for source, planned := range database.DataSources {
			if problem := propertyProblem(fmt.Sprintf("%s.dataSources[%d]", at, source), planned.Properties); problem != "" {
				return problem
			}
		}
	}
	return ""
}
