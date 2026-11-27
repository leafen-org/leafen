package app

import (
	"encoding/json"
	"math"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Deleting the rows of a table that a condition picks reads the table on a
// read connection, by rowid a window at a time, and deletes each window's
// picked rows in one short write that asks the condition again of those
// rows alone: the write lock is held for the rows that go, never while the
// table is read, and a row a change made meanwhile keeps stays. A window
// with none picked writes nothing.

// retireWindowRows is how many rows a window reads by default, and so the
// most rows one short write deletes.
const retireWindowRows = 256

// retireAllBoundRows deletes every row of a table that the condition, naming
// the bound values, picks, a default window at a time.
func retireAllBoundRows(app core.App, table string, picked func(alias string) string, bound dbx.Params) error {
	_, err := retireBoundRowsByWindow(app, table, picked, bound, retireWindowRows, math.MaxInt)
	return err
}

// retireBoundRowsByWindow deletes the rows of a table that the condition,
// given a row's alias and naming values, picks, up to limit of them, and
// reports whether it stopped before the table's end: bound holds the values,
// by any names but after, window and rows.
func retireBoundRowsByWindow(
	app core.App, table string, picked func(alias string) string, bound dbx.Params, window, limit int,
) (bool, error) {
	with := func(params dbx.Params) dbx.Params {
		for name, value := range bound {
			params[name] = value
		}
		return params
	}
	var after int64
	gone := 0
	for {
		rows := []struct {
			Row    int64 `db:"row"`
			Picked bool  `db:"picked"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[listed]].rowid AS [[row]], ` + picked("[[listed]]") + ` AS [[picked]]
			FROM {{` + table + `}} AS [[listed]]
			WHERE [[listed]].rowid > {:after} ORDER BY [[listed]].rowid LIMIT {:window}
		`).Bind(with(dbx.Params{"after": after, "window": window})).All(&rows); err != nil {
			return true, err
		}
		ids, full := []int64{}, false
		for _, row := range rows {
			if row.Picked {
				if full = gone+len(ids) == limit; full {
					break
				}
				ids = append(ids, row.Row)
			}
		}
		if len(ids) > 0 {
			encoded, err := json.Marshal(ids)
			if err != nil {
				return true, err
			}
			result, err := app.DB().NewQuery(`
				DELETE FROM {{` + table + `}} AS [[listed]]
				WHERE [[listed]].rowid IN (SELECT [[value]] FROM json_each({:rows})) AND ` + picked("[[listed]]") + `
			`).Bind(with(dbx.Params{"rows": string(encoded)})).Execute()
			if err != nil {
				return true, err
			}
			deleted, err := result.RowsAffected()
			if err != nil {
				return true, err
			}
			gone += int(deleted)
		}
		if full {
			return true, nil
		}
		if len(rows) < window {
			return false, nil
		}
		if gone >= limit {
			return true, nil
		}
		after = rows[len(rows)-1].Row
	}
}
