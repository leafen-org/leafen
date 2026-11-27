package app

import (
	"encoding/json"
	"slices"
	"strings"
)

// Relations a plan may gather into one (notion_migration_roles.go) are often
// filled side by side, and a row that fills both with different pages
// relates to both once gathered — a deal with two clients, counted twice by
// a sum or rollup over the column (2026-11-16: a synthetic move gathered a
// client and a former client relation, and five of twenty deals came with two
// clients, found only when the totals were read after the move). So the scan
// counts, for each two relations of a database, the rows that fill both and
// how many of those relate to different pages through them, and the one
// confirmation says it for each relation the plan gathers.

// notionMigrationCountRow counts a row the scan read into its database: the
// rows, and each two relations the row fills both of.
func notionMigrationCountRow(entry *notionMigrationDatabase, row notionProviderObject) {
	entry.Rows++
	type filled struct {
		id    string
		pages []string
		cut   bool
	}
	relations := []filled{}
	for _, property := range row.Properties {
		if property.Type != "relation" {
			continue
		}
		values := []struct {
			ID string `json:"id"`
		}{}
		_ = json.Unmarshal(property.Settings, &values)
		if len(values) == 0 {
			continue
		}
		pages := make([]string, 0, len(values))
		for _, value := range values {
			pages = append(pages, notionMigrationSourceKey(value.ID))
		}
		slices.Sort(pages)
		relations = append(relations, filled{id: property.ID, pages: slices.Compact(pages), cut: property.HasMore})
	}
	slices.SortFunc(relations, func(a, b filled) int { return strings.Compare(a.id, b.id) })
	for first := 0; first < len(relations); first++ {
		for second := first + 1; second < len(relations); second++ {
			a, b := relations[first], relations[second]
			overlap := notionMigrationOverlapOf(entry, a.id, b.id)
			overlap.Rows++
			// A list Notion cut short can't be told the same.
			if a.cut || b.cut || !slices.Equal(a.pages, b.pages) {
				overlap.Differ++
			}
		}
	}
}

// notionMigrationOverlapOf is a database's count for two relations, made
// when there is none yet.
func notionMigrationOverlapOf(entry *notionMigrationDatabase, property, other string) *notionMigrationRelationOverlap {
	for index := range entry.RelationOverlaps {
		if entry.RelationOverlaps[index].Property == property && entry.RelationOverlaps[index].Other == other {
			return &entry.RelationOverlaps[index]
		}
	}
	entry.RelationOverlaps = append(entry.RelationOverlaps, notionMigrationRelationOverlap{Property: property, Other: other})
	return &entry.RelationOverlaps[len(entry.RelationOverlaps)-1]
}

// notionMigrationOverlapBetween is the scan's count for two relations of a
// database, in either order.
func notionMigrationOverlapBetween(database notionMigrationDatabase, property, other string) notionMigrationRelationOverlap {
	left, right := notionMigrationPropertyKey(property), notionMigrationPropertyKey(other)
	for _, overlap := range database.RelationOverlaps {
		a, b := notionMigrationPropertyKey(overlap.Property), notionMigrationPropertyKey(overlap.Other)
		if (a == left && b == right) || (a == right && b == left) {
			return overlap
		}
	}
	return notionMigrationRelationOverlap{Property: property, Other: other}
}
