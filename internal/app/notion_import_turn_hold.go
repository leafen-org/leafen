package app

import (
	"errors"
	"sync"
	"time"
)

// A Notion import's turn that makes what it read holds the one write
// connection while its transaction lasts, and people's saves and agents'
// writes wait behind it. A turn makes its items in windows, each its own
// transaction, sized so each holds the connection briefly: the next window
// takes fewer items after one that held long, and more again, up to the
// turn's notionImportApplyItems, after short ones. Between windows the
// connection goes to whoever waits for it. (2026-11-16, making a
// 31,000-block move on a NAS-class server: turns of 32 items held it up to
// 4.8 s, and an agent's write took up to 4.2 s, 1.4 s at the 95th
// percentile; in windows the longest hold was 0.8 s, mostly under 0.2 s, and
// the write took 0.35 s at the 95th percentile. The making took 782 s
// where it took 686 s, for the windows' own work.)

// notionImportTurnHold is how long a window may hold the write connection
// (the migration's ledger windows too, notion_migration_ledger_window.go).
const notionImportTurnHold = 100 * time.Millisecond

// notionImportWindowSizes keeps, by job, how many items its next window
// takes; a job not in it (a server started since) begins at
// notionImportFirstWindowItems and finds its size within a turn or two.
var notionImportWindowSizes sync.Map

const notionImportFirstWindowItems = 8

func notionImportWindowItems(jobID string) int {
	if items, ok := notionImportWindowSizes.Load(jobID); ok {
		return items.(int)
	}
	return notionImportFirstWindowItems
}

// notionHoldSizedWindow is how many items the window after one of items
// that held the write connection for held takes, up to most: fewer after a
// long hold, twice as many after a short one.
func notionHoldSizedWindow(items, most int, held time.Duration) int {
	items = max(1, items)
	switch {
	case held > notionImportTurnHold:
		return max(1, int(float64(items)*float64(notionImportTurnHold)/float64(held)))
	case held < notionImportTurnHold/2:
		return min(most, items*2)
	}
	return items
}

// notionImportWindowMade is what a window made, how long it held the write
// connection and where it left the job: more is whether the turn may go on
// making.
type notionImportWindowMade struct {
	count   int
	held    time.Duration
	cursor  string
	payload notionImportJobPayload
	more    bool
}

// makeNotionImportWindows makes a turn's items, a window at a time.
func (r *durableJobRunner) makeNotionImportWindows(job storedJob, payload notionImportJobPayload) error {
	leftOut := 0
	for left := notionImportApplyItems; left > 0; {
		made, err := r.makeNotionImportWindow(job, payload, min(left, notionImportWindowItems(job.ID)))
		// What Leafen refuses is left out, and the window made again without
		// it (notion_import_left_out.go); after so many, in the next turn.
		refused := &notionImportItemRefusedError{}
		if errors.As(err, &refused) {
			if job, payload, err = r.leaveNotionImportItemOut(job, payload, refused); err != nil || leftOut+1 >= notionImportLeftOutPerTurn {
				return err
			}
			leftOut++
			continue
		}
		if err != nil {
			return err
		}
		if !made.more {
			notionImportWindowSizes.Delete(job.ID)
			return nil
		}
		notionImportWindowSizes.Store(job.ID, notionHoldSizedWindow(made.count, notionImportApplyItems, made.held))
		left -= made.count
		job.Cursor, payload = made.cursor, made.payload
		// Under memory pressure the turn ends here, the next at the memory's
		// pace (memory_pressure_long_work.go).
		if memoryLongWorkHeld() {
			return nil
		}
	}
	return nil
}
