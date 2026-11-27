package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A plan's option values (the 2026-10-14 recommended structure decision,
// 6): the import makes a select's, a status's or a multi-select's options
// under the names the plan gives them, so a merged database's options join
// its target's by those names. Options given one name — two renamed alike,
// or one renamed to another kept as it is — become one: the first in
// Notion's order stays and the others aren't made; each row's value is the
// one that stays, a multi-select's without repeats.

// notionOptionMerge is, for one property, the option each option the
// import doesn't make becomes.
type notionOptionMerge map[string]string

// optionMerge reads which options of a staged property become another.
func (run *notionMigrationRunPlan) optionMerge(app core.App, jobID, propertySource string, plan notionDatabasePropertyPlan) notionOptionMerge {
	if run == nil || !notionMigrationOptionTypes[plan.Type] {
		return nil
	}
	databaseID, propertyID, found := strings.Cut(propertySource, ":property:")
	if !found {
		return nil
	}
	if len(run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)].Values) < 1 {
		return nil
	}
	names := run.finalOptionNames(propertySource, plan)
	options, _ := plan.Config["options"].([]any)
	stays, merge := map[string]string{}, notionOptionMerge{}
	for _, option := range options {
		fields, _ := option.(map[string]any)
		id, _ := fields["id"].(string)
		folded := strings.ToLower(strings.TrimSpace(names[id]))
		if first, taken := stays[folded]; taken {
			merge[id] = first
			continue
		}
		stays[folded] = id
	}
	if len(merge) == 0 {
		return nil
	}
	return merge
}

// finalOptionNames are a staged property's option names, by ID, as the
// plan's values leave them.
func (run *notionMigrationRunPlan) finalOptionNames(propertySource string, plan notionDatabasePropertyPlan) map[string]string {
	renamed := map[string]string{}
	if run != nil {
		if databaseID, propertyID, found := strings.Cut(propertySource, ":property:"); found {
			for _, value := range run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)].Values {
				if to := strings.TrimSpace(value.To); to != "" {
					renamed[value.From] = to
				}
			}
		}
	}
	names := map[string]string{}
	options, _ := plan.Config["options"].([]any)
	for _, option := range options {
		fields, _ := option.(map[string]any)
		id, _ := fields["id"].(string)
		name, _ := fields["name"].(string)
		if to, ok := renamed[name]; ok {
			name = to
		}
		names[id] = name
	}
	return names
}

// plannedOptions gives a staged property the import makes its options as
// the plan leaves them: under their names, without those that become
// another, a status's groups too.
func (run *notionMigrationRunPlan) plannedOptions(app core.App, jobID string, source storedNotionSource) (storedNotionSource, error) {
	if run == nil {
		return source, nil
	}
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return source, err
	}
	if !notionMigrationOptionTypes[plan.Type] || !run.hasOptionValues() {
		return source, nil
	}
	databaseID, propertyID, _ := strings.Cut(source.SourceID, ":property:")
	if len(run.columns[notionMigrationSourceKey(databaseID)][notionMigrationPropertyKey(propertyID)].Values) == 0 {
		return source, nil
	}
	merge := run.optionMerge(app, jobID, source.SourceID, plan)
	names := run.finalOptionNames(source.SourceID, plan)
	options, _ := plan.Config["options"].([]any)
	kept := make([]any, 0, len(options))
	for _, option := range options {
		fields, _ := option.(map[string]any)
		if id, _ := fields["id"].(string); merge[id] == "" {
			renamed := map[string]any{}
			for key, value := range fields {
				renamed[key] = value
			}
			renamed["name"] = names[id]
			kept = append(kept, renamed)
		}
	}
	plan.Config["options"] = kept
	if groups, ok := plan.Config["statusGroups"].([]any); ok {
		for _, group := range groups {
			fields, _ := group.(map[string]any)
			members, _ := fields["optionIds"].([]any)
			left := make([]any, 0, len(members))
			for _, member := range members {
				if id, _ := member.(string); merge[id] == "" {
					left = append(left, member)
				}
			}
			fields["optionIds"] = left
		}
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return source, err
	}
	source.Text = string(encoded)
	return source, nil
}

// mergedOptionValue gives a row's option value the options that stay.
func (run *notionMigrationRunPlan) mergedOptionValue(app core.App, jobID, propertySource string, value notionDatabaseRowValuePlan) (notionDatabaseRowValuePlan, error) {
	if run == nil || !notionMigrationOptionTypes[value.Type] || value.Value == nil {
		return value, nil
	}
	source, found, err := notionImportStagedSource(app, jobID, propertySource)
	if err != nil || !found {
		return value, err
	}
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return value, err
	}
	merge := run.optionMerge(app, jobID, propertySource, plan)
	if len(merge) == 0 {
		return value, nil
	}
	if id, single := value.Value.(string); single {
		if stays := merge[id]; stays != "" {
			value.Value = stays
		}
		return value, nil
	}
	ids, _ := value.Value.([]any)
	seen, kept := map[string]bool{}, make([]any, 0, len(ids))
	for _, item := range ids {
		id, _ := item.(string)
		if stays := merge[id]; stays != "" {
			id = stays
		}
		if !seen[id] {
			seen[id] = true
			kept = append(kept, id)
		}
	}
	value.Value = kept
	return value, nil
}

func (run *notionMigrationRunPlan) hasOptionValues() bool {
	if run == nil {
		return false
	}
	for _, columns := range run.columns {
		for _, entry := range columns {
			if len(entry.Values) > 0 {
				return true
			}
		}
	}
	return false
}

// notionStagedOptionNames are a staged property's option names in Notion,
// by ID.
func notionStagedOptionNames(app core.App, jobID, propertySource string) (map[string]string, error) {
	names := map[string]string{}
	source, found, err := notionImportStagedSource(app, jobID, propertySource)
	if err != nil || !found {
		return names, err
	}
	plan := notionDatabasePropertyPlan{}
	if err := json.Unmarshal([]byte(source.Text), &plan); err != nil {
		return names, err
	}
	options, _ := plan.Config["options"].([]any)
	for _, option := range options {
		fields, _ := option.(map[string]any)
		id, _ := fields["id"].(string)
		name, _ := fields["name"].(string)
		names[id] = name
	}
	return names, nil
}

// notionOptionTypeFits says whether an option type's values go into a
// column of an option type: any into a multi-select, a select's or a
// status's into the other.
func notionOptionTypeFits(from, to string) bool {
	if from == to {
		return true
	}
	if !notionMigrationOptionTypes[from] || !notionMigrationOptionTypes[to] {
		return false
	}
	return to == graphValueTypeMultiSelect || from != graphValueTypeMultiSelect
}

// notionOptionValueAs is an option value as a column of another option type
// holds it; false when it doesn't go there.
func notionOptionValueAs(value notionDatabaseRowValuePlan, columnType string) (notionDatabaseRowValuePlan, bool) {
	if !notionOptionTypeFits(value.Type, columnType) {
		return value, false
	}
	if id, single := value.Value.(string); single && columnType == graphValueTypeMultiSelect {
		value.Value = []any{id}
	}
	value.Type = columnType
	return value, true
}
