package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"sync"
	"time"
)

// The pair's beats at the replica's place (decided 2026-10-17): a primary
// with a standby raises the beat in "replica/lease.json" every few seconds,
// and the standby raises its own in "replica/standby.json". Neither compares
// clocks with the other: each watches the other's beat change by its own.
// A standby that sees the primary's beat stand still long enough takes the
// replica over; a primary that can't raise its beat for a shorter while stops
// taking changes first, so even two servers that can't see each other never
// both write.

const (
	replicaLeaseName   = "lease.json"
	replicaStandbyName = "standby.json"
	// The primary raises its beat this often, and pauses once it couldn't
	// for this long; the standby takes over once the beat stood still for
	// longer than that, by its own clock.
	replicaBeatEvery     = 2 * time.Second
	replicaPauseAfter    = 10 * time.Second
	replicaTakeOverAfter = 30 * time.Second
	// A standby whose beat stood still this long is shown as gone, and
	// administrators hear.
	replicaStandbyGoneAfter = 5 * time.Minute
	// A standby taking over from a primary that still answers asks first
	// (handover.json); the primary stops taking changes, sends what it has,
	// and says up to which transaction (handover-ready.json), so nothing is
	// lost. The standby waits this long for that answer, and a primary whose
	// asking standby never took over goes on after twice as long.
	replicaHandoverName      = "handover.json"
	replicaHandoverReadyName = "handover-ready.json"
	replicaHandoverWait      = 30 * time.Second
)

// replicaHandover is a standby asking a primary that answers to hand over;
// Nonce is this asking's, which the answer carries back, so an answer left
// from an earlier asking is never taken for this one's (2026-10-18).
type replicaHandover struct {
	Holder string `json:"holder"`
	Nonce  string `json:"nonce,omitempty"`
}

// replicaHandoverReady is the primary's answer: it takes no more changes,
// and the replica holds them all up to this transaction.
type replicaHandoverReady struct {
	Holder string `json:"holder"`
	TXID   uint64 `json:"txid"`
	Nonce  string `json:"nonce,omitempty"`
}

// replicaLease is the primary's beat. Echo answers a joining standby's
// probe (replica_pair_check.go).
type replicaLease struct {
	Holder  string `json:"holder"`
	Beat    int64  `json:"beat"`
	Version string `json:"version"`
	Origin  string `json:"origin,omitempty"`
	Echo    string `json:"echo,omitempty"`
	// Refused answers a joining mark made with a connection code made again since.
	Refused string `json:"refused,omitempty"`
}

// replicaStandbyBeat is the standby's beat and how far it has followed.
type replicaStandbyBeat struct {
	Holder   string `json:"holder"`
	Beat     int64  `json:"beat"`
	Version  string `json:"version"`
	Origin   string `json:"origin,omitempty"`
	Followed uint64 `json:"followed"`
	PairedAt int64  `json:"pairedAt"`
}

func readReplicaJSON(store scheduledBackupStore, name string, target any) (bool, error) {
	raw, err := scheduledBackupStoreReadAll(store, scheduledBackupStoreJoin(replicaRoot(), name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, target)
}

func writeReplicaJSON(store scheduledBackupStore, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := store.MakeDir(replicaRoot()); err != nil {
		return err
	}
	return store.Replace(scheduledBackupStoreJoin(replicaRoot(), name), raw)
}

// sinceBoth is how long since then by the monotonic clock or the wall
// clock, whichever is longer. A paused virtual machine or a sleeping
// computer stops the first (Linux CLOCK_MONOTONIC, macOS
// mach_absolute_time), so a server waking from either would otherwise think
// hardly any time had passed and take changes while its standby had taken
// over (2026-10-18). A wall clock set forward only pauses the server until
// its next beat.
func sinceBoth(then, now time.Time) time.Duration {
	return max(now.Sub(then), now.Round(0).Sub(then.Round(0)))
}

// beatWatch tells, by this server's own clock, how long another server's
// beat has stood still.
type beatWatch struct {
	mu      sync.Mutex
	holder  string
	beat    int64
	changed time.Time
}

// see notes a beat read now, answering how long it has stood still.
func (watch *beatWatch) see(holder string, beat int64, now time.Time) time.Duration {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	if holder != watch.holder || beat != watch.beat || watch.changed.IsZero() {
		watch.holder, watch.beat, watch.changed = holder, beat, now
	}
	return now.Sub(watch.changed)
}

func (watch *beatWatch) still(now time.Time) time.Duration {
	watch.mu.Lock()
	defer watch.mu.Unlock()
	if watch.changed.IsZero() {
		return 0
	}
	return now.Sub(watch.changed)
}

// primaryLease is a primary's side of the pair, kept by its replication
// runner across sessions: whether a standby is paired, its beat, and the
// primary's own, which starts from the time so it rises across restarts.
type primaryLease struct {
	mu       sync.Mutex
	beat     int64
	lastBeat time.Time
	wroteOK  time.Time
	standby  *replicaStandbyBeat
	watch    beatWatch
	paused   bool
	// heard is when the standby last answered the primary's own direct
	// check (replica_pair_check.go): a primary that can't reach the place but
	// hears its standby goes on; paused, it waits for its beat.
	heard time.Time
}

// beatIfDue raises the primary's beat when a standby is paired, and reads
// the standby's; owned says the replica was just found still this server's,
// without which no beat counts. It answers whether the primary should pause:
// paired, and unable to raise its beat for too long.
func (lease *primaryLease) beatIfDue(store scheduledBackupStore, holder, joinToken string, now time.Time, owned bool) bool {
	lease.mu.Lock()
	due := owned && sinceBoth(lease.lastBeat, now) >= replicaBeatEvery
	lease.mu.Unlock()
	if due {
		standby := &replicaStandbyBeat{}
		present, err := readReplicaJSON(store, replicaStandbyName, standby)
		// A standby joining marks the place; the beat answers it, paired or
		// not yet, so the standby sees the place carry marks both ways in
		// time.
		probe := replicaPairProbe{}
		probed, probeErr := readReplicaJSON(store, replicaPairProbeName, &probe)
		echo, refused := "", ""
		if probeErr == nil && probed && probe.Holder != holder {
			if replicaJoinProofValid(joinToken, probe) {
				echo = probe.Nonce
			} else {
				refused = probe.Nonce
			}
		}
		lease.mu.Lock()
		lease.lastBeat = now
		if err == nil {
			// A standby beat this server wrote itself, from when it was the
			// standby, isn't a standby's.
			if present && standby.Holder != holder {
				lease.standby = standby
				lease.watch.see(standby.Holder, standby.Beat, now)
			} else {
				lease.standby = nil
			}
		}
		paired := lease.standby != nil
		if lease.beat == 0 {
			lease.beat = now.UnixMilli()
		}
		lease.beat++
		beat := lease.beat
		lease.mu.Unlock()
		if paired || echo != "" || refused != "" {
			if err := writeReplicaJSON(store, replicaLeaseName, replicaLease{Holder: holder, Beat: beat,
				Version: ReleaseVersion(), Origin: configuredPublicOrigin(), Echo: echo, Refused: refused}); err == nil {
				lease.mu.Lock()
				lease.wroteOK = now
				lease.mu.Unlock()
			}
		}
	}
	return lease.settle(now)
}

// settle answers whether the primary should pause now: paired, unable to
// raise its beat for too long, and not hearing its standby directly either.
func (lease *primaryLease) settle(now time.Time) bool {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.standby == nil {
		lease.paused = false
		return false
	}
	if lease.wroteOK.IsZero() {
		lease.wroteOK = now
	}
	lease.paused = lease.cutOff(now)
	return lease.paused
}

// cutOff is a primary that couldn't raise its beat, nor hear its standby,
// for long enough that the standby may take over; one paused so stays till
// its beat goes out again, whatever it hears (replica_pair_check.go). The
// lease's lock is held.
func (lease *primaryLease) cutOff(now time.Time) bool {
	return sinceBoth(lease.wroteOK, now) >= replicaPauseAfter &&
		(lease.paused || lease.heard.IsZero() || sinceBoth(lease.heard, now) >= replicaPauseAfter)
}

// staleNow is, at a request, whether a paired primary is cut off already:
// its runner may not have run since it woke from a pause or a sleep.
func (lease *primaryLease) staleNow(now time.Time) bool {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.standby != nil && !lease.wroteOK.IsZero() && lease.cutOff(now)
}

// hear notes the standby answering the primary's own direct check.
func (lease *primaryLease) hear(now time.Time) {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	lease.heard = now
}

// beatLate is whether the beat hasn't gone out for a while: then the
// primary asks its standby directly.
func (lease *primaryLease) beatLate(now time.Time) bool {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.standby != nil && !lease.wroteOK.IsZero() && sinceBoth(lease.wroteOK, now) >= 2*replicaBeatEvery
}

// pausedWithout says, while no session runs, whether a primary that was
// paired must pause: it can't raise its beat, so a standby may take over.
func (lease *primaryLease) pausedWithout(paired bool, started, now time.Time) bool {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	last := lease.wroteOK
	if last.IsZero() {
		last = started
	}
	heard := lease.heard.IsZero() || sinceBoth(lease.heard, now) >= replicaPauseAfter
	lease.paused = paired && sinceBoth(last, now) >= replicaPauseAfter && (lease.paused || heard)
	return lease.paused
}

// forget lets go of the standby, as when the primary dismisses it.
func (lease *primaryLease) forget() {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	lease.standby, lease.paused = nil, false
}

// pairedNow is whether a standby was seen at the last beat.
func (lease *primaryLease) pairedNow() bool {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.standby != nil
}

// snapshot is what the screen shows of the standby.
func (lease *primaryLease) snapshot(now time.Time) (*replicaStandbyBeat, time.Duration, bool) {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.standby == nil {
		return nil, 0, lease.paused
	}
	copied := *lease.standby
	return &copied, lease.watch.still(now), lease.paused
}
