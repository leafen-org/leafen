package app

import (
	"fmt"
	"strings"
)

// Each relation a plan gathers into another (notion_migration_roles.go) is
// part of the one confirmation with what the scan counted of it
// (notion_migration_relation_overlap.go): the rows that fill both, and of
// those the rows that relate to different pages through the two — each of
// those relates to both pages once gathered. The agent says it with the
// count before the person approves, instead of the person finding a deal
// with two clients when they add up the moved rows.

type leafenMCPNotionMigrationConfirmGathered struct {
	// The database and the two relations, as Notion shows them.
	Database string `json:"database"`
	Relation string `json:"relation"`
	Into     string `json:"into"`
	// Rows that fill both, and of those the rows that relate to different
	// pages through the two; of the rows the scan read when it stopped
	// counting at its cap (rows_capped).
	RowsBoth       int    `json:"rows_both"`
	RowsDiffer     int    `json:"rows_differ"`
	RowsCapped     bool   `json:"rows_capped,omitzero"`
	Recommendation string `json:"recommendation"`
}

const (
	notionMigrationConfirmGatherDiffer = "%d rows fill both with different pages: gathered, each of them relates to both, " +
		"so a rollup or a total by the related pages counts those rows twice. Say that count in the one question and ask whether the two are the same thing — " +
		"gather them anyway, or keep both columns — and recommend keeping both columns unless the person says the rows should relate to both."
	notionMigrationConfirmGatherSame = "%d rows fill both, each with the same pages: gathered, they keep one value. Gathering loses nothing; recommend it."
	notionMigrationConfirmGatherNone = "No row fills both: gathered, each row keeps the one value it has. Gathering loses nothing; recommend it."
)

// notionMigrationConfirmGatheredOf lists each relation a plan gathers, with
// the scan's count of the rows that fill both.
func notionMigrationConfirmGatheredOf(plan notionMigrationPlan, summary notionMigrationSummary) []leafenMCPNotionMigrationConfirmGathered {
	scanned := map[string]notionMigrationDatabase{}
	for _, database := range summary.Databases {
		scanned[notionMigrationSourceKey(database.ID)] = database
	}
	gathered := []leafenMCPNotionMigrationConfirmGathered{}
	for _, planned := range plan.Databases {
		database, found := scanned[notionMigrationSourceKey(planned.SourceID)]
		if !found || planned.Action == "skip" {
			continue
		}
		entries := append([]notionMigrationPlanProperty{}, planned.Properties...)
		for _, source := range planned.DataSources {
			entries = append(entries, source.Properties...)
		}
		for _, entry := range entries {
			into := strings.TrimSpace(entry.Into)
			if into == "" {
				continue
			}
			overlap := notionMigrationOverlapBetween(database, entry.SourceID, into)
			item := leafenMCPNotionMigrationConfirmGathered{
				Database: database.Title, Relation: notionMigrationScannedPropertyName(database, entry.SourceID),
				Into: notionMigrationScannedPropertyName(database, into), RowsBoth: overlap.Rows, RowsDiffer: overlap.Differ,
				RowsCapped: database.RowsCapped,
			}
			switch {
			case overlap.Differ > 0:
				item.Recommendation = fmt.Sprintf(notionMigrationConfirmGatherDiffer, overlap.Differ)
			case overlap.Rows > 0:
				item.Recommendation = fmt.Sprintf(notionMigrationConfirmGatherSame, overlap.Rows)
			default:
				item.Recommendation = notionMigrationConfirmGatherNone
			}
			gathered = append(gathered, item)
		}
	}
	return gathered
}

// notionMigrationScannedPropertyName is a property's name as the scan read
// it, of the database or one of its other data sources.
func notionMigrationScannedPropertyName(database notionMigrationDatabase, id string) string {
	key := notionMigrationPropertyKey(id)
	properties := append([]notionMigrationProperty{}, database.Properties...)
	for _, source := range database.DataSources {
		properties = append(properties, source.Properties...)
	}
	for _, property := range properties {
		if notionMigrationPropertyKey(property.ID) == key {
			return property.Name
		}
	}
	return id
}
