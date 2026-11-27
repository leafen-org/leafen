package app

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An ID property's allocator keeps the next number of its database and how
// far it has read and numbered the database's rows.

const (
	maxDatabaseUniqueIDValue   int64 = 9007199254740991
	databaseUniqueIDScanWindow       = 64
)

var errDatabaseUniqueIDAllocatorInvalid = errors.New("the unique ID allocator is invalid")

type storedDatabaseUniqueIDAllocator struct {
	Owner            string `db:"owner"`
	WorkspaceID      string `db:"workspace_id"`
	PageID           string `db:"page_id"`
	DataSourceID     string `db:"data_source_id"`
	PropertyID       string `db:"property_id"`
	PropertyRevision int64  `db:"property_revision"`
	ScanAfter        string `db:"scan_after"`
	NextValue        int64  `db:"next_value"`
	MaximumSeen      int64  `db:"maximum_seen"`
	Initialized      bool   `db:"initialized"`
	UpdatedAt        int64  `db:"updated_at"`
}

type storedDatabaseUniqueIDScanValue struct {
	RowID string `db:"row_id"`
	Value string `db:"value"`
}

func loadDatabaseUniqueIDAllocator(
	app core.App,
	work storedDatabaseUniqueIDWork,
) (storedDatabaseUniqueIDAllocator, bool, error) {
	rows := []storedDatabaseUniqueIDAllocator{}
	err := app.DB().NewQuery(`
		SELECT [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[property_id]],
			[[property_revision]], [[scan_after]], [[next_value]], [[maximum_seen]],
			[[initialized]], [[updated_at]]
		FROM {{leafen_database_unique_id_allocators}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[property_id]] = {:propertyId}
		LIMIT 1
	`).Bind(dbx.Params{
		"owner": work.Owner, "pageId": work.PageID, "propertyId": work.PropertyID,
	}).All(&rows)
	if err != nil || len(rows) == 0 {
		return storedDatabaseUniqueIDAllocator{}, false, err
	}
	row := rows[0]
	if row.WorkspaceID != work.WorkspaceID || row.DataSourceID != work.DataSourceID ||
		row.NextValue < 1 || row.MaximumSeen < 0 ||
		row.NextValue > maxDatabaseUniqueIDValue+1 || row.MaximumSeen > maxDatabaseUniqueIDValue ||
		row.Initialized && row.NextValue != row.MaximumSeen+1 {
		return storedDatabaseUniqueIDAllocator{}, false, errDatabaseUniqueIDAllocatorInvalid
	}
	return row, true, nil
}

// loadDatabaseUniqueIDScanWindow reads one more than a window of an ID
// property's stored numbers after a row, in row ID order.
func loadDatabaseUniqueIDScanWindow(
	app core.App,
	work storedDatabaseUniqueIDWork,
	after string,
) ([]storedDatabaseUniqueIDScanValue, error) {
	rows := []storedDatabaseUniqueIDScanValue{}
	err := app.DB().NewQuery(`
		SELECT [[value]].[[row_id]], [[value]].[[value]]
		FROM {{leafen_database_row_values}} [[value]]
		JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]] = [[value]].[[row_id]]
		WHERE [[value]].[[owner]] = {:owner} AND [[value]].[[page_id]] = {:pageId}
			AND [[value]].[[property_id]] = {:propertyId} AND [[value]].[[row_id]] > {:after}
			AND [[row]].[[data_source_id]] = {:sourceId}
		ORDER BY [[value]].[[row_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": work.Owner, "pageId": work.PageID, "propertyId": work.PropertyID,
		"after": after, "sourceId": work.DataSourceID, "limit": databaseUniqueIDScanWindow + 1,
	}).All(&rows)
	return rows, err
}

// parseDatabaseUniqueIDValue reads a stored number; anything but a whole
// number in range is not one.
func parseDatabaseUniqueIDValue(raw string) (int64, bool) {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return 0, false
	}
	number, ok := value.(float64)
	if !ok || number < 1 || number > float64(maxDatabaseUniqueIDValue) || math.Trunc(number) != number {
		return 0, false
	}
	return int64(number), true
}

// saveDatabaseUniqueIDProgress records a scan window. A scan that starts
// over also starts numbering over, so rows added meanwhile get numbers.
func saveDatabaseUniqueIDProgress(app core.App, state storedDatabaseUniqueIDAllocator) error {
	state.UpdatedAt = time.Now().UTC().UnixMilli()
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_unique_id_allocators}}
			([[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[property_id]],
			 [[property_revision]], [[scan_after]], [[next_value]], [[maximum_seen]],
			 [[initialized]], [[updated_at]])
		VALUES
			({:owner}, {:workspaceId}, {:pageId}, {:sourceId}, {:propertyId},
			 {:propertyRevision}, {:scanAfter}, {:nextValue}, {:maximumSeen},
			 {:initialized}, {:updatedAt})
		ON CONFLICT ([[owner]], [[page_id]], [[property_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[data_source_id]] = excluded.[[data_source_id]],
			[[property_revision]] = excluded.[[property_revision]],
			[[scan_after]] = excluded.[[scan_after]],
			[[next_value]] = excluded.[[next_value]],
			[[maximum_seen]] = excluded.[[maximum_seen]],
			[[initialized]] = excluded.[[initialized]],
			[[backfilled]] = CASE WHEN excluded.[[initialized]] = 1 THEN [[backfilled]] ELSE 0 END,
			[[backfill_after]] = CASE WHEN excluded.[[initialized]] = 1 THEN [[backfill_after]] ELSE '' END,
			[[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"owner": state.Owner, "workspaceId": state.WorkspaceID,
		"pageId": state.PageID, "sourceId": state.DataSourceID,
		"propertyId": state.PropertyID, "propertyRevision": state.PropertyRevision,
		"scanAfter": state.ScanAfter, "nextValue": state.NextValue,
		"maximumSeen": state.MaximumSeen, "initialized": state.Initialized,
		"updatedAt": state.UpdatedAt,
	}).Execute()
	return err
}
