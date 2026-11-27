package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// fillDatabaseContainerPickerIcons gives each found source its own icon,
// which every list naming a source draws before its name, the cylinder only
// where it has none, as Notion's (seen 2026-11-15: 데이터 소스 관리's rows,
// "추천" and "기존 데이터 소스", the search's "/ source" — one source 🍎,
// another 🍋, a third none). A database of one source shows that source's.
func fillDatabaseContainerPickerIcons(app core.App, items []databaseContainerPickerItem) error {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]bool{}
	ids := []any{}
	for _, item := range items {
		if item.DataSourceID != "" && !seen[item.DataSourceID] {
			seen[item.DataSourceID] = true
			ids = append(ids, item.DataSourceID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows := []struct {
		ID       string `db:"id"`
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source]].[[id]], ` + dataSourceIconSQL("[[source]]") + ` AS [[icon]], ` + dataSourceIconTypeSQL("[[source]]") + ` AS [[icon_type]]
		FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[id]] IN (` + inlineDatabaseInList("source", ids) + `)
	`).Bind(inlineDatabaseInParams("source", ids)).All(&rows); err != nil {
		return err
	}
	icons := map[string][2]string{}
	for _, row := range rows {
		if row.Icon != "" && row.IconType != "" && row.IconType != graphDataSourceIconNone {
			icons[row.ID] = [2]string{row.Icon, row.IconType}
		}
	}
	for index := range items {
		if icon, found := icons[items[index].DataSourceID]; found {
			items[index].Icon, items[index].IconType = icon[0], icon[1]
		}
	}
	return nil
}
