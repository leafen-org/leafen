package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/benbjohnson/litestream"
	"github.com/pocketbase/pocketbase/core"
)

// The replica going to one place (replica_runner.go): the place's replica
// taken or checked, Litestream sending the database, the files' list
// following, and the owner read again before writes; its close sends the
// last of both (replica_session_stop.go).

// replicationSession is the replica going to one place.
type replicationSession struct {
	runner      *replicationRunner
	destination string
	holder      string
	store       scheduledBackupStore
	key         *scheduledBackupKey
	client      *replicaStoreClient
	litestream  *litestream.Store
	database    *litestream.DB
	files       *replicaFileSync
	// ending is when the place stops answering (replica_session_stop.go).
	ending *replicaSessionEnding

	mu             sync.Mutex
	ownerCheckedAt time.Time
	movedErr       error
	// beatPaused is the last beat's answer: paired, and cut off from both
	// the place and the standby. handoverPaused is handing over to the
	// standby that asked since handingOver.
	beatPaused     atomic.Bool
	handoverPaused atomic.Bool
	handingOver    time.Time
	askedAt        time.Time
	// handedOver is a ready answer written: from then on the server takes
	// no changes until the standby's ownership reaches it.
	handedOver bool
	// The beat goes on its own clock (beatLoop); pairSecret is what the
	// pair signs their direct checks with.
	stopBeat   context.CancelFunc
	beating    sync.WaitGroup
	pairSecret string
	// The screen's direct check of the standby: when it was last asked, and
	// whether one is under way (replica_pair_check.go).
	lookedAtStandby time.Time
	looking         atomic.Bool
	// verifiedAt is when the place was last found to hold the replica up
	// to where it was sent; verifyMisses counts the times in a row it didn't.
	verifiedAt   time.Time
	verifyMisses int
}

// paused is whether the server takes no changes for this session.
func (session *replicationSession) paused() bool {
	return session.beatPaused.Load() || session.handoverPaused.Load()
}

// startSession takes or checks the place's replica and starts sending.
func (runner *replicationRunner) startSession(ctx context.Context, stored storedScheduledBackup) (session *replicationSession, err error) {
	app := runner.app
	dataDir := app.DataDir()
	opened, err := openReplicaSessionPlace(ctx, app, stored)
	if err != nil {
		return nil, err
	}
	store := opened.store
	defer func() {
		if err != nil {
			opened.close()
		}
	}()
	secret, err := sealingBackupSecretOf(app, stored)
	if err != nil {
		return nil, err
	}
	var key *scheduledBackupKey
	if !secret.empty() {
		if err := store.MakeDir(scheduledBackupRootName); err != nil {
			return nil, err
		}
		if key, err = scheduledBackupKeyIn(store, scheduledBackupRootName, secret, true); err != nil {
			return nil, err
		}
	}
	keyID := ""
	if key != nil {
		keyID = key.id
	}
	holder, err := loadReplicaHolder(dataDir)
	if err != nil {
		return nil, err
	}
	destination, place := replicaDestination(app, stored), replicaPlace(stored)
	owner, err := readReplicaOwner(store)
	if err != nil {
		return nil, err
	}
	switch {
	case owner != nil && owner.Holder == holder.Holder && owner.KeyID == keyID:
		// This server's replica, named by another place before (as a
		// folder mounted elsewhere): the database's working state starts
		// from the replica again rather than from the other place's.
		if holder.Claimed != place {
			if err := os.RemoveAll(filepath.Join(dataDir, ".data.db-litestream")); err != nil {
				return nil, err
			}
		}
		// Taken over moments ago: the server replaced reads the new owner
		// before this one writes; a clock set back never makes it wait
		// longer than that.
		if wait := min(replicaTakeOverWait-time.Since(time.UnixMilli(owner.Since)), replicaTakeOverWait); wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	case owner != nil && owner.Holder != holder.Holder:
		if holder.Claimed == place {
			return nil, errReplicaMoved{origin: owner.Origin}
		}
		return nil, errReplicaOwnedElsewhere
	case owner == nil && holder.Claimed == place:
		return nil, errReplicaMissing
	default:
		// A replica of its own sealed another way, or none: it starts
		// afresh here.
		if err := resetReplica(app, store); err != nil {
			return nil, err
		}
		if err := writeReplicaOwner(store, replicaOwner{Holder: holder.Holder, Origin: configuredPublicOrigin(),
			KeyID: keyID, Since: time.Now().UnixMilli()}); err != nil {
			return nil, err
		}
		// Two servers given the same empty place at once both write their
		// claim, and the later one holds it: the other steps back as from a
		// place another server holds, rather than finding itself taken over
		// and stopping (2026-10-18: it told its people their data had moved
		// to the other server).
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(replicaClaimSettle):
		}
		claimed, err := readReplicaOwner(store)
		if err != nil {
			return nil, err
		}
		if claimed == nil || claimed.Holder != holder.Holder {
			return nil, errReplicaOwnedElsewhere
		}
	}
	if holder.Claimed != place {
		holder.Claimed = place
		if err := saveReplicaHolder(dataDir, holder); err != nil {
			return nil, err
		}
	}
	files, err := openReplicaFileSync(app, store, scheduledBackupRootName, key)
	if err != nil {
		return nil, err
	}
	session = &replicationSession{runner: runner, destination: destination, holder: holder.Holder, store: store, key: key,
		files: files, ownerCheckedAt: time.Now(), pairSecret: holder.PairSecret}
	session.client = &replicaStoreClient{store: store, root: scheduledBackupStoreJoin(replicaRoot(), replicaDatabaseName),
		key: key, spool: leafenTempDir(dataDir), guard: session.guard}
	session.database = litestream.NewDB(filepath.Join(dataDir, dataDatabaseFile))
	replica := litestream.NewReplicaWithClient(session.database, session.client)
	session.database.Replica = replica
	session.litestream = litestream.NewStore([]*litestream.DB{session.database}, litestream.CompactionLevels{
		{Level: 0}, {Level: 1, Interval: 30 * time.Second}, {Level: 2, Interval: 5 * time.Minute}, {Level: 3, Interval: time.Hour},
	})
	quiet := slog.New(replicaLogHandler{inner: app.Logger().Handler()})
	session.litestream.Logger = quiet
	session.database.SetLogger(quiet)
	session.database.ShutdownSyncTimeout = 3 * time.Second
	// From here the place outlives the runner, for the close's last sends.
	if !opened.outlive(ctx) {
		return nil, ctx.Err()
	}
	session.ending = opened.ending
	if err := session.litestream.Open(ctx); err != nil {
		return nil, fmt.Errorf("starting the database replica: %w", err)
	}
	beatCtx, stopBeat := context.WithCancel(context.Background())
	session.stopBeat = stopBeat
	session.beating.Add(1)
	go session.beatLoop(beatCtx)
	return session, nil
}

// beatLoop raises the beat on its own clock, apart from the files' sending,
// which may take long: in the same loop, a long file held the beat back for
// ten times as long as it took, long enough for the standby to take over a
// primary that was there (2026-10-18).
func (session *replicationSession) beatLoop(ctx context.Context) {
	defer session.beating.Done()
	ticker := time.NewTicker(replicaBeatEvery / 2)
	defer ticker.Stop()
	for {
		session.beatOnce(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// beatOnce beats while the replica is this server's; a primary whose beat
// is late asks its standby directly (replica_pair_check.go).
func (session *replicationSession) beatOnce(ctx context.Context, now time.Time) {
	err := session.guard(ctx)
	lease := &session.runner.lease
	paused := lease.beatIfDue(session.store, session.holder, replicaJoinTokenNow(session.runner.app.DataDir()), now, err == nil)
	if paired := lease.pairedNow(); err == nil {
		session.notePaired(paired)
	}
	// Asked even when the place can't be read — the place failing alone
	// mustn't stop the primary — but not once another server holds the
	// replica.
	var moved errReplicaMoved
	if err == nil && lease.pairedNow() && replicaPairRecheckDue(session.runner.directCheck(), session.lookedAtStandby, now) {
		// Once paired, the screen says whether the pair reach each other,
		// and follows once they do.
		session.lookedAtStandby = now
		session.lookAtStandby(ctx)
	}
	if !errors.As(err, &moved) && lease.beatLate(now) {
		if session.askStandby(ctx) {
			lease.hear(time.Now())
		}
		paused = lease.settle(time.Now())
	}
	session.beatPaused.Store(paused)
	session.runner.notePause(session)
}

// resetReplica clears a place's replica and what this server kept of one,
// so the next start sends everything.
func resetReplica(app core.App, store scheduledBackupStore) error {
	for _, part := range []string{replicaDatabaseName, replicaFilesName} {
		if err := store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), part)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(app.DataDir(), ".data.db-litestream")); err != nil {
		return err
	}
	if err := ensureReplicaFileTable(app); err != nil {
		return err
	}
	_, err := app.AuxDB().NewQuery(`DELETE FROM {{leafen_replica_files}}`).Execute()
	return err
}

// guard tells, before a write, whether the replica is still this server's:
// read again once the last read is a few seconds old.
func (session *replicationSession) guard(context.Context) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.movedErr != nil {
		return session.movedErr
	}
	if sinceBoth(session.ownerCheckedAt, time.Now()) < replicaOwnerCheckEvery {
		// Between reads, the owner file is at least still there: a place
		// that went empty (an unmounted share) takes no write.
		if _, err := session.store.Stat(scheduledBackupStoreJoin(replicaRoot(), replicaOwnerName)); errors.Is(err, fs.ErrNotExist) {
			session.ownerCheckedAt = time.Time{}
			return errReplicaMissing
		} else if err != nil {
			return err
		}
		return nil
	}
	owner, err := readReplicaOwner(session.store)
	if err != nil {
		return err
	}
	session.ownerCheckedAt = time.Now()
	switch {
	case owner == nil:
		// Checked again at the next write, not kept: a place that comes
		// back as it was goes on.
		session.ownerCheckedAt = time.Time{}
		return errReplicaMissing
	case owner.Holder != session.holder:
		moved := errReplicaMoved{origin: owner.Origin}
		session.movedErr = moved
		select {
		case session.runner.moved <- moved:
		default:
		}
	}
	return session.movedErr
}

// tick sends the files that changed, once the replica is still this
// server's.
func (session *replicationSession) tick(ctx context.Context) error {
	// The beat goes on by itself (beatLoop).
	if err := session.guard(ctx); err != nil {
		return err
	}
	if sinceBoth(session.verifiedAt, time.Now()) >= replicaVerifyEvery {
		session.verifiedAt = time.Now()
		if err := session.verifyPlace(ctx); err != nil {
			return err
		}
	}
	if _, err := session.files.tick(ctx); err != nil {
		// Cut short by a stop: the close's last send follows.
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return session.handOverIfAsked(ctx, time.Now())
}

// errReplicaWentBack is a place that no longer holds the replica up to
// where it was sent.
var errReplicaWentBack = errors.New("the replica's place went back to an older copy of the replica")

// verifyPlace checks that the place still restores to what was sent: a
// place that went back to an older copy of itself — a share restored from a
// snapshot — leaves a gap no restore crosses while every write still
// succeeds and the replica looks current (2026-10-18). Found twice in a row,
// ten seconds apart, the replica starts afresh.
func (session *replicationSession) verifyPlace(ctx context.Context) error {
	sent := session.database.Replica.Pos().TXID
	if sent == 0 {
		return nil
	}
	_, err := litestream.CalcRestorePlan(ctx, session.client, sent, time.Time{}, slog.New(slog.DiscardHandler))
	if err == nil {
		session.verifyMisses = 0
		return nil
	}
	if !errors.Is(err, litestream.ErrTxNotAvailable) {
		// A place that can't be listed is unreachable, not gone back.
		return err
	}
	session.verifyMisses++
	if session.verifyMisses < 2 {
		session.verifiedAt = time.Now().Add(10*time.Second - replicaVerifyEvery)
		return nil
	}
	return errReplicaWentBack
}

// handOverIfAsked answers a standby that asks to take over while this
// server still answers: no more changes, everything sent, and up to which
// transaction the replica holds them.
func (session *replicationSession) handOverIfAsked(ctx context.Context, now time.Time) error {
	if session.handingOver.IsZero() && now.Sub(session.askedAt) < replicaBeatEvery {
		return nil
	}
	session.askedAt = now
	asked := replicaHandover{}
	present, err := readReplicaJSON(session.store, replicaHandoverName, &asked)
	if err != nil {
		return err
	}
	standby, _, _ := session.runner.lease.snapshot(now)
	switch {
	case session.handedOver && now.Sub(session.handingOver) <= 2*replicaHandoverWait:
		// Handed over: the standby takes the replica; this server waits for
		// its owner read to say so, taking nothing meanwhile.
		session.handoverPaused.Store(true)
		return nil
	case !present || standby == nil || asked.Holder != standby.Holder:
		session.handingOver, session.handedOver = time.Time{}, false
		session.handoverPaused.Store(false)
		return nil
	}
	if session.handingOver.IsZero() {
		session.handingOver = now
		session.runner.app.Logger().Warn("replica: the standby asks to take over; taking no more changes")
	}
	if now.Sub(session.handingOver) > 2*replicaHandoverWait {
		// The standby that asked never took over: go on, and leave no
		// answer behind for a later asking.
		session.handingOver, session.handedOver = time.Time{}, false
		session.handoverPaused.Store(false)
		_ = session.store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), replicaHandoverName))
		_ = session.store.RemoveAll(scheduledBackupStoreJoin(replicaRoot(), replicaHandoverReadyName))
		return nil
	}
	session.handoverPaused.Store(true)
	// Requests that began before the pause finish first; then the
	// database's last changes and the files go out at once.
	if now.Sub(session.handingOver) < time.Second {
		return nil
	}
	if err := session.database.SyncAndWait(ctx); err != nil {
		return err
	}
	if _, err := session.files.tick(ctx); err != nil {
		return err
	}
	if session.behind() {
		return nil
	}
	position, err := session.database.Pos()
	if err != nil {
		return err
	}
	if err := writeReplicaJSON(session.store, replicaHandoverReadyName, replicaHandoverReady{Holder: session.holder, TXID: uint64(position.TXID),
		Nonce: asked.Nonce}); err != nil {
		return err
	}
	session.handedOver = true
	return nil
}

// notePaired keeps, in this server's mark, whether a standby follows it.
func (session *replicationSession) notePaired(paired bool) {
	dataDir := session.runner.app.DataDir()
	holder, err := loadReplicaHolder(dataDir)
	if err != nil || holder.Paired == paired {
		return
	}
	holder.Paired = paired
	_ = saveReplicaHolder(dataDir, holder)
}

// behind is whether the database has changes the replica lacks.
func (session *replicationSession) behind() bool {
	local, err := session.database.Pos()
	if err != nil {
		return true
	}
	return session.database.Replica.Pos().TXID < local.TXID
}

// replicaLogHandler passes on Litestream's warnings and errors, not its
// routine notes.
type replicaLogHandler struct{ inner slog.Handler }

func (handler replicaLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn && handler.inner.Enabled(ctx, level)
}

func (handler replicaLogHandler) Handle(ctx context.Context, record slog.Record) error {
	// The server's log can't write a duration, and drops the whole line
	// (2026-11-04: the replica's "monitor error" lines of an outage were lost).
	readable := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Value.Kind() == slog.KindDuration {
			attr.Value = slog.StringValue(attr.Value.Duration().String())
		}
		readable.AddAttrs(attr)
		return true
	})
	return handler.inner.Handle(ctx, readable)
}

func (handler replicaLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return replicaLogHandler{inner: handler.inner.WithAttrs(attrs)}
}

func (handler replicaLogHandler) WithGroup(name string) slog.Handler {
	return replicaLogHandler{inner: handler.inner.WithGroup(name)}
}

// followingReplica is a replica to follow from, noting what the server notes
// (warnings and errors) rather than each step Litestream applies: a replica
// without a database logs through a database it never opens.
func followingReplica(app core.App, client *replicaStoreClient) *litestream.Replica {
	carrier := litestream.NewDB(filepath.Join(leafenTempDir(app.DataDir()), "replica-following.db"))
	carrier.SetLogger(slog.New(replicaLogHandler{inner: app.Logger().Handler()}))
	return litestream.NewReplicaWithClient(carrier, client)
}
