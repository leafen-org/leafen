package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// interactiveAutomationUndoRow is what a button's run did to one row, for
// the notice's "실행 취소": it takes back the rows the run made and the values
// it set, leaving a value changed since, as Notion's does (seen 2026-11-05).
type interactiveAutomationUndoRow struct {
	DatabaseID string                               `json:"databaseId"`
	PageID     string                               `json:"pageId"`
	Created    bool                                 `json:"created,omitempty"`
	Before     map[string]interactiveAutomationCell `json:"before,omitempty"`
	Wrote      map[string]interactiveAutomationCell `json:"wrote,omitempty"`
}

type interactiveAutomationCell struct {
	Type  string          `json:"type"`
	Value string          `json:"value"`
	Spans []graphTextSpan `json:"spans,omitempty"`
}

func loadInteractiveAutomationRowCells(app core.App, ownerID, pageID, rowID string) (map[string]interactiveAutomationCell, error) {
	rows := []struct {
		PropertyID string `db:"property_id"`
		Type       string `db:"type"`
		Value      string `db:"value"`
		Spans      string `db:"spans"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[property_id]], [[type]], [[value]], [[spans]] FROM {{leafen_database_row_values}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[row_id]] = {:rowId}`).Bind(dbx.Params{
		"owner": ownerID, "pageId": pageID, "rowId": rowID,
	}).All(&rows); err != nil {
		return nil, err
	}
	cells := make(map[string]interactiveAutomationCell, len(rows))
	for _, row := range rows {
		cell := interactiveAutomationCell{Type: row.Type, Value: row.Value}
		if row.Spans != "" {
			_ = json.Unmarshal([]byte(row.Spans), &cell.Spans)
		}
		cells[row.PropertyID] = cell
	}
	return cells, nil
}

// noteInteractiveAutomationUndoRows keeps, before the phase writes, what each
// of its rows held; a value an earlier phase set keeps what it held first.
func noteInteractiveAutomationUndoRows(app core.App, authorization pageAuthorization, plan interactiveAutomationPlan, progress *interactiveAutomationProgress) error {
	for id, row := range plan.Rows {
		pageID := row.PageID
		if pageID == "" {
			pageID = authorization.Page.Id
		}
		entry, known := progress.UndoRows[id]
		if !known {
			entry = interactiveAutomationUndoRow{DatabaseID: row.DatabaseID, PageID: pageID, Created: row.Insert,
				Before: map[string]interactiveAutomationCell{}, Wrote: map[string]interactiveAutomationCell{}}
		}
		cells := map[string]interactiveAutomationCell{}
		if !entry.Created {
			var err error
			if cells, err = loadInteractiveAutomationRowCells(app, authorization.Workspace.OwnerID, pageID, id); err != nil {
				return err
			}
		}
		for propertyID, value := range row.Values {
			if _, written := entry.Wrote[propertyID]; !written {
				if held, ok := cells[propertyID]; ok {
					entry.Before[propertyID] = held
				}
			}
			entry.Wrote[propertyID] = interactiveAutomationCell{Type: value.Type, Value: value.Value, Spans: value.Spans}
		}
		if progress.UndoRows == nil {
			progress.UndoRows = map[string]interactiveAutomationUndoRow{}
		}
		progress.UndoRows[id] = entry
	}
	return nil
}

// interactiveAutomationUndoRowOperations are the writes taking back a run's
// edits, by the page holding each: a value it set returns to what it held
// unless it has changed since.
func interactiveAutomationUndoRowOperations(app core.App, ownerID string, progress *interactiveAutomationProgress) (map[string][]graphOperation, error) {
	ids := make([]string, 0, len(progress.UndoRows))
	for id := range progress.UndoRows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	operations := map[string][]graphOperation{}
	for _, id := range ids {
		entry := progress.UndoRows[id]
		position, stored := "", ""
		err := app.DB().NewQuery(`SELECT [[position]], COALESCE([[subitem_parent_id]], '') FROM {{leafen_database_rows}}
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:databaseId} AND [[id]] = {:id}
				AND `+databaseRowNotTrashedSQL("[[id]]")).Bind(dbx.Params{
			"owner": ownerID, "pageId": entry.PageID, "databaseId": entry.DatabaseID, "id": id,
		}).Row(&position, &stored)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if entry.Created {
			continue
		}
		cells, err := loadInteractiveAutomationRowCells(app, ownerID, entry.PageID, id)
		if err != nil {
			return nil, err
		}
		values := map[string]graphRowValue{}
		for propertyID, wrote := range entry.Wrote {
			if cells[propertyID].Value != wrote.Value {
				continue
			}
			if before, held := entry.Before[propertyID]; held {
				values[propertyID] = graphRowValue{Type: before.Type, Value: json.RawMessage(before.Value), Spans: before.Spans}
			} else if cleared, valid := interactiveAutomationValueForType(graphRowValueCleared, wrote.Type); valid {
				values[propertyID] = graphRowValue{Type: wrote.Type, Value: cleared}
			}
		}
		if len(values) == 0 {
			continue
		}
		operations[entry.PageID] = append(operations[entry.PageID], graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: id, DataSourceID: entry.DatabaseID, Position: position, SubitemParentID: optionalDatabaseRowParent(stored), Values: values,
		}})
	}
	return operations, nil
}

// trashInteractiveAutomationUndoRows takes back the rows the run made: to
// trash, as a page deleted here goes; one gone already stays gone.
func trashInteractiveAutomationUndoRows(ctx context.Context, ygo *pocketBaseYgoPersistence, principalID, executionID string, progress *interactiveAutomationProgress) error {
	ids := make([]string, 0, len(progress.UndoRows))
	for id, entry := range progress.UndoRows {
		if entry.Created {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		authority, err := authorizeDatabaseRowPageInTrash(ygo.app, principalID, id, permissionWorkspaceWrite)
		if errors.Is(err, errDatabaseRowPageNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if authority.TrashedAt != 0 {
			continue
		}
		if _, err := ygo.changeDatabaseRowTrash(ctx, principalID, id, databaseRowPageActionInput{
			RequestID: automationStableID("hbt", executionID, id), ExpectedAuthorityRevision: authority.AuthorityRevision,
		}, true); err != nil {
			return err
		}
	}
	return nil
}
