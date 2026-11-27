package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
)

const (
	durableJobLocalLane             = "local"
	durableJobNotionProviderLane    = "notion-provider"
	durableJobLocalLaneWorkers      = 1
	durableJobNotionProviderWorkers = 4
)

type scheduledReadyJob struct {
	ID          string `db:"id"`
	Kind        string `db:"kind"`
	PayloadJSON string `db:"payload_json"`
	LaneKey     string `db:"lane_key"`
}

type scheduledJobResult struct {
	jobs []scheduledReadyJob
	err  error
	// failed is the job whose turn err is (a batch stops at it).
	failed  string
	settled bool
}

type scheduledDatabaseAutomationDeliveryResult struct {
	result databaseAutomationDeliveryPassResult
	err    error
}

type durableJobScheduler struct {
	activeIDs       map[string]struct{}
	activeKeys      map[string]struct{}
	localActive     int
	notionActive    int
	results         chan scheduledJobResult
	deliveryResults chan scheduledDatabaseAutomationDeliveryResult
	deliveryActive  bool
	wait            sync.WaitGroup
	progress        *durableJobProgress
	holds           durableJobHolds
}

func newDurableJobScheduler() *durableJobScheduler {
	return &durableJobScheduler{
		activeIDs:       map[string]struct{}{},
		activeKeys:      map[string]struct{}{},
		results:         make(chan scheduledJobResult, durableJobLocalLaneWorkers+durableJobNotionProviderWorkers),
		deliveryResults: make(chan scheduledDatabaseAutomationDeliveryResult, 1),
		holds:           newDurableJobHolds(),
	}
}

func (r *durableJobRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	scheduler := newDurableJobScheduler()
	defer scheduler.wait.Wait()
	// A runner that stops moving leaves its stacks (jobs_scheduler_watch.go).
	progress := &durableJobProgress{}
	go r.watchProgress(ctx, progress)
	scheduler.progress = progress
	scans := r.newDurableJobScans()
	for {
		now := r.clock.now()
		scheduleAgain, ok := r.runScans(ctx, now, scheduler.localActive, scans)
		if !ok {
			return
		}
		// A read the store refuses waits and is logged once (readRefused),
		// not tried and logged again each round.
		dispatched, wait, err := 0, scheduler.holds.readWait("job.dispatch", now), error(nil)
		if wait == 0 {
			dispatched, wait, err = r.dispatchReady(ctx, scheduler)
			if err == nil {
				scheduler.holds.readSettled("job.dispatch")
			} else if !r.readRefused(ctx, scheduler, "job.dispatch", err, &wait) {
				return
			}
		}
		deliveryDispatched, deliveryWait, deliveryErr := false, scheduler.holds.readWait("database-automation.delivery", now), error(nil)
		if deliveryWait == 0 {
			deliveryDispatched, deliveryWait, deliveryErr = r.dispatchDatabaseAutomationDelivery(ctx, scheduler, now)
			if deliveryErr == nil {
				scheduler.holds.readSettled("database-automation.delivery")
			} else if !r.readRefused(ctx, scheduler, "database-automation.delivery", deliveryErr, &deliveryWait) {
				return
			}
		}
		wait = earliestDurableJobSchedulerWait(wait, deliveryWait)
		if dispatched > 0 || deliveryDispatched {
			continue
		}
		if scheduleAgain {
			continue
		}
		wait, rescan := normalizeDurableJobSchedulerWait(scheduler.active(), wait)
		if rescan {
			continue
		}

		var timerOwner durableJobTimer
		var timer <-chan time.Time
		if wait > 0 {
			timerOwner = r.clock.newTimer(wait)
			timer = timerOwner.channelValue()
		}
		select {
		case <-ctx.Done():
			if timerOwner != nil {
				timerOwner.stop()
			}
			return
		case <-r.wake:
		case result := <-scheduler.results:
			scheduler.complete(result.jobs)
			settleErr := result.err
			if !result.settled {
				settleErr = r.settleReadyJob(ctx, result.jobs[0].ID, result.err)
			}
			if settleErr != nil {
				if errors.Is(settleErr, context.Canceled) && ctx.Err() != nil {
					if timerOwner != nil {
						timerOwner.stop()
					}
					return
				}
				// The job waits before its next turn (jobs_scheduler_hold.go);
				// those of its batch before it went through.
				failed := result.failed
				if failed == "" {
					failed = result.jobs[0].ID
				}
				for index, job := range result.jobs {
					if job.ID == failed {
						scheduler.holds.settled(result.jobs[:index])
						break
					}
				}
				if scheduler.holds.hold(failed, r.clock.now()) {
					logDurableJobDiagnostic(
						r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), failed,
						"job.dispatch", "settle", settleErr,
					)
				}
			} else {
				scheduler.holds.settled(result.jobs)
			}
		case result := <-scheduler.deliveryResults:
			scheduler.completeDatabaseAutomationDelivery()
			if result.err != nil {
				if errors.Is(result.err, context.Canceled) && ctx.Err() != nil {
					if timerOwner != nil {
						timerOwner.stop()
					}
					return
				}
				if scheduler.holds.holdDelivery(r.clock.now()) {
					logDurableJobDiagnostic(
						r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "",
						"database-automation.delivery", "settle", result.err,
					)
				}
			} else {
				scheduler.holds.deliverySettled()
			}
			if result.err == nil && result.result.HasMore {
				r.notify()
			}
		case <-timer:
		}
		if timerOwner != nil {
			timerOwner.stop()
		}
	}
}

// readRefused has a read the store refused wait before its next try, logging
// the first refusal in a row; false once the runner is stopping instead.
func (r *durableJobRunner) readRefused(ctx context.Context, scheduler *durableJobScheduler, read string, err error, wait *time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	held, first := scheduler.holds.readRefused(read, r.clock.now())
	if first {
		logDurableJobDiagnostic(r.app, errorCodeJobRunnerFailed, newDiagnosticReference(), "", read, "select", err)
	}
	*wait = held
	return true
}

func (r *durableJobRunner) dispatchReady(ctx context.Context, scheduler *durableJobScheduler) (int, time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, -1, err
	}
	ready := make([]scheduledReadyJob, 0, durableJobBatchSize)
	// Local jobs take turns by their owner: each person's first ready job,
	// oldest first, then each one's second, so one person queueing
	// thousands doesn't push everyone else's exports, imports and copies
	// behind them (2026-10-18).
	err := r.app.DB().NewQuery(`
		WITH [[classified]] AS (
			SELECT [[id]], [[kind]], [[payload_json]], [[not_before]], [[created]], [[owner]],
				CASE WHEN [[kind]] = {:notionKind} THEN
					CASE WHEN json_valid([[payload_json]])
						AND COALESCE(json_extract([[payload_json]], '$.connectionId'), '') <> ''
					THEN {:notionLane} || ':' || json_extract([[payload_json]], '$.connectionId')
					ELSE {:notionLane} || ':invalid:' || [[id]] END
				ELSE {:localLane} || ':' || [[id]] END AS [[lane_key]]
			FROM {{leafen_jobs}}
			WHERE [[status]] = 'queued' AND [[not_before]] <= {:now}
		), [[ranked]] AS (
			SELECT [[id]], [[kind]], [[payload_json]], [[not_before]], [[created]], [[lane_key]],
				ROW_NUMBER() OVER (
					PARTITION BY [[lane_key]] ORDER BY [[not_before]], [[created]], [[id]]
				) AS [[lane_rank]],
				CASE WHEN [[kind]] = {:notionKind} THEN 1 ELSE ROW_NUMBER() OVER (
					PARTITION BY [[kind]] = {:notionKind}, [[owner]] ORDER BY [[not_before]], [[created]], [[id]]
				) END AS [[owner_turn]]
			FROM [[classified]]
		), [[lane_heads]] AS (
			SELECT [[id]], [[kind]], [[payload_json]], [[not_before]], [[created]], [[lane_key]], [[owner_turn]]
			FROM [[ranked]] WHERE [[lane_rank]] = 1
		), [[bounded]] AS (
			SELECT [[id]], [[kind]], [[payload_json]], [[not_before]], [[created]], [[lane_key]], [[owner_turn]],
				ROW_NUMBER() OVER (
					PARTITION BY CASE WHEN [[kind]] = {:notionKind} THEN {:notionLane} ELSE {:localLane} END
					ORDER BY [[owner_turn]], [[not_before]], [[created]], [[id]]
				) AS [[class_rank]]
			FROM [[lane_heads]]
		)
		SELECT [[id]], [[kind]], [[payload_json]], [[lane_key]]
		FROM [[bounded]] WHERE [[class_rank]] <= {:limit}
		ORDER BY [[owner_turn]], [[not_before]], [[created]], [[id]]
	`).WithContext(ctx).Bind(dbx.Params{
		"notionKind": notionImportJobKind, "notionLane": durableJobNotionProviderLane,
		"localLane": durableJobLocalLane, "now": r.clock.now().UnixMilli(),
		"limit": durableJobBatchSize,
	}).All(&ready)
	if err != nil {
		return 0, -1, err
	}
	dispatched := 0
	now := r.clock.now()
	localJobs := make([]scheduledReadyJob, 0, durableJobBatchSize)
	// Under memory pressure the jobs take their turns at its pace, people's
	// requests going on (memory_pressure_long_work.go).
	paced, localDone := false, func() {}
	for _, job := range ready {
		if _, active := scheduler.activeIDs[job.ID]; active || scheduler.holds.held(job.ID, now) {
			continue
		}
		if job.Kind != notionImportJobKind {
			if scheduler.localActive == 0 && len(localJobs) == 0 {
				ok, done := memoryLongWorkTurn()
				if !ok {
					paced = true
					continue
				}
				localDone = done
			}
			if scheduler.localActive == 0 {
				localJobs = append(localJobs, job)
			}
			continue
		}
		if !scheduler.canDispatchNotion(job) || job.LaneKey != notionProviderLaneKey(job) {
			continue
		}
		ok, done := memoryLongWorkTurn()
		if !ok {
			paced = true
			continue
		}
		scheduler.startNotion(job)
		dispatched++
		go func(candidate scheduledReadyJob) {
			defer scheduler.wait.Done()
			defer done()
			// People's edits first (write_lane.go).
			yieldToPeople(ctx)
			result := scheduledJobResult{jobs: []scheduledReadyJob{candidate}, err: r.applyDurableJobTurn(ctx, candidate.ID),
				failed: candidate.ID, settled: true}
			select {
			case scheduler.results <- result:
			case <-ctx.Done():
			}
		}(job)
	}
	if len(localJobs) > 0 {
		scheduler.startLocal(localJobs)
		dispatched += len(localJobs)
		go func(batch []scheduledReadyJob) {
			defer scheduler.wait.Done()
			defer localDone()
			var batchErr error
			failed := ""
			for _, candidate := range batch {
				if err := ctx.Err(); err != nil {
					batchErr = err
					break
				}
				// People's edits first (write_lane.go).
				yieldToPeople(ctx)
				if scheduler.progress != nil {
					scheduler.progress.begin(candidate.ID, time.Now())
				}
				err := r.applyDurableJobTurn(ctx, candidate.ID)
				if scheduler.progress != nil {
					scheduler.progress.end()
				}
				if err != nil {
					batchErr, failed = err, candidate.ID
					break
				}
			}
			result := scheduledJobResult{jobs: batch, err: batchErr, failed: failed, settled: true}
			select {
			case scheduler.results <- result:
			case <-ctx.Done():
			}
		}(localJobs)
	}
	held, heldWait := scheduler.holds.waiting(now)
	wait, err := r.nextUnheldJobWait(held)
	if paced && err == nil {
		// Those waiting on the memory are asked again shortly, not at once.
		return dispatched, memoryLongWorkBreath, nil
	}
	return dispatched, earliestDurableJobSchedulerWait(wait, heldWait), err
}

// Administrator backups stay on the single local lane because PocketBase's
// backup snapshot is instance-wide and must not overlap another local mutation.
func (r *durableJobRunner) applyAdministratorScheduledJob(ctx context.Context, job storedJob) (bool, error) {
	if job.Kind != administratorBackupJobKind {
		return false, nil
	}
	return true, r.applyAdministratorBackupChunk(ctx, job)
}

func (scheduler *durableJobScheduler) canDispatchNotion(job scheduledReadyJob) bool {
	if scheduler.notionActive >= memoryLongWorkers(durableJobNotionProviderWorkers) {
		return false
	}
	_, active := scheduler.activeKeys[job.LaneKey]
	return !active
}

func (scheduler *durableJobScheduler) startNotion(job scheduledReadyJob) {
	scheduler.activeIDs[job.ID] = struct{}{}
	scheduler.activeKeys[job.LaneKey] = struct{}{}
	scheduler.notionActive++
	scheduler.wait.Add(1)
}

func (scheduler *durableJobScheduler) startLocal(jobs []scheduledReadyJob) {
	for _, job := range jobs {
		scheduler.activeIDs[job.ID] = struct{}{}
	}
	scheduler.localActive++
	scheduler.wait.Add(1)
}

func (scheduler *durableJobScheduler) complete(jobs []scheduledReadyJob) {
	for _, job := range jobs {
		delete(scheduler.activeIDs, job.ID)
	}
	if jobs[0].Kind == notionImportJobKind {
		delete(scheduler.activeKeys, jobs[0].LaneKey)
		scheduler.notionActive--
	} else {
		scheduler.localActive--
	}
}

func (scheduler *durableJobScheduler) active() int {
	active := scheduler.localActive + scheduler.notionActive
	if scheduler.deliveryActive {
		active++
	}
	return active
}

func earliestDurableJobSchedulerWait(left, right time.Duration) time.Duration {
	if left < 0 {
		return right
	}
	if right < 0 || left <= right {
		return left
	}
	return right
}

func (r *durableJobRunner) dispatchDatabaseAutomationDelivery(
	ctx context.Context,
	scheduler *durableJobScheduler,
	now time.Time,
) (bool, time.Duration, error) {
	if scheduler.deliveryActive {
		return false, -1, nil
	}
	if held := scheduler.holds.deliveryWait(now); held > 0 {
		return false, held, nil
	}
	wait, err := nextDatabaseAutomationDeliveryWait(r.app, now)
	if err != nil || wait != 0 {
		return false, wait, err
	}
	scheduler.deliveryActive = true
	scheduler.wait.Add(1)
	go func() {
		defer scheduler.wait.Done()
		result, runErr := processDatabaseAutomationDeliveryPass(
			ctx, r.app, r.clock.now(), r.databaseAutomationDeliveryProvider,
		)
		select {
		case scheduler.deliveryResults <- scheduledDatabaseAutomationDeliveryResult{result: result, err: runErr}:
		case <-ctx.Done():
		}
	}()
	return true, 0, nil
}

func (scheduler *durableJobScheduler) completeDatabaseAutomationDelivery() {
	scheduler.deliveryActive = false
}

func normalizeDurableJobSchedulerWait(active int, wait time.Duration) (time.Duration, bool) {
	if wait != 0 {
		return wait, false
	}
	if active > 0 {
		return -1, false
	}
	return 0, true
}

func notionProviderLaneKey(job scheduledReadyJob) string {
	payload := notionImportJobPayload{}
	if json.Unmarshal([]byte(job.PayloadJSON), &payload) == nil && payload.ConnectionID != "" {
		return durableJobNotionProviderLane + ":" + payload.ConnectionID
	}
	return durableJobNotionProviderLane + ":invalid:" + job.ID
}

func (r *durableJobRunner) drainReady(ctx context.Context) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	ready := make([]readyJob, 0, durableJobBatchSize)
	err := r.app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_jobs}}
		WHERE [[status]] = 'queued' AND [[not_before]] <= {:now}
		ORDER BY [[not_before]], [[created]], [[id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"now": r.clock.now().UnixMilli(), "limit": durableJobBatchSize}).All(&ready)
	if err != nil {
		return -1, err
	}
	for _, job := range ready {
		if err := ctx.Err(); err != nil {
			return -1, err
		}
		if err := r.settleReadyJob(ctx, job.ID, r.apply(ctx, job.ID)); err != nil {
			return -1, err
		}
	}
	if len(ready) > 0 {
		return 0, nil
	}
	return r.nextReadyJobWait()
}

func (r *durableJobRunner) nextReadyJobWait() (time.Duration, error) {
	return r.nextUnheldJobWait(nil)
}
