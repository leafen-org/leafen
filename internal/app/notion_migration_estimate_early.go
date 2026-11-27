package app

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// A few minutes into a migration's run, once it has read some bodies, how
// fast it reads them says more of its time than the scan could: how deep the
// bodies go, how fast Notion answers this connection, how fast this server
// keeps what came. So the run weighs the time it said against that pace once,
// early: when the reading at that pace — a fifth slower, as its first bodies
// may say too little — and the stages after it at the plan's longer ends would
// end later than said, the end moves there and the person hears of it once,
// in their inbox (notifyNotionMigrationLater); an earlier end is never
// promised, the time left only goes down with the pace
// (notion_migration_time_left.go). Later, a stage clearly late still moves it
// (notionMigrationLaterEnd). (2026-11-20: a move said 20 minutes to an hour
// and was still reading an hour and a half in; the person waited past the
// time said with no word of it.) Its minutes are the reads': the time the
// reading spent on its databases and files is left out, and the rows still
// to read hold what rows hold (notion_migration_reading_work.go).

const (
	// When the reading's pace is weighed: five minutes and twenty bodies in,
	// or ten minutes and five bodies on a slow road.
	notionMigrationEarlyAfter      = 5 * time.Minute
	notionMigrationEarlyBodies     = 20
	notionMigrationEarlySlowAfter  = 10 * time.Minute
	notionMigrationEarlySlowBodies = 5
	// The reading's pace so far, a fifth slower.
	notionMigrationEarlyMargin = 1.2
)

// notionMigrationEarlyEnd weighs, once, a run's reading pace against the end
// it promised, and keeps when it did (readCheckedAt): the later end that
// pace calls for, or nil.
func notionMigrationEarlyEnd(
	app core.App, job storedJob, payload notionImportJobPayload, estimate *notionMigrationEstimate, now time.Time,
) (*notionMigrationEnd, error) {
	if estimate.Stage != notionMigrationStageReading || estimate.ReadCheckedAt > 0 {
		return nil, nil
	}
	since := notionMigrationStageSince(*estimate, now)
	bodies, err := notionMigrationReadingBodies(app, job.ID, estimate.Stage)
	if err != nil {
		return nil, err
	}
	done, total := notionMigrationStageWork(job, payload, *estimate, bodies)
	ready := since >= notionMigrationEarlyAfter.Minutes() && done >= notionMigrationEarlyBodies ||
		since >= notionMigrationEarlySlowAfter.Minutes() && done >= notionMigrationEarlySlowBodies
	if !ready {
		return nil, nil
	}
	if total <= done {
		estimate.ReadCheckedAt = now.UnixMilli()
		return nil, nil
	}
	// By its reads' own pace, once it has one (notion_migration_reading_pace.go).
	reading, paced := notionMigrationReadingLeft(*estimate, bodies, done, total, since)
	if !paced {
		return nil, nil
	}
	estimate.ReadCheckedAt = now.UnixMilli()
	left := reading*notionMigrationEarlyMargin + float64(estimate.MakeMinutes+estimate.CommentMinutes) + 3
	endsAt := now.UnixMilli() + int64(left*float64(time.Minute.Milliseconds()))
	if endsAt <= estimate.EndsAt {
		return nil, nil
	}
	end := &notionMigrationEnd{reason: "slower_reading"}
	if counted, found := estimate.Pages+estimate.Rows, bodies.Read+bodies.Waiting; float64(found) > float64(counted)*1.1 {
		end.reason, end.found, end.counted = "more_found", found, counted
	}
	end.leftMinutes = notionMigrationRoundUp(left)
	end.endsAt = now.UnixMilli() + int64(end.leftMinutes)*time.Minute.Milliseconds()
	return end, nil
}
