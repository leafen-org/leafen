package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceExportScopeAuthorization struct {
	Workspace  workspaceAuthorization
	PageID     string
	DatabaseID string
	BlockID    string
}

type storedWorkspaceExportScopePage struct {
	PageID             string `db:"page_id"`
	ChildCreatedCursor string `db:"child_created_cursor"`
	ChildIDCursor      string `db:"child_id_cursor"`
	Discovered         int    `db:"discovered"`
	Root               bool   `db:"is_root"`
}

type storedWorkspaceExportScopeBlock struct {
	BlockID             string `db:"block_id"`
	PageID              string `db:"page_id"`
	ChildPositionCursor string `db:"child_position_cursor"`
	ChildIDCursor       string `db:"child_id_cursor"`
	Discovered          int    `db:"discovered"`
}

type discoveredWorkspaceExportPage struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Created  string `db:"created"`
	// RowPage marks a page of the page's databases, which the scope passes
	// through for its body's sub-pages and databases.
	RowPage bool `db:"row_page"`
}

type discoveredWorkspaceExportBlock struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
}

func normalizeStoredWorkspaceExportPayload(payload *workspaceExportJobPayload) {
	if payload.Scope.Kind == "" && payload.Scope.TargetID == "" &&
		(payload.Format == workspaceExportFormatNativeArchive || payload.Format == workspaceExportFormatMarkdownCSV) {
		payload.Scope.Kind = workspaceExportScopeWorkspace
	}
}

// workspaceExportRendersDocuments reports the formats that render readable
// pages (Markdown & CSV, HTML, printable HTML) from the captured inventory.
func workspaceExportRendersDocuments(format string) bool {
	return format == workspaceExportFormatMarkdownCSV || format == workspaceExportFormatHTML ||
		format == workspaceExportFormatPrintHTML
}

func normalizeWorkspaceExportScope(format string, input workspaceExportScope) (workspaceExportScope, bool) {
	scope := workspaceExportScope{Kind: strings.TrimSpace(input.Kind), TargetID: strings.TrimSpace(input.TargetID)}
	switch scope.Kind {
	case workspaceExportScopeWorkspace:
		return scope, scope.TargetID == "" &&
			(format == workspaceExportFormatNativeArchive || format == workspaceExportFormatMarkdownCSV ||
				format == workspaceExportFormatHTML)
	case workspaceExportScopePage:
		return scope, scope.TargetID != "" &&
			(format == workspaceExportFormatPageMarkdown || format == workspaceExportFormatPageNativeArchive ||
				format == workspaceExportFormatNotionMarkdown || workspaceExportRendersDocuments(format))
	case workspaceExportScopeDatabase:
		return scope, scope.TargetID != "" &&
			(format == workspaceExportFormatDatabaseCSV || format == workspaceExportFormatNotionMarkdown ||
				format == workspaceExportFormatPageNativeArchive || workspaceExportRendersDocuments(format))
	case workspaceExportScopeBlockSubtree:
		return scope, scope.TargetID != "" && format == workspaceExportFormatNotionMarkdown
	default:
		return workspaceExportScope{}, false
	}
}

func normalizeWorkspaceExportOptions(format string, values map[string]bool) (workspaceExportOptions, bool) {
	if len(values) == 0 || len(values) > maxWorkspaceExportOptions {
		return workspaceExportOptions{}, false
	}
	allowed := map[string]bool{"includeFiles": true}
	if workspaceExportRendersDocuments(format) {
		allowed["createFolders"] = true
		allowed["includeSubpages"] = true
		allowed["defaultDatabaseViews"] = true
	}
	if format == workspaceExportFormatHTML || format == workspaceExportFormatPrintHTML {
		allowed["includeComments"] = true
	}
	if format == workspaceExportFormatHTML || format == workspaceExportFormatMarkdownCSV {
		allowed["leavePrivatePagesOut"] = true
	}
	if format == workspaceExportFormatNotionMarkdown {
		allowed["includeTranscript"] = true
	}
	for key := range values {
		if key == "" || len(key) > maxWorkspaceExportOptionKeyCharacters || !allowed[key] {
			return workspaceExportOptions{}, false
		}
	}
	includeFiles, present := values["includeFiles"]
	if !present {
		return workspaceExportOptions{}, false
	}
	options := workspaceExportOptions{IncludeFiles: includeFiles}
	if format == workspaceExportFormatNativeArchive || format == workspaceExportFormatPageNativeArchive {
		return workspaceExportOptions{IncludeFiles: true}, includeFiles && len(values) == 1
	}
	if workspaceExportRendersDocuments(format) {
		options.CreateFolders = values["createFolders"]
		options.IncludeSubpages = values["includeSubpages"]
		options.IncludeComments = values["includeComments"]
		options.DefaultDatabaseViews = values["defaultDatabaseViews"]
		options.LeavePrivatePagesOut = values["leavePrivatePagesOut"]
	}
	if format == workspaceExportFormatNotionMarkdown {
		options.IncludeTranscript = values["includeTranscript"]
	}
	return options, true
}

// authorizeWorkspaceToExport is authorizeWorkspace for an export. An app's
// workspace belongs to the app's account, so a server's administrator exports
// it as that account: the copy they are told to keep before removing the app
// (docs/app-platform.md 6).
func authorizeWorkspaceToExport(
	app core.App, actorID, workspaceID string, requirement permissionRequirement,
) (workspaceAuthorization, error) {
	authorization, err := authorizeWorkspace(app, actorID, workspaceID, requirement)
	if !isPermissionDenied(err) {
		return authorization, err
	}
	account := ""
	if err := app.DB().NewQuery(`SELECT [[account_id]] FROM {{leafen_app_workspaces}} WHERE [[workspace_id]] = {:workspace}`).
		Bind(dbx.Params{"workspace": workspaceID}).Row(&account); err != nil {
		return authorization, permissionDenied()
	}
	if administrator, adminErr := isAdministrator(app, actorID); adminErr != nil || !administrator {
		return authorization, permissionDenied()
	}
	return authorizeWorkspace(app, account, workspaceID, requirement)
}

func authorizeWorkspaceExportScope(
	app core.App,
	actorID string,
	workspaceID string,
	scope workspaceExportScope,
) (workspaceExportScopeAuthorization, error) {
	result := workspaceExportScopeAuthorization{}
	// Only the workspace owner exports the whole workspace, as in Notion.
	if scope.Kind == workspaceExportScopeWorkspace {
		authorization, err := authorizeWorkspaceToExport(app, actorID, workspaceID, permissionWorkspaceOwner)
		result.Workspace = authorization
		return result, err
	}
	pageID := scope.TargetID
	if scope.Kind == workspaceExportScopeDatabase {
		if err := app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_data_sources}}
			WHERE [[id]] = {:targetId} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{"targetId": scope.TargetID, "workspaceId": workspaceID}).Row(&pageID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, permissionDenied()
			}
			return result, err
		}
		result.DatabaseID = scope.TargetID
	} else if scope.Kind == workspaceExportScopeBlockSubtree {
		if err := app.DB().NewQuery(`
			SELECT [[page_id]] FROM {{leafen_blocks}}
			WHERE [[id]] = {:targetId} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{"targetId": scope.TargetID, "workspaceId": workspaceID}).Row(&pageID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, permissionDenied()
			}
			return result, err
		}
		result.BlockID = scope.TargetID
	}
	page, err := authorizePageAccessRole(app, actorID, pageID, pageAccessRoleView)
	if err != nil || page.Workspace.WorkspaceID != workspaceID {
		if err == nil {
			err = permissionDenied()
		}
		return result, err
	}
	result.Workspace = page.Workspace
	result.PageID = pageID
	return result, nil
}

// seedWorkspaceExportScope records a scoped export's root. A database page
// exported on its own in a readable format is a row page, which the export
// shows as its database shows it: its title, properties and body.
func seedWorkspaceExportScope(
	app core.App, jobID, format string, scope workspaceExportScope, authorization workspaceExportScopeAuthorization,
) error {
	if scope.Kind == workspaceExportScopeWorkspace {
		return nil
	}
	if scope.Kind == workspaceExportScopeBlockSubtree {
		_, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_export_scope_blocks}}
				([[job_id]], [[block_id]], [[page_id]], [[parent_id]], [[is_root]], [[expanded]], [[discovered]])
			VALUES ({:jobId}, {:blockId}, {:pageId}, '', TRUE, FALSE, 0)
			ON CONFLICT ([[job_id]], [[block_id]]) DO NOTHING
		`).Bind(dbx.Params{"jobId": jobID, "blockId": authorization.BlockID, "pageId": authorization.PageID}).Execute()
		return err
	}
	rowPage := false
	if scope.Kind == workspaceExportScopePage && workspaceExportRendersDocuments(format) {
		var dataSourceID string
		if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:pageId}`).
			Bind(dbx.Params{"pageId": authorization.PageID}).Row(&dataSourceID); err != nil {
			return err
		}
		rowPage = dataSourceID != ""
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_export_scope_pages}}
			([[job_id]], [[page_id]], [[parent_id]], [[is_root]], [[expanded]], [[discovered]], [[row_page]])
		VALUES ({:jobId}, {:pageId}, '', TRUE, FALSE, 0, {:rowPage})
		ON CONFLICT ([[job_id]], [[page_id]]) DO NOTHING
	`).Bind(dbx.Params{"jobId": jobID, "pageId": authorization.PageID, "rowPage": rowPage}).Execute()
	return err
}

// workspaceExportRootRowSQL reports whether the row the expression names is a
// page export's root: a database page exported on its own.
func workspaceExportRootRowSQL(rowID string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[root_row]]
		WHERE [[root_row]].[[job_id]] = {:jobId} AND [[root_row]].[[page_id]] = ` + rowID + `
			AND [[root_row]].[[is_root]] AND [[root_row]].[[row_page]])`
}

// workspaceExportRootRowSourceSQL reports whether the data source the
// expression names holds a page export's root database page, whose
// properties the page shows.
func workspaceExportRootRowSourceSQL(sourceID string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} AS [[root_row]]
		JOIN {{leafen_pages}} AS [[root_page]] ON [[root_page]].[[id]] = [[root_row]].[[page_id]]
		WHERE [[root_row]].[[job_id]] = {:jobId} AND [[root_row]].[[is_root]] AND [[root_row]].[[row_page]]
			AND [[root_page]].[[data_source_id]] = ` + sourceID + `)`
}

func (r *durableJobRunner) captureWorkspaceExportScopeTurn(job storedJob, payload workspaceExportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(current.PayloadJSON), &payload); err != nil {
			return err
		}
		normalizeStoredWorkspaceExportPayload(&payload)
		authorization, err := authorizeWorkspaceExportScope(txApp, current.Owner, current.WorkspaceID, payload.Scope)
		if isPermissionDenied(err) && pageAccessHeld(err) == "" {
			return failWorkspaceExportJob(txApp, current, errorCodePermissionDenied)
		}
		if err != nil {
			return err
		}
		if authorization.Workspace.OwnerID != payload.AuthorityOwnerID {
			return failWorkspaceExportJob(txApp, current, errorCodeWorkspaceExportSourceChanged)
		}
		pending := 0
		switch {
		case payload.Scope.Kind == workspaceExportScopeWorkspace && workspaceExportLeavesPrivatePagesOut(payload):
			// Pages shared out of someone's private pages are private pages too.
		case payload.Scope.Kind == workspaceExportScopeWorkspace:
			remaining := false
			payload.ScopeCursor, remaining, err = discoverWorkspaceExportSharedPages(
				txApp, current, authorization.Workspace, payload.ScopeCursor,
			)
			if remaining {
				pending = 1
			}
		case payload.Scope.Kind == workspaceExportScopeBlockSubtree:
			err = expandWorkspaceExportBlockScope(txApp, current, authorization)
		case payload.Format == workspaceExportFormatPageNativeArchive ||
			workspaceExportRendersDocuments(payload.Format) && payload.Options.IncludeSubpages &&
				payload.Scope.Kind == workspaceExportScopePage:
			err = expandWorkspaceExportPageScope(txApp, current, payload, authorization)
		default:
			_, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_workspace_export_scope_pages}} SET [[expanded]] = TRUE
				WHERE [[job_id]] = {:jobId}
			`).Bind(dbx.Params{"jobId": current.ID}).Execute()
		}
		if err != nil {
			return err
		}
		if payload.Scope.Kind != workspaceExportScopeWorkspace {
			pending, err = workspaceExportScopePending(txApp, current.ID, payload.Scope.Kind)
			if err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		if pending == 0 && payload.Scope.Kind == workspaceExportScopeWorkspace &&
			payload.Format == workspaceExportFormatNativeArchive {
			if payload.OmittedPages, err = countWorkspaceExportSharedRows(txApp, current.ID); err != nil {
				return err
			}
		}
		if pending == 0 {
			payload.Phase = workspaceExportPhaseInventory
			total, countErr := countWorkspaceExportScopedInventory(txApp, authorization.Workspace, current, payload)
			if countErr != nil {
				return countErr
			}
			current.Total = total
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[payload_json]] = {:payload}, [[total_items]] = {:total},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(encoded), "total": current.Total,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}

func expandWorkspaceExportPageScope(
	app core.App, job storedJob, payload workspaceExportJobPayload, authorization workspaceExportScopeAuthorization,
) error {
	var parent storedWorkspaceExportScopePage
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[child_created_cursor]], [[child_id_cursor]], [[discovered]], [[is_root]]
		FROM {{leafen_workspace_export_scope_pages}}
		WHERE [[job_id]] = {:jobId} AND [[expanded]] = FALSE
		ORDER BY [[discovered]], [[page_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID}).One(&parent); err != nil {
		return err
	}
	// A page's children are its sub-pages and the pages of its databases,
	// whose own sub-pages and databases are the page's too. A database's
	// archive takes of the page holding it only the database's pages. A child
	// behind a deletion's fence stays out with everything under it.
	subPages, rows := "TRUE", "TRUE"
	if parent.Root && workspaceExportDatabaseArchive(payload) {
		subPages, rows = "FALSE", workspaceExportDatabaseArchiveSourceSQL("[[row]].[[data_source_id]]")
	}
	children := []discoveredWorkspaceExportPage{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[created]], [[row_page]] FROM (
			SELECT [[page]].[[id]], [[page]].[[parent_id]], [[page]].[[created]], FALSE AS [[row_page]]
			FROM {{leafen_pages}} AS [[page]]
			WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[owner]] = {:ownerId}
				AND [[page]].[[parent_id]] = {:parentId}
				AND [[page]].[[notion_import_staging]] = FALSE AND [[page]].[[in_trash]] = FALSE AND ` + subPages + `
				AND ` + pageWithoutOwnDeletionFenceSQL("[[page]].[[id]]") + `
			UNION ALL
			SELECT [[page]].[[id]], [[page]].[[parent_id]], [[page]].[[created]], TRUE AS [[row_page]]
			FROM {{leafen_database_rows}} AS [[row]]
			JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[row]].[[id]]
			WHERE [[row]].[[workspace_id]] = {:workspaceId} AND [[row]].[[owner]] = {:ownerId}
				AND [[row]].[[page_id]] = {:parentId} AND ` + rows + `
				AND ` + pageWithoutOwnDeletionFenceSQL("[[page]].[[id]]") + `
		)
		WHERE [[created]] > {:afterCreated} OR ([[created]] = {:afterCreated} AND [[id]] > {:afterId})
		ORDER BY [[created]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": authorization.Workspace.WorkspaceID, "ownerId": authorization.Workspace.OwnerID,
		"parentId": parent.PageID, "afterCreated": parent.ChildCreatedCursor,
		"afterId": parent.ChildIDCursor, "limit": workspaceExportScopeTurnItems + 1, "targetId": payload.Scope.TargetID,
	}).All(&children); err != nil {
		return err
	}
	window := children
	if len(window) > workspaceExportScopeTurnItems {
		window = window[:workspaceExportScopeTurnItems]
	}
	for _, child := range window {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_export_scope_pages}}
				([[job_id]], [[page_id]], [[parent_id]], [[is_root]], [[expanded]], [[discovered]], [[row_page]])
			VALUES ({:jobId}, {:pageId}, {:parentId}, FALSE, FALSE, {:discovered}, {:rowPage})
			ON CONFLICT ([[job_id]], [[page_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": job.ID, "pageId": child.ID, "parentId": parent.PageID,
			"discovered": parent.Discovered + 1, "rowPage": child.RowPage,
		}).Execute(); err != nil {
			return err
		}
	}
	expanded := len(children) <= workspaceExportScopeTurnItems
	afterCreated, afterID := parent.ChildCreatedCursor, parent.ChildIDCursor
	if len(window) > 0 {
		afterCreated, afterID = window[len(window)-1].Created, window[len(window)-1].ID
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_workspace_export_scope_pages}}
		SET [[expanded]] = {:expanded}, [[child_created_cursor]] = {:afterCreated}, [[child_id_cursor]] = {:afterId}
		WHERE [[job_id]] = {:jobId} AND [[page_id]] = {:pageId} AND [[expanded]] = FALSE
	`).Bind(dbx.Params{
		"expanded": expanded, "afterCreated": afterCreated, "afterId": afterID,
		"jobId": job.ID, "pageId": parent.PageID,
	}).Execute()
	return err
}

func expandWorkspaceExportBlockScope(app core.App, job storedJob, authorization workspaceExportScopeAuthorization) error {
	var parent storedWorkspaceExportScopeBlock
	if err := app.DB().NewQuery(`
		SELECT [[block_id]], [[page_id]], [[child_position_cursor]], [[child_id_cursor]], [[discovered]]
		FROM {{leafen_workspace_export_scope_blocks}}
		WHERE [[job_id]] = {:jobId} AND [[expanded]] = FALSE
		ORDER BY [[discovered]], [[block_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID}).One(&parent); err != nil {
		return err
	}
	children := []discoveredWorkspaceExportBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[parent_id]], [[position]] FROM {{leafen_blocks}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[owner]] = {:ownerId}
			AND [[page_id]] = {:pageId} AND [[parent_id]] = {:parentId}
			AND ([[position]] > {:afterPosition} OR ([[position]] = {:afterPosition} AND [[id]] > {:afterId}))
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": authorization.Workspace.WorkspaceID, "ownerId": authorization.Workspace.OwnerID,
		"pageId": parent.PageID, "parentId": parent.BlockID,
		"afterPosition": parent.ChildPositionCursor, "afterId": parent.ChildIDCursor,
		"limit": workspaceExportScopeTurnItems + 1,
	}).All(&children); err != nil {
		return err
	}
	window := children
	if len(window) > workspaceExportScopeTurnItems {
		window = window[:workspaceExportScopeTurnItems]
	}
	for _, child := range window {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_export_scope_blocks}}
				([[job_id]], [[block_id]], [[page_id]], [[parent_id]], [[is_root]], [[expanded]], [[discovered]])
			VALUES ({:jobId}, {:blockId}, {:pageId}, {:parentId}, FALSE, FALSE, {:discovered})
			ON CONFLICT ([[job_id]], [[block_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": job.ID, "blockId": child.ID, "pageId": parent.PageID,
			"parentId": parent.BlockID, "discovered": parent.Discovered + 1,
		}).Execute(); err != nil {
			return err
		}
	}
	expanded := len(children) <= workspaceExportScopeTurnItems
	afterPosition, afterID := parent.ChildPositionCursor, parent.ChildIDCursor
	if len(window) > 0 {
		afterPosition, afterID = window[len(window)-1].Position, window[len(window)-1].ID
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_workspace_export_scope_blocks}}
		SET [[expanded]] = {:expanded}, [[child_position_cursor]] = {:afterPosition}, [[child_id_cursor]] = {:afterId}
		WHERE [[job_id]] = {:jobId} AND [[block_id]] = {:blockId} AND [[expanded]] = FALSE
	`).Bind(dbx.Params{
		"expanded": expanded, "afterPosition": afterPosition, "afterId": afterID,
		"jobId": job.ID, "blockId": parent.BlockID,
	}).Execute()
	return err
}

func workspaceExportScopePending(app core.App, jobID, scopeKind string) (int, error) {
	table := workspaceExportScopePagesTable
	if scopeKind == workspaceExportScopeBlockSubtree {
		table = workspaceExportScopeBlocksTable
	}
	var count int
	err := app.DB().NewQuery("SELECT COUNT(*) FROM {{" + table + "}} WHERE [[job_id]] = {:jobId} AND [[expanded]] = FALSE").
		Bind(dbx.Params{"jobId": jobID}).Row(&count)
	return count, err
}
