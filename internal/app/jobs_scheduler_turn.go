package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
)

// durableJobTurnBudget is how long one job's turn goes on with its next
// windows before the runner moves on to other work.
const durableJobTurnBudget = 150 * time.Millisecond

// applyDurableJobTurn runs a job's windows one after another, each settled
// as it ends, while the turn stays short and the job only paused for the
// ordinary wait between windows (durableJobReadyDelay), not for one it chose
// (a retry's, Notion's limits, an operation it waits for). Each window was
// one turn, followed by that wait, so the wait took nearly all of a large
// job: a workspace export of 100,000 rows listed four windows of 32 a
// second, some 13 minutes (2026-11-11). Each window is its own transaction,
// and people's edits still go first between them (write_lane.go).
func (r *durableJobRunner) applyDurableJobTurn(ctx context.Context, jobID string) error {
	deadline := time.Now().Add(durableJobTurnBudget)
	for {
		if err := r.settleReadyJob(ctx, jobID, r.apply(ctx, jobID)); err != nil {
			return err
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			return nil
		}
		again, err := r.durableJobGoesOn(jobID)
		if err != nil || !again {
			return err
		}
		yieldToPeople(ctx)
	}
}

// durableJobGoesOn says whether a job whose window just ended may take its
// next one at once: it is still queued, waits no longer than the ordinary
// pause, and, when it imports from Notion, only applies what it read there
// (its reads of Notion keep their own pace).
func (r *durableJobRunner) durableJobGoesOn(jobID string) (bool, error) {
	job := struct {
		Kind      string `db:"kind"`
		Status    string `db:"status"`
		NotBefore int64  `db:"not_before"`
		Phase     string `db:"phase"`
	}{}
	err := r.app.DB().NewQuery(`
		SELECT [[kind]], [[status]], [[not_before]],
			CASE WHEN json_valid([[payload_json]]) THEN COALESCE(json_extract([[payload_json]], '$.phase'), '') ELSE '' END AS [[phase]]
		FROM {{leafen_jobs}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": jobID}).One(&job)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if job.Status != durableJobStatusQueued || job.NotBefore > r.clock.now().Add(durableJobReadyDelay).UnixMilli() {
		return false, nil
	}
	return job.Kind != notionImportJobKind || job.Phase == notionImportPhaseApply, nil
}
