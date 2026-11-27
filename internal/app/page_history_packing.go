package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// History written before its rows were deflated (page_history_compression.go)
// is deflated too, a turn at a time: the job runner walks the events and
// then the checkpoints by row, the waiting row of
// leafen_page_history_packing keeping where it got to, and the row goes
// when both are done. The same bytes come back from every row. The space a
// packed row gives back is reused by the database rather than returned to
// the disk.

const (
	pageHistoryPackingEventsTurn      = 500
	pageHistoryPackingCheckpointsTurn = 16
)

// pageHistoryPackingScan paces the walk: on at once while rows wait, a
// minute after a failure, done once the waiting row goes.
type pageHistoryPackingScan struct {
	pending bool
	next    time.Time
}

func (r *durableJobRunner) scanPageHistoryPacking(ctx context.Context, now time.Time, scan *pageHistoryPackingScan) {
	if !scan.pending || now.Before(scan.next) {
		return
	}
	more, err := packPageHistoryTurn(r.app)
	scan.pending, scan.next = more || err != nil, now
	if err != nil {
		scan.next = now.Add(time.Minute)
		if ctx.Err() == nil {
			logDurableJobDiagnostic(r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "", "page-history.packing", "pack", err)
		}
		return
	}
	if more {
		r.notify()
	}
}

// packPageHistoryTurn deflates one turn of history rows written before and
// says whether more wait.
func packPageHistoryTurn(app core.App) (bool, error) {
	more := false
	err := app.RunInTransaction(func(tx core.App) error {
		var eventsAfter, checkpointsAfter int64
		err := tx.DB().NewQuery(`SELECT [[events_after]], [[checkpoints_after]] FROM {{leafen_page_history_packing}} WHERE [[id]] = 1`).
			Row(&eventsAfter, &checkpointsAfter)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if eventsAfter >= 0 {
			last, err := packPageHistoryEvents(tx, eventsAfter)
			if err != nil {
				return err
			}
			more = true
			_, err = tx.DB().NewQuery(`UPDATE {{leafen_page_history_packing}} SET [[events_after]] = {:last} WHERE [[id]] = 1`).
				Bind(dbx.Params{"last": last}).Execute()
			return err
		}
		last, err := packPageHistoryCheckpoints(tx, checkpointsAfter)
		if err != nil {
			return err
		}
		if last < 0 {
			_, err = tx.DB().NewQuery(`DELETE FROM {{leafen_page_history_packing}}`).Execute()
			return err
		}
		more = true
		_, err = tx.DB().NewQuery(`UPDATE {{leafen_page_history_packing}} SET [[checkpoints_after]] = {:last} WHERE [[id]] = 1`).
			Bind(dbx.Params{"last": last}).Execute()
		return err
	})
	return more, err
}

// packPageHistoryEvents deflates the events after a row and gives the last
// row it looked at, or -1 once none are left.
func packPageHistoryEvents(app core.App, after int64) (int64, error) {
	rows := []struct {
		Row     int64  `db:"row"`
		Payload []byte `db:"payload"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT rowid AS [[row]], [[payload]] FROM {{leafen_page_history_events}}
		WHERE rowid > {:after} ORDER BY rowid LIMIT {:limit}
	`).Bind(dbx.Params{"after": after, "limit": pageHistoryPackingEventsTurn}).All(&rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return -1, nil
	}
	for _, row := range rows {
		packed := packPageHistory(row.Payload)
		if len(packed) == len(row.Payload) {
			continue
		}
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_page_history_events}} SET [[payload]] = {:payload} WHERE rowid = {:row}`).
			Bind(dbx.Params{"payload": packed, "row": row.Row}).Execute(); err != nil {
			return 0, err
		}
	}
	return rows[len(rows)-1].Row, nil
}

// packPageHistoryCheckpoints deflates the checkpoints after a row and gives
// the last row it looked at, or -1 once none are left.
func packPageHistoryCheckpoints(app core.App, after int64) (int64, error) {
	rows := []struct {
		Row                int64  `db:"row"`
		PageJSON           string `db:"page_json"`
		BlocksJSON         string `db:"blocks_json"`
		StructuralState    []byte `db:"structural_state"`
		CollaborationState []byte `db:"collaboration_state"`
		DatabasesJSON      string `db:"databases_json"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT rowid AS [[row]], [[page_json]], [[blocks_json]], [[structural_state]], [[collaboration_state]], [[databases_json]]
		FROM {{leafen_page_history_checkpoints}} WHERE rowid > {:after} ORDER BY rowid LIMIT {:limit}
	`).Bind(dbx.Params{"after": after, "limit": pageHistoryPackingCheckpointsTurn}).All(&rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return -1, nil
	}
	for _, row := range rows {
		texts := []string{row.PageJSON, row.BlocksJSON, row.DatabasesJSON}
		waiting := false
		for _, text := range texts {
			waiting = waiting || (!isPackedPageHistory([]byte(text)) && isPackedPageHistory(packPageHistory([]byte(text))))
		}
		for _, state := range [][]byte{row.StructuralState, row.CollaborationState} {
			waiting = waiting || len(packPageHistory(state)) != len(state)
		}
		if !waiting {
			continue
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_page_history_checkpoints}} SET [[page_json]] = {:page}, [[blocks_json]] = {:blocks},
				[[structural_state]] = {:structural}, [[collaboration_state]] = {:collaboration}, [[databases_json]] = {:databases}
			WHERE rowid = {:row}
		`).Bind(dbx.Params{"page": packPageHistoryText(row.PageJSON), "blocks": packPageHistoryText(row.BlocksJSON),
			"structural": packPageHistory(row.StructuralState), "collaboration": packPageHistory(row.CollaborationState),
			"databases": packPageHistoryText(row.DatabasesJSON), "row": row.Row}).Execute(); err != nil {
			return 0, err
		}
	}
	return rows[len(rows)-1].Row, nil
}
