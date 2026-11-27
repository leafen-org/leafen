package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
)

// A job runner that stops moving leaves its stacks behind (2026-10-14: an
// import stayed queued at 10,700 of 30,006 items, its next turn long due,
// with the runner idle, and it couldn't be made to happen again). When a
// job's turn has run for ten minutes, or a due job has waited ten minutes
// with no turn running (a Notion import waits its connection's turn, so not
// one of those), the server writes every goroutine's stack to a file
// in its data folder's diagnostics and says so in its log, once an hour at
// most: the next time, what held the runner is there to read.

const (
	durableJobStuckAfter    = 10 * time.Minute
	durableJobWatchEvery    = time.Minute
	durableJobStuckReported = time.Hour
)

// durableJobProgress is how far the runner has come: the job whose turn the
// local lane is running and since when.
type durableJobProgress struct {
	turnAt   atomic.Int64
	turnJob  atomic.Value
	reported atomic.Int64
}

func (progress *durableJobProgress) begin(jobID string, now time.Time) {
	progress.turnJob.Store(jobID)
	progress.turnAt.Store(now.UnixMilli())
}

func (progress *durableJobProgress) end() {
	progress.turnAt.Store(0)
}

// watchProgress looks every minute until the runner stops.
func (r *durableJobRunner) watchProgress(ctx context.Context, progress *durableJobProgress) {
	ticker := time.NewTicker(durableJobWatchEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkProgress(progress, time.Now())
		}
	}
}

func (r *durableJobRunner) checkProgress(progress *durableJobProgress, now time.Time) {
	if last := progress.reported.Load(); last > 0 && now.Sub(time.UnixMilli(last)) < durableJobStuckReported {
		return
	}
	what, jobID := "", ""
	if at := progress.turnAt.Load(); at > 0 && now.Sub(time.UnixMilli(at)) > durableJobStuckAfter {
		jobID, _ = progress.turnJob.Load().(string)
		what = fmt.Sprintf("a job's turn has run since %s", time.UnixMilli(at).UTC().Format(time.RFC3339))
	} else if at == 0 {
		due := struct {
			ID        string `db:"id"`
			NotBefore int64  `db:"not_before"`
		}{}
		if err := r.app.DB().NewQuery(`
			SELECT [[id]], [[not_before]] FROM {{leafen_jobs}}
			WHERE [[status]] = 'queued' AND [[not_before]] <= {:due} AND [[kind]] != {:notionKind}
			ORDER BY [[not_before]] LIMIT 1
		`).Bind(dbx.Params{"due": now.Add(-durableJobStuckAfter).UnixMilli(), "notionKind": notionImportJobKind}).One(&due); err != nil || due.ID == "" {
			return
		}
		jobID = due.ID
		what = fmt.Sprintf("a job due since %s waits with no turn running", time.UnixMilli(due.NotBefore).UTC().Format(time.RFC3339))
	}
	if what == "" {
		return
	}
	progress.reported.Store(now.UnixMilli())
	folder := filepath.Join(r.app.DataDir(), ".diagnostics")
	path := filepath.Join(folder, fmt.Sprintf("job-runner-stuck-%d.txt", now.Unix()))
	if err := os.MkdirAll(folder, 0o700); err != nil {
		r.app.Logger().Error("leafen job runner stuck", "job", jobID, "what", what, "error", err)
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err == nil {
		fmt.Fprintf(file, "%s: %s (job %s)\n\n", now.UTC().Format(time.RFC3339), what, jobID)
		err = pprof.Lookup("goroutine").WriteTo(file, 2)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	r.app.Logger().Error("leafen job runner stuck; its stacks are written", "job", jobID, "what", what, "stacks", path, "error", err)
}
