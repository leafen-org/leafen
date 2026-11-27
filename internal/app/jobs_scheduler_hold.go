package app

import (
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
)

// durableJobUnsettledHoldMax is the longest a job whose turns the store keeps
// refusing waits between them.
const durableJobUnsettledHoldMax = time.Minute

// A job whose turn could not be settled — the store refused the write that
// sets its next turn (read-only, the disk full), so it stays queued and ready
// as it was — waits in the runner's memory instead: durableJobStoreRetryDelay,
// then twice that for each turn in a row the store refuses, up to
// durableJobUnsettledHoldMax. Without it the runner took the same job again at
// once, round after round, logging each. The automation deliveries' pass,
// which writes as it goes, waits the same way after a pass that failed. Once
// the store takes writes again the next turn settles as any other, and the
// wait is forgotten.
type durableJobHold struct {
	until    time.Time
	refusals int
}

type durableJobHolds struct {
	jobs     map[string]durableJobHold
	delivery durableJobHold
	// reads are the runner's own reads that begin its rounds — which jobs are
	// ready and when the next is due, whether an automation delivery is — by
	// what they read, while the store refuses them (readRefused).
	reads map[string]durableJobHold
}

func newDurableJobHolds() durableJobHolds {
	return durableJobHolds{jobs: map[string]durableJobHold{}, reads: map[string]durableJobHold{}}
}

// readRefused has a read the store refused (it can't be read: a broken file,
// a disk gone) wait before it is tried again, as a turn whose write the store
// refuses does: durableJobStoreRetryDelay, doubling each refusal in a row up
// to durableJobUnsettledHoldMax. It gives the wait and whether this is the
// first refusal in a row, the one worth a diagnostic of its own. Without it
// the runner read again every 100 ms and logged each.
func (holds *durableJobHolds) readRefused(read string, now time.Time) (time.Duration, bool) {
	held := holds.reads[read].next(now)
	holds.reads[read] = held
	return held.until.Sub(now), held.refusals == 1
}

// readWait is how long a refused read still waits; 0 once it may run again.
func (holds *durableJobHolds) readWait(read string, now time.Time) time.Duration {
	if held, ok := holds.reads[read]; ok && now.Before(held.until) {
		return held.until.Sub(now)
	}
	return 0
}

// readSettled forgets a read's refusals once the store answers it again.
func (holds *durableJobHolds) readSettled(read string) {
	delete(holds.reads, read)
}

func durableJobHoldDelay(refusals int) time.Duration {
	delay := durableJobStoreRetryDelay
	for turn := 1; turn < refusals && delay < durableJobUnsettledHoldMax; turn++ {
		delay *= 2
	}
	return min(delay, durableJobUnsettledHoldMax)
}

func (held durableJobHold) next(now time.Time) durableJobHold {
	refusals := held.refusals + 1
	return durableJobHold{until: now.Add(durableJobHoldDelay(refusals)), refusals: refusals}
}

// hold has a job wait after a turn it couldn't settle; it says whether this is
// the first such turn in a row, the one worth a diagnostic of its own.
func (holds *durableJobHolds) hold(jobID string, now time.Time) bool {
	held := holds.jobs[jobID].next(now)
	holds.jobs[jobID] = held
	return held.refusals == 1
}

// settled forgets the waits of jobs whose turns went through.
func (holds *durableJobHolds) settled(jobs []scheduledReadyJob) {
	for _, job := range jobs {
		delete(holds.jobs, job.ID)
	}
}

func (holds *durableJobHolds) held(jobID string, now time.Time) bool {
	held, ok := holds.jobs[jobID]
	return ok && now.Before(held.until)
}

// waiting lists the jobs still waiting, with the time until the first of them
// may go again (-1 when none waits). A wait long over whose job never came
// back (it was cancelled or finished elsewhere) is forgotten.
func (holds *durableJobHolds) waiting(now time.Time) ([]string, time.Duration) {
	ids := make([]string, 0, len(holds.jobs))
	next := time.Duration(-1)
	for id, held := range holds.jobs {
		if !now.Before(held.until) {
			if now.Sub(held.until) > durableJobUnsettledHoldMax {
				delete(holds.jobs, id)
			}
			continue
		}
		ids = append(ids, id)
		next = earliestDurableJobSchedulerWait(next, held.until.Sub(now))
	}
	return ids, next
}

func (holds *durableJobHolds) holdDelivery(now time.Time) bool {
	holds.delivery = holds.delivery.next(now)
	return holds.delivery.refusals == 1
}

func (holds *durableJobHolds) deliverySettled() {
	holds.delivery = durableJobHold{}
}

// deliveryWait is how long the deliveries' pass still waits; 0 once it may run.
func (holds *durableJobHolds) deliveryWait(now time.Time) time.Duration {
	if now.Before(holds.delivery.until) {
		return holds.delivery.until.Sub(now)
	}
	return 0
}

// nextUnheldJobWait is nextReadyJobWait for the jobs not waiting in memory.
func (r *durableJobRunner) nextUnheldJobWait(held []string) (time.Duration, error) {
	query := `SELECT MIN([[not_before]]) FROM {{leafen_jobs}} WHERE [[status]] = 'queued'`
	params := dbx.Params{}
	if len(held) > 0 {
		names := make([]string, len(held))
		for index, id := range held {
			name := "held" + strconv.Itoa(index)
			names[index] = "{:" + name + "}"
			params[name] = id
		}
		query += ` AND [[id]] NOT IN (` + strings.Join(names, ", ") + `)`
	}
	var next sql.NullInt64
	if err := r.app.DB().NewQuery(query).Bind(params).Row(&next); err != nil {
		return -1, err
	}
	if !next.Valid {
		return -1, nil
	}
	wait := time.Duration(next.Int64-r.clock.now().UnixMilli()) * time.Millisecond
	if wait < 0 {
		return 0, nil
	}
	return wait, nil
}
