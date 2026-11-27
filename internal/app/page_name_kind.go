package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pageNameKind is what a list's row says of the page it names for the app
// to call it while it has no title, as it calls a page everywhere else
// (web/src/lib/pageTitle.ts untitledPageDisplayTitle): its kind — a
// full-page database "새 데이터베이스", as Notion names one — and, a
// database, whether it is a view block, "제목 없음", or, one of two or more
// sources of its own, their titles, which name it "A 및 B"
// (full_page_database_sources_name.go).
type pageNameKind struct {
	Kind         string                `db:"kind"`
	ViewBlock    bool                  `db:"view_block"`
	SourceTitles []databaseSourceTitle `db:"-"`
}

// pageNameKindOf is one page's naming kind, read again only for a database.
func pageNameKindOf(app core.App, page *core.Record) (pageNameKind, error) {
	if readPageKind(page.GetString("kind")) != pageKindDatabase {
		return pageNameKind{Kind: pageKindPage}, nil
	}
	kinds, err := loadPageNameKinds(app, []string{page.Id})
	if err != nil {
		return pageNameKind{}, err
	}
	return kinds[page.Id], nil
}

// loadPageNameKinds reads the naming kinds of pages a list names apart from
// the reader's page list — an administrator's pages, a guest's, an import's
// — by their ids; a page not found is a page.
func loadPageNameKinds(app core.App, ids []string) (map[string]pageNameKind, error) {
	kinds := make(map[string]pageNameKind, len(ids))
	if len(ids) == 0 {
		return kinds, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		ID string `db:"id"`
		pageNameKind
	}{}
	if err := app.DB().NewQuery(`SELECT [[page]].[[id]], COALESCE([[page]].[[kind]], '') AS [[kind]], ` + viewBlockDatabasePageSQL + ` AS [[view_block]]
		FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).
		Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	databases := []string{}
	for _, row := range rows {
		kinds[row.ID] = pageNameKind{Kind: readPageKind(row.Kind), ViewBlock: row.ViewBlock}
		if kinds[row.ID].Kind == pageKindDatabase && !row.ViewBlock {
			databases = append(databases, row.ID)
		}
	}
	titles, err := untitledFullPageDatabaseSourceTitles(app, databases)
	for id, named := range titles {
		kind := kinds[id]
		kind.SourceTitles = named
		kinds[id] = kind
	}
	return kinds, err
}
