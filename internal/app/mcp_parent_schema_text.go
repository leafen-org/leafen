package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A database page's fetch ends with the table of the data source it is in.
// Notion's gives that table whole (seen 2026-11-11: after </page>, a
// <parent-data-source-schema> holding the source's SQLite table, every
// option of a select in its column's note, as its data source's fetch
// does). Leafen gives it short, a difference on purpose (user 2026-11-26,
// an agent's real use: to find one row's files column it took every option
// of every select): the same columns, so the page's properties are set,
// pages beside it made and the source queried by them as before, but each
// option list cut to its first mcpParentSchemaOptionsShown names with how
// many more, and the notes on how SQL reads a column and the clock said
// once, short. The head names the data source's own fetch for the rest, as
// notion-update-data-source and querying need it, and says no more than a
// write to this page needs: the clock only where a date can be set, and
// how a text value reads only where one reads escaped
// (mcpFetchTextValueNote), as each row an agent reads said them all again
// (user 2026-11-26, an agent's real use: its tokens ran out reading).

// mcpParentSchemaOptionsShown is how many names of an option list a
// database page's short table shows.
const mcpParentSchemaOptionsShown = 10

// mcpParentSchemaText is what follows a database page's fetch: its data
// source's table, short, and which of its columns aren't set.
func mcpParentSchemaText(app core.App, authority mcpAuthority, dataSourceID string) (string, error) {
	// A database page of a data source in the trash gives its table too.
	location, err := loadMCPDataSourceLocationToRead(app, dataSourceID)
	if err != nil {
		return "", err
	}
	properties, _, err := loadMCPDataSourceSchema(app, location, authority.PrincipalID)
	if err != nil {
		return "", err
	}
	ids, err := loadNotionPropertyIDs(app, location.ID)
	if err != nil {
		return "", err
	}
	visible := mcpSQLiteTableOrder(mcpSQLiteVisibleProperties(properties), ids)
	// The address the rest of the answer names it by: its public API ID,
	// as <ancestor-path>'s parent-data-source (mcp_data_source_public_ids.go).
	source := mcpDataSourceURL(app, location.ID)
	head := mcpFetchParentTableHead(source, mcpParentSchemaOptionsShown)
	if slices.ContainsFunc(visible, func(property notionMCPDataSourceProperty) bool { return property.Type == graphValueTypeDate }) {
		head += " " + mcpTimesOnClockShort(authority.clock())
	}
	lines := []string{"", "", fmt.Sprintf(`<parent-data-source-schema url="%s">`, source), head}
	if readOnly := mcpSQLiteReadOnlyColumns(visible); len(readOnly) > 0 {
		lines = append(lines, "These columns are read-only, so don't set them: "+strings.Join(readOnly, ", ")+".")
	}
	return strings.Join(append(lines,
		"<sqlite-table>", mcpSQLiteTableTextOf(app, location.ID, visible, mcpParentSQLiteColumns, ""), "</sqlite-table>",
		"</parent-data-source-schema>",
	), "\n"), nil
}

// mcpParentSQLiteColumns are a property's columns as a database page's
// short table gives them: the data source's own (mcpSQLiteColumns), an
// option list cut short, a date's notes short, a relation's without how to
// query it, and none on the times Leafen sets, which the table names
// read-only.
func mcpParentSQLiteColumns(property notionMCPDataSourceProperty) []mcpSQLiteColumn {
	columns := mcpSQLiteColumns(property)
	names := func() string { return mcpSQLiteListShort(mcpOptionNames(mcpPropertyOptions(property.Config))) }
	switch property.Type {
	case graphValueTypeSelect:
		columns[0].Comment = "one of " + names()
	case graphValueTypeStatus:
		columns[0].Comment = mcpSQLiteStatusNote(property, names())
	case graphValueTypeMultiSelect:
		columns[0].Comment = mcpSQLiteNamesNote(names())
	case graphValueTypeCreatedTime, graphValueTypeLastEditedTime:
		columns[0].Comment = ""
	case graphValueTypeRelation:
		target, _ := property.Config["relationDatabaseId"].(string)
		columns[0].Comment = "JSON array of URLs of pages in " + mcpDataSourceURLPrefix + target
	case graphValueTypeDate:
		for index := range columns {
			columns[index].Comment = map[string]string{
				"start": mcpSQLiteDateStartShort, "end": mcpSQLiteDateEndShort, "is_datetime": mcpSQLiteDateTimeShort,
			}[columns[index].Part]
		}
	}
	return columns
}

// mcpSQLiteListShort is an option list's names as a JSON list, its first
// mcpParentSchemaOptionsShown and how many more.
func mcpSQLiteListShort(names []string) string {
	if len(names) <= mcpParentSchemaOptionsShown {
		return mcpSQLiteList(names)
	}
	return fmt.Sprintf("%s and %d more", mcpSQLiteList(names[:mcpParentSchemaOptionsShown]), len(names)-mcpParentSchemaOptionsShown)
}
