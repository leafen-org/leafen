package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database template opens in the row page window, as Notion's opens as a
// page (decided 2026-10-30): its page record, look and body are a page's,
// while its values stay the template's own (database_toolbar_config.go) and
// it stays out of the rows, so nothing that reads rows or their values
// meets it. Only what a person does in that window takes a template — its
// read, the page's look and comments; everything else that reads a row page
// (trash, moves, sends, reminders, links) doesn't know one.

// databaseTemplatePageEpochSQL is a page record's time as a row page's, in
// milliseconds.
func databaseTemplatePageEpochSQL(value string) string {
	return `COALESCE(CAST(ROUND((julianday(` + value + `) - 2440587.5) * 86400000) AS INTEGER), 0)`
}

// authorizeDatabaseTemplatePage reads a template as a row page's authority
// for someone with the role on its database's page, as its editor is
// (authorizeDatabaseTemplateBody). A deleted one reads as a row in the trash,
// opened from there, and refuses writes as its body does.
func authorizeDatabaseTemplatePage(
	app core.App, principalID, templateID string, minimum pageAccessRole,
) (databaseRowPageAuthority, error) {
	if _, err := authorizeDatabaseTemplateBody(app, principalID, templateID, minimum); isPermissionDenied(err) {
		return databaseRowPageAuthority{}, databaseRowPageNotFoundError()
	} else if err != nil {
		return databaseRowPageAuthority{}, err
	}
	authority := databaseRowPageAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[template]].[[id]] AS [[row_id]], [[template]].[[owner]], [[template]].[[workspace_id]],
			[[template]].[[page_id]] AS [[container_page_id]], [[template]].[[data_source_id]],
			[[template]].[[position]], '' AS [[subitem_parent_id]],
			COALESCE([[state]].[[authority_revision]], 1) AS [[authority_revision]],
			[[record]].[[icon]], [[record]].[[icon_type]], [[record]].[[cover]], [[record]].[[cover_position]],
			[[record]].[[is_locked]], [[record]].[[font]], [[record]].[[small_text]], [[record]].[[full_width]],
			[[record]].[[table_of_contents_display]], [[record]].[[archived_at]], [[record]].[[archived_by]],
			[[record]].[[created_by]], [[record]].[[last_edited_by]],
			` + databaseTemplatePageEpochSQL("[[record]].[[created]]") + ` AS [[created]],
			` + databaseTemplatePageEpochSQL("[[record]].[[updated]]") + ` AS [[updated]],
			0 AS [[body_snapshot_revision]],
			CASE WHEN COALESCE([[state]].[[deleted_at]], '') = '' THEN 0 ELSE ` + databaseTemplateTrashedAtSQL + ` END AS [[trashed_at]],
			COALESCE([[state]].[[deleted_by]], '') AS [[trashed_by]],
			CASE WHEN COALESCE([[state]].[[deleted_at]], '') = '' THEN '' ELSE [[template]].[[id]] END AS [[trash_root_id]],
			TRUE AS [[template]]
		FROM {{leafen_database_templates}} [[template]]
		JOIN {{leafen_pages}} [[record]] ON [[record]].[[id]] = [[template]].[[id]] AND [[record]].[[database_template]]
		LEFT JOIN {{leafen_database_toolbar_template_state}} [[state]] ON [[state]].[[template_id]] = [[template]].[[id]]
		WHERE [[template]].[[id]] = {:id} AND ` + databaseSourceNotTrashedSQL("[[template]].[[data_source_id]]") + `
	`).Bind(dbx.Params{"id": templateID}).One(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseRowPageAuthority{}, databaseRowPageNotFoundError()
	}
	return authority, err
}

// authorizeDatabaseRowOrTemplatePage reads a row page, or else a template
// opened as one.
func authorizeDatabaseRowOrTemplatePage(
	app core.App, principalID, pageID string, requirement permissionRequirement,
) (databaseRowPageAuthority, error) {
	authority, err := authorizeDatabaseRowPage(app, principalID, pageID, requirement)
	if !errors.Is(err, errDatabaseRowPageNotFound) {
		return authority, err
	}
	return authorizeDatabaseTemplatePage(app, principalID, pageID, pageAccessRoleForWorkspaceRequirement(requirement))
}

// databaseTemplatePageTitle is a template's name as the lists of pages
// visited name it — Home's, the library's and search's — which is its
// database's menus' name (databaseTemplateNameSQL): Notion's Home lists a
// template opened lately under its name (seen 2026-11-11).
func databaseTemplatePageTitle(app core.App, templateID string) string {
	title := ""
	if err := app.DB().NewQuery(`SELECT ` + databaseTemplateNameSQL("[[template]]") +
		` FROM {{leafen_database_templates}} AS [[template]] WHERE [[template]].[[id]] = {:id}`).
		Bind(dbx.Params{"id": templateID}).Row(&title); err != nil {
		return ""
	}
	return title
}
