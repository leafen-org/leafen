package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A database of several data sources without a name of its own goes by its
// sources' names in fetch, as Notion's MCP names it (seen 2026-11-22 on
// worker w2295's inline database of two sources, the second unnamed: "The
// title of this Database is: w2295 E1 및 새 데이터 소스", the data sources
// "w2295 E1" and "새 데이터 소스", their states' "name" "w2295 E1" and "",
// and the <database> tag on the page it is in holding no title, as an
// unnamed database's doesn't), listed in its reader's language as Leafen's
// sidebar lists them (untitledDatabaseSourcesNameIn: two "A 및 B", more "A,
// B, C"). A data source among several without a name reads as a new data
// source; one alone as a new database (mcpDatabaseShownTitle), as Notion's
// ("The title of this Data Source is: 새 데이터베이스").

// mcpReaderLanguage is the language a connection's person reads Leafen in,
// as fetch and search name what has no name: the language the server writes
// them its texts in (serverTextLanguage) with no request of theirs, as an
// agent's requests are the agent's. So one person reads one language on an
// agent's path — the names fetch gives, the questions put to them (an
// action's, the trash's, a restore's), the screens a refusal names, the
// instructions' —, by the rule their mail and notices read
// (server_text_language.go). Fetch named what has no name in English for
// them while the questions were Korean (2026-11-27).
func mcpReaderLanguage(app core.App, principalID string) string {
	language, err := serverTextLanguage(app, principalID, nil)
	if err != nil {
		return accountLanguageEn
	}
	return language
}

// mcpDatabaseSourcesShownTitle is a database's name from its sources' names,
// as fetch heads a database without a name of its own: one it shows from
// another database among them by its linked view's name, "C 및 X 보기", as
// the screen names it (database_source_titles.go).
func mcpDatabaseSourcesShownTitle(app core.App, principalID string, names []databaseSourceTitle) string {
	if len(names) > 1 {
		return untitledDatabaseSourcesNameIn(mcpReaderLanguage(app, principalID), names)
	}
	first := ""
	if len(names) == 1 {
		first = names[0].Title
	}
	return mcpDatabaseShownTitle(app, principalID, first)
}

// mcpDataSourceShownTitle is a data source's name as fetch heads it.
func mcpDataSourceShownTitle(app core.App, principalID string, location mcpDataSourceLocation) string {
	if strings.TrimSpace(location.Title) != "" {
		return location.Title
	}
	return mcpUntitledDataSourceNameIn(app, mcpReaderLanguage(app, principalID), location)
}

// mcpDataSourceAmongSeveral reports whether a data source's own database
// shows other sources beside it.
func mcpDataSourceAmongSeveral(app core.App, location mcpDataSourceLocation) bool {
	containerID, err := loadDatabaseSQLSourceContainerID(app, location)
	if err != nil {
		return false
	}
	return databaseShowsSeveralSources(app, containerID)
}

// databaseShowsSeveralSources reports whether a database shows more than one
// data source.
func databaseShowsSeveralSources(app core.App, containerID string) bool {
	members, err := loadMCPDatabaseContainerShownMembers(app, containerID)
	return err == nil && len(members) > 1
}

// notionMarkdownInlineDatabaseTitle is the title an inline database's tag
// holds on the page it is in, with the spans that spell it: its first
// source's name, or for one of several sources the name it keeps of its own
// (databaseContainerNames), none while it has none; and whether it is one of
// several, which a title its tag gives names (notionMarkdownRename).
func notionMarkdownInlineDatabaseTitle(app core.App, database notionMarkdownDatabase) (string, []graphTextSpan, bool) {
	if database.Kind == databaseContainerKindSource && databaseShowsSeveralSources(app, database.ContainerID) {
		names, err := databaseContainerNames(app, []string{database.ContainerID})
		if name := names[database.ContainerID]; err == nil && name.Several {
			return name.Title, name.Spans, true
		}
		return "", nil, false
	}
	return database.Title, sourceNameSpansForPage(app, database.DataSourceID, database.Title), false
}

// mcpDatabaseNameSaid is a database's name as databaseNameFrom decides it,
// each name in it said as say says a name by the spans that spell it: one of
// two or more sources without a name of its own by their names listed as
// fetch lists them (mcpDatabaseSourcesShownTitle), a full-page one by its
// page's title as page says it, one of several inline by the name it keeps,
// any other by its first source's.
func mcpDatabaseNameSaid(
	app core.App, principalID string, name databaseContainerName,
	say func(spans []graphTextSpan, title string) string, page func(pageID, title string) string,
) string {
	said := func(read databaseNameRead) string {
		if spelledName(read.Spans) != read.Title {
			return read.Title
		}
		return say(read.Spans, read.Title)
	}
	switch {
	case len(name.Sources) > 1:
		listed := make([]databaseSourceTitle, len(name.Sources))
		for index, source := range name.Sources {
			listed[index] = databaseSourceTitle{Title: said(source), Linked: source.Linked}
		}
		return mcpDatabaseSourcesShownTitle(app, principalID, listed)
	case name.FullPage:
		return page(name.PageID, name.Title)
	}
	return said(databaseNameRead{Title: name.Title, Spans: name.Spans})
}

// mcpDatabaseListed reports whether a database goes by its sources' names
// listed, having no name of its own (mcpDatabaseNameSaid).
func mcpDatabaseListed(name databaseContainerName) bool {
	return len(name.Sources) > 1
}

// mcpDatabaseNameAsIndexed is a database's name as Notion's search keeps it
// (mcpTitleAsIndexed), where Notion's trash tools and a path name a
// database.
func mcpDatabaseNameAsIndexed(app core.App, principalID string, name databaseContainerName) string {
	return mcpDatabaseNameSaid(app, principalID, name, mcpSpansAsIndexed, func(pageID, title string) string {
		return mcpTitleAsIndexed(app, pageID, title)
	})
}

// mcpDatabaseNameAsSearched is a database's name as a search's highlight
// says it (mcpTitleAsSearched).
func mcpDatabaseNameAsSearched(app core.App, principalID string, name databaseContainerName) string {
	return mcpDatabaseNameSaid(app, principalID, name, mcpSpansAsSearched, func(pageID, title string) string {
		return mcpTitleAsSearched(app, pageID, title)
	})
}

// mcpDatabaseNameShown is a database's name as Notion's fetch names a title
// (mcpSpansAsShown), where a list or a search names a database.
func mcpDatabaseNameShown(app core.App, authority mcpAuthority, workspaceID string, name databaseContainerName) string {
	return mcpDatabaseNameSaid(app, authority.PrincipalID, name, func(spans []graphTextSpan, title string) string {
		return mcpSpansAsShown(app, authority, workspaceID, spans, title)
	}, func(pageID, title string) string {
		return mcpTitleAsShown(app, authority, workspaceID, pageID, title)
	})
}
