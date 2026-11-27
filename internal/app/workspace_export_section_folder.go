package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// workspaceExportSectionFolder is the folder a page's or a database's
// readable export (Markdown & CSV, HTML) puts everything in, as Notion's
// export now does by the sidebar section the page stands in (seen
// 2026-11-13 and 2026-11-14: a page, a database and a full-page database
// under a private page, every file under "개인 페이지 & 공유된 페이지/"):
// a page outside any teamspace, private or shared with its exporter, stands
// in "<개인 페이지> & <공유된 페이지>", the sidebar's two names in the
// export's language. A teamspace's page and a workspace export keep no
// such folder until Notion's is seen there; "" for those.
func workspaceExportSectionFolder(app core.App, payload workspaceExportJobPayload, language string) (string, error) {
	query := ""
	switch payload.Scope.Kind {
	case workspaceExportScopePage:
		query = `SELECT COALESCE([[teamspace_id]], '') FROM {{leafen_pages}} WHERE [[id]] = {:target}`
	case workspaceExportScopeDatabase:
		query = `SELECT COALESCE([[page]].[[teamspace_id]], '') FROM {{leafen_data_sources}} [[source]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[source]].[[page_id]]
			WHERE [[source]].[[id]] = {:target}`
	default:
		return "", nil
	}
	teamspaceID := ""
	if err := app.DB().NewQuery(query).Bind(dbx.Params{"target": payload.Scope.TargetID}).Row(&teamspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if teamspaceID != "" {
		return "", nil
	}
	if language == accountLanguageKo {
		return "개인 페이지 & 공유된 페이지/", nil
	}
	return "Private & Shared/", nil
}
