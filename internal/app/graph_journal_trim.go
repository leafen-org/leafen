package app

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A page's two sync journals are kept to what they are for (2026-10-18: a
// 7-hour soak left 127 MB of receipts and 88 MB of changes, a row for every
// write ever made).
//
// Receipts (leafen_graph_mutations) tell a retried change from a new one. A
// browser's outbox keeps a change until it is answered and sends it again
// when it is back online, so a retry comes as late as the device stayed
// away after losing an answer: receipts stay 30 days. A page's newest one
// always stays, its cursor being where the page's next change goes. A retry
// later than that is taken as a change again, as a late offline change is;
// what the page's documents take from it, they already hold.
//
// Changes (leafen_graph_changes) bring a screen that fell behind up to date
// from its cursor. A page keeps its newest 500 and those of the last day,
// for open screens and ones opened again within a day. The older ones,
// when they weigh more than the page's document, become one change at the
// last of their places holding the document as it is: a screen catching up
// from any earlier place arrives at the same document, as with migration
// 508 (schema_baseline.json).
//
// The job runner walks both journals by row, a turn at a time between
// pauses, keeping where it got to in leafen_graph_journal_trim, and starts
// over six hours after reaching their ends; a page's changes are also
// looked at every 250 of them.

const (
	graphReceiptsKept         = 30 * 24 * time.Hour
	graphChangesKeptNewest    = 500
	graphChangesKeptRecent    = 24 * time.Hour
	graphJournalTurnRows      = 1000
	graphJournalRestAfterWalk = 6 * time.Hour
	graphJournalTurnPause     = 2 * time.Second
	graphJournalIdleLook      = time.Minute
	graphChangeLookEvery      = 250
	graphChangeFoldPagesTurn  = 4
	// Older changes weighing less than this aren't worth reading the page's
	// document for.
	graphChangeFoldLeast = 64 << 10
)

type graphJournalPage struct {
	Owner  string `db:"owner"`
	PageID string `db:"page_id"`
}

// graphChangesToLook holds the pages whose changes reached another 250
// since they were last looked at; a restart loses them to the next walk.
var graphChangesToLook = struct {
	sync.Mutex
	pages map[graphJournalPage]struct{}
}{pages: map[graphJournalPage]struct{}{}}

func noteGraphChangeWritten(owner, pageID string, cursor int) {
	if cursor <= 0 || cursor%graphChangeLookEvery != 0 {
		return
	}
	graphChangesToLook.Lock()
	graphChangesToLook.pages[graphJournalPage{Owner: owner, PageID: pageID}] = struct{}{}
	graphChangesToLook.Unlock()
}

// graphJournalScan paces the trimming and remembers, for a walk, the pages
// whose older changes weighed less than their document.
type graphJournalScan struct {
	next  time.Time
	light map[graphJournalPage]int64
}

func (r *durableJobRunner) scanGraphJournals(ctx context.Context, now time.Time, scan *graphJournalScan) {
	if now.Before(scan.next) {
		return
	}
	if scan.light == nil {
		scan.light = map[graphJournalPage]int64{}
	}
	more, err := trimGraphJournalsTurn(r.app, now, scan)
	wait := graphJournalIdleLook
	if more {
		wait = graphJournalTurnPause
	}
	if err != nil {
		wait = graphJournalIdleLook
		if ctx.Err() == nil {
			logDurableJobDiagnostic(r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "", "graph-journals.trim", "trim", err)
		}
	}
	scan.next = now.Add(wait)
	time.AfterFunc(wait, r.notify)
}

// trimGraphJournalsTurn looks at the pages waiting to be looked at and takes
// one turn of the walk, and says whether the walk has more.
func trimGraphJournalsTurn(app core.App, now time.Time, scan *graphJournalScan) (bool, error) {
	graphChangesToLook.Lock()
	waiting := make([]graphJournalPage, 0, graphChangeFoldPagesTurn)
	for page := range graphChangesToLook.pages {
		if len(waiting) == graphChangeFoldPagesTurn {
			break
		}
		waiting = append(waiting, page)
		delete(graphChangesToLook.pages, page)
	}
	graphChangesToLook.Unlock()
	for _, page := range waiting {
		if err := app.RunInTransaction(func(tx core.App) error {
			_, err := foldGraphChanges(tx, page, now, nil)
			return err
		}); err != nil {
			return false, err
		}
	}
	more := false
	err := app.RunInTransaction(func(tx core.App) error {
		var receiptsAfter, changesAfter, restUntil int64
		err := tx.DB().NewQuery(`SELECT [[receipts_after]], [[changes_after]], [[rest_until]] FROM {{leafen_graph_journal_trim}} WHERE [[id]] = 1`).
			Row(&receiptsAfter, &changesAfter, &restUntil)
		if errors.Is(err, sql.ErrNoRows) || err == nil && restUntil > now.UnixMilli() {
			return nil
		}
		if err != nil {
			return err
		}
		set := func(column string, value int64) error {
			_, err := tx.DB().NewQuery(`UPDATE {{leafen_graph_journal_trim}} SET [[` + column + `]] = {:value} WHERE [[id]] = 1`).
				Bind(dbx.Params{"value": value}).Execute()
			return err
		}
		more = true
		if receiptsAfter >= 0 {
			last, err := trimGraphReceipts(tx, receiptsAfter, now)
			if err != nil {
				return err
			}
			return set("receipts_after", last)
		}
		if changesAfter >= 0 {
			last, err := foldGraphChangesAfter(tx, changesAfter, now, scan.light)
			if err != nil {
				return err
			}
			return set("changes_after", last)
		}
		// Both walked: the next walk is in six hours.
		more = false
		clear(scan.light)
		_, err = tx.DB().NewQuery(`UPDATE {{leafen_graph_journal_trim}} SET [[receipts_after]] = 0, [[changes_after]] = 0,
			[[rest_until]] = {:rest} WHERE [[id]] = 1`).Bind(dbx.Params{"rest": now.Add(graphJournalRestAfterWalk).UnixMilli()}).Execute()
		return err
	})
	return more, err
}

// trimGraphReceipts lets go of the receipts past their 30 days among the
// rows after one, a page's newest staying, and gives the last row looked at,
// or -1 at the end.
func trimGraphReceipts(app core.App, after int64, now time.Time) (int64, error) {
	var until sql.NullInt64
	if err := app.DB().NewQuery(`SELECT MAX([[row]]) FROM (SELECT rowid AS [[row]] FROM {{leafen_graph_mutations}}
		WHERE rowid > {:after} ORDER BY rowid LIMIT {:limit})`).
		Bind(dbx.Params{"after": after, "limit": graphJournalTurnRows}).Row(&until); err != nil {
		return 0, err
	}
	if !until.Valid {
		return -1, nil
	}
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_graph_mutations}} WHERE rowid IN (
		SELECT [[receipt]].rowid FROM {{leafen_graph_mutations}} AS [[receipt]]
		WHERE [[receipt]].rowid > {:after} AND [[receipt]].rowid <= {:until} AND [[receipt]].[[created]] < {:before}
			AND [[receipt]].[[cursor]] < (SELECT MAX([[newest]].[[cursor]]) FROM {{leafen_graph_mutations}} AS [[newest]]
				WHERE [[newest]].[[owner]] = [[receipt]].[[owner]] AND [[newest]].[[page_id]] = [[receipt]].[[page_id]]))`).
		Bind(dbx.Params{"after": after, "until": until.Int64, "before": now.Add(-graphReceiptsKept).UnixMilli()}).Execute()
	return until.Int64, err
}

// foldGraphChangesAfter looks at the pages with changes older than a day
// among the rows after one, a few pages a turn, and gives the last row
// looked at, or -1 at the end.
func foldGraphChangesAfter(app core.App, after int64, now time.Time, light map[graphJournalPage]int64) (int64, error) {
	rows := []struct {
		Row     int64  `db:"row"`
		Owner   string `db:"owner"`
		PageID  string `db:"page_id"`
		Created int64  `db:"created"`
	}{}
	if err := app.DB().NewQuery(`SELECT rowid AS [[row]], [[owner]], [[page_id]], [[created]] FROM {{leafen_graph_changes}}
		WHERE rowid > {:after} ORDER BY rowid LIMIT {:limit}`).
		Bind(dbx.Params{"after": after, "limit": graphJournalTurnRows}).All(&rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return -1, nil
	}
	before := now.Add(-graphChangesKeptRecent).UnixMilli()
	looked := map[graphJournalPage]struct{}{}
	last := after
	for _, row := range rows {
		page := graphJournalPage{Owner: row.Owner, PageID: row.PageID}
		if _, seen := looked[page]; row.Created < before && !seen {
			if len(looked) == graphChangeFoldPagesTurn {
				break
			}
			looked[page] = struct{}{}
			if _, err := foldGraphChanges(app, page, now, light); err != nil {
				return 0, err
			}
		}
		last = row.Row
	}
	return last, nil
}

// foldGraphChanges makes a page's changes older than its newest 500 and
// than a day one change holding its document, when they weigh more than
// the document; light remembers, for a walk, the pages whose didn't.
func foldGraphChanges(app core.App, page graphJournalPage, now time.Time, light map[graphJournalPage]int64) (bool, error) {
	params := dbx.Params{"owner": page.Owner, "pageId": page.PageID, "before": now.Add(-graphChangesKeptRecent).UnixMilli(),
		"newest": graphChangesKeptNewest}
	var byCount, byAge sql.NullInt64
	if err := app.DB().NewQuery(`SELECT [[cursor]] FROM {{leafen_graph_changes}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
		ORDER BY [[cursor]] DESC LIMIT 1 OFFSET {:newest}`).Bind(params).Row(&byCount); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if !byCount.Valid {
		return false, nil
	}
	params["byCount"] = byCount.Int64
	if err := app.DB().NewQuery(`SELECT MAX([[cursor]]) FROM {{leafen_graph_changes}} WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
		AND [[cursor]] <= {:byCount} AND [[created]] < {:before}`).Bind(params).Row(&byAge); err != nil {
		return false, err
	}
	if !byAge.Valid {
		return false, nil
	}
	params["cut"] = byAge.Int64
	var count, weight int64
	if err := app.DB().NewQuery(`SELECT COUNT(*), COALESCE(SUM(length([[ygo_update]])), 0) FROM {{leafen_graph_changes}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[cursor]] <= {:cut}`).Bind(params).Row(&count, &weight); err != nil {
		return false, err
	}
	if count < 2 || weight < graphChangeFoldLeast || light != nil && light[page] >= weight/2 {
		return false, nil
	}
	doc, _, err := loadYgoStructuralDoc(app, page.PageID)
	if err != nil {
		return false, err
	}
	update := crdt.EncodeStateAsUpdateV1(doc, nil)
	if int64(len(update)) >= weight {
		if light != nil {
			light[page] = weight
		}
		return false, nil
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_graph_changes}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[cursor]] < {:cut}`).Bind(params).Execute(); err != nil {
		return false, err
	}
	params["update"] = update
	_, err = app.DB().NewQuery(`UPDATE {{leafen_graph_changes}} SET [[ygo_update]] = {:update}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[cursor]] = {:cut}`).Bind(params).Execute()
	return err == nil, err
}
