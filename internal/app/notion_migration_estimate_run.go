package app

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// While a migration's run goes on, Leafen checks every little while whether
// it still ends by the time it promised (notion_migration_estimate.go). The
// time left counts down to that end, or to the earlier one the run's pace
// puts it at (notion_migration_time_left.go), and goes past the promised end
// only near it, quietly, for a run a little behind; only when
// the run at its pace would end clearly later (notionMigrationLaterEnd) does
// the promise move, later still, with the reason — rows holding more than the sample showed, more
// than the scan counted, Notion answering slower — and the sentence the
// agent and the screen say becomes the change (2026-11-16: the minutes left
// went 40, 35, 30, 38, 18 as the reading found more). The time the run stood
// with Leafen off — stopped, restarting, the computer asleep — is none of its
// time: its pace, its end and its whole time leave it out (2026-11-16: a run
// whose server was off for 1h10m came back saying making in Leafen was slow,
// about 1h45m in all, with a few minutes of work left).

const (
	notionMigrationFollowEvery = 30 * time.Second
	// A run looked at every half minute while it goes that went longer
	// without a look stood with Leafen off.
	notionMigrationOffAfter = 3 * time.Minute
)

var notionMigrationFollowed = struct {
	sync.Mutex
	at map[string]time.Time
}{at: map[string]time.Time{}}

// followNotionImportRun keeps a running migration's estimate: the stage it
// is in, since when, and its promised end; and a direct import's reading
// clock (notion_import_reading_clock.go), into the turn's payload as well.
// Every turn calls it; it looks at most every half minute a job.
func followNotionImportRun(app core.App, job storedJob, payload *notionImportJobPayload) {
	reading := payload.MigrationID == ""
	if reading && payload.Phase != notionImportPhaseScope {
		return
	}
	notionMigrationFollowed.Lock()
	if last, seen := notionMigrationFollowed.at[job.ID]; seen && time.Since(last) < notionMigrationFollowEvery {
		notionMigrationFollowed.Unlock()
		return
	}
	for other, at := range notionMigrationFollowed.at {
		if time.Since(at) > time.Hour {
			delete(notionMigrationFollowed.at, other)
		}
	}
	notionMigrationFollowed.at[job.ID] = time.Now()
	notionMigrationFollowed.Unlock()
	if reading {
		if err := keepNotionImportReadingClock(app, job.ID, &payload.notionImportReadingClock, time.Now()); err != nil {
			logDurableJobDiagnostic(app, errorCodeJobRunnerFailed, "", job.ID, "notion-import.reading-clock", "keep", err)
		}
		return
	}
	if err := keepNotionMigrationEstimate(app, job, *payload, time.Now()); err != nil {
		logDurableJobDiagnostic(app, errorCodeJobRunnerFailed, "", job.ID, "notion-migration.estimate", "keep", err)
	}
}

func keepNotionMigrationEstimate(app core.App, job storedJob, payload notionImportJobPayload, now time.Time) error {
	stored, err := loadNotionMigration(app, job.Owner, job.WorkspaceID, payload.MigrationID)
	if err != nil {
		return err
	}
	report := emptyNotionMigrationRunReport()
	if stored.RunReport != "" {
		if err := json.Unmarshal([]byte(stored.RunReport), &report); err != nil {
			return err
		}
	}
	// A run started before runs kept their estimate, or the estimate of the
	// run before this one: reckoned now, from when this one started, at the
	// stage it is in (no stage before it is told).
	fresh := report.Estimate == nil || report.Estimate.StartedAt < job.Created
	if fresh {
		if err := startNotionMigrationEstimate(app, stored, job.Created); err != nil {
			return err
		}
		if stored, err = loadNotionMigration(app, job.Owner, job.WorkspaceID, payload.MigrationID); err != nil {
			return err
		}
		report = emptyNotionMigrationRunReport()
		if err := json.Unmarshal([]byte(stored.RunReport), &report); err != nil || report.Estimate == nil {
			return err
		}
	}
	estimate := *report.Estimate
	// The time since the last look with Leafen off moves the stage's start
	// and the promised end, so neither the pace nor the time left counts it.
	previous := estimate.FollowedAt
	if off := notionRunOffTime(estimate.FollowedAt, now); off > 0 {
		estimate.OffMs += off
		estimate.StageAt += off
		estimate.EndsAt += off
		if estimate.PaceEndsAt > 0 {
			estimate.PaceEndsAt += off
		}
		previous = 0
	}
	estimate.FollowedAt = now.UnixMilli()
	stage := notionMigrationStageOfStep(notionImportStage(app, job, payload))
	changed := fresh
	if fresh {
		estimate.Stage = stage
	}
	// The reading's pace is its reads' own (notion_migration_reading_pace.go).
	if estimate.Stage == notionMigrationStageReading && stage == notionMigrationStageReading {
		aside, err := keepNotionMigrationReadingClock(app, job, payload, &estimate, previous, now)
		if err != nil {
			return err
		}
		changed = changed || aside
	}
	if estimate.Stage != stage {
		ended := estimate.Stage
		// How long it took, for a run again (notion_migration_estimate_took.go).
		notionMigrationStageTook(&estimate, ended, now.UnixMilli()-estimate.StageAt)
		estimate.Stage, estimate.StageAt, changed = stage, now.UnixMilli(), true
		// What it read, from now on (notion_migration_estimate_read_blocks.go).
		if ended == notionMigrationStageReading {
			if err := notionMigrationReadBlocks(app, job.ID, &estimate); err != nil {
				return err
			}
		}
		// Each stage that ended is told, with the time left from the stage
		// begun (notion_migration_notices.go, notion_migration_time_left.go).
		if _, err := keepNotionMigrationPace(app, job, payload, &estimate, now); err != nil {
			return err
		}
		if err := notifyNotionMigrationStagesEnded(app, job, payload.MigrationID, estimate, ended, stage); err != nil {
			return err
		}
	}
	// A few minutes into reading, its pace weighs the time said once
	// (notion_migration_estimate_early.go); then a stage clearly late moves it.
	later, err := notionMigrationEarlyEnd(app, job, payload, &estimate, now)
	if err != nil {
		return err
	}
	changed = changed || estimate.ReadCheckedAt != report.Estimate.ReadCheckedAt
	if later == nil {
		if later, err = notionMigrationLaterEnd(app, job, payload, estimate, now); err != nil {
			return err
		}
	}
	if later != nil {
		language, err := serverTextLanguage(app, job.Owner, nil)
		if err != nil {
			return err
		}
		estimate.EndsAt, estimate.Reason, estimate.ChangedAt = later.endsAt, later.reason, now.UnixMilli()
		estimate.MaxMinutes = max(estimate.MaxMinutes, notionMigrationRoundUp(float64(later.endsAt-estimate.StartedAt-estimate.OffMs)/float64(time.Minute.Milliseconds())))
		estimate.Say = notionMigrationEstimateChangeSay(language, later)
		changed = true
	}
	paced, err := keepNotionMigrationPace(app, job, payload, &estimate, now)
	if err != nil {
		return err
	}
	// The person hears of a later end in their inbox, the one notice of it
	// this run keeps (notion_migration_notices.go).
	if later != nil {
		if err := notifyNotionMigrationLater(app, job, payload.MigrationID, estimate, now); err != nil {
			return err
		}
	}
	// Its notices' heads say the time left now (notion_migration_time_said.go).
	left, faster := notionMigrationMinutesLeft(&estimate, now), notionMigrationFaster(estimate, now)
	if err := keepNotionMigrationNoticeHeads(app, job, payload.MigrationID, estimate, left, faster, false, now); err != nil {
		return err
	}
	if !changed && estimate.OffMs == report.Estimate.OffMs {
		return notionMigrationFollowedAt(app, payload.MigrationID, estimate.FollowedAt, paced, estimate.PaceEndsAt)
	}
	return updateNotionMigrationRunReport(app, payload.MigrationID, func(report *notionMigrationRunReport) {
		report.Estimate = &estimate
	})
}

// notionRunOffTime is how long a run — a migration's, or a direct import's
// reading — stood with Leafen off since it was last looked at (followedAt):
// a gap longer than the looks of a running run leave, less the half minute
// it may have gone on after that look.
func notionRunOffTime(followedAt int64, now time.Time) int64 {
	gap := now.UnixMilli() - followedAt
	if followedAt == 0 || gap <= notionMigrationOffAfter.Milliseconds() {
		return 0
	}
	return gap - notionMigrationFollowEvery.Milliseconds()
}

// notionMigrationFollowedAt keeps when a run was last looked at, alone, or
// with the end its pace puts it at when that moved: a look that changes
// nothing else writes only those.
func notionMigrationFollowedAt(app core.App, migrationID string, at int64, paced bool, paceEndsAt int64) error {
	set := `json_set([[run_report_json]], '$.estimate.followedAt', {:at})`
	if paced {
		set = `json_set([[run_report_json]], '$.estimate.followedAt', {:at}, '$.estimate.paceEndsAt', {:pace})`
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_migrations}}
		SET [[run_report_json]]=` + set + `
		WHERE [[id]]={:id} AND json_type([[run_report_json]], '$.estimate') = 'object'
	`).Bind(dbx.Params{"at": at, "pace": paceEndsAt, "id": migrationID}).Execute()
	return err
}

// notionMigrationEnd is a later end a run's pace calls for, and why.
type notionMigrationEnd struct {
	endsAt      int64
	leftMinutes int
	reason      string
	// For more_found: the bodies read and waiting against the scan's count;
	// for slower_comments: the blocks to ask.
	found, counted int
}

// A stage's pace is judged once it has gone a while and done a share of its
// work: before that it says more of how the stage starts than of how it goes
// (2026-11-19: three minutes in, the reading's first bodies said about 7
// hours; the comments asked of the pages first and of the blocks after were
// judged by the blocks alone, as if the pages' ten minutes had asked none,
// and said about 94 hours — the run took 3 hours 34 minutes, inside the
// first range).
const (
	notionMigrationPaceAfter = 5 * time.Minute
	notionMigrationPaceShare = 0.1
)

// notionMigrationLaterEnd is where the run's pace puts its end when that is
// clearly after the end it promised, or nil: the stage it is in at its own
// pace so far, every request of the stage counted against the whole stage's
// time, and the stages after it at the middle of the plan's range for them.
// Only past the promised end by more than a tenth of the whole, ten minutes
// at least, is the run late; then the new end allows a tenth more for this
// stage and the plan's longer end for those after it, as the first did.
func notionMigrationLaterEnd(
	app core.App, job storedJob, payload notionImportJobPayload, estimate notionMigrationEstimate, now time.Time,
) (*notionMigrationEnd, error) {
	since := notionMigrationStageSince(estimate, now)
	if since < notionMigrationPaceAfter.Minutes() {
		return nil, nil
	}
	end := notionMigrationEnd{}
	bodies, err := notionMigrationReadingBodies(app, job.ID, estimate.Stage)
	if err != nil {
		return nil, err
	}
	done, total := notionMigrationStageWork(job, payload, estimate, bodies)
	after := 0
	switch estimate.Stage {
	case notionMigrationStageReading:
		counted, found := estimate.Pages+estimate.Rows, bodies.Read+bodies.Waiting
		after = estimate.MakeMinutes + estimate.CommentMinutes
		end.reason = "slower_reading"
		if float64(found) > float64(counted)*1.1 {
			end.reason, end.found, end.counted = "more_found", found, counted
		}
	case notionMigrationStageCreating:
		after = estimate.CommentMinutes
		end.reason = "slower_making"
	case notionMigrationStageComments:
		end.reason, end.counted = "slower_comments", max(payload.CommentBlocksTotal, estimate.CommentBlocks)
	default:
		return nil, nil
	}
	if done < 20 || total <= done || float64(done) < float64(total)*notionMigrationPaceShare {
		return nil, nil
	}
	left := float64(total-done) / (float64(done) / since)
	if estimate.Stage == notionMigrationStageReading {
		// By its reads' own pace (notion_migration_reading_pace.go).
		var paced bool
		if left, paced = notionMigrationReadingLeft(estimate, bodies, done, total, since); !paced {
			return nil, nil
		}
	}
	minute := float64(time.Minute.Milliseconds())
	whole := float64(estimate.EndsAt-estimate.StartedAt-estimate.OffMs) / minute
	late := max(10, whole/10)
	if float64(now.UnixMilli())+(left+3+float64(after)*notionMigrationTypicalShare(estimate))*minute <= float64(estimate.EndsAt)+late*minute {
		return nil, nil
	}
	end.leftMinutes = notionMigrationRoundNear(left*1.1 + float64(after) + 3)
	end.endsAt = now.UnixMilli() + int64(end.leftMinutes)*time.Minute.Milliseconds()
	return &end, nil
}

// notionMigrationTypicalShare is where a stage not yet begun is reckoned in
// the plan's range for it, as a share of its longer end: at the middle of
// the whole run's range (notionMigrationEstimateOf), which the stages' longer
// ends make.
func notionMigrationTypicalShare(estimate notionMigrationEstimate) float64 {
	high := float64(estimate.ReadMinutes+estimate.MakeMinutes+estimate.CommentMinutes+3) * 1.2
	if high <= 0 || estimate.MinMinutes <= 0 {
		return 1
	}
	return min(1, (float64(estimate.MinMinutes)/high+1)/2)
}

// notionMigrationEstimateChangeSay is the sentence for a later end: that it
// takes longer and why, with no time of its own. How long it still takes is
// the time left beside it — the run's one reckoning, which the window, the
// card, the Completed row, the notice's head and the agent's next all say
// (notionMigrationMinutesLeft); a count of its own here, taken at the change
// and generous, stood next to it as a second time — "about 35 minutes more,
// about 45 in all" over "about 22 minutes left" — and grew stale as the run
// went on (2026-11-20).
func notionMigrationEstimateChangeSay(language string, end *notionMigrationEnd) string {
	if language == accountLanguageKo {
		why := map[string]string{
			"more_found":      fmt.Sprintf("Notion에 스캔이 센 것보다 본문이 많습니다(지금까지 %d개, 스캔은 %d개)", end.found, end.counted),
			"slower_reading":  "행마다 본문이 표본보다 많거나 Notion이 더디게 답합니다",
			"slower_making":   "리펜에 만드는 데 예상보다 오래 걸립니다",
			"slower_comments": fmt.Sprintf("블록 %d개에 댓글을 묻는 데 예상보다 오래 걸립니다", end.counted),
		}[end.reason]
		return fmt.Sprintf("예상보다 길어지고 있습니다 — %s. 남은 시간은 이제 지금 속도대로 셉니다. 그동안 다른 일을 하셔도 됩니다.", why)
	}
	why := map[string]string{
		"more_found":      fmt.Sprintf("Notion has more bodies than the scan counted (%d so far against %d)", end.found, end.counted),
		"slower_reading":  "rows hold more than the sample showed, or Notion answers more slowly",
		"slower_making":   "making it in Leafen takes longer than reckoned",
		"slower_comments": fmt.Sprintf("asking %d blocks for their comments takes longer than reckoned", end.counted),
	}[end.reason]
	return fmt.Sprintf("It’s taking longer than reckoned — %s. The time left now goes by this pace. You can do other things meanwhile.", why)
}
