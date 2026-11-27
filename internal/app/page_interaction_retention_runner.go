package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	pageInteractionRetentionTurnLimit    = 128
	pageInteractionRetentionTurnDelay    = time.Millisecond
	pageInteractionRetentionFailureDelay = 100 * time.Millisecond
	pageInteractionRetentionRunnerKey    = "leafenPageInteractionRetentionRunner"
)

type pageInteractionRetentionCandidate struct {
	EventID     string `db:"event_id"`
	WorkspaceID string `db:"workspace_id"`
	OccurredAt  int64  `db:"occurred_at"`
	ExpiresAt   int64  `db:"expires_at"`
	Days        int    `db:"days"`
	Held        bool   `db:"held"`
}

type pageInteractionRetentionTurn struct {
	Deleted int
	HasMore bool
	NextDue int64
}

type pageInteractionRetentionRunner struct {
	app  core.App
	wake chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	// Where a pass over the views due by the shortest period has got to:
	// views a longer period keeps stay behind it until the next pass.
	afterExpires int64
	afterID      string
}

func newPageInteractionRetentionRunner(app core.App) *pageInteractionRetentionRunner {
	return &pageInteractionRetentionRunner{app: app, wake: make(chan struct{}, 1)}
}

func (runner *pageInteractionRetentionRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel = cancel
	runner.done = make(chan struct{})
	go runner.run(ctx, runner.done)
}

func (runner *pageInteractionRetentionRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel := runner.cancel
	done := runner.done
	runner.cancel = nil
	runner.done = nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", errorCodePageAnalyticsFailed, ctx.Err())
	}
}

func (runner *pageInteractionRetentionRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *pageInteractionRetentionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().UTC()
		turn, err := runner.runTurn(ctx, now)
		wait := time.Duration(-1)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodePageAnalyticsFailed, "page-interaction.retention", err)
			wait = pageInteractionRetentionFailureDelay
		} else if turn.HasMore {
			wait = pageInteractionRetentionTurnDelay
		} else if turn.NextDue > 0 {
			wait = time.Duration(turn.NextDue-now.UnixMilli()) * time.Millisecond
			if wait < 0 {
				wait = 0
			}
		}

		var timer *time.Timer
		var timerC <-chan time.Time
		if wait >= 0 {
			timer = time.NewTimer(wait)
			timerC = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-runner.wake:
		case <-timerC:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func (runner *pageInteractionRetentionRunner) runTurn(
	ctx context.Context,
	now time.Time,
) (pageInteractionRetentionTurn, error) {
	turn := pageInteractionRetentionTurn{}
	afterExpires, afterID := runner.afterExpires, runner.afterID
	err := runner.app.RunInTransaction(func(txApp core.App) error {
		// A view expires a year after it; it goes once its workspace's
		// organization's period is past, and stays while it keeps them or a
		// legal hold covers it.
		shortest, err := shortestWorkspaceRetentionDays(txApp, "analytics_days")
		if err != nil || shortest == 0 {
			turn.NextDue = now.Add(24 * time.Hour).UnixMilli()
			return err
		}
		scan := int64(maxPageAnalyticsReadDays-min(shortest, pageAnalyticsRetentionScanDays)) * activityRetentionDay
		candidates := []pageInteractionRetentionCandidate{}
		if err := txApp.DB().NewQuery(`
			SELECT [[event_id]], [[workspace_id]], [[occurred_at]], [[expires_at]],
				` + workspaceRetentionDaysSQL("analytics_days", "{{leafen_page_interactions}}.[[workspace_id]]") + ` AS [[days]],
				` + legalHoldKeepsSQL(legalHoldRecordSQL{
			Organization: "{{leafen_page_interactions}}.[[organization_id]]", Workspace: "{{leafen_page_interactions}}.[[workspace_id]]",
			Pages: []string{"{{leafen_page_interactions}}.[[page_id]]"}, People: []string{"{{leafen_page_interactions}}.[[user_id]]"},
		}) + ` AS [[held]]
			FROM {{leafen_page_interactions}}
			WHERE [[interaction_type]] = 'page.viewed' AND [[expires_at]] <= {:due}
				AND ([[expires_at]] > {:afterExpires} OR ([[expires_at]] = {:afterExpires} AND [[event_id]] > {:afterID}))
			ORDER BY [[expires_at]], [[event_id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"due": now.UnixMilli() + scan, "afterExpires": afterExpires, "afterID": afterID,
			"limit": pageInteractionRetentionTurnLimit + 1,
		}).All(&candidates); err != nil {
			return err
		}
		turn.HasMore = len(candidates) > pageInteractionRetentionTurnLimit
		if turn.HasMore {
			candidates = candidates[:pageInteractionRetentionTurnLimit]
			last := candidates[len(candidates)-1]
			afterExpires, afterID = last.ExpiresAt, last.EventID
		} else {
			afterExpires, afterID = 0, ""
		}
		expired := []pageInteractionRetentionCandidate{}
		for _, candidate := range candidates {
			if candidate.Days > 0 && !candidate.Held && candidate.OccurredAt+int64(candidate.Days)*activityRetentionDay <= now.UnixMilli() {
				expired = append(expired, candidate)
			}
		}
		if len(expired) > 0 {
			params := dbx.Params{}
			ids := make([]string, len(expired))
			increments := map[string]int{}
			for index, candidate := range expired {
				key := fmt.Sprintf("event%d", index)
				params[key] = candidate.EventID
				ids[index] = "{:" + key + "}"
				increments[candidate.WorkspaceID]++
			}
			result, err := txApp.DB().NewQuery(`
				DELETE FROM {{leafen_page_interactions}}
				WHERE [[event_id]] IN (` + strings.Join(ids, ",") + `) AND [[interaction_type]] = 'page.viewed'
			`).WithContext(ctx).Bind(params).Execute()
			if err != nil {
				return err
			}
			deleted, err := result.RowsAffected()
			if err != nil || deleted != int64(len(expired)) {
				return errors.New("page interaction retention lost its selected frontier")
			}
			if err := bumpPageInteractionRevisionBy(txApp, increments); err != nil {
				return err
			}
			turn.Deleted = len(expired)
		}
		if turn.HasMore {
			return nil
		}
		var next sql.NullInt64
		if err := txApp.DB().NewQuery(`
			SELECT MIN([[expires_at]]) FROM {{leafen_page_interactions}}
			WHERE [[interaction_type]] = 'page.viewed'
		`).WithContext(ctx).Row(&next); err != nil {
			return err
		}
		if next.Valid {
			turn.NextDue = activityRetentionFallbackDue(time.UnixMilli(next.Int64-scan), now).UnixMilli()
		}
		return nil
	})
	if err == nil {
		runner.afterExpires, runner.afterID = afterExpires, afterID
	}
	return turn, err
}

func notifyPageInteractionRetentionAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(pageInteractionRetentionRunnerKey).(*pageInteractionRetentionRunner)
		if ok {
			runner.notify()
		}
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				notify()
			}
			return nil
		})
		return
	}
	notify()
}
