package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A whole-workspace export takes the pages its exporter opens, as Notion's
// does. Only the workspace owner makes one, who opens every teamspace page;
// of the private pages it takes the owner's own and the other people's pages
// shared with the owner, which its scope phase records in the job's scope
// pages. Other people's private pages stay out. A database page shared with
// the owner on its own, out of a database the owner doesn't open, goes into
// the readable formats as a page of its own, with its title, properties and
// body; a workspace archive, which brings databases back whole, leaves it out
// and counts it.

const (
	// workspaceExportSharedPageTurn is how many private pages one scope turn
	// of a workspace export checks for a share.
	workspaceExportSharedPageTurn = 256
	// workspaceExportOpenCountWindow is how many items one read counts when
	// a workspace export settles its total.
	workspaceExportOpenCountWindow = 1024
)

// workspaceExportPageFilter keeps a workspace export's inventory to the pages
// its exporter opens.
type workspaceExportPageFilter struct {
	JobID string
	// SharedRows also takes the database pages shared on their own, for the
	// readable formats.
	SharedRows bool
	// TeamspacesOnly keeps it to the teamspaces' pages, leaving private pages
	// out (workspaceExportLeavesPrivatePagesOut).
	TeamspacesOnly bool
}

func newWorkspaceExportPageFilter(job storedJob, payload workspaceExportJobPayload) workspaceExportPageFilter {
	return workspaceExportPageFilter{JobID: job.ID, SharedRows: workspaceExportRendersDocuments(payload.Format),
		TeamspacesOnly: workspaceExportLeavesPrivatePagesOut(payload)}
}

// workspaceExportLeavesPrivatePagesOut reports a readable workspace export
// made with Notion's "개인 페이지 포함" off, its default (seen 2026-11-13):
// it takes the teamspaces' pages and leaves out the private ones, the
// exporter's own and those shared with them out of someone else's. Notion
// keeps a page export's private and shared pages in one "개인 페이지 & 공유된
// 페이지" folder, as one kind. A workspace archive, which moves and restores
// a workspace, always takes them.
func workspaceExportLeavesPrivatePagesOut(payload workspaceExportJobPayload) bool {
	return payload.Scope.Kind == workspaceExportScopeWorkspace && payload.Options.LeavePrivatePagesOut &&
		(payload.Format == workspaceExportFormatHTML || payload.Format == workspaceExportFormatMarkdownCSV)
}

func (filter workspaceExportPageFilter) sql(alias string) string {
	if filter.TeamspacesOnly {
		return workspaceExportTeamspacePageSQL(alias)
	}
	if !filter.SharedRows {
		return workspaceExportPageOpensSQL(alias)
	}
	return `(` + workspaceExportPageOpensSQL(alias) + ` OR (` + alias + `.[[data_source_id]] != '' AND ` +
		workspaceExportSharedRowSQL(alias+`.[[id]]`) + `))`
}

// sharedRowSQL reports whether the row the expression names is a database
// page shared on its own that the export takes.
func (filter workspaceExportPageFilter) sharedRowSQL(rowID string) string {
	if !filter.SharedRows {
		return "FALSE"
	}
	return workspaceExportSharedRowSQL(rowID)
}

// sharedSourceSQL reports whether the data source the expression names holds
// a database page shared on its own that the export takes, whose properties
// the page shows.
func (filter workspaceExportPageFilter) sharedSourceSQL(sourceID string) string {
	if !filter.SharedRows {
		return "FALSE"
	}
	return `EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[shared_row]]
		JOIN {{leafen_pages}} AS [[shared_page]] ON [[shared_page]].[[id]] = [[shared_row]].[[page_id]]
		WHERE [[shared_row]].[[job_id]] = {:jobId} AND [[shared_row]].[[row_page]] = TRUE
			AND [[shared_page]].[[data_source_id]] = ` + sourceID + `)`
}

// parentSQL is the parent a page keeps in a workspace export: its own, or
// none when the export doesn't take it, so a page shared out of someone's
// private pages is a top page of the export.
func (filter workspaceExportPageFilter) parentSQL(alias string) string {
	return `(CASE WHEN ` + alias + `.[[parent_id]] = '' OR EXISTS (SELECT 1 FROM {{leafen_pages}} AS [[open_page]]
		WHERE [[open_page]].[[id]] = ` + alias + `.[[parent_id]] AND ` + filter.sql("[[open_page]]") + `)
		THEN ` + alias + `.[[parent_id]] ELSE '' END)`
}

// workspaceExportSharedRowSQL reports whether the row the expression names
// is recorded as a database page shared on its own.
func workspaceExportSharedRowSQL(rowID string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[shared_row]]
		WHERE [[shared_row]].[[job_id]] = {:jobId} AND [[shared_row]].[[page_id]] = ` + rowID + `
			AND [[shared_row]].[[row_page]] = TRUE)`
}

// workspaceExportPageOpensSQL reports whether the exporter opens the page
// aliased, a database row's page by its database page. A page behind a
// deletion's fence, or under one, is opened by no one.
func workspaceExportPageOpensSQL(alias string) string {
	return `((` + alias + `.[[teamspace_id]] != ''
		OR (` + alias + `.[[private_owner_id]] != '' AND ` + alias + `.[[private_owner_id]] = {:actorId})
		OR ` + workspaceExportInScope(`(CASE WHEN `+alias+`.[[data_source_id]] = '' THEN `+alias+`.[[id]]
			ELSE COALESCE((SELECT [[open_source]].[[page_id]] FROM {{leafen_data_sources}} AS [[open_source]]
				WHERE [[open_source]].[[id]] = `+alias+`.[[data_source_id]]), '') END)`) + `)
		AND ` + pageOutsideDeletionFenceSQL(alias+`.[[id]]`, `{:workspaceId}`) + `)`
}

// workspaceExportTeamspacePageSQL reports whether the page aliased stands in
// a teamspace, a database row's page by its database's page, outside a
// deletion's fence.
func workspaceExportTeamspacePageSQL(alias string) string {
	return `((` + alias + `.[[teamspace_id]] != '' OR (` + alias + `.[[data_source_id]] != '' AND EXISTS (
			SELECT 1 FROM {{leafen_data_sources}} AS [[team_source]]
			JOIN {{leafen_pages}} AS [[team_page]] ON [[team_page]].[[id]] = [[team_source]].[[page_id]]
			WHERE [[team_source]].[[id]] = ` + alias + `.[[data_source_id]] AND [[team_page]].[[teamspace_id]] != '')))
		AND ` + pageOutsideDeletionFenceSQL(alias+`.[[id]]`, `{:workspaceId}`) + `)`
}

// workspaceExportPageIDOpensSQL reports whether the exporter opens the page
// the expression names.
func workspaceExportPageIDOpensSQL(pageID string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_pages}} AS [[open_page]]
		WHERE [[open_page]].[[id]] = ` + pageID + ` AND ` + workspaceExportPageOpensSQL("[[open_page]]") + `)`
}

// discoverWorkspaceExportSharedPages checks one window of other people's
// private pages for a share with the exporter, records the shared ones, and
// reports the cursor after the window and whether any page remains. A
// database page counts as shared on its own when the exporter doesn't open
// its database's page; otherwise it goes with its database.
func discoverWorkspaceExportSharedPages(
	app core.App, job storedJob, authorization workspaceAuthorization, afterID string,
) (string, bool, error) {
	pages := []struct {
		ID           string `db:"id"`
		DatabasePage string `db:"database_page"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]], COALESCE([[source]].[[page_id]], '') AS [[database_page]]
		FROM {{leafen_pages}} AS [[page]]
		LEFT JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[page]].[[data_source_id]]
		WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
			AND [[page]].[[notion_import_staging]] = FALSE AND [[page]].[[teamspace_id]] = ''
			AND ([[page]].[[private_owner_id]] = '' OR [[page]].[[private_owner_id]] != {:actorId})
			AND [[page]].[[id]] > {:afterId}
			AND ` + pageOutsideDeletionFenceSQL("[[page]].[[id]]", "{:workspaceId}") + `
		ORDER BY [[page]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
		"actorId": authorization.PrincipalID, "afterId": afterID, "limit": workspaceExportSharedPageTurn + 1,
	}).All(&pages); err != nil {
		return afterID, false, err
	}
	window := pages
	if len(window) > workspaceExportSharedPageTurn {
		window = window[:workspaceExportSharedPageTurn]
	}
	opens := func(pageID string) (bool, error) {
		role, err := effectiveUserPageAccessRole(app, authorization.WorkspaceID, pageID, authorization.PrincipalID)
		if isPermissionDenied(err) {
			return false, nil
		}
		return err == nil && role >= pageAccessRoleView, err
	}
	for _, page := range window {
		shared, err := opens(page.ID)
		if err != nil {
			return afterID, false, err
		}
		rowPage := page.DatabasePage != ""
		if shared && rowPage {
			withDatabase, err := opens(page.DatabasePage)
			if err != nil {
				return afterID, false, err
			}
			shared = !withDatabase
		}
		if !shared {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_export_scope_pages}}
				([[job_id]], [[page_id]], [[parent_id]], [[is_root]], [[expanded]], [[discovered]], [[row_page]])
			VALUES ({:jobId}, {:pageId}, '', FALSE, TRUE, 0, {:rowPage})
			ON CONFLICT ([[job_id]], [[page_id]]) DO NOTHING
		`).Bind(dbx.Params{"jobId": job.ID, "pageId": page.ID, "rowPage": rowPage}).Execute(); err != nil {
			return afterID, false, err
		}
	}
	if len(window) > 0 {
		afterID = window[len(window)-1].ID
	}
	return afterID, len(pages) > workspaceExportSharedPageTurn, nil
}

// countWorkspaceExportSharedRows is how many database pages shared on their
// own a workspace export's scope found.
func countWorkspaceExportSharedRows(app core.App, jobID string) (int, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_workspace_export_scope_pages}}
		WHERE [[job_id]] = {:jobId} AND [[row_page]] = TRUE
	`).Bind(dbx.Params{"jobId": jobID}).Row(&count)
	return count, err
}

// countWorkspaceExportOpenInventory is how many items a workspace export
// takes, counted as the export reads them: the items of the pages its
// exporter opens, database pages' bodies and databases among them. A change
// while the export runs moves the workspace's revision, which the export
// checks. A workspace archive also refuses a linked view or relation that
// reaches a database the exporter does not open, as a page archive refuses
// one that leaves its pages, since the archive could not bring it back.
func countWorkspaceExportOpenInventory(
	app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload,
) (int, error) {
	if payload.Format == workspaceExportFormatNativeArchive {
		if err := validateWorkspaceExportLinksInside(app, authorization, job, "",
			workspaceExportPageIDOpensSQL("[[linked]].[[page_id]]"),
			workspaceExportPageIDOpensSQL("[[source]].[[page_id]]"),
		); err != nil {
			return 0, err
		}
	}
	filter := newWorkspaceExportPageFilter(job, payload)
	count, cursor := 0, ""
	for {
		rows, done, err := loadWorkspaceExportInventoryWindow(app, authorization, filter, cursor, workspaceExportOpenCountWindow)
		if err != nil {
			return 0, err
		}
		count += len(rows)
		if done {
			return count, nil
		}
		if len(rows) == 0 {
			return 0, errors.New(errorCodeWorkspaceExportArtifactInvalid)
		}
		cursor = rows[len(rows)-1].SortKey
	}
}
