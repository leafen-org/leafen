package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Continuous replication (decided 2026-10-17): with backups on, an
// administrator can have every change go out within seconds to the same
// place — the database through Litestream, the files and the secrets
// through the replica's file list — so another server can come back a few
// seconds behind after a disaster (replica_restore.go).
//
// Only one server writes a replica. "owner.json" beside it names the server
// that does by a mark this server keeps in its data folder, which no backup
// or replica carries. A server restored from the replica takes the mark's
// place; the server it replaced, back again, finds another named and stops
// sending and taking changes — it has moved — until an administrator keeps
// it apart as a server of its own. A server is never let write a replica
// another one holds.

const (
	replicationRunnerStoreKey = "leafenReplicationRunner"
	replicationEnv            = "LEAFEN_BACKUP_REPLICATION"
	replicaHolderName         = ".leafen-replica-holder"
	replicaFencedName         = ".leafen-replica-moved"
	replicaOwnerName          = "owner.json"
	// The database goes out each second; the files are compared as often,
	// or, where the data folder takes long to read, ten times as long as the
	// last reading took, so comparing takes at most a tenth of the time.
	replicaFilesEvery = time.Second
	// How long an owner read stays good for the writes after it, and how
	// long a server that takes a replica over waits before writing, so the
	// one it replaced has read the new owner.
	replicaOwnerCheckEvery = 5 * time.Second
	replicaTakeOverWait    = 15 * time.Second
	// A fresh claim is read back after this long, so of two servers
	// claiming an empty place at once only the one holding it goes on.
	replicaClaimSettle = 2 * time.Second
	// How often a running replica checks its place still restores to what
	// was sent (replicationSession.verifyPlace).
	replicaVerifyEvery = time.Minute
	// How long the replica may stay behind before administrators hear.
	replicaFailingAfter = time.Minute
	replicaRetryDelay   = 30 * time.Second
	replicaIdleDelay    = time.Hour
)

var (
	errReplicaOwnedElsewhere = errors.New("another server's replica is there")
	// errReplicaMissing is a place where this server's replica was and no
	// owner is now: an empty mount point, or a replica removed. It isn't
	// taken afresh by itself, as the standby may hold the replica elsewhere;
	// turning replication off and on starts it again.
	errReplicaMissing = errors.New("the replica this server wrote isn't at its place")
)

// replicationWanted is whether the replica should be going: backups on with
// a place, and replication turned on there or by the environment.
func replicationWanted(stored storedScheduledBackup) bool {
	if !stored.Enabled || !stored.targetSet() {
		return false
	}
	if set, on := replicationFromEnvironment(); set {
		return on
	}
	return stored.ReplicationEnabled
}

// replicationFromEnvironment reads LEAFEN_BACKUP_REPLICATION: whether it is
// set, and whether it turns replication on.
func replicationFromEnvironment() (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(replicationEnv))) {
	case "on", "true", "1", "yes":
		return true, true
	case "off", "false", "0", "no":
		return true, false
	}
	return false, false
}

// replicationStatus is what the screen shows of the replica.
type replicationStatus struct {
	State        string
	LastSyncAt   time.Time
	BehindSince  time.Time
	ErrorCode    string
	FailingSince time.Time
	MovedTo      string
	// Paused is a paired primary that can't raise its beat: it takes no
	// changes until it can, as its standby may take over.
	Paused bool
}

type replicationRunner struct {
	app     core.App
	wake    chan struct{}
	moved   chan errReplicaMoved
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	status  replicationStatus
	lease   primaryLease
	started time.Time
	// paused is read by every change the server takes.
	paused atomic.Bool
	// takeover asks a standby to take over now; standby is what the screen
	// shows of a standby's following.
	takeover chan struct{}
	standby  standbyStatus
	// direct is what a primary's last direct check of its standby found
	// (replica_pair_check.go); primaryServing, when a standby last answered
	// a check of its primary saying it takes changes.
	direct         string
	primaryServing time.Time
	// pausedSince is when a pause began, 0 unpaused (replica_remote_access.go).
	pausedSince atomic.Int64
}

func newReplicationRunner(app core.App) *replicationRunner {
	return &replicationRunner{app: app, wake: make(chan struct{}, 1), moved: make(chan errReplicaMoved, 1),
		takeover: make(chan struct{}, 1), status: replicationStatus{State: replicationStateOff}}
}

func (runner *replicationRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.cancel, runner.done, runner.started = cancel, make(chan struct{}), time.Now()
	go runner.run(ctx, runner.done)
}

// Shutdown stops the replica once everything else has stopped writing: the
// last changes go out first, within the shutdown's time.
func (runner *replicationRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	cancel, done := runner.cancel, runner.done
	runner.cancel, runner.done = nil, nil
	runner.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("replication shutdown: %w", ctx.Err())
	}
}

func (runner *replicationRunner) notify() {
	select {
	case runner.wake <- struct{}{}:
	default:
	}
}

func notifyReplicationRunner(app core.App) {
	if runner, ok := app.Store().Get(replicationRunnerStoreKey).(*replicationRunner); ok {
		runner.notify()
	}
}

func replicationStatusOf(app core.App) replicationStatus {
	if runner, ok := app.Store().Get(replicationRunnerStoreKey).(*replicationRunner); ok {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		return runner.status
	}
	return replicationStatus{State: replicationStateOff}
}

func (runner *replicationRunner) setStatus(change func(*replicationStatus)) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	change(&runner.status)
}

func (runner *replicationRunner) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	var session *replicationSession
	var standby *standbySession
	stop := func() {
		if session != nil {
			session.close()
			session = nil
		}
	}
	stopStandby := func() {
		if standby != nil {
			standby.close()
			standby = nil
		}
	}
	defer stop()
	defer stopStandby()
	retry := replicaRetryDelay
	for {
		wait := replicaFilesEvery
		stored, err := loadScheduledBackup(runner.app)
		dataDir := runner.app.DataDir()
		switch fence := readReplicaFence(dataDir); {
		// A moved server may follow the server that took its data over, as
		// its standby, still taking no changes of its own until it takes
		// over (the standby decision: an old primary goes down to standby).
		case fence != nil && (err != nil || !stored.StandbyEnabled):
			stop()
			stopStandby()
			runner.setStatus(func(status *replicationStatus) {
				*status = replicationStatus{State: replicationStateMoved, MovedTo: fence.MovedTo}
			})
			wait = replicaIdleDelay
		case err == nil && stored.StandbyEnabled:
			// A standby follows; it doesn't replicate data of its own.
			stop()
			runner.setStatus(func(status *replicationStatus) { *status = replicationStatus{State: replicationStateOff} })
			wait = replicaFilesEvery
			if runner.standbyStatus().TakingOver {
				wait = replicaIdleDelay
				break
			}
			if standby == nil {
				started, err := runner.startStandby(ctx, stored)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					runner.noteStandby(nil, stored, err)
					// A standby keeps watching for its primary: it tries again
					// every 30 seconds, not ever later as a failing replica
					// does (2026-10-18: after a minute's outage of the place it
					// came back to watching only minutes later).
					wait = replicaRetryDelay
					break
				}
				standby, retry, wait = started, replicaRetryDelay, 0
				break
			}
			gone, err := standby.tick(ctx, time.Now())
			if errors.Is(err, errStandbyDismissed) {
				stopStandby()
				_, _ = runner.app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[standby_enabled]] = 0,
					[[revision]] = [[revision]] + 1 WHERE [[id]] = 'default'`).Execute()
				runner.setStandby(standbyStatus{})
				break
			}
			runner.noteStandby(standby, stored, err)
			asked := false
			select {
			case <-runner.takeover:
				asked = true
			default:
			}
			// A primary that still answers directly that it takes changes,
			// or asked lately saying so, isn't taken over by itself
			// (replica_pair_check.go).
			if err == nil && gone && !asked && stored.StandbyAutoTakeover &&
				(runner.primaryServingHeard(time.Now()) || standby.askPrimary(ctx, stored.StandbyPrimaryOrigin)) {
				gone = false
				runner.noteStandby(standby, stored, nil)
			}
			if err == nil && (asked || (gone && stored.StandbyAutoTakeover)) {
				runner.markTakingOver(true)
				if err := runner.takeOver(ctx, standby, stored); err != nil {
					runner.markTakingOver(false)
					runner.noteStandby(standby, stored, err)
					stopStandby()
					break
				}
				closeScheduledBackupStore(standby.store)
				standby = nil
			} else if err != nil {
				// Following that stopped starts again.
				stopStandby()
			}
		case err != nil || !replicationWanted(stored):
			stop()
			stopStandby()
			runner.setStandby(standbyStatus{})
			runner.setStatus(func(status *replicationStatus) { *status = replicationStatus{State: replicationStateOff} })
			wait = replicaIdleDelay
		case session != nil && session.destination != replicaDestination(runner.app, stored):
			stop()
			wait = 0
		case session == nil:
			runner.setStatus(func(status *replicationStatus) {
				if status.State != replicationStateFailing {
					status.State = replicationStateStarting
				}
			})
			started, err := runner.startSession(ctx, stored)
			var moved errReplicaMoved
			switch {
			case errors.As(err, &moved):
				runner.becomeMoved(moved)
				wait = 0
			case err != nil:
				if ctx.Err() != nil {
					return
				}
				runner.failed(err)
				wait, retry = retry, min(retry*2, 5*time.Minute)
			default:
				session, retry = started, replicaRetryDelay
				wait = 0
			}
		default:
			began := time.Now()
			err := session.tick(ctx)
			wait = max(replicaFilesEvery, 10*time.Since(began))
			var moved errReplicaMoved
			switch {
			case errors.As(err, &moved):
				stop()
				runner.becomeMoved(moved)
				wait = 0
			case errors.Is(err, errReplicaWentBack):
				// The place lost what was sent: the replica goes again from
				// the start, database and files.
				runner.app.Logger().Warn("replica: the place went back to an older copy of the replica; sending it again from the start")
				stop()
				if err := runner.resetPlaceReplica(ctx, stored); err != nil {
					runner.failed(err)
					wait = replicaRetryDelay
				} else {
					wait = 0
				}
			default:
				runner.observe(session, err)
			}
		}
		runner.notePause(session)
		select {
		case <-ctx.Done():
			return
		case moved := <-runner.moved:
			stop()
			runner.becomeMoved(moved)
		case <-runner.wake:
		case <-time.After(wait):
		}
	}
}

// resetPlaceReplica clears the place's replica and what this server kept of
// it, keeping the owner, so the next session sends everything again.
func (runner *replicationRunner) resetPlaceReplica(ctx context.Context, stored storedScheduledBackup) error {
	store, err := openScheduledBackupStore(ctx, runner.app, stored)
	if err != nil {
		return err
	}
	defer closeScheduledBackupStore(store)
	return resetReplica(runner.app, store)
}

// notePause keeps whether a paired primary must pause: from its session's
// beat, or, with no session going, from how long since its last beat.
func (runner *replicationRunner) notePause(session *replicationSession) {
	paused := false
	if session != nil {
		paused = session.paused()
	} else if holder, err := loadReplicaHolder(runner.app.DataDir()); err == nil {
		stored, loadErr := loadScheduledBackup(runner.app)
		paused = loadErr == nil && replicationWanted(stored) &&
			runner.lease.pausedWithout(holder.Paired, runner.started, time.Now())
	}
	runner.setStatus(func(status *replicationStatus) { status.Paused = paused && status.State != replicationStateMoved })
	runner.paused.Store(paused)
	runner.notePausedRemoteAccess(paused)
}

// replicationPaused is whether this server takes no changes for now: a
// paired primary that can't raise its beat.
func replicationPaused(app core.App) bool {
	runner, ok := app.Store().Get(replicationRunnerStoreKey).(*replicationRunner)
	// Judged at the request too: a primary waking from a sleep takes no
	// change before its beat goes out again, which reads the owner first.
	return ok && (runner.paused.Load() || runner.lease.staleNow(time.Now()))
}

// failed notes a start or a tick that didn't go: the screen shows it, and
// past a minute behind, administrators hear.
func (runner *replicationRunner) failed(err error) {
	code := replicationErrorCode(err)
	now := time.Now()
	alert := false
	runner.setStatus(func(status *replicationStatus) {
		status.ErrorCode = code
		if status.BehindSince.IsZero() {
			status.BehindSince = now
		}
		if now.Sub(status.BehindSince) >= replicaFailingAfter || code == errorCodeReplicaOwnedElsewhere {
			if status.State != replicationStateFailing {
				status.FailingSince, alert = now, true
			}
			status.State = replicationStateFailing
		}
	})
	runner.app.Logger().Warn("replica: couldn't send changes", "error", err)
	if alert {
		if err := alertReplicationFailing(runner.app, code, now); err != nil {
			runner.app.Logger().Warn("replica: couldn't tell administrators", "error", err)
		}
	}
}

// observe reads how far behind the replica is after a tick. A place that
// fails is tried again as it was: the database's changes wait in its
// working folder and go out once the place answers.
func (runner *replicationRunner) observe(session *replicationSession, tickErr error) {
	err := tickErr
	if err == nil {
		err = session.client.lastError()
	}
	behind := session.behind()
	if err == nil && !behind {
		runner.setStatus(func(status *replicationStatus) {
			*status = replicationStatus{State: replicationStateRunning, LastSyncAt: time.Now()}
		})
		return
	}
	if err == nil {
		// Behind without an error: changes are on their way.
		now := time.Now()
		runner.setStatus(func(status *replicationStatus) {
			if status.BehindSince.IsZero() {
				status.BehindSince = now
			}
			if status.State != replicationStateFailing {
				status.State = replicationStateRunning
			}
		})
		if since := replicationStatusOfRunner(runner).BehindSince; now.Sub(since) < replicaFailingAfter {
			return
		}
		err = errors.New("the replica stays behind")
	}
	runner.failed(err)
}

func replicationStatusOfRunner(runner *replicationRunner) replicationStatus {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.status
}

func (runner *replicationRunner) standbyStatus() standbyStatus {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return runner.standby
}

func (runner *replicationRunner) setStandby(status standbyStatus) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.standby = status
}

func (runner *replicationRunner) markTakingOver(taking bool) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	runner.standby.TakingOver = taking
}

// noteStandby keeps what the screen shows of the standby's following.
func (runner *replicationRunner) noteStandby(session *standbySession, stored storedScheduledBackup, err error) {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	status := runner.standby
	status.PrimaryOrigin = stored.StandbyPrimaryOrigin
	// A takeover refused for the primary's data from before the schema
	// baseline stays said while this server follows: that data stays so.
	if status.ErrorCode != errorCodeDataBeforeSchemaBaseline {
		status.ErrorCode = ""
	}
	if err != nil {
		status.ErrorCode = replicationErrorCode(err)
		if errors.Is(err, errStandbyOlder) {
			status.ErrorCode = errorCodeStandbyVersionOlder
		}
	}
	if session != nil {
		if session.primary.Origin != "" {
			status.PrimaryOrigin = session.primary.Origin
			// The primary's beat, which only its holder writes, gives its
			// address now: one it changed is kept for when this server starts
			// again before the next beat (2026-10-20).
			if webOrigin(session.primary.Origin) != "" && session.primary.Origin != stored.StandbyPrimaryOrigin && stored.StandbyEnabled {
				keepStandbyPrimaryOriginBehind(runner.app, stored.StandbyPrimaryOrigin, session.primary.Origin)
			}
		}
		status.PrimaryVersion = session.primary.Version
		status.SeenPrimary = session.primary.Holder != ""
		status.PrimaryStill = session.watch.still(time.Now())
		status.Followed, status.FollowedAt = session.followed, session.followedAt
		status.DirectCheck = session.direct
	}
	runner.standby = status
}
