package app

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The pages of a data source in the trash are each a line of the trash, as
// Notion's trash lists them (seen 2026-11-12, a full-page database's second
// source, both of another's, its only one, and an inline database sent there
// whole: "H one row 1 · 한지테스트페이지 / … / H one"), placed through their
// source. Restoring one takes it out of its database to the person's private
// pages, as Notion's does after asking first, warning that the page goes to
// private pages and loses the access its deleted parent gave; deleting one
// forever deletes that row. A source is in the trash where its
// database keeps it (database_source_trash_in_place.go), with its database's
// block in the trash of deleted blocks (database_container_trash.go), or with
// its database deleted (deleted_databases.go), or with a page in the trash
// that holds its database, however deep. A row sent to the trash on its own
// before its source lists with the source's.

// trashedDataSourcesCTE names, for a WITH clause, a workspace's data sources
// in the trash as [[trashed_source]], each with when it went there and how
// many sources its database holds (an untitled one is called after that,
// databaseTitles.ts); a source in two of them lists twice. A page sent to
// the trash takes the databases in it, and in the pages under it, there with
// it, as Notion's trash then lists their pages (seen 2026-11-12: "K row 1 ·
// … / K one" beside "K one", a full-page database sent there; "P inline row
// 1 · … / P inline", "P full row 1 · … / P full" and "P sub inline row 1 ·
// … / P sub inline" beside "W674 P page", the page holding them).
func trashedDataSourcesCTE() string {
	return trashedPageTreeCTE() + `, [[trashed_source]]([[data_source_id]], [[deleted_at]], [[source_count]]) AS (
		SELECT [[kept]].[[data_source_id]], [[kept]].[[deleted_at]], (
			SELECT COUNT(*) FROM {{leafen_database_container_sources}} AS [[kept_sibling]]
			WHERE [[kept_sibling]].[[container_id]] = [[kept]].[[container_id]] AND [[kept_sibling]].[[source_container_id]] = ''
		)
		FROM {{leafen_data_source_trash_in_place}} AS [[kept]]
		JOIN {{leafen_database_containers}} AS [[kept_container]]
			ON [[kept_container]].[[id]] = [[kept]].[[container_id]] AND [[kept_container]].[[kind]] = 'source'
		WHERE [[kept_container]].[[workspace_id]] = {:workspaceId} AND [[kept]].[[expires_at]] > {:now}
			AND ` + databaseSourceTrashedInPlaceSQL("[[kept]].[[data_source_id]]") + `
		UNION ALL
		SELECT [[membership]].[[data_source_id]], [[tombstone]].[[deleted_at]], (
			SELECT COUNT(*) FROM {{leafen_database_container_sources}} AS [[trash_sibling]]
			WHERE [[trash_sibling]].[[container_id]] = [[trash]].[[container_id]] AND [[trash_sibling]].[[source_container_id]] = ''
		)
		FROM {{leafen_database_container_trash}} AS [[trash]]
		JOIN {{leafen_block_trash_tombstones}} AS [[tombstone]]
			ON [[tombstone]].[[root_block_id]] = [[trash]].[[root_block_id]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[trash]].[[container_id]] AND [[membership]].[[source_container_id]] = ''
		WHERE [[trash]].[[kind]] = 'source' AND [[trash]].[[container_id]] = [[trash]].[[root_block_id]]
			AND [[tombstone]].[[workspace_id]] = {:workspaceId} AND [[tombstone]].[[expires_at]] > {:now}
		UNION ALL
		SELECT [[listed]].[[value]], [[deleted]].[[deleted_at]], json_array_length([[deleted]].[[data_source_ids]])
		FROM {{leafen_deleted_databases}} AS [[deleted]], json_each([[deleted]].[[data_source_ids]]) AS [[listed]]
		WHERE [[deleted]].[[workspace_id]] = {:workspaceId} AND [[deleted]].[[expires_at]] > {:now}
		UNION ALL
		SELECT [[tree_source]].[[id]], [[trashed_tree]].[[deleted_at]], COALESCE((
			SELECT COUNT(*) FROM {{leafen_database_container_sources}} AS [[tree_sibling]]
			WHERE [[tree_sibling]].[[source_container_id]] = '' AND [[tree_sibling]].[[container_id]] = (
				SELECT [[tree_own]].[[container_id]] FROM {{leafen_database_container_sources}} AS [[tree_own]]
				WHERE [[tree_own]].[[data_source_id]] = [[tree_source]].[[id]] AND [[tree_own]].[[source_container_id]] = ''
				LIMIT 1)
		), 0)
		FROM [[trashed_tree]]
		JOIN {{leafen_data_sources}} AS [[tree_source]] ON [[tree_source]].[[page_id]] = [[trashed_tree]].[[id]]
		)`
}

// trashedSource is a data source in the trash, with where its rows are kept.
type trashedSource struct {
	DataSourceID string `db:"data_source_id"`
	DeletedAt    int64  `db:"deleted_at"`
	SourceCount  int    `db:"source_count"`
	Owner        string `db:"owner"`
	PageID       string `db:"page_id"`
	Title        string `db:"title"`
}

// listTrashedSourceRows reads the pages of a workspace's data sources in the
// trash, newest source first from after on, up to limit, as the trash's row
// list reads its own: the sources in the trash first, then up to limit rows
// of each in its own order of the rows its database keeps, so a page reads
// as many rows as it lists, however many the workspace's databases hold.
func listTrashedSourceRows(app core.App, workspaceID string, after timeIDCursor, limit int) ([]storedDatabaseRowTrash, error) {
	sources := []trashedSource{}
	if err := app.DB().NewQuery(`
		WITH ` + trashedDataSourcesCTE() + `
		SELECT [[source]].[[id]] AS [[data_source_id]], MAX([[trashed_source]].[[deleted_at]]) AS [[deleted_at]],
			MAX([[trashed_source]].[[source_count]]) AS [[source_count]],
			[[source]].[[owner]], [[source]].[[page_id]], [[source]].[[title]]
		FROM [[trashed_source]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[trashed_source]].[[data_source_id]]
		GROUP BY [[source]].[[id]]
		ORDER BY [[deleted_at]] DESC, [[source]].[[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "now": time.Now().UTC().UnixMilli()}).All(&sources); err != nil {
		return nil, err
	}
	stored := []storedDatabaseRowTrash{}
	// Sources sent there at one moment list their rows in one order.
	for start := 0; start < len(sources) && len(stored) < limit; {
		end := start + 1
		for end < len(sources) && sources[end].DeletedAt == sources[start].DeletedAt {
			end++
		}
		at := sources[start].DeletedAt
		if after.ID != "" && at > after.At {
			start = end
			continue
		}
		afterID := ""
		if after.ID != "" && at == after.At {
			afterID = after.ID
		}
		moment := []storedDatabaseRowTrash{}
		for _, source := range sources[start:end] {
			rows, err := readTrashedSourceRows(app, workspaceID, source, afterID, limit-len(stored))
			if err != nil {
				return nil, err
			}
			moment = append(moment, rows...)
		}
		slices.SortFunc(moment, func(a, b storedDatabaseRowTrash) int { return strings.Compare(a.RowID, b.RowID) })
		stored = append(stored, moment[:min(len(moment), limit-len(stored))]...)
		start = end
	}
	return stored, nil
}

// readTrashedSourceRows reads up to limit pages of a data source in the
// trash after the row afterID, in their IDs' order, where its database keeps
// them (idx_leafen_database_row_pages_container), each with its own last
// edit, where the trash places it.
func readTrashedSourceRows(
	app core.App, workspaceID string, source trashedSource, afterID string, limit int,
) ([]storedDatabaseRowTrash, error) {
	rows := []storedDatabaseRowTrash{}
	err := app.DB().NewQuery(`
		SELECT [[row_page]].[[row_id]], [[row_page]].[[container_page_id]] AS [[page_id]], [[row_page]].[[data_source_id]],
			{:deletedAt} AS [[trashed_at]], [[row_record]].[[last_edited_by]] AS [[trashed_by]],
			[[row_record]].[[icon]], [[row_record]].[[icon_type]], [[row_page]].[[authority_revision]],
			COALESCE((
				SELECT [[value]].[[value]] FROM {{leafen_database_row_values}} AS [[value]]
				JOIN {{leafen_database_properties}} AS [[property]]
					ON [[property]].[[id]] = [[value]].[[property_id]] AND [[property]].[[type]] = 'title'
				WHERE [[value]].[[row_id]] = [[row_page]].[[row_id]] LIMIT 1
			), '') AS [[title]], FALSE AS [[template]], TRUE AS [[source_in_trash]],
			{:sourceTitle} AS [[source_title]], {:sourceCount} AS [[source_count]], [[row_page]].[[updated]] AS [[edited_at]]
		FROM {{leafen_database_row_pages}} AS [[row_page]] INDEXED BY [[idx_leafen_database_row_pages_container]]
		JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]] = [[row_page]].[[row_id]]
		WHERE [[row_page]].[[owner]] = {:owner} AND [[row_page]].[[workspace_id]] = {:workspaceId}
			AND [[row_page]].[[container_page_id]] = {:pageId} AND [[row_page]].[[data_source_id]] = {:sourceId}
			AND [[row_page]].[[row_id]] > {:afterId} AND [[row_record]].[[deletion_job_id]] = ''
			-- One sent to the trash before its source lists with it, as
			-- Notion's (seen 2026-11-12: "H one row 2 · … / H one").
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} AS [[own]]
				WHERE [[own]].[[row_id]] = [[row_page]].[[row_id]] AND [[own]].[[deleting]] = 1)
			-- One being deleted for good is gone from the trash.
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_mutation_jobs}} AS [[deleting]]
				WHERE [[deleting]].[[owner_id]] = [[row_page]].[[owner]] AND [[deleting]].[[page_id]] = [[row_page]].[[container_page_id]]
					AND [[deleting]].[[target_kind]] = 'row' AND [[deleting]].[[target_id]] = [[row_page]].[[row_id]]
					AND [[deleting]].[[status]] = 'pending'
			)
		ORDER BY [[row_page]].[[row_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": source.Owner, "workspaceId": workspaceID, "pageId": source.PageID, "sourceId": source.DataSourceID,
		"afterId": afterID, "deletedAt": source.DeletedAt, "sourceTitle": source.Title, "sourceCount": source.SourceCount,
		"limit": limit,
	}).All(&rows)
	return rows, err
}

// trashedSourceRowAuthority reads a row whose data source is in the trash and
// authorizes the person on it.
func trashedSourceRowAuthority(
	app core.App, principalID, rowID string, requirement permissionRequirement,
) (databaseRowPageAuthority, error) {
	authority, _, err := trashedSourceRowAuthorityAt(app, principalID, rowID, requirement)
	return authority, err
}

// trashedSourceRowAuthorityAt is trashedSourceRowAuthority with when its
// source went to the trash.
func trashedSourceRowAuthorityAt(
	app core.App, principalID, rowID string, requirement permissionRequirement,
) (databaseRowPageAuthority, int64, error) {
	authority, _, sentAt, err := trashedSourceRowContainer(app, principalID, rowID, requirement)
	return authority, sentAt, err
}

// trashedSourceRowContainer is trashedSourceRowAuthorityAt with the page
// holding its database, which answers for it.
func trashedSourceRowContainer(
	app core.App, principalID, rowID string, requirement permissionRequirement,
) (databaseRowPageAuthority, pageAuthorization, int64, error) {
	authority, err := readDatabaseRowPageAuthorityWhere(app, rowID, "TRUE")
	if err != nil {
		return databaseRowPageAuthority{}, pageAuthorization{}, 0, err
	}
	var sentAt int64
	if err := app.DB().NewQuery(`
		WITH ` + trashedDataSourcesCTE() + `
		SELECT COALESCE(MAX([[deleted_at]]), 0) FROM [[trashed_source]] WHERE [[data_source_id]] = {:sourceId}
	`).Bind(dbx.Params{
		"workspaceId": authority.WorkspaceID, "now": time.Now().UTC().UnixMilli(), "sourceId": authority.DataSourceID,
	}).Row(&sentAt); err != nil {
		return databaseRowPageAuthority{}, pageAuthorization{}, 0, err
	}
	if sentAt == 0 {
		return databaseRowPageAuthority{}, pageAuthorization{}, 0, errDatabaseRowPageNotFound
	}
	// The page itself sits in the trash with its source (page_ancestry.go),
	// so the page holding its database answers for it.
	container, err := authorizePageBody(app, principalID, authority.ContainerPageID, requirement)
	if isPermissionDenied(err) {
		return databaseRowPageAuthority{}, pageAuthorization{}, 0, errDatabaseRowPageNotFound
	}
	if err != nil {
		return databaseRowPageAuthority{}, pageAuthorization{}, 0, err
	}
	if container.Workspace.OwnerID != authority.OwnerID || container.Workspace.WorkspaceID != authority.WorkspaceID {
		return databaseRowPageAuthority{}, pageAuthorization{}, 0, errDatabaseRowPageNotFound
	}
	return authority, container, sentAt, nil
}

// authorizeDatabaseRowPageRead authorizes reading a row's or a template's
// own page, the page of a data source in the trash among them: the one read
// for the page (loadDatabaseRowPageWindowInTransaction) and for what shows
// beside it — its comments, address and the reader's visit — so a page
// opened from the trash shows them as a page in the trash does. inTrash says
// it is such a page.
func authorizeDatabaseRowPageRead(app core.App, principalID, rowID string) (databaseRowPageAuthority, bool, error) {
	authority, err := authorizeDatabaseRowOrTemplatePage(app, principalID, rowID, permissionWorkspaceRead)
	return readTrashedSourceRowPage(app, principalID, rowID, authority, err)
}

// authorizeTrashedSourceRowPageRecord authorizes reading the page of a data
// source in the trash as a page, which a row's own page authority leaves out
// with its source (databaseRowPageContainer): through the page holding its
// database, as authorizeDatabaseRowPageRead does.
func authorizeTrashedSourceRowPageRecord(app core.App, principalID string, rowPage *core.Record) (pageAuthorization, error) {
	_, container, _, err := trashedSourceRowContainer(app, principalID, rowPage.Id, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	return pageAuthorization{Workspace: container.Workspace, Page: rowPage, AccessRole: container.AccessRole}, nil
}

// readTrashedSourceRowPage reads, for its own page, a page whose data source
// is in the trash, which the row's own read leaves out (in place, with its
// database's block, or with its database deleted) or reads as out of the
// trash (with its database's page, or a page above it, in the trash): as a
// page in the trash since its source went there, by whoever last edited it,
// as Notion opens it from its trash (seen 2026-11-12: "H one row 1" under "…
// / H one", its band "…님이 이 페이지를 6분 전에 휴지통으로 이동했습니다." with
// 페이지 복원 and 영구 삭제하기; "P full row 1" under "… / P full", a page
// holding its database in the trash). Any other page keeps what its own read
// found.
func readTrashedSourceRowPage(
	app core.App, principalID, rowID string, found databaseRowPageAuthority, readErr error,
) (databaseRowPageAuthority, bool, error) {
	if readErr == nil {
		if found.Template {
			return found, false, nil
		}
		if trashed, err := pageInTrashPath(app, found.ContainerPageID); err != nil || !trashed {
			return found, false, err
		}
	} else if !errors.Is(readErr, errDatabaseRowPageNotFound) {
		return found, false, readErr
	}
	authority, sentAt, err := trashedSourceRowAuthorityAt(app, principalID, rowID, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return found, false, readErr
	}
	if err != nil {
		return databaseRowPageAuthority{}, false, err
	}
	// One sent there before its source keeps when and by whom.
	if authority.TrashedAt == 0 {
		authority.TrashedAt, authority.TrashedBy = sentAt, authority.LastEditedBy
	}
	return authority, true, nil
}

// restoreTrashedSourceRow takes the page of a data source in the trash out of
// its database to the person's private pages.
func (p *pocketBaseYgoPersistence) restoreTrashedSourceRow(
	event *core.RequestEvent, userID, rowID string, input databaseRowPageActionInput,
) (databaseRowPageActionResponse, error) {
	authority, err := trashedSourceRowAuthority(event.App, userID, rowID, permissionWorkspaceWrite)
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	if authority.AuthorityRevision != input.ExpectedAuthorityRevision {
		return databaseRowPageActionResponse{}, errDatabaseRowPageConflict
	}
	receipt, err := p.convertDatabaseRowToPage(event.Request.Context(), databaseRowToPageRequest{
		PrincipalID: userID, RowID: rowID, ContainerPageID: authority.ContainerPageID, FromTrashedSource: true,
		MutationID:  databaseRowPageActionIdentity("restore-from-source", userID, rowID, input.RequestID),
		RequestHash: databaseRowPageActionRequestHash("restore-from-source", rowID, input),
	})
	if err != nil {
		return databaseRowPageActionResponse{}, err
	}
	return databaseRowPageActionResponse{RowID: rowID, Duplicate: receipt.Duplicate}, nil
}

// deleteTrashedSourceRowForever deletes the page of a data source in the
// trash for good, as a row deleted from its database is, unless a legal hold
// keeps it. Like deleting a page from the trash, it needs the workspace
// owner's authority.
func deleteTrashedSourceRowForever(
	app core.App, userID, rowID string, input databaseRowPageActionInput,
) (databaseRowPageActionResponse, error) {
	response := databaseRowPageActionResponse{RowID: rowID}
	err := app.RunInTransaction(func(txApp core.App) error {
		authority, err := trashedSourceRowAuthority(txApp, userID, rowID, permissionWorkspaceOwner)
		if err != nil {
			return err
		}
		if authority.AuthorityRevision != input.ExpectedAuthorityRevision {
			return errDatabaseRowPageConflict
		}
		organizationID, err := loadLegalHoldWorkspaceOrganization(txApp, authority.WorkspaceID)
		if err != nil {
			return err
		}
		if err := assertNoActiveLegalHoldForPermanentDelete(txApp, organizationID, legalHoldDeletionTarget{
			WorkspaceID: authority.WorkspaceID, PageIDs: []string{rowID},
		}); err != nil {
			return err
		}
		requestID := "trashed-source-row-delete:" + rowID + ":" + input.RequestID
		admission, err := admitDatabaseMutationDeletionInTransaction(txApp, userID, authority.ContainerPageID, databaseMutationInput{
			RequestID: requestID, MutationID: requestID, TargetKind: databaseMutationTargetRow, TargetID: rowID,
		})
		if errors.Is(err, errDatabaseMutationNotFound) {
			return errDatabaseRowPageNotFound
		}
		if err != nil {
			return err
		}
		response.ContainerPageID, response.AuthorityRevision = authority.ContainerPageID, authority.AuthorityRevision
		response.Duplicate, response.JobID, response.Status = admission.Duplicate, admission.Job.ID, databaseRowPageDeletionPending
		if admission.Job.Status == databaseMutationStatusCompleted {
			response.Status = databaseRowPageDeletionCompleted
		}
		return nil
	})
	if err == nil {
		notifyDatabaseMutationDeletionAfterCommit(app)
	}
	return response, err
}

// trashedSourceRow reports whether the row is a page of a data source in the
// trash the person may read; its trash actions are its own, before a row's.
func trashedSourceRow(app core.App, principalID, rowID string) (bool, error) {
	_, err := trashedSourceRowAuthority(app, principalID, rowID, permissionWorkspaceRead)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return false, nil
	}
	return err == nil, err
}
