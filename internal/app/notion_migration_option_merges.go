package app

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Options Notion keeps apart — '진행중' and '진행 중', 'Doing' and 'In
// progress' — are different values: filters, boards and searches in Notion
// find each on its own rows. A plan's option values may give two of them one
// name, and the import then makes them one option (notion_migration_values.go);
// 2026-11-19 a real move did that on the agent's own recommendation ("they
// mean the same thing"), and the next agent found Notion's five '진행 중' rows
// gone. So the scan counts the rows holding each option, the one
// confirmation names each option the values make one with its rows before
// the person approves (confirm.merged_options), and the report says it with
// the counts.

// notionMigrationOptionMerge is one name a plan's option values give to two
// or more options of one Notion property.
type notionMigrationOptionMerge struct {
	DatabaseID, Database, Property string
	// The name they share, as the first of them in Notion's order takes it.
	Into string
	// The Notion options that become it, in Notion's order, with the rows of
	// each the scan read; RowsCounted is false for a scan made before it
	// counted them.
	Options     []notionMigrationMergedOption
	RowsCounted bool
	RowsCapped  bool
}

type notionMigrationMergedOption struct {
	Name string `json:"name"`
	Rows int    `json:"rows"`
}

// notionMigrationCountScannedRow counts a row the scan read into its
// database: the rows, each two relations it fills both of
// (notion_migration_relation_overlap.go) and the options it holds.
func notionMigrationCountScannedRow(entry *notionMigrationDatabase, source string, row notionProviderObject) {
	notionMigrationCountRow(entry, row)
	notionMigrationCountOptions(entry, source, row)
}

// notionMigrationCountOptions counts a row the scan read into the options it
// holds, of the database's properties or, for a row of another of its data
// sources, of that one's.
func notionMigrationCountOptions(entry *notionMigrationDatabase, source string, row notionProviderObject) {
	properties := entry.Properties
	for index := range entry.DataSources {
		if source != "" && notionMigrationSourceKey(entry.DataSources[index].ID) == notionMigrationSourceKey(source) {
			properties = entry.DataSources[index].Properties
		}
	}
	for _, value := range row.Properties {
		if value.Type != "select" && value.Type != "status" && value.Type != "multi_select" {
			continue
		}
		names := []struct {
			Name string `json:"name"`
		}{}
		if value.Type == "multi_select" {
			_ = json.Unmarshal(value.Settings, &names)
		} else if one := (struct {
			Name string `json:"name"`
		}{}); json.Unmarshal(value.Settings, &one) == nil && one.Name != "" {
			names = append(names, one)
		}
		for index := range properties {
			property := &properties[index]
			if notionMigrationPropertyKey(property.ID) != notionMigrationPropertyKey(value.ID) {
				continue
			}
			if len(property.OptionRows) != len(property.Options) {
				property.OptionRows = make([]int, len(property.Options))
			}
			counted := map[int]bool{}
			for _, held := range names {
				for at, name := range property.Options {
					if name == held.Name && !counted[at] {
						counted[at] = true
						property.OptionRows[at]++
					}
				}
			}
		}
	}
}

// notionMigrationOptionMergesOf lists, for each database a plan brings, each
// name its option values give to two or more options of one property — as
// the import makes them one (notionMigrationRunPlan.optionMerge): their names
// as the values leave them, the same but for case and the spaces about them.
func notionMigrationOptionMergesOf(plan notionMigrationPlan, summary notionMigrationSummary) []notionMigrationOptionMerge {
	scanned := map[string]notionMigrationDatabase{}
	for _, database := range summary.Databases {
		scanned[notionMigrationSourceKey(database.ID)] = database
	}
	merges := []notionMigrationOptionMerge{}
	for _, planned := range plan.Databases {
		database, found := scanned[notionMigrationSourceKey(planned.SourceID)]
		if !found || planned.Action == "skip" {
			continue
		}
		lists := [][]notionMigrationProperty{database.Properties}
		entries := [][]notionMigrationPlanProperty{planned.Properties}
		for _, source := range planned.DataSources {
			for _, other := range database.DataSources {
				if notionMigrationSourceKey(other.ID) == notionMigrationSourceKey(source.SourceID) {
					lists, entries = append(lists, other.Properties), append(entries, source.Properties)
				}
			}
		}
		for index, list := range lists {
			for _, entry := range entries[index] {
				if entry.Action != "map" || strings.TrimSpace(entry.Into) != "" || len(entry.Values) == 0 {
					continue
				}
				for _, property := range list {
					if notionMigrationPropertyKey(property.ID) == notionMigrationPropertyKey(entry.SourceID) && notionMigrationOptionTypes[property.LeafenType] {
						merges = append(merges, notionMigrationPropertyOptionMerges(database, property, entry.Values)...)
					}
				}
			}
		}
	}
	return merges
}

func notionMigrationPropertyOptionMerges(
	database notionMigrationDatabase, property notionMigrationProperty, values []notionMigrationPlanValue,
) []notionMigrationOptionMerge {
	renamed := map[string]string{}
	for _, value := range values {
		if to := strings.TrimSpace(value.To); to != "" {
			renamed[value.From] = to
		}
	}
	counted := len(property.OptionRows) == len(property.Options)
	groups, order := map[string]*notionMigrationOptionMerge{}, []string{}
	for index, name := range property.Options {
		final := name
		if to, ok := renamed[name]; ok {
			final = to
		}
		key := strings.ToLower(strings.TrimSpace(final))
		group := groups[key]
		if group == nil {
			group = &notionMigrationOptionMerge{
				DatabaseID: database.ID, Database: database.Title, Property: property.Name, Into: strings.TrimSpace(final),
				RowsCounted: counted, RowsCapped: database.RowsCapped,
			}
			groups[key], order = group, append(order, key)
		}
		option := notionMigrationMergedOption{Name: name}
		if counted {
			option.Rows = property.OptionRows[index]
		}
		group.Options = append(group.Options, option)
	}
	merges := []notionMigrationOptionMerge{}
	for _, key := range order {
		if len(groups[key].Options) > 1 {
			merges = append(merges, *groups[key])
		}
	}
	return merges
}

// changed are the options of a merge that come under another name than
// their own, with the rows that change.
func (merge notionMigrationOptionMerge) changed() ([]notionMigrationMergedOption, int) {
	options, rows := []notionMigrationMergedOption{}, 0
	for _, option := range merge.Options {
		if option.Name != merge.Into {
			options = append(options, option)
			rows += option.Rows
		}
	}
	return options, rows
}

// leafenMCPNotionMigrationConfirmMergedOptions is one name the plan's option
// values give to options Notion keeps apart, as the one confirmation says it.
type leafenMCPNotionMigrationConfirmMergedOptions struct {
	Database string `json:"database"`
	Property string `json:"property"`
	// The name they become, and each Notion option that becomes it with the
	// rows that hold it, of the rows the scan read (rows_capped when it
	// stopped counting at its cap; rows_counted false when the scan was made
	// before it counted them — scan again for the counts).
	Into           string                        `json:"into"`
	Options        []notionMigrationMergedOption `json:"options"`
	RowsChanged    int                           `json:"rows_changed"`
	RowsCounted    bool                          `json:"rows_counted"`
	RowsCapped     bool                          `json:"rows_capped,omitzero"`
	Recommendation string                        `json:"recommendation"`
}

const notionMigrationConfirmMergedOptions = "In Notion these are separate options: once one, %d rows that hold %s hold '%s' instead, " +
	"and Notion's filters, boards and searches by those names no longer find them apart. Say it in the one question with that count. " +
	"Keep it only if the person asked to make them one; else recommend leaving them apart (take these values out of the plan) — " +
	"the report's suggestions for tidying name options written two ways after the run."

// notionMigrationConfirmMergedOptionsOf is each name the plan's option values
// give to two or more options, for the one confirmation.
func notionMigrationConfirmMergedOptionsOf(plan notionMigrationPlan, summary notionMigrationSummary) []leafenMCPNotionMigrationConfirmMergedOptions {
	listed := []leafenMCPNotionMigrationConfirmMergedOptions{}
	for _, merge := range notionMigrationOptionMergesOf(plan, summary) {
		changed, rows := merge.changed()
		names := make([]string, 0, len(changed))
		for _, option := range changed {
			names = append(names, "'"+option.Name+"'")
		}
		listed = append(listed, leafenMCPNotionMigrationConfirmMergedOptions{
			Database: merge.Database, Property: merge.Property, Into: merge.Into, Options: merge.Options,
			RowsChanged: rows, RowsCounted: merge.RowsCounted, RowsCapped: merge.RowsCapped,
			Recommendation: fmt.Sprintf(notionMigrationConfirmMergedOptions, rows, strings.Join(names, ", "), merge.Into),
		})
	}
	return listed
}

var notionMigrationOptionMergeText = map[string]map[string]string{
	accountLanguageKo: {
		"line":   "%s · %s: %s → '%s' — Notion에서 따로 있던 옵션을 하나로 합침",
		"option": "'%s'(%d행)",
		"capped": "'%s'(%d행 이상)",
		"bare":   "'%s'",
		"comma":  ", ",
	},
	accountLanguageEn: {
		"line":   "%s · %s: %s → ‘%s’ — options separate in Notion made one",
		"option": "‘%s’ (%d rows)",
		"capped": "‘%s’ (%d or more rows)",
		"bare":   "‘%s’",
		"comma":  ", ",
	},
}

// notionMigrationOptionMergeLines are the report's lines for each name the
// plan's option values gave to options Notion keeps apart: the options that
// came under another name, with their rows, and the name they took.
func notionMigrationOptionMergeLines(language string, plan notionMigrationPlan, summary notionMigrationSummary, name func(string) string) []string {
	text := notionMigrationOptionMergeText[language]
	if text == nil {
		text = notionMigrationOptionMergeText[accountLanguageEn]
	}
	lines := []string{}
	for _, merge := range notionMigrationOptionMergesOf(plan, summary) {
		changed, _ := merge.changed()
		parts := make([]string, 0, len(changed))
		for _, option := range changed {
			switch {
			case !merge.RowsCounted:
				parts = append(parts, fmt.Sprintf(text["bare"], option.Name))
			case merge.RowsCapped:
				parts = append(parts, fmt.Sprintf(text["capped"], option.Name, option.Rows))
			default:
				parts = append(parts, fmt.Sprintf(text["option"], option.Name, option.Rows))
			}
		}
		lines = append(lines, fmt.Sprintf(text["line"], name(merge.Database), merge.Property, strings.Join(parts, text["comma"]), merge.Into))
	}
	return lines
}
