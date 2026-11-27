package app

import (
	"math"
	"slices"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// How long a running migration still takes, one reckoning for its import
// window, the import screen, its agent's status and the inbox notices
// (notionMigrationMinutesLeft): down to the end the run's pace puts it at
// (paceEndsAt), never past the end it promised at the start
// (notion_migration_estimate_run.go) but near it, for a run a little behind
// it (notionMigrationPaceNear). It once counted down to the promised end
// alone, so a run going faster said "about 11 minutes left" a minute before
// it ended (2026-11-19).
//
// The run knows how much of the stage it is in is done: once a tenth is, the
// rest of the stage goes at the pace it has gone, and the stages after it at
// the longer end of the plan's pace for what they count (notionMigrationPacedLeft). Before that the
// end kept from the stage before stands, and with none yet, the promised
// end — unless, some minutes in, the stage's pace so far ends it clearly
// later (notionMigrationLaggingLeft). The run's look every half minute keeps that end: earlier when the
// pace says so, later only when the pace says clearly later, so the time
// left goes down with the clock and doesn't wander up and down with each
// look (2026-11-16: 40, 35, 30, 38, 18 minutes) — and near the end, a pace
// a little later moves it later quietly, the time left going down slower
// until it meets the pace's end. In its last minute and a half it says it is
// almost done.

const (
	// The check against Notion, the report and the rest, as the plan's
	// shorter end has it (notionMigrationEstimateOf).
	notionMigrationCheckMinutes = 1
	// How much of a stage is done before its own pace is gone by.
	notionMigrationStagePaced = 0.1
	// How much earlier than the end it keeps the pace must put the end to move
	// it, a look's worth; and how much later: a tenth of the time left, five
	// minutes at least.
	notionMigrationPaceEarlier    = 30 * time.Second
	notionMigrationPaceLaterShare = 0.1
	notionMigrationPaceLaterLeast = 5 * time.Minute
	// How much longer than the time the end kept leaves a stage's pace so far
	// must say, before a tenth of it is done, to move the end later: half
	// again (notionMigrationLaggingLeft).
	notionMigrationLaggingShare = 1.5
	// Near the end it keeps, a pace a little later moves it later by half a
	// look at each look, and while the pace leaves more, never into the
	// last minute and a half, said as almost done (notionMigrationPaceNear).
	notionMigrationPaceNearStep   = notionMigrationFollowEvery / 2
	notionMigrationAlmostDoneLeft = 90 * time.Second
)

// notionMigrationMinutesLeft is how long a running migration still takes, in
// whole minutes, at least one; 0 without an estimate. The promised end is
// counted up to the minute, as it was said; the pace's to the nearest, so a
// run a minute and a half from its end is near its end.
func notionMigrationMinutesLeft(estimate *notionMigrationEstimate, now time.Time) int {
	if estimate == nil || estimate.EndsAt == 0 {
		return 0
	}
	end, paced := notionMigrationShownEnd(*estimate)
	round := math.Ceil
	if paced {
		round = math.Round
	}
	return max(1, int(round(float64(end-now.UnixMilli())/float64(time.Minute.Milliseconds()))))
}

// notionMigrationShownEnd is the end a running migration's time left counts
// down to, and whether it is its pace's: the end its pace keeps, which goes
// no further than the promised end but near it, quietly, for a run a little
// behind (keepNotionMigrationPace); otherwise the promised end.
func notionMigrationShownEnd(estimate notionMigrationEstimate) (int64, bool) {
	if estimate.PaceEndsAt > 0 && estimate.PaceEndsAt != estimate.EndsAt {
		return estimate.PaceEndsAt, true
	}
	return estimate.EndsAt, false
}

// notionMigrationTimeShare is how much of a running migration's time has
// gone, in thousandths: the time since it started, less the time Leafen was
// off, of that and the time left by the same reckoning. Its bar fills by it
// through every stage, so it never starts again at nothing when a stage
// begins, and agrees with the time left beside it (2026-11-20: a bar that
// went empty with each stage, and slid while Notion was read, looked empty
// a sixth of the way in). Never full while it runs.
func notionMigrationTimeShare(estimate notionMigrationEstimate, now time.Time) int {
	end, _ := notionMigrationShownEnd(estimate)
	gone := now.UnixMilli() - estimate.StartedAt - estimate.OffMs
	if estimate.StartedAt == 0 || estimate.EndsAt == 0 || gone <= 0 {
		return 0
	}
	left := max(0, end-now.UnixMilli())
	return min(990, int(gone*1000/(gone+left)))
}

// notionMigrationAlmostDone reports whether a run's time left is said as
// almost done: its last minute, under a minute and a half to the nearest
// (notionMigrationAlmostDoneLeft).
func notionMigrationAlmostDone(minutes int) bool {
	return minutes == 1
}

// keepNotionMigrationPace moves the end the run's pace puts it at, as above,
// and reports whether it moved: earlier when the pace says so; later when it
// says clearly later, up to the promised end — past it only near it,
// quietly, until the promise moves with its notice (notionMigrationLaterEnd);
// and a little later only near it, quietly (notionMigrationPaceNear).
func keepNotionMigrationPace(
	app core.App, job storedJob, payload notionImportJobPayload, estimate *notionMigrationEstimate, now time.Time,
) (bool, error) {
	bodies, err := notionMigrationReadingBodies(app, job.ID, estimate.Stage)
	if err != nil {
		return false, err
	}
	left, paced := notionMigrationPacedLeft(*estimate, job, payload, bodies, now)
	lagging := false
	if !paced {
		if left, lagging = notionMigrationLaggingLeft(*estimate, job, payload, bodies, now); !lagging {
			return false, nil
		}
	}
	end := now.UnixMilli() + int64(left*float64(time.Minute.Milliseconds()))
	kept, moved := estimate.PaceEndsAt, end
	later := max(notionMigrationPaceLaterLeast.Milliseconds(), int64(float64(kept-now.UnixMilli())*notionMigrationPaceLaterShare))
	switch {
	case kept > 0 && end >= kept-notionMigrationPaceEarlier.Milliseconds() && end <= kept+later:
		moved = notionMigrationPaceNear(kept, end, now)
	case lagging && (kept == 0 || float64(end-now.UnixMilli()) < float64(kept-now.UnixMilli())*notionMigrationLaggingShare):
		return false, nil
	case end > max(estimate.EndsAt, kept):
		moved = notionMigrationPaceNear(max(estimate.EndsAt, kept), end, now)
	}
	if moved == kept {
		return false, nil
	}
	estimate.PaceEndsAt = moved
	return true, nil
}

// notionMigrationPaceNear is where an end the time left counts down to goes
// when the run's pace ends it later: near it — once what it leaves is no
// more than how much later the pace says and the last minute and a half —
// later by half a look at each look, so the time left goes down at half the
// clock's pace, never up, and meets the pace's end as the last minute and a
// half begins; and while the pace leaves more, never into that last minute
// and a half, said as almost done. Further from it the end stands: much of
// a stage left, its pace says less of where it ends. (2026-11-21: a move's
// comments went a little slower than the end kept, within the five minutes
// a pace must pass it by to move it later; the time left said almost done
// for the last two minutes and a half of the run.)
func notionMigrationPaceNear(kept, end int64, now time.Time) int64 {
	at, last := now.UnixMilli(), notionMigrationAlmostDoneLeft.Milliseconds()
	if end <= kept || kept-at > end-kept+last {
		return kept
	}
	return min(end, max(kept+notionMigrationPaceNearStep.Milliseconds(), at+last))
}

// notionMigrationPacedLeft is the minutes the run's pace leaves it, and
// whether it has one to go by: the stage it is in at its own pace once a
// tenth of it is done, and the stages after it at the longer end of the plan's
// pace for what they count (notionMigrationStageAhead) — each stage's work
// is of its own kind, Notion's answers or Leafen's making. The check against
// Notion, last, has no count of its own: it goes from when it began.
func notionMigrationPacedLeft(
	estimate notionMigrationEstimate, job storedJob, payload notionImportJobPayload, bodies notionMigrationBodies, now time.Time,
) (float64, bool) {
	since := max(0, notionMigrationStageSince(estimate, now))
	order := []string{notionMigrationStageReading, notionMigrationStageCreating, notionMigrationStageComments, notionMigrationStageVerifying}
	current := slices.Index(order, estimate.Stage)
	switch {
	case current < 0:
		return 0, false
	case estimate.Stage == notionMigrationStageVerifying:
		return max(0, notionMigrationStageAhead(estimate, estimate.Stage)-since), true
	}
	done, total := notionMigrationStageWork(job, payload, estimate, bodies)
	if done <= 0 || total <= 0 || since <= 0 || float64(done) < float64(total)*notionMigrationStagePaced {
		return 0, false
	}
	share := min(1, float64(done)/float64(total))
	left := since * (1 - share) / share
	if estimate.Stage == notionMigrationStageReading && total > done {
		// By its reads' own pace (notion_migration_reading_pace.go).
		var paced bool
		if left, paced = notionMigrationReadingLeft(estimate, bodies, done, total, since); !paced {
			return 0, false
		}
	}
	for _, stage := range order[current+1:] {
		left += notionMigrationStageAhead(estimate, stage)
	}
	return left, true
}

// notionMigrationLaggingLeft is the minutes a stage not yet a tenth done
// leaves at its pace so far, and whether that counts: once the stage has gone
// its first minutes and done some of its work, and only to put the end later
// than the one kept, by half again the time that leaves
// (keepNotionMigrationPace). A stage much slower than the plan's pace for it
// would otherwise count the time left down to the end its start was given
// while most of it is left, and say it is almost done (2026-11-21: the
// comments' blocks asked at a third of that pace, 650 of 19,001 asked, with
// about 95 minutes said and falling); one a little slower waits for its
// tenth, as its first minutes say less (the same day: five minutes of the
// pages' comments, asked ten a turn and slower than the blocks after them,
// moved the end 27 minutes later). Reading isn't judged
// so: its first bodies go slower than the rest (2026-11-19), and its own
// check weighs it a few minutes in (notion_migration_estimate_early.go).
func notionMigrationLaggingLeft(
	estimate notionMigrationEstimate, job storedJob, payload notionImportJobPayload, bodies notionMigrationBodies, now time.Time,
) (float64, bool) {
	since := notionMigrationStageSince(estimate, now)
	order := []string{notionMigrationStageCreating, notionMigrationStageComments, notionMigrationStageVerifying}
	current := slices.Index(order, estimate.Stage)
	if current < 0 || estimate.Stage == notionMigrationStageVerifying || since < notionMigrationPaceAfter.Minutes() {
		return 0, false
	}
	done, total := notionMigrationStageWork(job, payload, estimate, bodies)
	if done <= 0 || total <= done {
		return 0, false
	}
	left := since * float64(total-done) / float64(done)
	for _, stage := range order[current+1:] {
		left += notionMigrationStageAhead(estimate, stage)
	}
	return left, true
}

// notionMigrationStageAhead is the minutes a stage not yet begun is counted
// at: the longer end of the plan's pace for it (notionMigrationEstimateOf) —
// making each page, row and block at the slower making, asking each page, row
// and block with comments at Notion's slower answering — and the check
// against Notion with the report, a minute. At the middle of the plan's pace,
// a stage that went near its longer end moved the end later once a tenth of
// it was done (2026-11-21: comments asked 2.45 a second against the middle's
// 2.73 were reckoned to put the end about 25 minutes later at their tenth, on
// a move of 19,001 blocks); at the longer end
// the stage's own pace mostly brings the end earlier, as the person asked:
// a generous time, and no waiting past it once they think it is done
// (2026-11-20).
func notionMigrationStageAhead(estimate notionMigrationEstimate, stage string) float64 {
	switch stage {
	case notionMigrationStageCreating:
		made := float64(estimate.Pages + estimate.Rows + estimate.Blocks)
		return made / notionMigrationSlowMadePerSecond / 60
	case notionMigrationStageComments:
		if estimate.CommentMinutes == 0 {
			return 0
		}
		asked := float64(estimate.Pages + estimate.Rows + estimate.CommentBlocks)
		return asked / notionMigrationSlowRequestsPerSecond / 60
	case notionMigrationStageVerifying:
		return notionMigrationCheckMinutes
	}
	return 0
}

// notionMigrationStageWork is how much of the stage a run is in is done, of
// all: the reads done of all it has and will have (readsAll), at least the
// scan's count of bodies; what is made; the pages and blocks asked for their
// comments. 0 of 0 where it isn't counted.
func notionMigrationStageWork(
	job storedJob, payload notionImportJobPayload, estimate notionMigrationEstimate, bodies notionMigrationBodies,
) (int, int) {
	switch estimate.Stage {
	case notionMigrationStageReading:
		reads := bodies.ofRows(estimate).readsAll(estimate.Pages + estimate.Rows)
		return bodies.ReadsDone, max(estimate.Pages+estimate.Rows, reads)
	case notionMigrationStageCreating:
		return job.Processed, job.Total
	case notionMigrationStageComments:
		// The pages and rows are asked first, then the blocks: until the
		// blocks are counted, the plan's count of them stands.
		pages := payload.CommentPagesDone
		if payload.CommentBlocks && pages == 0 {
			// A run started before the pages asked were counted.
			pages = estimate.Pages + estimate.Rows
		}
		if payload.CommentBlocks {
			// The blocks are counted by a turn of their own once the pages
			// are asked; a look between the two saw all asked, and the time
			// left said almost done with the blocks to go (2026-11-21: for
			// over a minute, 65 minutes before a move's end).
			blocks := payload.CommentBlocksTotal
			if blocks == 0 && payload.CommentBlocksDone == 0 {
				blocks = estimate.CommentBlocks
			}
			return pages + payload.CommentBlocksDone, pages + blocks
		}
		// The pages and rows it counted to ask, once it has, as its line says.
		asking := estimate.Pages + estimate.Rows
		if payload.CommentPagesTotal > 0 {
			asking = payload.CommentPagesTotal
		}
		return pages + payload.CommentBlocksDone, max(asking, pages) + estimate.CommentBlocks
	}
	return 0, 0
}
