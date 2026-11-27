package app

import (
	"fmt"
	"strings"
)

// The lists a migration's answers to its agent would carry grow with the
// move: a done move of a few hundred table views listed as many items left
// at Leafen's defaults, a reported property up to 2,000 row values, the check
// up to 200 differences — while the agent says each by its group, as the
// report page folds them (2026-11-21, the person: a status answer of 310
// items the agent only counted). So an answer says them by their groups and
// counts, and lists their items only when asked: leafen_notion_migration_status
// with list formats_left, differences or reported_values, a window at a time,
// as every answer that gives a growing list does (mcp_answer_lists.go).
// What the next asks the agent to do one by one stays in the answer: the
// check's differences it can fix (notionMigrationVerifyFixable).

const (
	notionMigrationStatusListFormatsLeft    = "formats_left"
	notionMigrationStatusListDifferences    = "differences"
	notionMigrationStatusListReportedValues = "reported_values"
)

var notionMigrationStatusLists = []string{
	notionMigrationStatusListFormatsLeft, notionMigrationStatusListDifferences, notionMigrationStatusListReportedValues,
}

// leafenMCPNotionReportedValue is a value of a property the plan reported,
// as list reported_values gives it: the property as runReport.reported names
// it, and the row the value was in (none for a computed property's
// definition).
type leafenMCPNotionReportedValue struct {
	DatabaseID string `json:"database_id"`
	PropertyID string `json:"property_id"`
	Property   string `json:"property"`
	RowID      string `json:"row_id,omitempty"`
	RowTitle   string `json:"row_title,omitempty"`
	Value      string `json:"value"`
}

// leafenMCPNotionFormatsLeftGroup is a group of what stays at Leafen's
// defaults: the kind and reason its items share, the report page's line for
// it and how many things it counts.
type leafenMCPNotionFormatsLeftGroup struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	Say    string `json:"say"`
	Count  int    `json:"count"`
}

// notionMigrationFormatsLeftGroupsSaid are the groups as the status gives
// them, each with its kind and reason (notionMigrationFormatsLeftGroups keys
// them so).
func notionMigrationFormatsLeftGroupsSaid(groups []notionMigrationReportGroup) []leafenMCPNotionFormatsLeftGroup {
	said := make([]leafenMCPNotionFormatsLeftGroup, 0, len(groups))
	for _, group := range groups {
		kind, reason, _ := strings.Cut(group.Key, "\x00")
		said = append(said, leafenMCPNotionFormatsLeftGroup{Kind: kind, Reason: reason, Say: cmpOr(group.One, group.Say), Count: group.Count})
	}
	return said
}

// notionMigrationVerifyFixable reports whether the check's next asks the
// agent to set a difference as Notion has it: one with no reason Leafen
// knows, and a fix a tool makes.
func notionMigrationVerifyFixable(difference notionMigrationVerifyDifference) bool {
	return difference.Fix != "" && difference.Reason == notionMigrationVerifyUnexplained
}

// notionMigrationVerifyAnswered is a check as an answer carries it: of what
// differs, only what its next asks to fix; remainingTotal counts them all.
func notionMigrationVerifyAnswered(verify notionMigrationVerify) notionMigrationVerify {
	fixable := []notionMigrationVerifyDifference{}
	for _, difference := range verify.Remaining {
		if notionMigrationVerifyFixable(difference) {
			fixable = append(fixable, difference)
		}
	}
	verify.Remaining = fixable
	return verify
}

// notionMigrationAnswered is a migration as an agent's answer carries it: its
// run report's reported properties with their counts and without their row
// values (a computed property's definition stays), and its check as
// notionMigrationVerifyAnswered gives it. The migration itself is left as it
// is.
func notionMigrationAnswered(migration notionMigration) notionMigration {
	if len(migration.RunReport.Reported) > 0 {
		reported := make([]notionMigrationReportedProperty, 0, len(migration.RunReport.Reported))
		for _, property := range migration.RunReport.Reported {
			kept := []notionMigrationReportedValue{}
			for _, value := range property.Values {
				if value.RowID == "" {
					kept = append(kept, value)
				}
			}
			property.Values = kept
			reported = append(reported, property)
		}
		migration.RunReport.Reported = reported
	}
	if verify := migration.RunReport.Verify; verify != nil {
		answered := notionMigrationVerifyAnswered(*verify)
		migration.RunReport.Verify = &answered
	}
	return migration
}

// notionMigrationStatusListingOf reads a status call's list and start_cursor.
func notionMigrationStatusListingOf(list, cursor string) (mcpAnswerListing, error) {
	return mcpAnswerListingOf(list, cursor, notionMigrationStatusLists...)
}

// fillNotionMigrationStatusList puts the window of the list the call asked
// for in the answer, and starts its next with what the window says of
// itself.
func fillNotionMigrationStatusList(
	output *leafenMCPNotionMigrationStatusOutput, listing mcpAnswerListing, migration notionMigration, left []leafenMCPNotionFormatLeft,
) {
	said, note := "", ""
	switch listing.List {
	case "":
		return
	case notionMigrationStatusListFormatsLeft:
		page := mcpAnswerWindowOf(listing, listing.List, left)
		output.FormatsLeft, output.NextCursor, said = page.Items, page.NextCursor, page.said()
		if migration.RunState != notionMigrationRunDone {
			note = " It lists them once the run is done."
		}
	case notionMigrationStatusListDifferences:
		remaining := []notionMigrationVerifyDifference{}
		if verify := migration.RunReport.Verify; verify == nil {
			note = " The run checks itself against Notion at its end."
		} else if remaining = verify.Remaining; verify.RemainingTotal > len(remaining) {
			note = fmt.Sprintf(" The check keeps %d of the %d it found, those to fix first; check_groups counts them all.", len(remaining), verify.RemainingTotal)
		}
		page := mcpAnswerWindowOf(listing, listing.List, remaining)
		output.Differences, output.NextCursor, said = page.Items, page.NextCursor, page.said()
	case notionMigrationStatusListReportedValues:
		page := mcpAnswerWindowOf(listing, listing.List, notionMigrationReportedValuesListed(migration.RunReport.Reported))
		output.ReportedValues, output.NextCursor, said = page.Items, page.NextCursor, page.said()
	}
	output.Next = said + note + " The rest of this answer is as it is without list. " + output.Next
}

// notionMigrationReportedValuesListed are the reported properties' values,
// property by property, as the run kept them.
func notionMigrationReportedValuesListed(reported []notionMigrationReportedProperty) []leafenMCPNotionReportedValue {
	values := []leafenMCPNotionReportedValue{}
	for _, property := range reported {
		for _, value := range property.Values {
			values = append(values, leafenMCPNotionReportedValue{
				DatabaseID: property.DatabaseID, PropertyID: property.PropertyID, Property: property.Name,
				RowID: value.RowID, RowTitle: value.RowTitle, Value: value.Value,
			})
		}
	}
	return values
}
