package app

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Trashing a database row, like trashing a Notion page, hides it with all of
// its sub-items until someone restores it; rows trashed together come back
// together. After the retention period, or when someone deletes it from the
// trash, the database deletion lifecycle deletes each row for good.

const (
	databaseRowTrashRetention = recoverableBlockTrashRetention
	databaseRowTrashPurgeSize = 32
)

func databaseRowTrashTime(value int64) string {
	if value == 0 {
		return ""
	}
	return time.UnixMilli(value).UTC().Format(time.RFC3339Nano)
}

// databaseRowNotTrashedSQL narrows a query to rows that are not in trash.
func databaseRowNotTrashedSQL(rowExpression string) string {
	return `NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} AS [[row_trash_marker]]
		WHERE [[row_trash_marker]].[[row_id]] = ` + rowExpression + `)`
}

// databaseRowTrashedChildrenSQL counts a row's sub-items that are in trash,
// which its stored sub-item count still holds.
func databaseRowTrashedChildrenSQL(row string) string {
	return `(SELECT COUNT(*) FROM {{leafen_database_rows}} AS [[child]]
		JOIN {{leafen_database_row_trash}} AS [[trashed_child]] ON [[trashed_child]].[[row_id]] = [[child]].[[id]]
		WHERE [[child]].[[owner]] = ` + row + `.[[owner]] AND [[child]].[[page_id]] = ` + row + `.[[page_id]]
			AND [[child]].[[data_source_id]] = ` + row + `.[[data_source_id]]
			AND [[child]].[[subitem_parent_id]] = ` + row + `.[[id]])`
}

// trashDatabaseRowEvent puts the row and those of its sub-items that are not
// in trash yet into one trash event named by the row.
func trashDatabaseRowEvent(
	app core.App,
	authority databaseRowPageAuthority,
	actorID string,
	now time.Time,
) (int, error) {
	// The walk goes through sub-items already in trash to those under them,
	// and marks each item not yet in trash, however many there are.
	tree, err := databaseRowTreeRead(app, authority, false)
	if err != nil {
		return 0, err
	}
	ids := make([]string, len(tree))
	for index, row := range tree {
		ids[index] = row.ID
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return 0, err
	}
	params := dbx.Params{
		"rowId": authority.RowID, "ids": string(encoded), "actorId": actorID,
		"now": now.UnixMilli(), "expiresAt": now.Add(databaseRowTrashRetention).UnixMilli(),
	}
	var count int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM json_each({:ids}) AS [[tree]]
		WHERE ` + databaseRowNotTrashedSQL("[[tree]].[[value]]"),
	).Bind(params).Row(&count); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, errDatabaseRowPageConflict
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_row_trash}}
			([[row_id]], [[owner]], [[workspace_id]], [[root_row_id]],
			 [[trashed_by]], [[trashed_at]], [[expires_at]], [[deleting]])
		SELECT [[row]].[[id]], [[row]].[[owner]], [[row]].[[workspace_id]],
			{:rowId}, {:actorId}, {:now}, {:expiresAt}, 0
		FROM {{leafen_database_rows}} AS [[row]]
		WHERE [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]"),
	).Bind(params).Execute(); err != nil {
		return 0, err
	}
	notifyRecoverableBlockTrashRetentionAfterCommit(app)
	recordDatabaseRowTrashWebhookEvent(app, authority, actorID, true)
	return count, touchDatabaseRowTrashEvent(app, authority.RowID, now)
}

// restoreDatabaseRowEvent brings back every row of the trash event.
func restoreDatabaseRowEvent(app core.App, rootRowID, actorID string, now time.Time) (int, error) {
	var deleting int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_row_trash}}
		WHERE [[root_row_id]] = {:rootId} AND [[deleting]] = 1
	`).Bind(dbx.Params{"rootId": rootRowID}).Row(&deleting); err != nil {
		return 0, err
	}
	if deleting != 0 {
		return 0, errDatabaseRowPageConflict
	}
	if err := touchDatabaseRowTrashEvent(app, rootRowID, now); err != nil {
		return 0, err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_row_trash_spans}} ([[row_id]], [[trashed_at]], [[restored_at]])
		SELECT [[row_id]], [[trashed_at]], MAX([[trashed_at]], {:now}) FROM {{leafen_database_row_trash}}
		WHERE [[root_row_id]] = {:rootId}
		ON CONFLICT ([[row_id]], [[trashed_at]]) DO UPDATE SET [[restored_at]] = excluded.[[restored_at]]
	`).Bind(dbx.Params{"rootId": rootRowID, "now": now.UnixMilli()}).Execute(); err != nil {
		return 0, err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_row_trash}} WHERE [[root_row_id]] = {:rootId}
	`).Bind(dbx.Params{"rootId": rootRowID}).Execute()
	if err != nil {
		return 0, err
	}
	restored, err := result.RowsAffected()
	if err == nil && restored > 0 {
		if authority, err := loadDatabaseRowPageAuthority(app, rootRowID); err == nil {
			recordDatabaseRowTrashWebhookEvent(app, authority, actorID, false)
		}
	}
	return int(restored), err
}

// touchDatabaseRowTrashEvent moves the row page revision of each row of the
// event, so open row pages read their trash state again.
func touchDatabaseRowTrashEvent(app core.App, rootRowID string, now time.Time) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_pages}}
		SET [[authority_revision]] = [[authority_revision]] + 1, [[updated]] = {:now}
		WHERE [[row_id]] IN (
			SELECT [[row_id]] FROM {{leafen_database_row_trash}}
			INDEXED BY [[idx_leafen_database_row_trash_root]]
			WHERE [[root_row_id]] = {:rootId}
		)
	`).Bind(dbx.Params{"rootId": rootRowID, "now": now.UnixMilli()}).Execute()
	return err
}

// deleteDatabaseRowTrashEvent hands every row of the trash event to the
// database deletion lifecycle, as a person or, By, the system; the rows stay
// hidden until they are gone.
func deleteDatabaseRowTrashEvent(app core.App, rootRowID, actorID string) (int, error) {
	return deleteDatabaseRowTrashEventBy(app, rootRowID, personAccessSubject(actorID))
}

func deleteDatabaseRowTrashEventBy(app core.App, rootRowID string, subject pageAccessSubject) (int, error) {
	rows := []struct {
		RowID  string `db:"row_id"`
		PageID string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[trash]].[[row_id]], [[row]].[[page_id]]
		FROM {{leafen_database_row_trash}} AS [[trash]]
		INDEXED BY [[idx_leafen_database_row_trash_root]]
		JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]] = [[trash]].[[row_id]]
		WHERE [[trash]].[[root_row_id]] = {:rootId} AND [[trash]].[[deleting]] = 0
		ORDER BY [[trash]].[[row_id]]
	`).Bind(dbx.Params{"rootId": rootRowID}).All(&rows); err != nil {
		return 0, err
	}
	for _, row := range rows {
		requestID := "row-trash-delete:" + row.RowID
		if _, err := admitDatabaseMutationDeletionInTransactionBy(app, subject, row.PageID, databaseMutationInput{
			RequestID: requestID, MutationID: requestID,
			TargetKind: databaseMutationTargetRow, TargetID: row.RowID,
		}); err != nil {
			return 0, err
		}
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_trash}} SET [[deleting]] = 1 WHERE [[root_row_id]] = {:rootId}
	`).Bind(dbx.Params{"rootId": rootRowID}).Execute(); err != nil {
		return 0, err
	}
	if len(rows) > 0 {
		notifyDatabaseMutationDeletionAfterCommit(app)
	}
	return len(rows), nil
}

// deleteExpiredDatabaseRowTrashSlice deletes the trash events whose retention
// ended, as the workspace's permission owner. A hold is judged once for each
// event whose time is up (HAVING), not for each of its rows.
func deleteExpiredDatabaseRowTrashSlice(app core.App, now int64) (int64, error) {
	events := []struct {
		RootRowID   string `db:"root_row_id"`
		WorkspaceID string `db:"workspace_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[trash]].[[root_row_id]], MIN([[trash]].[[workspace_id]]) AS [[workspace_id]]
		FROM {{leafen_database_row_trash}} AS [[trash]]
		INDEXED BY [[idx_leafen_database_row_trash_expiry]]
		WHERE [[trash]].[[deleting]] = 0 AND [[trash]].[[expires_at]] <= {:now}
		GROUP BY [[trash]].[[root_row_id]] HAVING NOT ` + databaseRowTrashHeldSQL("[[trash]].[[root_row_id]]") + `
		ORDER BY MIN([[trash]].[[expires_at]]), [[trash]].[[root_row_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"now": now, "limit": databaseRowTrashPurgeSize}).All(&events); err != nil {
		return 0, err
	}
	for _, event := range events {
		workspace, err := app.FindRecordById(workspacesCollection, event.WorkspaceID)
		if err != nil {
			return 0, err
		}
		if err := holdDatabaseRowTrashEvent(
			app, event.RootRowID, workspacePermissionOwnerID(workspace), event.WorkspaceID,
			pageTrashRetentionRequestPrefix+event.RootRowID,
		); err != nil {
			return 0, err
		}
	}
	return int64(len(events)), nil
}

// storedDatabaseRowTrash is a trashed row, or a deleted template, as the
// trash list reads it.
type storedDatabaseRowTrash struct {
	RowID             string `db:"row_id"`
	PageID            string `db:"page_id"`
	DataSourceID      string `db:"data_source_id"`
	TrashedAt         int64  `db:"trashed_at"`
	TrashedBy         string `db:"trashed_by"`
	Icon              string `db:"icon"`
	IconType          string `db:"icon_type"`
	AuthorityRevision int    `db:"authority_revision"`
	Title             string `db:"title"`
	Template          bool   `db:"template"`
	// SourceInTrash is a page of a data source in the trash
	// (database_source_trash_rows.go), SourceTitle its source's name and
	// SourceCount how many sources that source's database holds.
	SourceInTrash bool   `db:"source_in_trash"`
	SourceTitle   string `db:"source_title"`
	SourceCount   int    `db:"source_count"`
	// EditedAt is such a page's own last edit: Notion's trash places it
	// there among the other lines, not at when its source went there (seen
	// 2026-11-12: "N row 1 edited" above "N row 2", both below "W695 M2"
	// sent there before their database).
	EditedAt int64 `db:"edited_at"`
}

// listDatabaseRowTrash lists a window of the trash events of rows whose
// pages the person can read, newest first, from the cursor on. It looks at
// a bounded batch of events each time; next goes on from the last one looked
// at, "" once all are.
func listDatabaseRowTrashFor(ctx context.Context, app core.App, principalID, workspaceID, cursor string) ([]databaseRowTrashItem, string, error) {
	if _, err := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead); err != nil {
		return nil, "", err
	}
	after, err := decodeTimeIDCursor(cursor)
	if err != nil {
		return nil, "", errDatabaseRowTrashCursor
	}
	scan := databaseRowTrashListWindow * 4
	stored := []storedDatabaseRowTrash{}
	if err := app.DB().NewQuery(`
		SELECT [[trash]].[[row_id]], [[row]].[[page_id]], [[row]].[[data_source_id]],
			[[trash]].[[trashed_at]], [[trash]].[[trashed_by]], [[row_record]].[[icon]], [[row_record]].[[icon_type]],
			[[page]].[[authority_revision]],
			COALESCE((
				SELECT [[value]].[[value]] FROM {{leafen_database_row_values}} AS [[value]]
				JOIN {{leafen_database_properties}} AS [[property]]
					ON [[property]].[[id]] = [[value]].[[property_id]] AND [[property]].[[type]] = 'title'
				WHERE [[value]].[[row_id]] = [[trash]].[[row_id]] LIMIT 1
			), '') AS [[title]]
		FROM {{leafen_database_row_trash}} AS [[trash]]
		INDEXED BY [[idx_leafen_database_row_trash_workspace]]
		JOIN {{leafen_database_rows}} AS [[row]] ON [[row]].[[id]] = [[trash]].[[row_id]]
		JOIN {{leafen_database_row_pages}} AS [[page]] ON [[page]].[[row_id]] = [[trash]].[[row_id]]
		JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]] = [[trash]].[[row_id]]
		WHERE [[trash]].[[workspace_id]] = {:workspaceId} AND [[trash]].[[deleting]] = 0
			AND [[row_record]].[[deletion_job_id]] = ''
			AND [[trash]].[[expires_at]] > {:now} AND [[trash]].[[root_row_id]] = [[trash]].[[row_id]]
			AND ` + databaseSourceNotTrashedSQL("[[row]].[[data_source_id]]") + `
			AND ({:afterId} = '' OR [[trash]].[[trashed_at]] < {:afterAt}
				OR ([[trash]].[[trashed_at]] = {:afterAt} AND [[trash]].[[row_id]] > {:afterId}))
		ORDER BY [[trash]].[[trashed_at]] DESC, [[trash]].[[row_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "limit": scan + 1, "afterAt": after.At, "afterId": after.ID,
		"now": time.Now().UTC().UnixMilli(),
	}).All(&stored); err != nil {
		return nil, "", err
	}
	// Deleted templates are listed with the rows, in one order.
	templates, err := listDatabaseTemplateTrash(app, workspaceID, after, scan+1)
	if err != nil {
		return nil, "", err
	}
	stored = append(stored, templates...)
	// So are the pages of data sources in the trash.
	sourceRows, err := listTrashedSourceRows(app, workspaceID, after, scan+1)
	if err != nil {
		return nil, "", err
	}
	stored = append(stored, sourceRows...)
	slices.SortStableFunc(stored, func(a, b storedDatabaseRowTrash) int {
		if a.TrashedAt != b.TrashedAt {
			return cmp.Compare(b.TrashedAt, a.TrashedAt)
		}
		return strings.Compare(a.RowID, b.RowID)
	})
	more := len(stored) > scan
	if more {
		stored = stored[:scan]
	}
	items := []databaseRowTrashItem{}
	type pageState struct{ readable, trashed bool }
	pages := map[string]pageState{}
	// Its path names an inline database, as a search's does.
	locations := newInlineDatabaseLocations(app, principalID, workspaceID).reading(ctx)
	for index, row := range stored {
		state, known := pages[row.PageID]
		if !known {
			_, err := authorizePage(app, principalID, row.PageID, permissionWorkspaceRead)
			if err != nil && !isPermissionDenied(err) {
				return nil, "", err
			}
			state = pageState{readable: err == nil}
			if err == nil {
				if state.trashed, err = pageInTrashPath(app, row.PageID); err != nil {
					return nil, "", err
				}
			}
			pages[row.PageID] = state
		}
		// A row in a page in the trash, or under one, is a line as its
		// source's, its database having gone there with the page.
		if !state.readable || state.trashed && !row.SourceInTrash {
			continue
		}
		title := ""
		if row.Title != "" && json.Unmarshal([]byte(row.Title), &title) != nil {
			title = ""
		}
		location, err := locations.of(row.PageID, row.DataSourceID)
		if err != nil {
			return nil, "", err
		}
		item := databaseRowTrashItem{
			RowID: row.RowID, ContainerPageID: row.PageID, DataSourceID: row.DataSourceID,
			Title: title, Icon: row.Icon, IconType: row.IconType,
			TrashedAt: databaseRowTrashTime(row.TrashedAt), TrashedBy: row.TrashedBy,
			AuthorityRevision: row.AuthorityRevision,
			InlineDatabase:    location.Inline, DatabaseTitle: location.Title, Template: row.Template,
			SourceInTrash: row.SourceInTrash, SourceTitle: row.SourceTitle, SourceCount: row.SourceCount,
			EditedAt: databaseRowTrashTime(row.EditedAt),
		}
		// A row is called by its database's word for its untitled pages, and
		// so is a template with neither a title nor a name of its own.
		item.UntitledName = location.UntitledRow
		items = append(items, item)
		if len(items) == databaseRowTrashListWindow && (index+1 < len(stored) || more) {
			return items, encodeTimeIDCursor(row.TrashedAt, row.RowID), nil
		}
	}
	if more && len(stored) > 0 {
		last := stored[len(stored)-1]
		return items, encodeTimeIDCursor(last.TrashedAt, last.RowID), nil
	}
	return items, "", nil
}

var errDatabaseRowTrashCursor = errListWindowCursor

// databaseRowTrashEventRoot is the root row of the trash event the row is
// in; a row not in the trash is to be moved there first.
func databaseRowTrashEventRoot(authority databaseRowPageAuthority) (string, error) {
	if authority.TrashedAt == 0 || authority.TrashRootID == "" {
		return "", errDatabaseRowPageNotTrashed
	}
	return authority.TrashRootID, nil
}

// A row's page that the trash's actions find where they don't act: one to
// delete for good or restore that isn't in the trash. Each is still a
// conflict to callers that only ask whether the row is as they read it.
var (
	errDatabaseRowPageNotTrashed = fmt.Errorf("%w: not in Trash", errDatabaseRowPageConflict)
	errDatabaseRowPageNotInTrash = fmt.Errorf("%w: not in Trash to restore", errDatabaseRowPageConflict)
)

// enforceDatabaseRowTrashBoundary keeps a trashed row as it was trashed: a
// person restores it before changing it. The relation pair runner still
// keeps two-way relations of trashed rows in step.
func enforceDatabaseRowTrashBoundary(app core.App, operations []graphOperation) error {
	rowIDs := map[string]struct{}{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			rowIDs[operation.Row.ID] = struct{}{}
		case operation.Kind == graphOperationKindUpsertDependencyEdge && operation.DependencyEdge != nil:
			rowIDs[operation.DependencyEdge.PredecessorRowID] = struct{}{}
			rowIDs[operation.DependencyEdge.SuccessorRowID] = struct{}{}
		}
	}
	if len(rowIDs) == 0 {
		return nil
	}
	encoded, err := databaseMutationIDJSON(rowIDs)
	if err != nil {
		return err
	}
	var trashed int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_row_trash}}
		WHERE [[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))
	`).Bind(dbx.Params{"rowIds": encoded}).Row(&trashed); err != nil {
		return err
	}
	if trashed != 0 {
		return graphPreconditionError(errorCodeDatabaseRowPageInTrash, "A row in Trash can be changed after it's restored.")
	}
	return nil
}

// trashedDatabaseRowIDs returns which of the rows are in trash.
func trashedDatabaseRowIDs(app core.App, rowIDs []string) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	if len(rowIDs) == 0 {
		return result, nil
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	trashed := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]] FROM {{leafen_database_row_trash}}
		WHERE [[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))
	`).Bind(dbx.Params{"rowIds": string(encoded)}).Column(&trashed); err != nil {
		return nil, err
	}
	for _, rowID := range trashed {
		result[rowID] = struct{}{}
	}
	return result, nil
}

// importDatabaseRowTrashState keeps an imported row in trash as the archive
// had it; the importer becomes the one who trashed it, and the retention
// period runs from the time it was trashed.
func importDatabaseRowTrashState(app core.App, rowID, rootRowID, actorID, trashedAt string) error {
	trashed, err := time.Parse(time.RFC3339Nano, trashedAt)
	if err != nil {
		return workspaceImportInputError()
	}
	if rootRowID == "" {
		rootRowID = rowID
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_database_row_trash}}
			([[row_id]], [[owner]], [[workspace_id]], [[root_row_id]],
			 [[trashed_by]], [[trashed_at]], [[expires_at]], [[deleting]])
		SELECT [[id]], [[owner]], [[workspace_id]], {:rootId}, {:actorId}, {:trashedAt}, {:expiresAt}, 0
		FROM {{leafen_database_rows}} WHERE [[id]] = {:rowId}
		ON CONFLICT ([[row_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"rowId": rowID, "rootId": rootRowID, "actorId": actorID,
		"trashedAt": trashed.UnixMilli(), "expiresAt": trashed.Add(databaseRowTrashRetention).UnixMilli(),
	}).Execute()
	if err == nil {
		notifyRecoverableBlockTrashRetentionAfterCommit(app)
	}
	return err
}

// expireDatabaseRowTrash ends the retention of every row trash event of the
// workspace, for emptying the trash.
func expireDatabaseRowTrash(app core.App, workspaceID string, now int64) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_row_trash}}
		SET [[expires_at]] = MAX({:now}, [[trashed_at]] + 1)
		WHERE [[workspace_id]] = {:workspaceId} AND [[deleting]] = 0 AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "now": now}).Execute()
	if err == nil {
		notifyRecoverableBlockTrashRetentionAfterCommit(app)
	}
	return err
}
