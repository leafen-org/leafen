package app

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

// Reverting a connection's changes takes back what they did to the rows of
// the page's databases, as a version's restore does: a row they added goes to
// the trash, one they removed comes back from it, and one they edited takes
// the values they changed back. A row someone else changed afterwards is kept
// as it is, and the revert names it.

// pageHistoryRevertRow is what the reverted versions did to a row.
type pageHistoryRevertRow struct {
	// Change is their first change to it, and Trashed whether they left it in
	// the trash.
	Change  string
	Trashed bool
	// Types are the properties they edited, by ID, and Before those as they were.
	Types  map[string]string
	Before map[string]pageHistoryRowValue
	open   bool
}

// notePageHistoryRevertRow keeps a version's change to a row: a reverted
// version's opens or adds to what is to be taken back, another's closes it, as
// does a reverted one from before changes kept the values they replaced.
func notePageHistoryRevertRow(rows map[string]*pageHistoryRevertRow, change pageHistoryRowChange, reverted bool) {
	row := rows[change.ID]
	if !reverted || len(change.Values) > 0 && change.Before == nil {
		if row != nil {
			row.open = false
		}
		return
	}
	if row == nil || !row.open {
		row = &pageHistoryRevertRow{Change: change.Change, Types: map[string]string{}, Before: map[string]pageHistoryRowValue{}, open: true}
		rows[change.ID] = row
	}
	switch change.Change {
	case pageHistoryChangeRowTrashed:
		row.Trashed = true
	case pageHistoryChangeRowRestored:
		row.Trashed = false
	}
	for _, value := range change.Values {
		if _, noted := row.Types[value.PropertyID]; !noted {
			row.Types[value.PropertyID] = value.Type
			row.Before[value.PropertyID] = change.Before[value.PropertyID]
		}
	}
}

// revertPageHistoryRows carries out a plan's rows, a database at a time, and
// answers how many it took back. One that has gone from the page is kept.
func (p *pocketBaseYgoPersistence) revertPageHistoryRows(
	ctx context.Context, actorID, pageID, requestID string, plan *pageHistoryRevertPlan,
) (int, error) {
	ids := make([]string, 0, len(plan.Rows))
	for id := range plan.Rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	restores := map[string]*pageHistoryDatabaseRestore{}
	reverted := 0
	for _, id := range ids {
		row := plan.Rows[id]
		authority, err := readDatabaseRowPageAuthority(p.app, id)
		if errors.Is(err, sql.ErrNoRows) {
			plan.Kept = append(plan.Kept, pageHistoryRevertKept{Field: id, Reason: pageHistoryRevertChangedLater})
			continue
		}
		if err != nil {
			return 0, err
		}
		restore := restores[authority.DataSourceID]
		if restore == nil {
			restore = &pageHistoryDatabaseRestore{sourceID: authority.DataSourceID}
			restores[authority.DataSourceID] = restore
		}
		took := false
		if row.Change == pageHistoryChangeRowAdded || row.Change == pageHistoryChangeRowRestored {
			if authority.TrashedAt == 0 {
				restore.rowsAway = append(restore.rowsAway, authority)
				took = true
			}
		} else {
			if row.Trashed || row.Change == pageHistoryChangeRowRemoved {
				if authority.TrashedAt != 0 {
					root, err := databaseRowTrashEventRoot(authority)
					if err != nil {
						return 0, err
					}
					restore.rowsBack = append(restore.rowsBack, root)
					took = true
				}
			}
			values, _, err := pageHistoryRowRestoreValues(row.Types, row.Before, nil)
			if err != nil {
				return 0, err
			}
			if len(values) > 0 {
				restore.rows = append(restore.rows, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
					ID: id, DataSourceID: authority.DataSourceID, SubitemParentID: optionalDatabaseRowParent(authority.SubitemParentID),
					Position: authority.Position, Values: values,
				}})
				took = true
			}
		}
		if took {
			reverted++
		}
	}
	sources := make([]string, 0, len(restores))
	for sourceID := range restores {
		sources = append(sources, sourceID)
	}
	sort.Strings(sources)
	for _, sourceID := range sources {
		if err := p.applyPageHistoryDatabaseRestore(ctx, actorID, pageID, requestID, *restores[sourceID]); err != nil {
			return 0, err
		}
	}
	return reverted, nil
}
