package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// nameFormOptionTarget says where a relation's list searches, "〈데이터베이스〉
// 내 검색", with the source's own icon before its name, as Notion's (seen
// 2026-11-15: "🍎 사과 DB 내 검색"); none where it has none, the cylinder then.
func nameFormOptionTarget(app core.App, source string, response *formOptionsResponse) error {
	row := struct {
		Title    string `db:"title"`
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[title]], [[icon]], [[icon_type]] FROM {{leafen_data_sources}} WHERE [[id]] = {:source}`).
		Bind(dbx.Params{"source": source}).One(&row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	response.TargetTitle = row.Title
	if row.Icon != "" && row.IconType != "" && row.IconType != graphDataSourceIconNone {
		response.TargetIcon, response.TargetIconType = row.Icon, row.IconType
	}
	return nil
}
