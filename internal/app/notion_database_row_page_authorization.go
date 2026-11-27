package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionDataSourceParentAuthority struct {
	ContainerID string `db:"container_id"`
	OwnerID     string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	DataSource  string `db:"data_source_id"`
	Title       string `db:"title"`
	CreatedAt   int64  `db:"created"`
	UpdatedAt   int64  `db:"updated"`
}

type notionDatabaseRowPageAuthorization struct {
	Identity  notionOAuthBearerIdentity
	Authority databaseRowPageAuthority
	Container notionDataSourceParentAuthority
	Page      pageAuthorization
}

type notionDatabaseRowPagePublicIdentities struct {
	PageID       string
	DatabaseID   string
	DataSourceID string
}

func authorizeNotionDatabaseRowPage(
	event *core.RequestEvent,
	publicPageID, requiredScope string,
	requirement permissionRequirement,
) (notionDatabaseRowPageAuthorization, bool, error) {
	return authorizeNotionDatabaseRowPageState(event, publicPageID, requiredScope, requirement, notionRowOutOfTrash)
}

// notionRowTrashReach is how far into the trash a row's authorization
// reaches.
type notionRowTrashReach uint8

const (
	// notionRowOutOfTrash reaches a row out of the trash; a read also one in
	// its own trash.
	notionRowOutOfTrash notionRowTrashReach = iota
	// notionRowOwnTrash also admits a write to a row in its own trash, for
	// the request that changes its trash state.
	notionRowOwnTrash
	// notionRowAnyTrash also reaches a row under its database, or a page, in
	// the trash: for the Public API's read of it, or a change the trash
	// refuses next (notion_trash_reads.go).
	notionRowAnyTrash
)

// authorizeNotionDatabaseRowPageState authorizes a row page as far into the
// trash as reach goes.
func authorizeNotionDatabaseRowPageState(
	event *core.RequestEvent,
	publicPageID, requiredScope string,
	requirement permissionRequirement,
	reach notionRowTrashReach,
) (notionDatabaseRowPageAuthorization, bool, error) {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, requiredScope, requirement)
	if err != nil || !ok {
		return notionDatabaseRowPageAuthorization{}, false, err
	}
	identity, err := resolveNotionObjectIdentity(
		event.App, notionObjectIdentityRowPage, publicPageID,
	)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionDatabaseRowPageAuthorization{}, false, notionPageNotFound(event)
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	authorization, err := authorizeNotionDatabaseRowPageCurrentState(
		event.App, workspace.Identity, identity.InternalID, requirement, reach,
	)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return notionDatabaseRowPageAuthorization{}, false, notionPageNotFound(event)
	}
	if errors.Is(err, errDatabaseRowPageInTrash) {
		return notionDatabaseRowPageAuthorization{}, false, notionDatabaseRowPageInTrashError(event)
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	return authorization, true, nil
}

func authorizeNotionDatabaseRowPageCurrent(
	app core.App,
	identity notionOAuthBearerIdentity,
	rowID string,
	requirement permissionRequirement,
) (notionDatabaseRowPageAuthorization, error) {
	return authorizeNotionDatabaseRowPageCurrentState(app, identity, rowID, requirement, notionRowOutOfTrash)
}

func authorizeNotionDatabaseRowPageCurrentState(
	app core.App,
	identity notionOAuthBearerIdentity,
	rowID string,
	requirement permissionRequirement,
	reach notionRowTrashReach,
) (notionDatabaseRowPageAuthorization, error) {
	authorize := authorizeDatabaseRowPage
	switch reach {
	case notionRowOwnTrash:
		authorize = authorizeDatabaseRowPageInTrash
	case notionRowAnyTrash:
		authorize = authorizeDatabaseRowPageAnyTrash
	}
	authority, err := authorize(
		app, identity.PrincipalID, strings.TrimSpace(rowID), requirement,
	)
	if errors.Is(err, errDatabaseRowPageNotFound) ||
		err == nil && authority.WorkspaceID != identity.WorkspaceID {
		return notionDatabaseRowPageAuthorization{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	pending, err := notionDatabaseRowPageDeletionPending(app, authority)
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	if pending {
		return notionDatabaseRowPageAuthorization{}, errDatabaseRowPageNotFound
	}
	load := loadNotionDataSourceParentAuthority
	if reach == notionRowAnyTrash {
		load = loadNotionDataSourceParentAuthorityAnyTrash
	}
	container, err := load(app, authority.WorkspaceID, authority.DataSourceID)
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	page, err := authorizePageBody(app, identity.PrincipalID, container.PageID, requirement)
	// The row's own authority above decided the write — a row shared to
	// edit — so the page holding its database need only be read, as on the
	// screen.
	if isPermissionDenied(err) && requirement != permissionWorkspaceRead {
		page, err = authorizePageBody(app, identity.PrincipalID, container.PageID, permissionWorkspaceRead)
	}
	// A connection reaches a row through its database, or the row itself
	// put in its pages from the row's "연결" menu.
	if isPermissionDenied(err) || err == nil && (page.Page.GetBool("in_trash") && reach != notionRowAnyTrash ||
		page.Workspace.WorkspaceID != identity.WorkspaceID ||
		!notionOAuthIdentityAllowsPage(app, identity, page) && identity.ResourceIDs[authority.RowID] != "page") {
		return notionDatabaseRowPageAuthorization{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	return notionDatabaseRowPageAuthorization{
		Identity: identity, Authority: authority, Container: container, Page: page,
	}, nil
}

// notionDatabaseRowPageDeletionPending is whether a change pending on the
// row's database removes it, once a read scope (access_memo.go;
// primeRelationRowsRemembered).
func notionDatabaseRowPageDeletionPending(
	app core.App,
	authority databaseRowPageAuthority,
) (bool, error) {
	key := rowDeletionPendingMemoKey{authority.OwnerID, authority.ContainerPageID, authority.RowID}
	return remembered(app, key, func() (bool, error) {
		var count int
		err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_mutation_jobs}}
			WHERE [[owner_id]]={:ownerId} AND [[page_id]]={:pageId}
				AND [[target_kind]]='row' AND [[target_id]]={:rowId}
				AND [[status]]='pending'
		`).Bind(dbx.Params{
			"ownerId": authority.OwnerID, "pageId": authority.ContainerPageID,
			"rowId": authority.RowID,
		}).Row(&count)
		return count != 0, err
	})
}

type rowDeletionPendingMemoKey struct{ ownerID, pageID, rowID string }

func authorizeNotionDataSourceParent(
	event *core.RequestEvent,
	publicDataSourceID, requiredScope string,
	requirement permissionRequirement,
) (notionDatabaseRowPageAuthorization, bool, error) {
	return authorizeNotionDataSourceParentBy(event, publicDataSourceID, requiredScope, requirement,
		func(app core.App, identity notionOAuthBearerIdentity, dataSourceID string) (notionDatabaseRowPageAuthorization, error) {
			return authorizeNotionDataSourceParentCurrent(app, identity, dataSourceID, requirement)
		})
}

// authorizeNotionDataSourceParentBy is authorizeNotionDataSourceParent with
// the data source judged by current: a new item's, say
// (authorizeNotionDataSourceItemParent).
func authorizeNotionDataSourceParentBy(
	event *core.RequestEvent,
	publicDataSourceID, requiredScope string,
	requirement permissionRequirement,
	current func(core.App, notionOAuthBearerIdentity, string) (notionDatabaseRowPageAuthorization, error),
) (notionDatabaseRowPageAuthorization, bool, error) {
	noteNotionAskedObject(event, "data_source", publicDataSourceID)
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, requiredScope, requirement)
	if err != nil || !ok {
		return notionDatabaseRowPageAuthorization{}, false, err
	}
	identity, err := resolveNotionObjectIdentity(
		event.App, notionObjectIdentityDataSource, strings.TrimSpace(publicDataSourceID),
	)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionDatabaseRowPageAuthorization{}, false, notionPageNotFound(event)
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	authorization, err := current(event.App, workspace.Identity, identity.InternalID)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return notionDatabaseRowPageAuthorization{}, false, notionPageNotFound(event)
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	return authorization, true, nil
}

func authorizeNotionDataSourceParentCurrent(
	app core.App,
	identity notionOAuthBearerIdentity,
	dataSourceID string,
	requirement permissionRequirement,
) (notionDatabaseRowPageAuthorization, error) {
	return authorizeNotionDataSourceParentReach(app, identity, dataSourceID, requirement, false)
}

// authorizeNotionDataSourceParentReach with anyTrash also reaches a data
// source in the trash or under a page in it (notion_trash_reads.go).
func authorizeNotionDataSourceParentReach(
	app core.App,
	identity notionOAuthBearerIdentity,
	dataSourceID string,
	requirement permissionRequirement,
	anyTrash bool,
) (notionDatabaseRowPageAuthorization, error) {
	load := loadNotionDataSourceParentAuthority
	if anyTrash {
		load = loadNotionDataSourceParentAuthorityAnyTrash
	}
	container, err := load(app, identity.WorkspaceID, strings.TrimSpace(dataSourceID))
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	page, err := authorizePageBody(app, identity.PrincipalID, container.PageID, requirement)
	if isPermissionDenied(err) || err == nil && (page.Page.GetBool("in_trash") && !anyTrash ||
		page.Workspace.WorkspaceID != identity.WorkspaceID ||
		!notionOAuthIdentityAllowsPage(app, identity, page)) {
		return notionDatabaseRowPageAuthorization{}, errDatabaseRowPageNotFound
	}
	if err != nil {
		return notionDatabaseRowPageAuthorization{}, err
	}
	return notionDatabaseRowPageAuthorization{
		Identity: identity, Container: container, Page: page,
	}, nil
}

// authorizeNotionDataSourceItemParent is the data source a new item goes
// in: one whose page the person may edit.
func authorizeNotionDataSourceItemParent(
	app core.App,
	identity notionOAuthBearerIdentity,
	dataSourceID string,
) (notionDatabaseRowPageAuthorization, error) {
	return authorizeNotionDataSourceItemParentReach(app, identity, dataSourceID, false)
}

// authorizeNotionDataSourceItemParentReach with anyTrash also takes a data
// source in the trash or under a page in it, which the Public API makes an
// item in, in the trash with it (notion_trash_reads.go).
func authorizeNotionDataSourceItemParentReach(
	app core.App,
	identity notionOAuthBearerIdentity,
	dataSourceID string,
	anyTrash bool,
) (notionDatabaseRowPageAuthorization, error) {
	return authorizeNotionDataSourceParentReach(app, identity, dataSourceID, permissionWorkspaceWrite, anyTrash)
}

// loadNotionDataSourceParentAuthority reads a data source's database out of
// the trash, once a read scope (access_memo.go): an agent's answer judges
// every related row it names, most of them of a few databases (2026-11-12:
// read again for each of 500 rows).
func loadNotionDataSourceParentAuthority(
	app core.App,
	workspaceID, dataSourceID string,
) (notionDataSourceParentAuthority, error) {
	return remembered(app, notionDataSourceParentMemoKey{workspaceID, dataSourceID}, func() (notionDataSourceParentAuthority, error) {
		return readNotionDataSourceParentAuthority(app, workspaceID, dataSourceID)
	})
}

type notionDataSourceParentMemoKey struct{ workspaceID, dataSourceID string }

func readNotionDataSourceParentAuthority(
	app core.App,
	workspaceID, dataSourceID string,
) (notionDataSourceParentAuthority, error) {
	rows := []notionDataSourceParentAuthority{}
	err := app.DB().NewQuery(`
		SELECT [[container]].[[id]] AS [[container_id]], [[container]].[[owner]],
			[[container]].[[workspace_id]], [[container]].[[page_id]],
			[[source]].[[id]] AS [[data_source_id]], [[source]].[[title]],
			[[source]].[[created]], [[source]].[[updated]]
		FROM {{leafen_data_sources}} AS [[source]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[data_source_id]]=[[source]].[[id]]
			AND [[membership]].[[owner]]=[[source]].[[owner]]
			AND [[membership]].[[workspace_id]]=[[source]].[[workspace_id]]
			AND [[membership]].[[source_container_id]]=''
		JOIN {{leafen_database_containers}} AS [[container]]
			ON [[container]].[[id]]=[[membership]].[[container_id]]
			AND [[container]].[[owner]]=[[source]].[[owner]]
			AND [[container]].[[workspace_id]]=[[source]].[[workspace_id]]
			AND [[container]].[[page_id]]=[[source]].[[page_id]]
			AND [[container]].[[kind]]='source'
		WHERE [[source]].[[workspace_id]]={:workspaceId} AND [[source]].[[id]]={:dataSourceId}
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
				WHERE [[trash]].[[container_id]]=[[container]].[[id]])
			AND NOT ` + databaseSourceTrashedInPlaceSQL("[[source]].[[id]]") + `
		ORDER BY [[container]].[[id]] LIMIT 2
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "dataSourceId": dataSourceID,
	}).All(&rows)
	if err != nil {
		return notionDataSourceParentAuthority{}, err
	}
	if len(rows) != 1 {
		return notionDataSourceParentAuthority{}, errDatabaseRowPageNotFound
	}
	return rows[0], nil
}

func ensureNotionDatabaseRowPagePublicIdentities(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
) (notionDatabaseRowPagePublicIdentities, error) {
	container := authorization.Container
	if container.ContainerID == "" {
		var err error
		container, err = loadNotionDataSourceParentAuthority(
			app, authorization.Authority.WorkspaceID, authorization.Authority.DataSourceID,
		)
		if err != nil {
			return notionDatabaseRowPagePublicIdentities{}, err
		}
	}
	pageID, err := ensureNotionObjectIdentity(
		app, notionObjectIdentityRowPage, container.WorkspaceID, authorization.Authority.RowID,
	)
	if err != nil {
		return notionDatabaseRowPagePublicIdentities{}, err
	}
	databaseID, err := ensureNotionObjectIdentity(
		app, notionObjectIdentityDatabase, container.WorkspaceID, container.ContainerID,
	)
	if err != nil {
		return notionDatabaseRowPagePublicIdentities{}, err
	}
	dataSourceID, err := ensureNotionObjectIdentity(
		app, notionObjectIdentityDataSource, container.WorkspaceID, container.DataSource,
	)
	if err != nil {
		return notionDatabaseRowPagePublicIdentities{}, err
	}
	return notionDatabaseRowPagePublicIdentities{
		PageID: pageID, DatabaseID: databaseID, DataSourceID: dataSourceID,
	}, nil
}

func resolveNotionDataSourceInternalID(
	app core.App,
	workspaceID, publicID string,
) (string, error) {
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDataSource, publicID)
	if err != nil {
		return "", err
	}
	if identity.Workspace != workspaceID {
		return "", errInvalidNotionObjectIdentity
	}
	return identity.InternalID, nil
}

func notionDatabaseRowPageInTrashError(event *core.RequestEvent) error {
	return notionPageValidationError(event, notionArchivedCommentMessage)
}
