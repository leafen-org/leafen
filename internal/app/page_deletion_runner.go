package app

import (
	"context"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A permanent deletion — of a page, or of the trash — goes on a turn at a
// time with the requests of the tab that asked for it, so one whose tab
// stopped asking (its server restarted, the tab reloaded or closed) went no
// further (2026-11-03 audit: 601 pages stayed "discovering" for a quarter of
// an hour, the API answering them as gone while the trash still showed
// them, and asking again was refused as a conflict). The runner takes on,
// every few seconds, the deletions no request has moved for a while, so
// each ends as other lasting jobs do.

const (
	pageDeletionRunnerInterval = 3 * time.Second
	// pageDeletionIdleAfter is how long a deletion waits for its tab first.
	pageDeletionIdleAfter   = 5 * time.Second
	pageDeletionRunnerBatch = 8
	// pageDeletionTurnBudget is how long a turn goes on advancing, a
	// request's worth at a time, as the asking tab would ask again.
	pageDeletionTurnBudget = 2 * time.Second
)

type pageDeletionRunner struct {
	app   core.App
	rooms workspaceRoomInvalidator
	mu    sync.Mutex
	stop  context.CancelFunc
	done  chan struct{}
}

func newPageDeletionRunner(app core.App, rooms workspaceRoomInvalidator) *pageDeletionRunner {
	return &pageDeletionRunner{app: app, rooms: rooms}
}

type idlePageDeletion struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
}

func (runner *pageDeletionRunner) runTurn(now time.Time) {
	idle := now.Add(-pageDeletionIdleAfter).UnixMilli()
	until := time.Now().Add(pageDeletionTurnBudget)
	for _, kind := range []struct {
		table   string
		advance func(core.App, string) (bool, error)
	}{
		{"leafen_page_permanent_delete_jobs", func(app core.App, id string) (bool, error) {
			job, err := advancePagePermanentDelete(app, id)
			return job.Status == pagePermanentDeleteStatusCompleted, err
		}},
		{"leafen_trash_empty_jobs", func(app core.App, id string) (bool, error) {
			job, err := advancePageTrashEmpty(app, id)
			return job.Status == trashEmptyStatusCompleted, err
		}},
	} {
		jobs := []idlePageDeletion{}
		if err := runner.app.DB().NewQuery(`SELECT [[id]], [[workspace_id]] FROM {{` + kind.table + `}}
			WHERE [[status]] = 'pending' AND [[updated_at]] <= {:idle} ORDER BY [[updated_at]] LIMIT {:batch}`).
			Bind(dbx.Params{"idle": idle, "batch": pageDeletionRunnerBatch}).All(&jobs); err != nil {
			runner.app.Logger().Warn("leafen page deletion runner couldn't read", "table", kind.table, "error", err)
			continue
		}
		for _, job := range jobs {
			// As the deletion's own request does, open pages of the workspace read again first.
			err := invalidatePermanentPageRooms(runner.rooms, job.WorkspaceID)
			for done := false; err == nil && !done && time.Now().Before(until); {
				done, err = kind.advance(runner.app, job.ID)
			}
			if err != nil {
				runner.app.Logger().Warn("leafen page deletion runner couldn't advance", "table", kind.table, "job", job.ID, "error", err)
			}
		}
	}
}

func (runner *pageDeletionRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.stop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.stop, runner.done = cancel, make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		ticker := time.NewTicker(pageDeletionRunnerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runner.runTurn(time.Now().UTC())
			}
		}
	}(runner.done)
}

func (runner *pageDeletionRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	stop, done := runner.stop, runner.done
	runner.stop, runner.done = nil, nil
	runner.mu.Unlock()
	if stop == nil {
		return nil
	}
	stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
