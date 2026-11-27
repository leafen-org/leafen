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
	mcpCredentialRegistryTurnLimit    = 64
	mcpCredentialRegistryTurnDelay    = time.Millisecond
	mcpCredentialRegistryFailureDelay = 100 * time.Millisecond
	mcpCredentialRegistryStoreKey     = "leafenMCPCredentialRegistryLifecycle"
)

var (
	errMCPConnectionCapacity  = errors.New(errorCodeMCPConnectionCapacity)
	errMCPOAuthClientCapacity = errors.New(errorCodeMCPOAuthClientCapacity)
)

type mcpCredentialRegistryTurn struct {
	DeletedConnections int64
	DeletedClients     int64
	HasMore            bool
	NextDue            int64
}

type mcpCredentialRegistryLifecycle struct {
	app   core.App
	clock durableJobClock
	wake  chan struct{}
	sse   *mcpSSESessionRuntime

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func (runner *mcpCredentialRegistryLifecycle) bindSSESessionRuntime(runtime *mcpSSESessionRuntime) {
	runner.sse = runtime
}

func newMCPCredentialRegistryLifecycle(app core.App) *mcpCredentialRegistryLifecycle {
	return newMCPCredentialRegistryLifecycleWithClock(app, realDurableJobClock{})
}

func newMCPCredentialRegistryLifecycleWithClock(
	app core.App,
	clock durableJobClock,
) *mcpCredentialRegistryLifecycle {
	return &mcpCredentialRegistryLifecycle{
		app: app, clock: clock, wake: make(chan struct{}, 1),
	}
}

func (runner *mcpCredentialRegistryLifecycle) Start() {
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

func (runner *mcpCredentialRegistryLifecycle) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeMCPConnectionFailed, ctx.Err())
	}
}

func (runner *mcpCredentialRegistryLifecycle) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *mcpCredentialRegistryLifecycle) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := runner.clock.now().UTC()
		turn, err := runner.runTurn(ctx, now)
		wait := time.Duration(-1)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logRunnerTurnError(runner.app, errorCodeMCPConnectionFailed, "mcp-credential-registry.cleanup", err)
			wait = mcpCredentialRegistryFailureDelay
		} else if turn.HasMore {
			wait = mcpCredentialRegistryTurnDelay
		} else if turn.NextDue > 0 {
			wait = time.Duration(turn.NextDue-now.UnixMilli()) * time.Millisecond
			if wait < 0 {
				wait = 0
			}
		}

		var clock durableJobTimer
		var timer <-chan time.Time
		if wait >= 0 {
			clock = runner.clock.newTimer(wait)
			timer = clock.channelValue()
		}
		select {
		case <-ctx.Done():
			if clock != nil {
				clock.stop()
			}
			return
		case <-runner.wake:
		case <-timer:
		}
		if clock != nil {
			clock.stop()
		}
	}
}

func (runner *mcpCredentialRegistryLifecycle) runTurn(
	ctx context.Context,
	now time.Time,
) (mcpCredentialRegistryTurn, error) {
	turn := mcpCredentialRegistryTurn{}
	if err := ctx.Err(); err != nil {
		return turn, err
	}
	nowMillis := now.UTC().UnixMilli()
	candidates, err := listMCPCredentialCleanupCandidates(
		ctx, runner.app, nowMillis, mcpCredentialRegistryTurnLimit,
	)
	if err != nil {
		return turn, err
	}
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return turn, err
		}
		outcome, cleanupErr := cleanupMCPCredentialCandidate(
			ctx, runner.app, candidate, nowMillis,
		)
		if cleanupErr != nil {
			if ctx.Err() != nil {
				return turn, ctx.Err()
			}
			if retryErr := scheduleMCPCredentialCleanupRetry(
				ctx, runner.app, candidate,
				nowMillis+mcpCredentialRegistryFailureDelay.Milliseconds(),
			); retryErr != nil {
				return turn, errors.Join(cleanupErr, retryErr)
			}
			logDurableJobDiagnostic(
				runner.app, errorCodeMCPConnectionFailed, newDiagnosticReference(), "",
				"mcp-credential-registry.cleanup", candidate.Kind+"-delete", cleanupErr,
			)
			continue
		}
		switch outcome {
		case mcpCredentialCleanupDeletedConnection:
			turn.DeletedConnections++
		case mcpCredentialCleanupDeletedClient:
			turn.DeletedClients++
		}
	}
	nextDue, found, err := nextMCPCredentialCleanupDue(ctx, runner.app)
	if err != nil {
		return turn, err
	}
	if found && nextDue <= nowMillis {
		turn.HasMore = true
	} else if found {
		turn.NextDue = nextDue
	}
	return turn, nil
}

func admitMCPConnectionInTransaction(app core.App, ownerID string, now int64) error {
	reached, err := mcpConnectionCapacityReached(app, ownerID, now)
	if err != nil {
		return err
	}
	if reached {
		return errMCPConnectionCapacity
	}
	return nil
}

func mcpConnectionCapacityReached(app core.App, ownerID string, now int64) (bool, error) {
	var boundaryID string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_mcp_connections}}
		WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0
			AND ([[oauth_client_id]] = '' OR [[refresh_expires_at]] > {:now})
		ORDER BY [[id]] LIMIT 1 OFFSET {:offset}
	`).Bind(dbx.Params{
		"ownerId": ownerID, "now": now, "offset": maxActiveMCPConnectionsPerOwner - 1,
	}).Row(&boundaryID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

// countActiveMCPConnections counts, up to the limit, the person's
// connections that hold a place under it.
func countActiveMCPConnections(app core.App, ownerID string, now int64) (int, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM {{leafen_mcp_connections}}
			WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0
				AND ([[oauth_client_id]] = '' OR [[refresh_expires_at]] > {:now})
			LIMIT {:limit}
		)
	`).Bind(dbx.Params{
		"ownerId": ownerID, "now": now, "limit": maxActiveMCPConnectionsPerOwner,
	}).Row(&count)
	return count, err
}

func admitMCPOAuthClientInTransaction(ctx context.Context, app core.App, now int64) error {
	reached, err := mcpOAuthClientCapacityReached(app)
	if err != nil {
		return err
	}
	if !reached {
		return nil
	}
	if err := cleanupMCPOAuthClientCapacityInTransaction(
		ctx, app, now, mcpCredentialRegistryTurnLimit,
	); err != nil {
		return err
	}
	reached, err = mcpOAuthClientCapacityReached(app)
	if err != nil {
		return err
	}
	if reached {
		return errMCPOAuthClientCapacity
	}
	return nil
}

func mcpOAuthClientCapacityReached(app core.App) (bool, error) {
	var boundaryID string
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_mcp_oauth_clients}}
		ORDER BY [[last_used_at]], [[id]] LIMIT 1 OFFSET {:offset}
	`).Bind(dbx.Params{"offset": maxMCPOAuthClients - 1}).Row(&boundaryID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func touchMCPOAuthClient(app core.App, clientID string, now int64) error {
	// A registered app stays while its person keeps it.
	if strings.HasPrefix(clientID, "lapp_") {
		_, err := findRegisteredAppClient(app, clientID)
		return err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_mcp_oauth_clients}}
		SET [[last_used_at]] = CASE
			WHEN [[last_used_at]] < {:now} THEN {:now} ELSE [[last_used_at]] END,
			[[cleanup_retry_at]] = 0
		WHERE [[id]] = {:clientId}
	`).Bind(dbx.Params{"clientId": clientID, "now": now}).Execute()
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return sql.ErrNoRows
	}
	notifyMCPCredentialRegistryAfterCommit(app)
	return nil
}

func cleanupMCPOAuthClientCapacityInTransaction(
	ctx context.Context,
	app core.App,
	now int64,
	limit int,
) error {
	if limit < 1 {
		return errors.New("MCP credential registry cleanup requires a positive limit")
	}
	candidates, err := listMCPOAuthClientCleanupCandidates(ctx, app, now, limit)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if _, cleanupErr := cleanupMCPOAuthClientCandidate(
			ctx, app, candidate.ID, now,
		); cleanupErr != nil {
			if retryErr := scheduleMCPCredentialCleanupRetry(
				ctx, app, candidate,
				now+mcpCredentialRegistryFailureDelay.Milliseconds(),
			); retryErr != nil {
				return errors.Join(cleanupErr, retryErr)
			}
			logDurableJobDiagnostic(
				app, errorCodeMCPConnectionFailed, newDiagnosticReference(), "",
				"mcp-credential-registry.cleanup", "client-admission-delete", cleanupErr,
			)
		}
	}
	return nil
}

func notifyMCPCredentialRegistryAfterCommit(app core.App) {
	notify := func() {
		runner, ok := app.Store().Get(mcpCredentialRegistryStoreKey).(*mcpCredentialRegistryLifecycle)
		if ok {
			runner.notify()
			if runner.sse != nil {
				runner.sse.reconcileCredentials()
			}
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
