package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An untitled full-page database of two or more sources of its own goes by
// its sources' names listed, "새 데이터 소스 및 새 데이터 소스", as Notion's
// names it in its page's title, its sidebar row, its path and its window
// (seen 2026-11-18, a database made on the screen and one made through the
// API alike; the API's database title read "New data source and New data
// source"), as an untitled inline one of several goes by them
// (inline_database_own_name.go). The page list carries its sources' titles,
// "" for an untitled one, for the app to name it in its reader's language
// (untitledPageDisplayTitle); a page with a title, a database of one source,
// a linked one or a view block carries none. A source added, renamed or put
// away lists its page again (page_list_changes_schema.go), and one sent to
// the trash with its views no longer counts, as its database's own name
// doesn't count it: one of several left with one so goes by that one's name,
// as Notion's (seen 2026-11-19: "A 및 B" with B sent to the trash from its
// 데이터 소스 관리 is "A" in its title, path, window and API title) — its page
// titled so as the trash leaves it (nameDatabaseByLeftSource), its page list
// carrying that one title alone while it reads none. A list naming pages apart from the
// page list — the inbox, recent edits, read history, search, an administrator's or a
// person's pages — carries them too (pageNameKind.SourceTitles), as Notion's
// search lists it by them (seen 2026-11-18), and so do a public page's own
// title, its site's pages, links and mentions, as its visitor reads the
// titles (publicLinkedPagesAsRead), and a connection's "연결 출처"
// (page_notion_connections.go).

// nameUntitledDatabasesBySources gives the untitled full-page databases of
// several sources among the listed pages their sources' titles, in their
// database's order.
func nameUntitledDatabasesBySources(app core.App, pages []*pageResponse) error {
	byID := map[string]*pageResponse{}
	ids := []string{}
	for _, page := range pages {
		if page.Kind == pageKindDatabase && page.Title == "" && !page.ViewBlock {
			byID[page.ID] = page
			ids = append(ids, page.ID)
		}
	}
	titles, err := untitledFullPageDatabaseSourceTitles(app, ids)
	for id, named := range titles {
		byID[id].SourceTitles = named
	}
	return err
}

// untitledFullPageDatabaseSourceTitles reads, of the pages listed by their
// ids, each untitled full-page database's own sources' titles while it holds
// two or more, in its database's order, those sent to the trash with their
// views left out — what names it in the page list and in every list naming
// it apart from that list (page_name_kind.go).
func untitledFullPageDatabaseSourceTitles(app core.App, ids []string) (map[string][]databaseSourceTitle, error) {
	sources, err := untitledFullPageDatabaseSourceNames(app, ids)
	titles := make(map[string][]databaseSourceTitle, len(sources))
	for id, names := range sources {
		titles[id] = databaseSourceTitlesOf(names)
	}
	return titles, err
}

// untitledFullPageDatabaseSourceTitlesAsRead is those titles as a reader's
// closer reads them — a public page's visitor's (public_content_names.go): a
// mention in a source's name that they can't open is unnamed, as the
// source's own name is to them.
func untitledFullPageDatabaseSourceTitlesAsRead(
	app core.App, closer func() searchMentionCloser, ids []string,
) (map[string][]databaseSourceTitle, error) {
	sources, err := untitledFullPageDatabaseSourceNames(app, ids)
	titles := make(map[string][]databaseSourceTitle, len(sources))
	for id, names := range sources {
		for _, name := range names {
			titles[id] = append(titles[id], databaseSourceTitle{Title: publicNameAsRead(closer, name.Title, name.Spans), Linked: name.Linked})
		}
	}
	return titles, err
}

// untitledFullPageDatabaseSourceNames reads those sources' names as written,
// each with its spans.
func untitledFullPageDatabaseSourceNames(app core.App, ids []string) (map[string][]databaseNameRead, error) {
	names := map[string][]databaseNameRead{}
	if len(ids) == 0 {
		return names, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		PageID string `db:"page_id"`
		Title  string `db:"title"`
		Spans  string `db:"title_spans"`
		Gone   bool   `db:"gone"`
		Linked bool   `db:"linked"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]] AS [[page_id]], [[source]].[[title]], COALESCE([[source]].[[title_spans]], '') AS [[title_spans]],
			[[place]].[[source_container_id]] != '' AS [[linked]],
			[[place]].[[data_source_id]] IN (
				SELECT [[gone]].[[value]] FROM json_each(COALESCE(NULLIF([[block]].[[content]], ''), '{}'),
					'$.` + databaseTrashedWithViewsField + `') AS [[gone]]) AS [[gone]]
		FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = ` + fullPageDatabaseContainerSQL + `
		JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]] = [[container]].[[block_id]] AND [[block]].[[page_id]] = [[container]].[[page_id]]
		JOIN {{leafen_database_container_sources}} [[place]] ON [[place]].[[container_id]] = [[container]].[[id]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[place]].[[data_source_id]]
		WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND COALESCE([[page]].[[title]], '') = ''
			AND COALESCE(json_extract(NULLIF([[block]].[[content]], ''), '$.linkedDatabaseSource'), 0) = 0
		ORDER BY [[page]].[[id]], [[place]].[[position]], [[place]].[[data_source_id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	sources, own := map[string][]databaseNameSource{}, map[string]int{}
	for _, row := range rows {
		own[row.PageID]++
		sources[row.PageID] = append(sources[row.PageID], databaseNameSource{
			databaseNameRead: databaseNameRead{Title: row.Title, Spans: decodeStoredTextSpans(row.Spans), Linked: row.Linked}, Gone: row.Gone,
		})
	}
	// Those that name it on the screen, one it shows from another database
	// among them (databaseNamingSources).
	for id, listed := range sources {
		if own[id] < 2 {
			continue
		}
		for _, source := range databaseNamingSources(listed,
			func(source databaseNameSource) bool { return source.Gone }, func(source databaseNameSource) bool { return source.Linked }) {
			names[id] = append(names[id], source.databaseNameRead)
		}
	}
	return names, nil
}

// namePagesBySources gives the untitled full-page databases of several
// sources among n listed pages, each read by its id, their sources' titles.
func namePagesBySources(app core.App, n int, idOf func(int) string, name func(int, []databaseSourceTitle)) error {
	ids := make([]string, n)
	for index := range ids {
		ids[index] = idOf(index)
	}
	titles, err := untitledFullPageDatabaseSourceTitles(app, ids)
	for index, id := range ids {
		if named, found := titles[id]; found {
			name(index, named)
		}
	}
	return err
}

// untitledDatabaseSourcesNameIn is such a database's name in a message the
// server writes, in its reader's language, as the app names it
// (untitledDatabaseSourcesName): two "A 및 B", more "A, B, C", an untitled
// source "새 데이터 소스", one it shows from another database by its linked
// view's name, "X 보기" (database_source_titles.go).
func untitledDatabaseSourcesNameIn(language string, titles []databaseSourceTitle) string {
	words, found := notionSourceListWords[language]
	if !found {
		words = notionSourceListWords[accountLanguageEn]
	}
	shown := make([]string, len(titles))
	for index, title := range titles {
		shown[index] = strings.TrimSpace(title.Title)
		switch {
		case title.Linked:
			if shown[index] == "" {
				shown[index] = words.database
			}
			shown[index] = fmt.Sprintf(words.view, shown[index])
		case shown[index] == "":
			shown[index] = words.untitled
		}
	}
	if len(shown) == 2 {
		return strings.Join(shown, words.and)
	}
	return strings.Join(shown, ", ")
}
