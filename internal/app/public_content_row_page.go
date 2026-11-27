package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A published database page shows its properties, as Notion's public
// database pages do: the first window of its read carries its database, the
// database's properties and its row with its values, as the page holding the
// database shows them.

// loadPublicDatabasePageItems reads the database, the properties and the row
// of a database page; a page that is not a database page has none.
func loadPublicDatabasePageItems(app core.App, owner, pageID string) ([]graphReadItem, error) {
	stored := []storedGraphReadItem{}
	if err := app.DB().NewQuery(`
		SELECT 'dataSource' AS [[kind]], '0' AS [[sort_key]],
			json_object('id', [[source]].[[id]], 'pageId', [[source]].[[page_id]], 'title', [[source]].[[title]],
				'icon', [[source]].[[icon]], 'iconType', [[source]].[[icon_type]]) AS [[payload]]
		FROM {{leafen_database_rows}} [[row]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[row]].[[data_source_id]]
		WHERE [[row]].[[id]] = {:pageId} AND [[row]].[[owner]] = {:owner}
		UNION ALL
		SELECT 'property', '1:' || [[property]].[[position]] || ':' || [[property]].[[id]],
			json_object('id', [[property]].[[id]], 'dataSourceId', [[property]].[[data_source_id]], 'type', [[property]].[[type]],
				'name', [[property]].[[name]], 'position', [[property]].[[position]], 'config', json([[property]].[[config]]))
		FROM {{leafen_database_rows}} [[row]]
		JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[data_source_id]] = [[row]].[[data_source_id]]
			AND [[property]].[[owner]] = [[row]].[[owner]]
		WHERE [[row]].[[id]] = {:pageId} AND [[row]].[[owner]] = {:owner}
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
		UNION ALL
		SELECT 'row', '2', ` + graphRowPayloadSQL + `
		WHERE [[row]].[[id]] = {:pageId} AND [[row]].[[owner]] = {:owner}
		ORDER BY [[sort_key]]
	`).Bind(dbx.Params{"pageId": pageID, "owner": owner}).All(&stored); err != nil {
		return nil, err
	}
	return graphReadItemsWithRowValues(app, owner, stored)
}

// loadPublicDatabasePageLayout is the layout a database page is drawn in,
// as Notion's published database pages follow their database's "레이아웃
// 사용자 지정" (seen 2026-11-12: the pinned properties under the title,
// "세부 정보 보기" and its panel, the property group under "속성"); nil
// where none is applied. A tab beside "콘텐츠" shows a visitor its data
// source's pages, as Notion's published page shows another database's tab
// with its table (seen 2026-11-12), where the database holding them is
// published with the page; one the visitor can't open is left out. It
// answers the pages holding the tabs' databases, whose pages the read
// carries (loadPublicLayoutTabItems).
func loadPublicDatabasePageLayout(app core.App, dataSourceID string, shown func(pageIDs []string) bool) (*databasePageLayout, []string, error) {
	response, err := loadDatabasePageLayout(app, dataSourceID)
	if err != nil || response.Layout == nil {
		return nil, nil, err
	}
	layout := *response.Layout
	tabs, holders := []databasePageLayoutTab{}, []string{}
	for _, tab := range layout.Tabs {
		holder := ""
		err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": tab.DataSourceID}).Row(&holder)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (holder == "" || !shown([]string{holder})) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		tabs = append(tabs, tab)
		holders = append(holders, holder)
	}
	layout.Tabs = tabs
	return &layout, holders, nil
}

// loadPublicLayoutTabItems reads, for a published database page's tabs, the
// databases they show as the visitor reads each database's own page: its
// data source, properties and pages in its first window. Items the read
// already carries aren't read twice.
func loadPublicLayoutTabItems(app core.App, owner string, layout *databasePageLayout, holders []string, carried []graphReadItem) ([]graphReadItem, error) {
	if layout == nil || len(holders) == 0 {
		return nil, nil
	}
	sources := map[string]bool{}
	for _, tab := range layout.Tabs {
		sources[tab.DataSourceID] = true
	}
	key := func(item graphReadItem) string { id, _ := item.Value["id"].(string); return item.Kind + "\x00" + id }
	seen := map[string]bool{}
	for _, item := range carried {
		seen[key(item)] = true
	}
	items := []graphReadItem{}
	for _, holder := range holders {
		window, err := loadContentGraphWindow(app, owner, holder, "", maxPublicContentGraphItems)
		if err != nil {
			return nil, err
		}
		for _, item := range window.Items {
			source, _ := item.Value["dataSourceId"].(string)
			if item.Kind == "dataSource" {
				source, _ = item.Value["id"].(string)
			}
			if (item.Kind != "dataSource" && item.Kind != "property" && item.Kind != "row") || !sources[source] || seen[key(item)] {
				continue
			}
			seen[key(item)] = true
			items = append(items, item)
		}
	}
	return items, nil
}

// publicContentPagesShown says whether the pages are in what the public
// address being read shows its visitor.
func publicContentPagesShown(app core.App, input publicContentSnapshotRequest, viewSecret string, pageIDs []string, now time.Time) bool {
	var err error
	switch input.SourceKind {
	case publicContentSourceView:
		_, _, err = authorizeViewLink(app, input.Token, viewSecret, pageIDs, now)
	case publicContentSourceShare:
		_, err = authorizePublicSharePages(app, input.Token, pageIDs, now)
	default:
		_, err = authorizePublicSitePages(app, input.SiteSlug, pageIDs, now)
	}
	return err == nil
}
