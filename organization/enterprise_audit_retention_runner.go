package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

type auditRetentionSliceResult struct {
	Discovered             int
	ProcessedOrganizations int
	Deleted                int
	PreservedByLegalHold   int
	HasMore                bool
	NextWakeAt             int64
}

type enterpriseAuditRetentionRunner struct {
	app   core.App
	clock leafen.DurableJobClock
	wake  chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	runMu  sync.Mutex
	// heldPast is where each organization's turn of a sweep has got to:
	// the events a legal hold keeps stay behind it (under runMu).
	heldPast map[string]leafen.StoredAuditRetentionEvent
}

func newEnterpriseAuditRetentionRunner(app core.App) *enterpriseAuditRetentionRunner {
	return newEnterpriseAuditRetentionRunnerWithClock(app, leafen.RealDurableJobClock{})
}

func newEnterpriseAuditRetentionRunnerWithClock(
	app core.App,
	clock leafen.DurableJobClock,
) *enterpriseAuditRetentionRunner {
	return &enterpriseAuditRetentionRunner{
		app: app, clock: clock, wake: make(chan struct{}, 1),
		heldPast: map[string]leafen.StoredAuditRetentionEvent{},
	}
}

func registerEnterpriseAuditRetentionCron(
	app core.App,
	runner *enterpriseAuditRetentionRunner,
) {
	app.Cron().MustAdd("leafen-enterprise-audit-retention", leafen.AuditRetentionCron, runner.notify)
}

func (r *enterpriseAuditRetentionRunner) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	go r.run(ctx, r.done)
}

func (r *enterpriseAuditRetentionRunner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.cancel = nil
	r.done = nil
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", leafen.ErrorCodeAuditRetentionRunnerFailed, ctx.Err())
	}
}

func (r *enterpriseAuditRetentionRunner) notify() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (runner *enterpriseAuditRetentionRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			leafen.LogDurableJobDiagnostic(
				runner.app, leafen.ErrorCodeAuditRetentionRunnerFailed, leafen.NewDiagnosticReference(), "",
				"enterprise.audit_retention", "slice", err,
			)
		}
		if wait == 0 {
			continue
		}

		var clock leafen.DurableJobTimer
		var timer <-chan time.Time
		if wait > 0 {
			clock = leafen.DurableJobClockNewTimer(runner.clock, wait)
			timer = leafen.DurableJobTimerChannelValue(clock)
		}
		select {
		case <-ctx.Done():
			if clock != nil {
				leafen.DurableJobTimerStop(clock)
			}
			return
		case <-runner.wake:
		case <-timer:
		}
		if clock != nil {
			leafen.DurableJobTimerStop(clock)
		}
	}
}

func (runner *enterpriseAuditRetentionRunner) runTurn(ctx context.Context) (time.Duration, error) {
	result, err := runner.runSlice(ctx, leafen.DurableJobClockNow(runner.clock).UTC())
	if err != nil {
		return -1, err
	}
	if !result.HasMore {
		return -1, nil
	}
	wait := time.Duration(
		result.NextWakeAt-leafen.DurableJobClockNow(runner.clock).UTC().UnixMilli(),
	) * time.Millisecond
	if wait <= 0 {
		return 0, nil
	}
	return wait, nil
}

func (r *enterpriseAuditRetentionRunner) runSlice(
	ctx context.Context,
	scheduledAt time.Time,
) (auditRetentionSliceResult, error) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return auditRetentionSliceResult{}, err
	}
	if scheduledAt.IsZero() {
		return auditRetentionSliceResult{}, leafen.ErrAuditRetentionInvalid
	}
	if leafen.AuditRetentionOrganizationsPerDiscovery < 1 ||
		leafen.AuditRetentionOrganizationsPerSlice < 1 ||
		leafen.AuditRetentionEventsPerOrganization < 1 ||
		leafen.AuditRetentionEventsPerSlice < leafen.AuditRetentionEventsPerOrganization ||
		leafen.OrganizationAuditEventsTable == "" || leafen.OrganizationLegalHoldsTable == "" {
		return auditRetentionSliceResult{}, leafen.ErrAuditRetentionUnavailable
	}
	scheduledAt = scheduledAt.UTC()
	logicalDate, sweepStartedAt := auditRetentionLogicalBoundary(scheduledAt)
	now := scheduledAt.UnixMilli()
	sweep := leafen.StoredAuditRetentionSweep{}
	if err := runAuditRetentionTransaction(ctx, r.app, func(txApp core.App) error {
		var err error
		sweep, err = leafen.LoadOrCreateAuditRetentionSweep(
			ctx, txApp, logicalDate, sweepStartedAt.UnixMilli(),
		)
		return err
	}); err != nil {
		return auditRetentionSliceResult{}, errors.Join(leafen.ErrAuditRetentionUnavailable, err)
	}
	if sweep.Status == "completed" {
		return auditRetentionSliceResult{}, nil
	}
	leaseToken := security.RandomString(32)
	acquired, err := leafen.AcquireAuditRetentionLease(ctx, r.app, sweep.ID, leaseToken, now)
	if err != nil {
		return auditRetentionSliceResult{}, errors.Join(leafen.ErrAuditRetentionUnavailable, err)
	}
	if !acquired {
		nextWakeAt, hasMore, wakeErr := leafen.NextAuditRetentionWakeAt(
			ctx, r.app, sweep.ID, now,
		)
		return auditRetentionSliceResult{
			HasMore: hasMore, NextWakeAt: nextWakeAt,
		}, wakeErr
	}
	leaseReleased := false
	defer func() {
		if !leaseReleased {
			_ = leafen.ReleaseAuditRetentionLease(ctx, r.app, sweep.ID, leaseToken, now)
		}
	}()

	result := auditRetentionSliceResult{}
	if sweep.DiscoveryComplete == 0 {
		err = runAuditRetentionTransaction(ctx, r.app, func(txApp core.App) error {
			var discoveryComplete bool
			result.Discovered, discoveryComplete, err = leafen.DiscoverAuditRetentionOrganizations(
				ctx, txApp, sweep, now,
			)
			if discoveryComplete {
				sweep.DiscoveryComplete = 1
			}
			return err
		})
		if err != nil {
			return result, errors.Join(leafen.ErrAuditRetentionUnavailable, err)
		}
	}

	workItems, err := leafen.LoadDueAuditRetentionWork(ctx, r.app, sweep.ID, now)
	if err != nil {
		return result, errors.Join(leafen.ErrAuditRetentionUnavailable, err)
	}
	remainingEvents := leafen.AuditRetentionEventsPerSlice
	for _, work := range workItems {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if remainingEvents <= 0 {
			break
		}
		deleted, preserved, processErr := r.processAuditRetentionWork(
			ctx, work, sweep, now, remainingEvents,
		)
		if processErr != nil {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			reference := leafen.NewDiagnosticReference()
			leafen.LogDurableJobDiagnostic(
				r.app, leafen.ErrorCodeAuditRetentionRunnerFailed, reference, work.Organization,
				"enterprise.audit_retention", "organization", processErr,
			)
			if markErr := r.markAuditRetentionRetry(ctx, work, now, processErr); markErr != nil {
				leafen.LogDurableJobDiagnostic(
					r.app, leafen.ErrorCodeAuditRetentionRunnerFailed, reference, work.Organization,
					"enterprise.audit_retention", "retry-record", markErr,
				)
				return result, errors.Join(processErr, markErr)
			}
			continue
		}
		result.ProcessedOrganizations++
		result.Deleted += deleted
		remainingEvents -= deleted
		if preserved {
			result.PreservedByLegalHold++
		}
	}

	completed, err := leafen.CompleteAuditRetentionSweepIfDrained(ctx, r.app, sweep.ID, now)
	if err != nil {
		return result, errors.Join(leafen.ErrAuditRetentionUnavailable, err)
	}
	if completed {
		leaseReleased = true
		return result, nil
	}
	if err := leafen.ReleaseAuditRetentionLease(ctx, r.app, sweep.ID, leaseToken, now); err != nil {
		return result, errors.Join(leafen.ErrAuditRetentionUnavailable, err)
	}
	leaseReleased = true
	result.NextWakeAt, result.HasMore, err = leafen.NextAuditRetentionWakeAt(
		ctx, r.app, sweep.ID, now,
	)
	return result, err
}

func auditRetentionLogicalBoundary(now time.Time) (string, time.Time) {
	now = now.UTC()
	boundary := time.Date(
		now.Year(), now.Month(), now.Day(), leafen.AuditRetentionLogicalHourUTC,
		leafen.AuditRetentionLogicalMinuteUTC, 0, 0, time.UTC,
	)
	if now.Before(boundary) {
		boundary = boundary.AddDate(0, 0, -1)
	}
	return boundary.Format(time.DateOnly), boundary
}

func (r *enterpriseAuditRetentionRunner) processAuditRetentionWork(
	ctx context.Context,
	work leafen.StoredAuditRetentionWork,
	sweep leafen.StoredAuditRetentionSweep,
	now int64,
	remainingEvents int,
) (int, bool, error) {
	deleted := 0
	preserved := false
	place := work.SweepID + "\x00" + work.Organization
	past, more := r.heldPast[place], false
	err := runAuditRetentionTransaction(ctx, r.app, func(txApp core.App) error {
		ownerID, policyVersion, heldAll, err := loadAuditRetentionCurrentAuthority(
			ctx, txApp, work.Organization,
		)
		if err != nil {
			return err
		}
		policy, err := leafen.LoadOrganizationAuditRetentionPolicyContext(
			ctx, txApp, work.Organization,
		)
		if err != nil {
			return err
		}
		if heldAll {
			preserved = true
			return leafen.UpdateAuditRetentionWork(ctx, txApp, work, "done", 0, 0, "", now)
		}
		limit := leafen.AuditRetentionEventsPerOrganization
		if remainingEvents < limit {
			limit = remainingEvents
		}
		// An organization that keeps its audit log loses nothing.
		cutoff := leafen.AuditRetentionCutoff(sweep.StartedAt, policy.RetentionDays)
		if cutoff == 0 {
			return leafen.UpdateAuditRetentionWork(ctx, txApp, work, "done", 0, 0, "", now)
		}
		// What a legal hold covers stays; the next turn goes on past it.
		events, err := leafen.LoadExpiredAuditRetentionEvents(
			ctx, txApp, work.Organization, cutoff, past, limit,
		)
		if err != nil {
			return err
		}
		status := "done"
		nextAttemptAt := int64(0)
		if len(events) == limit && limit > 0 {
			status = "backlog"
			nextAttemptAt = now + 2*60*1000
			more = true
		}
		if err := leafen.DeleteAuditRetentionEvents(ctx, txApp, work.Organization, events); err != nil {
			return err
		}
		if err := leafen.UpdateAuditRetentionWork(
			ctx, txApp, work, status, len(events), nextAttemptAt, "", now,
		); err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		if err := bumpOrganizationPolicyVersion(
			ctx, txApp, work.Organization, policyVersion, now,
		); err != nil {
			return err
		}
		lastEvent := events[len(events)-1]
		if err := leafen.RecordOrganizationAuditAtContext(
			ctx, txApp, work.Organization, ownerID,
			"organization_audit.retention_prune", "audit_retention_sweep",
			sweep.ID+":"+lastEvent.ID, now,
		); err != nil {
			return err
		}
		deleted = len(events)
		past = events[len(events)-1]
		return nil
	})
	if err == nil && more {
		r.heldPast[place] = past
	} else if err == nil {
		delete(r.heldPast, place)
	}
	return deleted, preserved, err
}

func loadAuditRetentionCurrentAuthority(
	ctx context.Context,
	app core.App,
	organizationID string,
) (string, int, bool, error) {
	type authorityRow struct {
		OwnerID       string `db:"owner_id"`
		PolicyVersion int    `db:"policy_version"`
	}
	row := authorityRow{}
	err := app.DB().NewQuery(`
		SELECT [[organization]].[[owner]] AS [[owner_id]], [[policy]].[[version]] AS [[policy_version]]
		FROM {{leafen_organizations}} [[organization]]
		JOIN {{leafen_organization_policy_state}} [[policy]]
			ON [[policy]].[[organization_id]] = [[organization]].[[id]]
		WHERE [[organization]].[[id]] = {:organizationId}
	`).WithContext(ctx).Bind(dbx.Params{"organizationId": organizationID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, leafen.ErrAuditRetentionUnavailable
	}
	if err != nil || row.OwnerID == "" || row.PolicyVersion < 0 {
		return "", 0, false, errors.Join(leafen.ErrAuditRetentionUnavailable, err)
	}
	// A hold over all of the organization keeps its whole log; narrower
	// holds keep what they cover, event by event.
	var heldAll int
	err = app.DB().NewQuery(`SELECT ` + retentionHoldSQL(leafen.LegalHoldRecordSQL{Organization: "{:organizationId}"})).
		WithContext(ctx).Bind(dbx.Params{"organizationId": organizationID}).Row(&heldAll)
	if err != nil {
		return "", 0, false, errors.Join(leafen.ErrAuditRetentionUnavailable, err)
	}
	return row.OwnerID, row.PolicyVersion, heldAll == 1, nil
}

func bumpOrganizationPolicyVersion(
	ctx context.Context,
	app core.App,
	organizationID string,
	priorVersion int,
	now int64,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_policy_state}}
		SET [[version]] = [[version]] + 1, [[updated]] = {:now}
		WHERE [[organization_id]] = {:organizationId} AND [[version]] = {:priorVersion}
	`).WithContext(ctx).Bind(dbx.Params{
		"now": now, "organizationId": organizationID, "priorVersion": priorVersion,
	}).Execute()
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return leafen.ErrAuditRetentionConflict
	}
	return nil
}

func (r *enterpriseAuditRetentionRunner) markAuditRetentionRetry(
	ctx context.Context,
	work leafen.StoredAuditRetentionWork,
	now int64,
	cause error,
) error {
	return runAuditRetentionTransaction(ctx, r.app, func(txApp core.App) error {
		status := "retry"
		nextAttemptAt := now + 2*60*1000
		if errors.Is(cause, leafen.ErrAuditRetentionInvalid) ||
			errors.Is(cause, leafen.ErrAuditRetentionPolicyInvalid) {
			status = "failed"
			nextAttemptAt = 0
		}
		return leafen.UpdateAuditRetentionWork(
			ctx, txApp, work, status, 0, nextAttemptAt,
			leafen.ErrorCodeAuditRetentionRunnerFailed, now,
		)
	})
}

func runAuditRetentionTransaction(
	ctx context.Context,
	app core.App,
	operation func(core.App) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return app.RunInTransaction(operation)
}
