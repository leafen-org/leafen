package app

import (
	"runtime/debug"
	"sync/atomic"
	"time"
)

// Under memory pressure the long work gives way, not people (2026-11-23:
// two Notion moves at once, of 7,324 and 4,390 rows, took a 2 GiB NAS past
// its high mark; the server refused 413 of people's requests for minutes —
// a guest's row sent to the trash came back "couldn't move" — and the moves,
// which hold the memory, went on). The work in the background — imports,
// exports, moves, copies, every durable job (jobs_scheduler.go) — takes its
// turns at the pace the memory allows, from the same reading of it that
// admits people's requests (memory_pressure.go):
//
//   - under the high mark, at full pace, as many at once as the memory the
//     server plans for has room for (memoryLongWorkers);
//   - past it, until the memory is back under the recovery mark, one turn at
//     a time with a breath after each, a move's making turn ending at its
//     next window (notion_import_turn_hold.go), and the memory the Go runtime
//     holds free going back to the system: a move slows down and still ends;
//   - past the critical mark, none, while people's requests wait too and the
//     memory the long work held goes back to the system, until the memory is
//     a little under that mark again.
//
// A server whose memory isn't read (no container limit) runs its long work
// at full pace, as before.

// memoryLongWorkBreath is how long, under pressure, the next turn of long
// work waits after one ends.
const memoryLongWorkBreath = 200 * time.Millisecond

// memoryFreeEvery is how often, at most, the server gives the system back
// what the Go runtime holds free: entering pressure, and while past the
// critical mark.
const memoryFreeEvery = 10 * time.Second

// serverMemoryPressure is the server's reading of its memory, once it reads
// one (registerMemoryPressure).
var serverMemoryPressure atomic.Pointer[memoryPressureController]

// memoryLongWorkTurn says whether a turn of long work may start now; one
// that may calls done once it ends.
func memoryLongWorkTurn() (bool, func()) {
	controller := serverMemoryPressure.Load()
	if controller == nil {
		return true, func() {}
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.refresh()
	now := controller.now()
	switch {
	case controller.critical:
		return false, nil
	case controller.underPressure || controller.sampleFailed != nil:
		if controller.longWorkRunning > 0 || now.Sub(controller.longWorkEnded) < memoryLongWorkBreath {
			return false, nil
		}
	}
	controller.longWorkRunning++
	return true, func() {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		controller.longWorkRunning--
		controller.longWorkEnded = controller.now()
	}
}

// memoryLongWorkHeld says whether long work under way should end its turn
// at its next stop: the memory is past the high mark.
func memoryLongWorkHeld() bool {
	controller := serverMemoryPressure.Load()
	if controller == nil {
		return false
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.refresh()
	return controller.underPressure || controller.critical || controller.sampleFailed != nil
}

// memoryLongWorkers is how many turns of one kind of long work go at once:
// most, on a server with the memory for them; on a smaller one, one for
// each GiB it plans for, at least one — past the high mark one at a time
// anyway (memoryLongWorkTurn).
func memoryLongWorkers(most int) int {
	limit, _ := serverMemoryLimit()
	return max(1, min(most, int(limit/memoryGiB)))
}

// freeHeld gives the system back what the Go runtime holds free, at most
// every memoryFreeEvery; the controller's lock is held.
func (controller *memoryPressureController) freeHeld(now time.Time) {
	if !controller.freedAt.IsZero() && now.Sub(controller.freedAt) < memoryFreeEvery {
		return
	}
	controller.freedAt = now
	go debug.FreeOSMemory()
}
