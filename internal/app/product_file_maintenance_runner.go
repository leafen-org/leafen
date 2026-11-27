package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const productFileMaintenanceRunnerStoreKey = "leafenProductFileMaintenanceRunner"

func fileMaintenanceOperationalCauseCode(err error) string {
	if isSQLiteBusyError(err) {
		return fileDatabaseBusyCauseCode
	}
	return ""
}

type productFileMaintenanceRunner struct {
	app          core.App
	clock        durableJobClock
	upload       productFileMaintenanceLane
	reference    productFileMaintenanceLane
	manualUpload productFileWorkspaceMaintenanceLane
	wake         chan struct{}

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	runMu  sync.Mutex
}

func newProductFileMaintenanceRunner(app core.App) *productFileMaintenanceRunner {
	clock := realDurableJobClock{}
	upload := newProductFileUploadCleanupRunnerWithClock(
		app, clock, cleanupProductFileUploadStorageWithContext,
	)
	reference := newProductFileReferenceCleanupRunnerWithClock(
		app, clock, cleanupProductFileReferenceStorageWithContext,
	)
	runner := newProductFileMaintenanceRunnerWithClock(
		app, clock, upload.runSlice, reference.runSlice,
	)
	runner.manualUpload = upload.runWorkspaceSlice
	return runner
}

func (runner *productFileMaintenanceRunner) runWorkspaceUploadSlice(
	ctx context.Context,
	scheduledAt time.Time,
	workspaceID string,
	manualJobID string,
) (productFileMaintenanceSliceResult, error) {
	if runner.manualUpload == nil {
		return productFileMaintenanceSliceResult{}, errFileMaintenanceStateInvalid
	}
	return runner.manualUpload(ctx, scheduledAt, workspaceID, manualJobID)
}

func newProductFileMaintenanceRunnerWithClock(
	app core.App,
	clock durableJobClock,
	upload productFileMaintenanceLane,
	reference productFileMaintenanceLane,
) *productFileMaintenanceRunner {
	return &productFileMaintenanceRunner{
		app: app, clock: clock, upload: upload, reference: reference,
		wake: make(chan struct{}, 1),
	}
}

func (runner *productFileMaintenanceRunner) Start() {
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

func (runner *productFileMaintenanceRunner) Shutdown(ctx context.Context) error {
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
		return fmt.Errorf("%s: %w", errorCodeFileMaintenanceFailed, ctx.Err())
	}
}

func (runner *productFileMaintenanceRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func (runner *productFileMaintenanceRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		wait, err := runner.runTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logDurableJobDiagnosticWithCause(
				runner.app, errorCodeFileMaintenanceFailed, newDiagnosticReference(), "",
				"file_maintenance.run", "runner-turn",
				fileMaintenanceOperationalCauseCode(err), err,
			)
			wait = time.Duration(fileMaintenanceRunnerRetrySeconds) * time.Second
		}
		if wait == 0 {
			continue
		}
		var jobTimer durableJobTimer
		var timer <-chan time.Time
		if wait > 0 {
			jobTimer = runner.clock.newTimer(wait)
			timer = jobTimer.channelValue()
		}
		select {
		case <-ctx.Done():
			if jobTimer != nil {
				jobTimer.stop()
			}
			return
		case <-runner.wake:
		case <-timer:
		}
		if jobTimer != nil {
			jobTimer.stop()
		}
	}
}

func (runner *productFileMaintenanceRunner) runTurn(
	ctx context.Context,
) (time.Duration, error) {
	result, err := runner.runSlice(ctx, runner.clock.now().UTC())
	if err != nil {
		return -1, err
	}
	if result.HasMore {
		return 0, nil
	}
	if result.NextDue == 0 {
		return -1, nil
	}
	wait := time.Duration(
		result.NextDue-runner.clock.now().UTC().UnixMilli(),
	) * time.Millisecond
	if wait <= 0 {
		return 0, nil
	}
	return wait, nil
}

func mergeProductFileMaintenanceSlice(
	combined *productFileMaintenanceSliceResult,
	next productFileMaintenanceSliceResult,
) {
	combined.Processed += next.Processed
	combined.HasMore = combined.HasMore || next.HasMore
	if combined.NextDue == 0 || (next.NextDue > 0 && next.NextDue < combined.NextDue) {
		combined.NextDue = next.NextDue
	}
	if combined.Workspaces == nil {
		combined.Workspaces = map[string]productFileMaintenanceWorkspaceStats{}
	}
	for workspaceID, stats := range next.Workspaces {
		current := combined.Workspaces[workspaceID]
		current.add(stats)
		combined.Workspaces[workspaceID] = current
	}
}

func (runner *productFileMaintenanceRunner) runSlice(
	ctx context.Context,
	scheduledAt time.Time,
) (productFileMaintenanceSliceResult, error) {
	runner.runMu.Lock()
	defer runner.runMu.Unlock()
	result := productFileMaintenanceSliceResult{
		Workspaces: map[string]productFileMaintenanceWorkspaceStats{},
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if scheduledAt.IsZero() || runner.upload == nil || runner.reference == nil {
		return result, errFileMaintenanceStateInvalid
	}
	pending, pendingErr := settleProductFileMaintenanceOutcomes(ctx, runner.app)
	if pendingErr != nil {
		return result, pendingErr
	}
	if pending.Settled > 0 {
		result.HasMore = true
		return result, nil
	}
	upload, uploadErr := runner.upload(ctx, scheduledAt)
	mergeProductFileMaintenanceSlice(&result, upload)
	reference, referenceErr := runner.reference(ctx, scheduledAt)
	mergeProductFileMaintenanceSlice(&result, reference)
	scannedObjects := 0
	for _, stats := range result.Workspaces {
		scannedObjects += stats.Scanned
	}
	if scannedObjects > fileMaintenanceCombinedTurnLimit ||
		result.Processed > fileMaintenanceCombinedTurnLimit+fileDeleteReceiptCleanupTurnLimit {
		return result, errFileMaintenanceStateInvalid
	}
	settlement, settlementErr := settleProductFileMaintenanceOutcomes(
		ctx, runner.app,
	)
	result.HasMore = result.HasMore || settlement.HasMore
	return result, errors.Join(uploadErr, referenceErr, settlementErr)
}
