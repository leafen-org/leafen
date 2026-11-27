package app

import (
	"context"
	"time"
)

// durableJobScans are the looks the runner takes before each dispatch, with
// their pacing: the automation schedules, sub-item and dependency pairs,
// the bodies waiting for their sub-pages' blocks, the databases an older
// archive brought with no block, the history's packing and the sync
// journals' trimming. They share the loop's wake-ups.
type durableJobScans struct {
	nextScheduleScanAt time.Time
	schedulePending    bool
	subitemPairs       databaseSubitemPairScan
	childPageBlocks    childPageBlockBackfillScan
	databaseBlocks     databaseBlockBackfillScan
	historyPacking     pageHistoryPackingScan
	graphJournals      graphJournalScan
}

func (r *durableJobRunner) newDurableJobScans() *durableJobScans {
	return &durableJobScans{
		subitemPairs:    databaseSubitemPairScan{pending: r.ygo != nil},
		childPageBlocks: childPageBlockBackfillScan{pending: true},
		databaseBlocks:  databaseBlockBackfillScan{failed: map[string]struct{}{}},
		historyPacking:  pageHistoryPackingScan{pending: true},
	}
}

// runScans takes the looks that are due. again says the schedules have more
// to admit at once though none was admitted; ok is false once ctx ends: a
// stopping server takes no further look, and the look under way, whose
// reads take ctx, ends with it.
func (r *durableJobRunner) runScans(ctx context.Context, now time.Time, localActive int, scans *durableJobScans) (again, ok bool) {
	scheduleResult := databaseAutomationScheduleAdmissionResult{}
	var scheduleErr error
	scheduleScanned := shouldScanDatabaseAutomationSchedules(now, scans.nextScheduleScanAt, scans.schedulePending, localActive)
	if scheduleScanned {
		scheduleResult, scheduleErr = admitDueDatabaseAutomationSchedules(r.app, r.clock.now())
		if scheduleErr == nil {
			// Repeating database templates share the schedules' scan.
			repeats, repeatErr := admitDueDatabaseTemplateRepeats(r.app, r.clock.now())
			scheduleResult.Admitted += repeats.Admitted
			scheduleResult.HasMore = scheduleResult.HasMore || repeats.HasMore
			scheduleErr = repeatErr
		}
		scans.nextScheduleScanAt = now.Truncate(time.Minute).Add(time.Minute)
		scans.schedulePending = scheduleErr == nil && scheduleResult.HasMore
		if scheduleErr != nil {
			if ctx.Err() != nil {
				return false, false
			}
			logDurableJobDiagnostic(
				r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "",
				"database-automation.schedule", "admit", scheduleErr,
			)
		}
	}
	for _, look := range []func(){
		func() { r.scanDatabaseSubitemPairs(ctx, now, &scans.subitemPairs) },
		func() { r.scanChildPageBlockBackfill(ctx, now, &scans.childPageBlocks) },
		func() { r.scanDatabaseBlockBackfill(ctx, &scans.databaseBlocks) },
		func() { r.scanPageHistoryPacking(ctx, now, &scans.historyPacking) },
		func() { r.scanGraphJournals(ctx, now, &scans.graphJournals) },
	} {
		if ctx.Err() != nil {
			return false, false
		}
		look()
	}
	again = scheduleScanned && scheduleErr == nil && scans.schedulePending && scheduleResult.Admitted == 0
	return again, ctx.Err() == nil
}
