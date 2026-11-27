package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
)

// A Notion view's filter keeps its conditions on a property people deleted,
// and Notion's API gives the view's filter without them (seen 2026-11-16: a
// view filtered on three properties, two of them deleted since, came through
// the API with the third alone; Notion's own record of the view still named
// all three). A deleted property doesn't move, so the view comes to Leafen
// without them. The reading of the person's Notion reads the view's own
// filter (notion_migration_formats_reader.js), and the plan's agenda — so the
// report too — says each such condition by its database and property; what
// the reading couldn't read, or no reading, it says it can't tell.

const (
	// A view's condition on a property deleted in Notion.
	notionMigrationAgendaDeletedFilter = "view_filter_deleted_property"
	// A database or view the reading couldn't read, whose conditions on
	// deleted properties no one can tell.
	notionMigrationAgendaFiltersUnread = "view_filters_unread"
	// notionMigrationDeletedFiltersMost bounds what one view keeps.
	notionMigrationDeletedFiltersMost = 100
)

// notionMigrationDeletedFilter is one such condition: the deleted property's
// Notion id and name.
type notionMigrationDeletedFilter struct {
	Property string `json:"property"`
	Name     string `json:"name"`
}

// notionMigrationDeletedFiltersKept bounds what the reader sent of a view.
func notionMigrationDeletedFiltersKept(filters []notionMigrationDeletedFilter) []notionMigrationDeletedFilter {
	kept := []notionMigrationDeletedFilter{}
	for _, filter := range filters {
		if filter.Property == "" || len(kept) == notionMigrationDeletedFiltersMost {
			continue
		}
		filter.Property, filter.Name = notionMigrationShortened(filter.Property, 100), notionMigrationShortened(strings.TrimSpace(filter.Name), 200)
		kept = append(kept, filter)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func notionMigrationShortened(text string, most int) string {
	if runes := []rune(text); len(runes) > most {
		return string(runes[:most])
	}
	return text
}

// notionMigrationDeletedFilterItems are the agenda's items of a migration's
// reading: each condition on a deleted property, by its database, and each
// database or view the reading couldn't read.
func (c *notionMigrationPlanChecker) notionMigrationDeletedFilterItems(reader notionMigrationFormatsReader) []notionMigrationIssue {
	items := []notionMigrationIssue{}
	rows := []struct {
		NotionID string `db:"notion_id"`
		Value    string `db:"value"`
	}{}
	if err := c.app.DB().NewQuery(`SELECT [[notion_id]],[[value]] FROM {{leafen_notion_migration_formats}}
		WHERE [[migration_id]]={:id} AND [[kind]]='view' ORDER BY [[notion_id]]`).
		Bind(dbx.Params{"id": c.stored.ID}).All(&rows); err != nil {
		c.err = err
		return items
	}
	read := map[string]bool{}
	for _, row := range rows {
		read[canonicalNotionID(row.NotionID)] = true
		view := notionMigrationViewFormat{}
		if json.Unmarshal([]byte(row.Value), &view) != nil {
			continue
		}
		for _, filter := range view.DeletedFilters {
			items = append(items, notionMigrationIssue{
				Code: notionMigrationAgendaDeletedFilter, SourceID: view.Database, PropertyID: filter.Property, Detail: filter.Name,
			})
		}
	}
	for _, unread := range reader.Unread {
		switch unread.Kind {
		case "database":
			items = append(items, notionMigrationIssue{Code: notionMigrationAgendaFiltersUnread, SourceID: unread.ID, Detail: unread.Reason})
		case "view":
			// A view whose form alone wasn't read had its filter read.
			if !read[canonicalNotionID(unread.ID)] {
				items = append(items, notionMigrationIssue{Code: notionMigrationAgendaFiltersUnread, SourceID: unread.Database, Detail: unread.Reason})
			}
		}
	}
	return items
}

func init() {
	for code, words := range map[string][2]string{
		notionMigrationAgendaDeletedFilter: {
			"Notion에서 지운 속성을 가리키는 보기 조건 %d개 — Notion이 연결에 주지 않고 지운 속성은 옮기지 않아, 리펜 보기에는 그 조건 없이 옵니다",
			"%d view conditions name a property deleted in Notion — Notion doesn’t give them to a connection and deleted properties don’t move, so the Leafen views come without them",
		},
		notionMigrationAgendaFiltersUnread: {
			"Notion에서 읽지 못한 데이터베이스·보기 %d개 — 지운 속성을 가리키는 조건이 있는지 알 수 없습니다",
			"%d databases or views couldn’t be read in Notion — whether their conditions name deleted properties can’t be told",
		},
	} {
		notionMigrationReportText[accountLanguageKo]["issue:"+code] = words[0]
		notionMigrationReportText[accountLanguageEn]["issue:"+code] = words[1]
	}
}
