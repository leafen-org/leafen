package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/core"
	ygowebsocket "github.com/reearth/ygo/provider/websocket"
)

const (
	ygoSettlementRetryDelay       = 100 * time.Millisecond
	ygoSettlementMaxPendingBytes  = maxYgoUpdateBytes
	ygoSettlementMaxPendingRooms  = 1024
	ygoSettlementCauseBusy        = "SQLITE_DATABASE_BUSY"
	ygoSettlementCauseUnavailable = "SQLITE_STORAGE_UNAVAILABLE"
)

type ygoPersistenceSettlement struct {
	updates             []attributedYgoUpdate
	diagnosticReference string
	state               string
	generation          uint64
	sequence            uint64
	isolationInFlight   bool
}

type ygoPersistenceSettlementOwner struct {
	app         core.App
	persistence *pocketBaseYgoPersistence
	clock       durableJobClock
	wake        chan struct{}

	mu                sync.Mutex
	pendingRooms      map[string]*ygoPersistenceSettlement
	overflow          *ygoPersistenceSettlement
	pendingBytes      int
	inFlight          bool
	nextSequence      uint64
	peakPendingBytes  int
	peakPendingRooms  int
	closeRoom         func(string) error
	cancel            context.CancelFunc
	done              chan struct{}
	workerStarts      atomic.Uint64
	retryTurns        atomic.Uint64
	wakeups           atomic.Uint64
	isolationAttempts atomic.Uint64
}

type ygoPersistenceSettlementMetrics struct {
	WorkerStarts      uint64
	RetryTurns        uint64
	Wakeups           uint64
	IsolationAttempts uint64
	PeakPendingBytes  int
	PeakPendingRooms  int
	PendingBytes      int
	FailedRooms       int
}

func newYgoPersistenceSettlementOwner(
	app core.App,
	persistence *pocketBaseYgoPersistence,
) *ygoPersistenceSettlementOwner {
	return &ygoPersistenceSettlementOwner{
		app: app, persistence: persistence, clock: realDurableJobClock{},
		wake: make(chan struct{}, 1), pendingRooms: map[string]*ygoPersistenceSettlement{},
	}
}

func (owner *ygoPersistenceSettlementOwner) bindCollaborationServer(server *ygowebsocket.Server) {
	if owner == nil || server == nil {
		return
	}
	owner.mu.Lock()
	owner.closeRoom = func(room string) error { return server.CloseRoom(room, true) }
	owner.mu.Unlock()
}

func (owner *ygoPersistenceSettlementOwner) Start() {
	if owner == nil {
		return
	}
	owner.mu.Lock()
	if owner.cancel != nil || owner.closeRoom == nil {
		owner.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	owner.cancel = cancel
	owner.done = done
	owner.workerStarts.Add(1)
	owner.mu.Unlock()
	go owner.run(ctx, done)
}

func (owner *ygoPersistenceSettlementOwner) Shutdown(ctx context.Context) error {
	if owner == nil {
		return nil
	}
	owner.mu.Lock()
	cancel := owner.cancel
	done := owner.done
	owner.cancel = nil
	owner.done = nil
	owner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", errorCodeYgoShutdown, ctx.Err())
	}
}

func ygoSettlementCauseCode(cause error) (string, bool) {
	if isSQLiteStorageUnavailableError(cause) {
		return ygoSettlementCauseUnavailable, true
	}
	if isSQLiteBusyError(cause) {
		return ygoSettlementCauseBusy, true
	}
	return "", false
}

func (owner *ygoPersistenceSettlementOwner) ensureRoomCapacityLocked(room string) bool {
	if _, exists := owner.pendingRooms[room]; exists || len(owner.pendingRooms) < ygoSettlementMaxPendingRooms {
		return true
	}
	oldestRoom := ""
	oldestSequence := ^uint64(0)
	for candidateRoom, candidate := range owner.pendingRooms {
		if candidate.state == collaborationStorageStatusRecovered && candidate.sequence < oldestSequence {
			oldestRoom = candidateRoom
			oldestSequence = candidate.sequence
		}
	}
	if oldestRoom != "" {
		owner.pendingBytes -= attributedYgoUpdatesBytes(owner.pendingRooms[oldestRoom].updates)
		delete(owner.pendingRooms, oldestRoom)
		return true
	}
	return false
}

func (owner *ygoPersistenceSettlementOwner) recordRecovered(room string, reference string) bool {
	if owner == nil || room == "" || !validDiagnosticReference(reference) {
		return false
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	prior := owner.pendingRooms[room]
	if prior != nil && prior.state == collaborationStorageStatusError {
		return false
	}
	if !owner.ensureRoomCapacityLocked(room) {
		return false
	}
	generation := uint64(1)
	if prior != nil {
		generation = prior.generation + 1
		owner.pendingBytes -= attributedYgoUpdatesBytes(prior.updates)
	}
	owner.nextSequence++
	owner.pendingRooms[room] = &ygoPersistenceSettlement{
		diagnosticReference: reference,
		state:               collaborationStorageStatusRecovered,
		generation:          generation,
		sequence:            owner.nextSequence,
	}
	return true
}

func (owner *ygoPersistenceSettlementOwner) oldestPendingRoomLocked(except string) string {
	oldestRoom := ""
	oldestSequence := ^uint64(0)
	for room, settlement := range owner.pendingRooms {
		if room != except && len(settlement.updates) > 0 && settlement.sequence < oldestSequence {
			oldestRoom = room
			oldestSequence = settlement.sequence
		}
	}
	return oldestRoom
}

func (owner *ygoPersistenceSettlementOwner) isolate(room string, generation uint64) {
	owner.isolationAttempts.Add(1)
	owner.mu.Lock()
	closeRoom := owner.closeRoom
	owner.mu.Unlock()
	if closeRoom != nil {
		err := closeRoom(room)
		if err != nil && !errors.Is(err, ygowebsocket.ErrRoomNotFound) {
			owner.app.Logger().Warn("leafen collaboration room isolation failed",
				"code", errorCodeYgoRoomInvalidationFailed,
				"operation", "ygo.persistence.isolate",
				"causeClass", fmt.Sprintf("%T", err))
		}
	}
	owner.mu.Lock()
	if settlement := owner.pendingRooms[room]; settlement != nil && settlement.generation == generation {
		settlement.isolationInFlight = false
	}
	owner.mu.Unlock()
}

func (owner *ygoPersistenceSettlementOwner) notify() {
	select {
	case owner.wake <- struct{}{}:
		owner.wakeups.Add(1)
	default:
	}
}

func (owner *ygoPersistenceSettlementOwner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		pending, err := owner.runTurn(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := time.Duration(-1)
		if pending || err != nil {
			wait = ygoSettlementRetryDelay
		}
		var timer durableJobTimer
		var timerChannel <-chan time.Time
		if wait >= 0 {
			timer = owner.clock.newTimer(wait)
			timerChannel = timer.channelValue()
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.stop()
			}
			return
		case <-owner.wake:
		case <-timerChannel:
		}
		if timer != nil {
			timer.stop()
		}
	}
}

func (owner *ygoPersistenceSettlementOwner) runTurn(ctx context.Context) (bool, error) {
	room, updates, generation, found := owner.nextPending()
	if !found {
		return false, nil
	}
	owner.retryTurns.Add(1)
	var err error
	if len(updates) == 0 {
		err = owner.probeWritable(ctx)
	} else {
		err = owner.persistence.storeAttributedUpdateBatchDurably(ctx, room, updates)
	}
	owner.mu.Lock()
	owner.inFlight = false
	settlement := owner.pendingRooms[room]
	if err == nil && settlement != nil && settlement.generation == generation {
		owner.pendingBytes -= attributedYgoUpdatesBytes(settlement.updates)
		settlement.updates = nil
		settlement.state = collaborationStorageStatusRecovered
	}
	if err == nil && owner.overflow != nil {
		owner.overflow.state = collaborationStorageStatusRecovered
	}
	pending := owner.hasPendingLocked()
	owner.mu.Unlock()
	return pending, err
}

func (owner *ygoPersistenceSettlementOwner) nextPending() (string, []attributedYgoUpdate, uint64, bool) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.inFlight {
		return "", nil, 0, false
	}
	room := ""
	sequence := ^uint64(0)
	for candidateRoom, settlement := range owner.pendingRooms {
		if settlement.state == collaborationStorageStatusError && settlement.sequence < sequence {
			room = candidateRoom
			sequence = settlement.sequence
		}
	}
	if room == "" {
		return "", nil, 0, false
	}
	owner.inFlight = true
	settlement := owner.pendingRooms[room]
	return room, cloneAttributedYgoUpdates(settlement.updates), settlement.generation, true
}

func (owner *ygoPersistenceSettlementOwner) hasPendingLocked() bool {
	if owner.overflow != nil && owner.overflow.state == collaborationStorageStatusError {
		return true
	}
	for _, settlement := range owner.pendingRooms {
		if settlement.state == collaborationStorageStatusError {
			return true
		}
	}
	return false
}

func (owner *ygoPersistenceSettlementOwner) probeWritable(ctx context.Context) error {
	_, err := owner.app.DB().NewQuery(`
		UPDATE {{leafen_schema_migrations}} SET [[applied_at]] = [[applied_at]]
		WHERE [[version]] = (SELECT MAX([[version]]) FROM {{leafen_schema_migrations}})
	`).WithContext(ctx).Execute()
	return err
}

func (owner *ygoPersistenceSettlementOwner) allowsRoom(room string) bool {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.overflow != nil && owner.overflow.state == collaborationStorageStatusError {
		return false
	}
	settlement := owner.pendingRooms[room]
	return settlement == nil || settlement.state != collaborationStorageStatusError
}

func (owner *ygoPersistenceSettlementOwner) status(room string) collaborationStorageStatusResponse {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	settlement := owner.pendingRooms[room]
	if settlement == nil {
		if owner.overflow != nil {
			return collaborationStorageStatusResponse{
				Status: owner.overflow.state, DiagnosticReference: owner.overflow.diagnosticReference,
				Generation: owner.overflow.generation, AutomaticRetry: true,
			}
		}
		return collaborationStorageStatusResponse{
			Status: collaborationStorageStatusHealthy, AutomaticRetry: true,
		}
	}
	return collaborationStorageStatusResponse{
		Status: settlement.state, DiagnosticReference: settlement.diagnosticReference,
		Generation: settlement.generation, AutomaticRetry: true,
	}
}

func (owner *ygoPersistenceSettlementOwner) metrics() ygoPersistenceSettlementMetrics {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	failedRooms := 0
	for _, settlement := range owner.pendingRooms {
		if settlement.state == collaborationStorageStatusError {
			failedRooms++
		}
	}
	return ygoPersistenceSettlementMetrics{
		WorkerStarts: owner.workerStarts.Load(), RetryTurns: owner.retryTurns.Load(),
		Wakeups: owner.wakeups.Load(), IsolationAttempts: owner.isolationAttempts.Load(),
		PeakPendingBytes: owner.peakPendingBytes, PeakPendingRooms: owner.peakPendingRooms,
		PendingBytes: owner.pendingBytes, FailedRooms: failedRooms,
	}
}

func logYgoSettlementDiagnostic(app core.App, reference string, causeCode string, cause error) {
	if app == nil {
		return
	}
	attributes := []any{
		"diagnosticReference", reference,
		"code", errorCodeYgoPersistence,
		"operation", "ygo.persistence.settle",
		"stage", "store-update",
		"status", http.StatusServiceUnavailable,
		"causeCode", causeCode,
		"sourcePosition", diagnosticSourcePosition(1),
		"causeClass", fmt.Sprintf("%T", cause),
	}
	app.Logger().Error("leafen realtime persistence failed", attributes...)
	productionDiagnosticLogger.Error("leafen realtime persistence failed", attributes...)
}
