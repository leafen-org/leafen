package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database item's page whose sharing is unlinked from its database's, as
// Notion's "변경 후 연결 해제" on an item (seen 2026-10-13): what the
// database's page gives no longer opens it. Its own shares do, with its
// teamspace's roles on it and the private owner of the page its database
// sits in, and the database's views show it to each person by that role —
// not at all without one, its values read-only below edit.

// authorizeUnlinkedRowPage authorizes an unlinked item's page as a page of
// its own: its teamspace's role on it and its private owner — those of the
// page its database sits in, which its record carries
// (schema_baseline.json, private_pages_schema.go) — and its own
// shares.
func authorizeUnlinkedRowPage(
	app core.App, principalID string, rowPage *core.Record, minimum pageAccessRole,
) (pageAuthorization, error) {
	if principalID == "" || minimum <= pageAccessRoleNone {
		return pageAuthorization{}, permissionDenied()
	}
	// Its database is still there, out of the trash.
	if _, err := databaseRowPageContainer(app, rowPage); err != nil {
		return pageAuthorization{}, err
	}
	workspaceID := rowPage.GetString("workspace")
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	if workspace.GetString("deletion_job_id") != "" || rowPage.GetString("owner") != workspace.GetString("owner") {
		return pageAuthorization{}, permissionDenied()
	}
	role := pageAccessRoleNone
	authority, err := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead)
	if err == nil {
		role = privatePageAccessRole(rowPage, principalID)
		if teamspaceID := rowPage.GetString("teamspace_id"); teamspaceID != "" {
			teamspaceAuthority, teamspaceRole, err := effectiveTeamspaceRoleOnPage(app, principalID, workspaceID, teamspaceID, rowPage.Id)
			if err != nil && !isPermissionDenied(err) {
				return pageAuthorization{}, err
			}
			if err == nil {
				authority, role = teamspaceAuthority, teamspaceRole
			}
		}
		// Its shares may give a member more (decidePersonPageRole).
		if role >= minimum {
			if shared, wider, err := pageSharesWiden(app, workspaceID, rowPage.Id, principalID, role); err != nil {
				return pageAuthorization{}, err
			} else if wider {
				role = shared
			}
			return pageAuthorization{Workspace: authority, Page: rowPage, AccessRole: role}, nil
		}
	} else if !isPermissionDenied(err) {
		return pageAuthorization{}, err
	}
	grantWorkspace, err := authorizePageGrantWorkspace(app, principalID, workspace)
	if err != nil {
		return pageAuthorization{}, err
	}
	grantRole, err := effectiveUserPageAccessRole(app, workspaceID, rowPage.Id, principalID)
	if err != nil {
		return pageAuthorization{}, err
	}
	if grantRole > role {
		authority, role = grantWorkspace, grantRole
	}
	if role < minimum {
		return pageAuthorization{}, permissionDenied()
	}
	return pageAuthorization{Workspace: authority, Page: rowPage, AccessRole: role}, nil
}

// databaseRowAccess is one reader's role in each unlinked item of some data
// sources; the other items follow their database.
type databaseRowAccess struct {
	roles map[string]pageAccessRole
}

// loadDatabaseRowAccess reads the reader's role in the data sources'
// unlinked items; nil when none of their items is unlinked. A visitor through
// a public link ("") holds none: an unlinked item left the published graphs
// above it (public_share_graph_upkeep.go), and Notion's site leaves it out
// of the database's views too (seen 2026-11-11: the table and board drop it
// and its own address says the page isn't found).
func loadDatabaseRowAccess(app core.App, readerID string, dataSourceIDs []string) (*databaseRowAccess, error) {
	if len(dataSourceIDs) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(dataSourceIDs)
	if err != nil {
		return nil, err
	}
	rowIDs := []string{}
	// From the databases to their workspaces' unlinked pages, each by key:
	// given the nightly statistics, SQLite's own order read every page
	// (2026-10-18).
	if err := app.DB().NewQuery(`
		SELECT [[unlink]].[[page_id]] FROM {{leafen_data_sources}} AS [[source]]
		CROSS JOIN {{leafen_page_share_unlinks}} [[unlink]] ON [[unlink]].[[workspace_id]] = [[source]].[[workspace_id]]
		CROSS JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[unlink]].[[page_id]] AND [[page]].[[data_source_id]] = [[source]].[[id]]
		WHERE [[source]].[[id]] IN (SELECT [[value]] FROM json_each({:sources}))
		ORDER BY [[unlink]].[[page_id]]
	`).Bind(dbx.Params{"sources": string(encoded)}).Column(&rowIDs); err != nil {
		return nil, err
	}
	return newDatabaseRowAccess(app, readerID, rowIDs)
}

// newDatabaseRowAccess is the reader's role in the unlinked items named;
// nil when there are none.
func newDatabaseRowAccess(app core.App, readerID string, rowIDs []string) (*databaseRowAccess, error) {
	if len(rowIDs) == 0 {
		return nil, nil
	}
	access := &databaseRowAccess{roles: make(map[string]pageAccessRole, len(rowIDs))}
	if readerID == "" {
		for _, rowID := range rowIDs {
			access.roles[rowID] = pageAccessRoleNone
		}
		return access, nil
	}
	for _, rowID := range rowIDs {
		page, err := app.FindRecordById(pagesCollection, rowID)
		if errors.Is(err, sql.ErrNoRows) {
			access.roles[rowID] = pageAccessRoleNone
			continue
		}
		if err != nil {
			return nil, err
		}
		authorization, err := authorizeUnlinkedRowPage(app, readerID, page, pageAccessRoleView)
		if isPermissionDenied(err) {
			access.roles[rowID] = pageAccessRoleNone
			continue
		}
		if err != nil {
			return nil, err
		}
		access.roles[rowID] = authorization.AccessRole
	}
	return access, nil
}

// role is the reader's role in an unlinked item, and whether it is one.
func (access *databaseRowAccess) role(rowID string) (pageAccessRole, bool) {
	if access == nil {
		return pageAccessRoleNone, false
	}
	role, unlinked := access.roles[rowID]
	return role, unlinked
}

// hides is whether the item is unlinked and the reader has no role in it.
func (access *databaseRowAccess) hides(rowID string) bool {
	role, unlinked := access.role(rowID)
	return unlinked && role == pageAccessRoleNone
}

// readOnly is whether the item is unlinked and the reader may not edit it.
func (access *databaseRowAccess) readOnly(rowID string) bool {
	role, unlinked := access.role(rowID)
	return unlinked && role < pageAccessRoleEdit
}

func unlinkedRowDeniedError() error {
	return &graphRequestError{
		status: http.StatusForbidden, code: errorCodeGraphRowAccessDenied,
		message: "You can't change this item.",
	}
}

// refuseUnlinkedRowChanges refuses a change to an unlinked item by whoever
// may not edit it: any change where they have no role in it, else its
// values changing or the item going (its place among the others is the
// view's to change).
func refuseUnlinkedRowChanges(app core.App, principalID string, operations []graphOperation) error {
	ids := []string{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			ids = append(ids, operation.Row.ID)
		case operation.Kind == graphOperationKindDeleteRow && operation.RowDeletion != nil:
			ids = append(ids, operation.RowDeletion.ID)
		}
	}
	access, err := unlinkedRowAccessFor(app, principalID, ids)
	if err != nil || access == nil {
		return err
	}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			if access.hides(operation.Row.ID) {
				return unlinkedRowDeniedError()
			}
			if !access.readOnly(operation.Row.ID) {
				continue
			}
			var source string
			if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": operation.Row.ID}).Row(&source); err != nil {
				return err
			}
			if source != operation.Row.DataSourceID {
				return unlinkedRowDeniedError()
			}
			changed, err := graphRowValuesChange(app, operation.Row)
			if err != nil {
				return err
			}
			if changed {
				return unlinkedRowDeniedError()
			}
		case operation.Kind == graphOperationKindDeleteRow && operation.RowDeletion != nil:
			if access.readOnly(operation.RowDeletion.ID) {
				return unlinkedRowDeniedError()
			}
		}
	}
	return nil
}

// refuseUnlinkedRowDeletion refuses an item's deletion by whoever may not
// edit it, where it is unlinked.
func refuseUnlinkedRowDeletion(app core.App, principalID, rowID string) error {
	access, err := unlinkedRowAccessFor(app, principalID, []string{rowID})
	if err != nil || access == nil {
		return err
	}
	if access.readOnly(rowID) {
		return unlinkedRowDeniedError()
	}
	return nil
}

// unlinkedRowAccessFor reads the principal's role in the unlinked items of
// the data sources the given items are in.
func unlinkedRowAccessFor(app core.App, principalID string, rowIDs []string) (*databaseRowAccess, error) {
	if len(rowIDs) == 0 || principalID == "" {
		return nil, nil
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	sources := []string{}
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[data_source_id]] FROM {{leafen_database_rows}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) ORDER BY [[data_source_id]]
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&sources); err != nil {
		return nil, err
	}
	return loadDatabaseRowAccess(app, principalID, sources)
}

// unlinkedPageIDs are those of the pages whose sharing is unlinked from
// their parent's.
func unlinkedPageIDs(app core.App, pageIDs []string) (map[string]bool, error) {
	unlinked := map[string]bool{}
	if len(pageIDs) == 0 {
		return unlinked, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_page_share_unlinks}} WHERE [[page_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		unlinked[id] = true
	}
	return unlinked, nil
}
