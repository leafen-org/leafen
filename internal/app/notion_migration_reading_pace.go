package app

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The pace of a migration's reading is its reads' own. Reading also lists
// each database's rows and fetches the files they carry before it goes on,
// which are none of its reads; and it reads the pages before the rows, each
// page several requests — its presentation, its blocks, what they name —
// where a row's body, or a block's that holds more, is one. So a reading
// weighed a few minutes in by all its time and its reads so far said hours
// where it took minutes (2026-11-21: a move spent its first minutes on its
// databases and their 295 files, and a few minutes in, with little but its
// pages read, said three and a half hours left; it was done 55 minutes after
// it started. A synthetic one shaped like it said 1,170 minutes, then, with
// that time aside, still 240 by the pages' pace; it took about 47.)
//
// Each look a reading takes (keepNotionMigrationReadingClock) that finds no
// read done since the look before, while a database is being read or before
// its first body, puts that time aside (readAsideMs), and the reading's pace
// leaves it out (notionMigrationStageSince); the files its bodies hold are
// fetched as they are read, and go with them. A look that finds reads but no
// page read since the one before, and no database being read, times those
// plain reads (plainMs of plainTimed). Once a few are timed, the plain reads
// still to do go at their time and the pages at the reads' so far; before
// that, while rows are most of what is left, the reading has no pace to go
// by (notionMigrationReadingLeft).

// How many plain reads a reading times before their pace counts.
const notionMigrationPlainTimed = 20

// notionMigrationStageSince is how long, in minutes, the stage a run is in
// has gone: reading less the time it spent on its databases.
func notionMigrationStageSince(estimate notionMigrationEstimate, now time.Time) float64 {
	gone := now.UnixMilli() - estimate.StageAt
	if estimate.Stage == notionMigrationStageReading {
		gone -= estimate.ReadAsideMs
	}
	return float64(gone) / float64(time.Minute.Milliseconds())
}

// notionMigrationReadingLeft is the minutes a reading's reads still to do
// take, of done reads of total in since minutes of its own, and whether it
// has a pace to go by, as above.
func notionMigrationReadingLeft(
	estimate notionMigrationEstimate, bodies notionMigrationBodies, done, total int, since float64,
) (float64, bool) {
	if done <= 0 || total <= done || since <= 0 {
		return 0, false
	}
	counted := estimate.Pages + estimate.Rows
	bodies = bodies.ofRows(estimate)
	left, perRead := float64(total-done), since/float64(done)
	pages := float64(min(total-done, bodies.pagesLeft(counted)))
	if estimate.PlainTimed >= notionMigrationPlainTimed && estimate.PlainMs > 0 {
		perPlain := float64(estimate.PlainMs) / float64(estimate.PlainTimed) / float64(time.Minute.Milliseconds())
		return pages*perRead + (left-pages)*perPlain, true
	}
	if rows := float64(min(total-done, bodies.rowsLeft(counted))); rows*2 >= left {
		return 0, false
	}
	return left * perRead, true
}

// rowsLeft is the rows still to read: waiting, and not yet listed of the
// scan's count of them.
func (bodies notionMigrationBodies) rowsLeft(counted int) int {
	return bodies.RowsWaiting + bodies.rowsUnlisted(counted)
}

// pagesLeft is the pages still to read: waiting, and not yet listed.
func (bodies notionMigrationBodies) pagesLeft(counted int) int {
	unlisted := max(0, counted-bodies.Read-bodies.Waiting)
	return bodies.Waiting - bodies.RowsWaiting + unlisted - bodies.rowsUnlisted(counted)
}

// rowsUnlisted is the rows not yet listed, of the bodies not yet listed.
func (bodies notionMigrationBodies) rowsUnlisted(counted int) int {
	unlisted := max(0, counted-bodies.Read-bodies.Waiting)
	return min(unlisted, max(0, bodies.rows-bodies.RowsRead-bodies.RowsWaiting))
}

// keepNotionMigrationReadingClock keeps, at a reading's look, the reads and
// pages it saw (readsSeen, pagesSeen), the time since the look before
// (previous; 0 for none, or for a time Leafen was off) aside or the plain
// reads', as above, and reports whether any of it changed.
func keepNotionMigrationReadingClock(
	app core.App, job storedJob, payload notionImportJobPayload, estimate *notionMigrationEstimate, previous int64, now time.Time,
) (bool, error) {
	bodies, err := notionMigrationReadingBodies(app, job.ID, notionMigrationStageReading)
	if err != nil {
		return false, err
	}
	gone := int64(0)
	if previous > 0 && previous < now.UnixMilli() {
		gone = now.UnixMilli() - previous
	}
	pagesRead := bodies.Read - bodies.RowsRead
	reads, pages := bodies.ReadsDone-estimate.ReadsSeen, pagesRead-estimate.PagesSeen
	aside := len(payload.PendingDatabaseIDs) > 0 || bodies.ReadsDone == 0
	changed := reads != 0 || pages != 0
	switch {
	case gone > 0 && reads == 0 && aside:
		estimate.ReadAsideMs += gone
		changed = true
	case gone > 0 && reads > 0 && pages == 0 && !aside:
		// While a database is read its time is its own, not the reads'.
		estimate.PlainMs += gone
		estimate.PlainTimed += reads
	}
	estimate.ReadsSeen, estimate.PagesSeen = bodies.ReadsDone, pagesRead
	return changed, nil
}
