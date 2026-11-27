package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database goes by one name wherever it is named apart from its own block
// — a mention of it, a link to it, a mention's card, the API's and MCP's
// words for a mention — as Notion's does (seen 2026-11-18, mentions and
// links on the screen and through the API): a full-page database by its
// page's title (full_page_database_name.go), an inline one of two or more
// sources of its own by the name it keeps (inline_database_own_name.go),
// either one without a name by its sources' names listed ("새 데이터 소스 및
// 가", the API's "New data source and 가"); any other — one of one source, a
// linked view — by its first source's name. A source sent to the trash with
// its views doesn't count, as it doesn't for its database's own name: an
// untitled full-page one left with one so goes by that one's name. MCP's
// fetch heads an inline one of several by that name too, its rows' ancestor
// line and path, its tag on its page and the trash tools' answers
// (databaseOwnName, mcpDatabaseOfSeveralTitle), in the trash or out of it,
// as the API's title (seen 2026-11-09: Notion's fetch names a database of
// two sources by its own title); one deleted from its page by the name it
// went by there (deleted_database_name.go); the API's title of it and of
// its child_database block (notion_database_api_name.go). databaseNameFrom
// alone decides, and databaseNamingSources which of its sources count: on
// the screen a source it shows from another database counts as one of its
// own does, while its API names it by its own alone (seen 2026-11-25 in
// Chrome: an inline database of one source of its own and one it links
// opened alone titled "C 및 X 보기", its window and path the same, a mention
// of it the same and, named, by its name, while the API's database title,
// its child_database block and a mention's words read "C", its data_sources
// listing C alone; one of two of its own and one linked "A, B, X 보기" on
// the screen, "A and B" through the API).

// databaseContainerName is a database's name as written: Title with the
// spans that spell it (none for a page's title, which its page reads), and,
// for one without a name of two or more sources, Sources, their names.
// Sourced says it holds a source at all; Several that it holds two or more
// sources counted, and so is named apart from them: an inline one by the
// name it keeps (Title), a full-page one by its page's title, either
// without one by Sources.
type databaseContainerName struct {
	PageID   string
	Kind     string
	FullPage bool
	Sourced  bool
	Several  bool
	Title    string
	Spans    []graphTextSpan
	Sources  []databaseNameRead
}

// databaseNameSource is one of a database's sources as it names it, Gone
// for one of its own sent to the trash with its views, which doesn't count,
// and Linked (databaseNameRead) for one it shows from another database.
type databaseNameSource struct {
	databaseNameRead
	Gone bool
}

// databaseNaming says where a database is named: on the screen — its
// block, its page, its path, the sidebar, a mention's card, search and MCP —
// or by its API, which names it by its own sources alone.
type databaseNaming int

const (
	databaseNamingShown databaseNaming = iota
	databaseNamingAPI
)

// databaseNameFrom decides a database's name where naming names it from
// what names it, in the order of its sources: whether it is full page, its
// page's title, its kind and its block's content.
func databaseNameFrom(
	fullPage bool, pageTitle, kind string, content []byte, sources []databaseNameSource, naming databaseNaming,
) databaseContainerName {
	if naming == databaseNamingAPI {
		sources = slices.DeleteFunc(slices.Clone(sources), func(source databaseNameSource) bool { return source.Linked })
	}
	own := []databaseNameRead{}
	for _, source := range databaseNamingSources(sources,
		func(source databaseNameSource) bool { return source.Gone }, func(source databaseNameSource) bool { return source.Linked }) {
		own = append(own, source.databaseNameRead)
	}
	several := kind == databaseContainerKindSource && len(own) > 1
	name := databaseContainerName{Kind: kind, FullPage: fullPage, Sourced: len(own) > 0, Several: several}
	// Its page's title names a full-page one, but its API's of one source of
	// its own by that one's, which its page's title keeps apart from while it
	// shows one from another database beside it (full_page_database_name.go:
	// seen 2026-11-25, the page "F" and its API title and child_database "F2").
	ownOne := naming == databaseNamingAPI && kind == databaseContainerKindSource && len(own) == 1
	switch {
	case fullPage && !ownOne:
		name.Title = pageTitle
	case several:
		kept := inlineDatabaseKeptName(content)
		name.Title, name.Spans = kept.Title, kept.Spans
	case len(own) > 0:
		name.Title, name.Spans = own[0].Title, own[0].Spans
	}
	// An untitled full-page one of several left with one source by the
	// trash goes by that one's name (full_page_database_sources_name.go).
	if name.Title == "" && (several || fullPage && kind == databaseContainerKindSource && len(sources) > 1 && len(own) == 1) {
		name.Sources = own
	}
	return name
}

// databaseNamingSources keeps, of a database's sources in its order, those
// that name it and count as its sources wherever it is named — its API
// title and child_database block, MCP, its path, the sidebar, search: its
// own but those sent to the trash with their views (gone), or all its own
// when every one was, and those it shows from other databases (linked),
// which its API leaves out before (databaseNameFrom), as Notion's lists
// them in its data_sources. Notion's leaves a source so sent out of its
// database's name and of its path (seen 2026-11-11 and 2026-11-19,
// full_page_database_name.go); the screen's app counts them alike
// (nativeDataSourceManagement.ts databaseNamingSourceIds). Its order is its
// own first and those it links after, whatever order they came in, as its
// block writes their positions (projectDatabaseContainerBlock) and Notion's
// lists them (seen 2026-11-25 in Chrome, two tabs: "A", "X" linked, then "B"
// of its own — "A, B, X 보기" while its tabs read A, X, B).
func databaseNamingSources[T any](sources []T, gone, linked func(T) bool) []T {
	ownKept := false
	for _, source := range sources {
		ownKept = ownKept || !linked(source) && !gone(source)
	}
	kept := make([]T, 0, len(sources))
	for _, source := range sources {
		if linked(source) || !ownKept || !gone(source) {
			kept = append(kept, source)
		}
	}
	return kept
}

// databaseContainerNames reads the names of the databases listed by their
// containers' IDs, as the screen names them.
func databaseContainerNames(app core.App, containerIDs []string) (map[string]databaseContainerName, error) {
	return databaseContainerNamesBy(app, containerIDs, databaseNamingShown)
}

// databaseContainerAPINames reads them as their API names them, by their own
// sources alone.
func databaseContainerAPINames(app core.App, containerIDs []string) (map[string]databaseContainerName, error) {
	return databaseContainerNamesBy(app, containerIDs, databaseNamingAPI)
}

// databaseContainerNamesBy reads them where naming names them.
func databaseContainerNamesBy(app core.App, containerIDs []string, naming databaseNaming) (map[string]databaseContainerName, error) {
	names := map[string]databaseContainerName{}
	encoded, err := json.Marshal(containerIDs)
	if len(containerIDs) == 0 || err != nil {
		return names, err
	}
	containers := []struct {
		ID        string `db:"id"`
		PageID    string `db:"page_id"`
		Kind      string `db:"kind"`
		PageTitle string `db:"page_title"`
		FullPage  bool   `db:"full_page"`
		Content   string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[page_id]], [[container]].[[kind]],
			COALESCE([[page]].[[title]], '') AS [[page_title]],
			[[container]].[[id]] = ` + fullPageDatabaseContainerSQL + ` AS [[full_page]],
			COALESCE(` + databaseBlockContentSQL("[[container]]", "[[block]]") + `, '') AS [[content]]
		FROM {{leafen_database_containers}} [[container]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[container]].[[page_id]]
		LEFT JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[container]].[[block_id]] AND [[block]].[[page_id]] = [[container]].[[page_id]]
		WHERE [[container]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&containers); err != nil {
		return nil, err
	}
	sources := []struct {
		ContainerID string `db:"container_id"`
		Title       string `db:"title"`
		Spans       string `db:"title_spans"`
		Gone        bool   `db:"gone"`
		Linked      bool   `db:"linked"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[place]].[[container_id]], [[source]].[[title]], COALESCE([[source]].[[title_spans]], '') AS [[title_spans]],
			[[place]].[[source_container_id]] != '' AS [[linked]],
			[[place]].[[data_source_id]] IN (
				SELECT [[trashed]].[[value]] FROM json_each(COALESCE(NULLIF(` + databaseBlockContentSQL("[[container]]", "[[block]]") + `, ''), '{}'),
					'$.` + databaseTrashedWithViewsField + `') AS [[trashed]]) AS [[gone]]
		FROM {{leafen_database_container_sources}} [[place]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[place]].[[data_source_id]]
		JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[place]].[[container_id]]
		LEFT JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[container]].[[block_id]] AND [[block]].[[page_id]] = [[container]].[[page_id]]
		WHERE [[place]].[[container_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[place]].[[container_id]], [[place]].[[position]], [[place]].[[data_source_id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&sources); err != nil {
		return nil, err
	}
	named := map[string][]databaseNameSource{}
	for _, source := range sources {
		named[source.ContainerID] = append(named[source.ContainerID], databaseNameSource{
			databaseNameRead: databaseNameRead{Title: source.Title, Spans: decodeStoredTextSpans(source.Spans), Linked: source.Linked},
			Gone:             source.Gone,
		})
	}
	for _, container := range containers {
		name := databaseNameFrom(container.FullPage, container.PageTitle, container.Kind, []byte(container.Content), named[container.ID], naming)
		name.PageID = container.PageID
		names[container.ID] = name
	}
	return names, nil
}

// databaseContainerNamesAsRead is those names as a reader's closer reads
// them, a mention they can't open in marks: each database's title, and the
// titles of the sources naming one without a name, a linked one marked
// (database_source_titles.go).
func databaseContainerNamesAsRead(
	app core.App, closer func() searchMentionCloser, containerIDs []string,
) (map[string]string, map[string][]databaseSourceTitle, error) {
	names, err := databaseContainerNames(app, containerIDs)
	if err != nil {
		return nil, nil, err
	}
	titles, sources := make(map[string]string, len(names)), map[string][]databaseSourceTitle{}
	pageTitles := map[string]*string{}
	for id, name := range names {
		titles[id] = databaseNameAsRead(closer, name.Title, name.Spans)
		if name.FullPage {
			title := name.Title
			pageTitles[name.PageID] = &title
		}
		for _, source := range name.Sources {
			sources[id] = append(sources[id], databaseSourceTitle{Title: databaseNameAsRead(closer, source.Title, source.Spans), Linked: source.Linked})
		}
	}
	// A full-page database's title as its page's is read (page_name_mentions.go).
	pageTitlesAsReadBy(app, closer, pageTitles, true)
	for id, name := range names {
		if name.FullPage {
			titles[id] = *pageTitles[name.PageID]
		}
	}
	return titles, sources, nil
}

// databaseContainerTitle is a database's name as Notion's API writes a
// mention of it (databaseContainerAPINames), by its own sources alone: one
// without a name of several by their names listed in English, "New data
// source" for an unnamed one; a database whose block left its page, which
// the app's trash keeps, by the name it went by there (deletedDatabaseAPINames).
func databaseContainerTitle(app core.App, containerID string) (string, bool, error) {
	names, err := databaseContainerAPINames(app, []string{containerID})
	if err != nil {
		return "", false, err
	}
	name, found := names[containerID]
	if !found || name.Kind != databaseContainerKindSource || !name.Sourced {
		if names, err = deletedDatabaseAPINames(app, []string{containerID}); err != nil {
			return "", false, err
		}
		if name, found = names[containerID]; !found {
			return "", false, nil
		}
	}
	if len(name.Sources) == 0 {
		return strings.TrimSpace(name.Title), true, nil
	}
	listed := make([]string, len(name.Sources))
	for index, source := range name.Sources {
		listed[index] = notionSourceShownName(source.Title, len(name.Sources))
	}
	return notionListedSourceNames(listed), true, nil
}
