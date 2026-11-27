//go:build !leafen_noapps

package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// The parent's side of app workers (docs/app-platform.md 11·15): a few warm
// worker processes per app, each running one call at a time, ended after
// five idle minutes or when a call doesn't come back in time. Apps share a
// small number of calls at once, so the server's own work stays ahead.

const (
	appWorkerIdle      = 5 * time.Minute
	appWorkerMaxOutput = 1 << 20
)

// appWorkerLimitSet bounds workers on a platform: calls per app and in all
// at once, each call's memory in 64 KB pages (unless its worker declares
// more, up to the most a call may have), and long-running workers at once
// (none where workers run inside the server).
type appWorkerLimitSet struct {
	perApp, atOnce              int
	memoryPages, maxMemoryPages uint32
	longRuns                    int
}

var (
	errAppWorkerBusy   = errors.New("the app's workers are busy")
	errAppWorkerHalted = errors.New("ended by the app's emergency stop")
	// errAppWorkerStopped wraps a worker that went away mid-call.
	errAppWorkerStopped = errors.New("the worker stopped")
)

// appWorkerRefusal is a call the module ended with a reason of its own
// (leafen.Fail): the app's words for the person, such as "open the Vault
// app in an organization", which its screen and an agent are given
// (2026-10-14: every refusal reached them as "The action failed."). A
// trap, an exit or the memory passing are the call's failure, whose words
// stay in the run's record.
type appWorkerRefusal struct{ Reason string }

func (refusal appWorkerRefusal) Error() string { return refusal.Reason }

// appWorkerCompileLimit bounds compiling a module, apart from its call's time.
const appWorkerCompileLimit = 2 * time.Minute

// appWorkerPastLimit is a call its time ended. The person and the app learn
// that much, none of what it was doing (2026-11-04: it read "The action
// failed.").
type appWorkerPastLimit struct{ Text string }

func (limit appWorkerPastLimit) Error() string { return limit.Text }

// appWorkerRefusalText is a refusal's reason as it is shown: one line, at
// most 500 characters.
func appWorkerRefusalText(err error) (string, bool) {
	var refusal appWorkerRefusal
	if !errors.As(err, &refusal) {
		return "", false
	}
	text := strings.Join(strings.Fields(refusal.Reason), " ")
	if runes := []rune(text); len(runes) > 500 {
		text = string(runes[:500])
	}
	return text, text != ""
}

var (
	// appWorkerStart starts one worker, as a process of its own or, where
	// the platform has none to give, inside the server; checks replace it.
	appWorkerStart = defaultAppWorkerStart
)

type appWorkerConn struct {
	in     io.Writer
	out    *bufio.Reader
	stop   func()
	loaded map[string]bool
	used   time.Time
	broken bool
	// halted is set when an emergency stop ends the call it runs.
	halted bool
}

type appWorkerPool struct {
	mu      sync.Mutex
	idle    map[string][]*appWorkerConn
	busy    map[string]map[*appWorkerConn]bool
	running map[string]int
	atOnce  chan struct{}
	nextID  int64
	// waiting are the calls of each app waiting for one of its workers, in
	// the order they came.
	waiting map[string][]int64
	// halts counts each app's emergency stops: a call that came before one
	// doesn't start after it.
	halts map[string]int
}

var appWorkers = &appWorkerPool{idle: map[string][]*appWorkerConn{}, busy: map[string]map[*appWorkerConn]bool{}, running: map[string]int{},
	atOnce: make(chan struct{}, appWorkerLimits.atOnce), waiting: map[string][]int64{}, halts: map[string]int{}}

func init() {
	appEmergencyCleanups = append(appEmergencyCleanups, appWorkers.halt)
}

func startAppWorkerProcess() (*appWorkerConn, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	folder, err := os.MkdirTemp("", "leafen-app-worker-")
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, "app-worker")
	command.Env = []string{}
	command.Dir = folder
	command.Stderr = io.Discard
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		_ = os.RemoveAll(folder)
		return nil, err
	}
	return &appWorkerConn{in: stdin, out: bufio.NewReader(stdout), loaded: map[string]bool{}, stop: func() {
		_ = stdin.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = os.RemoveAll(folder)
	}}, nil
}

// startAppWorkerInProcess serves a worker inside the server: the same calls
// over pipes, in wazero's sandbox, without a process of its own.
func startAppWorkerInProcess() (*appWorkerConn, error) {
	fromChild, childOut := io.Pipe()
	childIn, toChild := io.Pipe()
	go func() {
		// A worker that panics fails its call: its pipe closes.
		runGuarded(nil, "app worker in the server", func() { serveAppWorker(childIn, childOut) })
		_ = childOut.Close()
	}()
	return &appWorkerConn{in: toChild, out: bufio.NewReader(fromChild), loaded: map[string]bool{}, stop: func() {
		_ = toChild.Close()
		_ = childIn.Close()
		_ = fromChild.Close()
	}}, nil
}

type appWorkerCall struct {
	InstallID string
	Module    string
	Wasm      []byte
	Function  string
	Input     []byte
	Timeout   time.Duration
	// MemoryPages is the call's memory in 64 KB pages; 0 is the platform's.
	MemoryPages uint32
	// Long is a long-running worker's call (app_platform_long_runs.go).
	Long bool
	// Resident names an instance the worker keeps for the next such call.
	Resident string
	// MaxOutput is the most the call may answer; 0 is the platform's.
	MaxOutput int
}

// call runs one function on a worker of the app, answering its host calls
// with host; a worker that breaks or runs past its time is ended.
func (pool *appWorkerPool) call(ctx context.Context, call appWorkerCall, host func(string, []byte) ([]byte, error)) ([]byte, error) {
	pool.mu.Lock()
	halts := pool.halts[call.InstallID]
	pool.mu.Unlock()
	if err := pool.reserve(ctx, call.InstallID, halts); err != nil {
		return nil, err
	}
	select {
	case pool.atOnce <- struct{}{}:
	case <-ctx.Done():
		pool.unreserve(call.InstallID)
		return nil, ctx.Err()
	}
	defer func() { <-pool.atOnce }()
	// A call waiting when the app's emergency stop came doesn't start once a
	// worker is free (2026-11-03 audit: two waiting calls ran their whole
	// time after it).
	pool.mu.Lock()
	halted := pool.halts[call.InstallID] != halts
	pool.mu.Unlock()
	if halted {
		pool.unreserve(call.InstallID)
		return nil, errAppWorkerHalted
	}
	conn, id, err := pool.take(call.InstallID)
	if err != nil {
		return nil, err
	}
	// The app's place comes back however the call ends.
	defer pool.give(call.InstallID, conn)
	output, err := pool.exchange(conn, id, call, host)
	if conn.halted {
		return nil, errAppWorkerHalted
	}
	return output, err
}

// appWorkerSlotWait is how long a call waits for one of its app's workers:
// a drive's requests come several at once, and the app is slowed rather
// than refused (docs/app-platform.md 15; 2026-10-18: a third request
// failed at once while two ran).
const appWorkerSlotWait = 10 * time.Second

// reserve takes one of the app's calls at once, waiting in turn for one to
// end first when they are all taken; the app's place is taken before the
// server's, so an app waiting holds none of the server's places. A wait the
// app's emergency stop reaches ends with it.
func (pool *appWorkerPool) reserve(ctx context.Context, installID string, halts int) error {
	deadline, ticket := time.Now().Add(appWorkerSlotWait), int64(0)
	leave := func() {
		waiting := slices.DeleteFunc(pool.waiting[installID], func(each int64) bool { return each == ticket })
		if len(waiting) == 0 {
			delete(pool.waiting, installID)
		} else {
			pool.waiting[installID] = waiting
		}
	}
	for {
		pool.mu.Lock()
		if pool.halts[installID] != halts {
			leave()
			pool.mu.Unlock()
			return errAppWorkerHalted
		}
		waiting := pool.waiting[installID]
		if pool.running[installID] < appWorkerLimits.perApp && (len(waiting) == 0 || waiting[0] == ticket) {
			pool.running[installID]++
			leave()
			pool.mu.Unlock()
			return nil
		}
		if ticket == 0 {
			pool.nextID++
			ticket = pool.nextID
			pool.waiting[installID] = append(waiting, ticket)
		}
		pool.mu.Unlock()
		if !time.Now().Before(deadline) {
			pool.mu.Lock()
			leave()
			pool.mu.Unlock()
			return errAppWorkerBusy
		}
		select {
		case <-ctx.Done():
			pool.mu.Lock()
			leave()
			pool.mu.Unlock()
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (pool *appWorkerPool) unreserve(installID string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.running[installID]--
}

// take gives a reserved call a worker: an idle one, or one started for it.
func (pool *appWorkerPool) take(installID string) (*appWorkerConn, int64, error) {
	pool.mu.Lock()
	pool.nextID++
	id := pool.nextID
	if idle := pool.idle[installID]; len(idle) > 0 {
		conn := idle[len(idle)-1]
		pool.idle[installID] = idle[:len(idle)-1]
		pool.markBusy(installID, conn)
		pool.mu.Unlock()
		return conn, id, nil
	}
	pool.mu.Unlock()
	conn, err := appWorkerStart()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if err != nil {
		pool.running[installID]--
		return nil, 0, err
	}
	pool.markBusy(installID, conn)
	return conn, id, nil
}

func (pool *appWorkerPool) markBusy(installID string, conn *appWorkerConn) {
	if pool.busy[installID] == nil {
		pool.busy[installID] = map[*appWorkerConn]bool{}
	}
	pool.busy[installID][conn] = true
}

// halt ends at once every worker of an app the emergency stop reached: the
// calls it is running fail as ended by it, so do the calls waiting for one,
// and its idle workers go.
func (pool *appWorkerPool) halt(installID string) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.halts[installID]++
	for conn := range pool.busy[installID] {
		conn.halted = true
		go conn.stop()
	}
	for _, conn := range pool.idle[installID] {
		go conn.stop()
	}
	delete(pool.idle, installID)
}

func (pool *appWorkerPool) give(installID string, conn *appWorkerConn) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.running[installID]--
	delete(pool.busy[installID], conn)
	if len(pool.busy[installID]) == 0 {
		delete(pool.busy, installID)
	}
	if conn.broken || conn.halted {
		go conn.stop()
		return
	}
	conn.used = time.Now()
	pool.idle[installID] = append(pool.idle[installID], conn)
}

// exchange sends the call and answers the worker until its result comes,
// ending the worker if the answer is late or garbled.
func (pool *appWorkerPool) exchange(conn *appWorkerConn, id int64, call appWorkerCall, host func(string, []byte) ([]byte, error)) ([]byte, error) {
	timeout := call.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	pages := call.MemoryPages
	if pages == 0 {
		pages = appWorkerLimits.memoryPages
	}
	// Host calls count toward the call's own time; the margin is for the worker's.
	maxOutput := call.MaxOutput
	if maxOutput == 0 {
		maxOutput = appWorkerMaxOutput
	}
	started := time.Now()
	limit := timeout + 5*time.Second
	if !conn.loaded[call.Module] {
		limit += appWorkerCompileLimit
	}
	late := time.AfterFunc(limit, conn.stop)
	defer late.Stop()
	send := func(withWasm bool) error {
		message := appWorkerMessage{Op: "call", ID: id, Module: call.Module, Function: call.Function, Input: call.Input,
			TimeoutMs: timeout.Milliseconds(), MemoryPages: pages, MaxOutput: maxOutput, Long: call.Long, Resident: call.Resident}
		if withWasm {
			message.Wasm = call.Wasm
		}
		return writeAppWorkerFrame(conn.in, message)
	}
	if err := send(!conn.loaded[call.Module]); err != nil {
		conn.broken = true
		return nil, fmt.Errorf("%w: %w", errAppWorkerStopped, err)
	}
	conn.loaded[call.Module] = true
	for {
		message, err := readAppWorkerFrame(conn.out)
		if err != nil {
			conn.broken = true
			// The worker didn't come back in its time and its margin.
			if time.Since(started) >= timeout {
				return nil, appWorkerPastLimit{Text: fmt.Sprintf("the call ran past %s", timeout)}
			}
			return nil, fmt.Errorf("%w: %w", errAppWorkerStopped, err)
		}
		switch {
		case message.Op == "host" && message.ID == id:
			result, hostErr := answerAppWorkerHost(host, message.Name, message.Payload)
			reply := appWorkerMessage{Op: "host_reply", ID: id, HostID: message.HostID, Result: result}
			if hostErr != nil {
				var bound appRunBoundError
				reply.Error, reply.End = hostErr.Error(), errors.As(hostErr, &bound)
			}
			if err := writeAppWorkerFrame(conn.in, reply); err != nil {
				conn.broken = true
				return nil, fmt.Errorf("%w: %w", errAppWorkerStopped, err)
			}
		case message.Op == "result" && message.ID == id && message.Missing:
			// The worker let the module go; send it again.
			late.Reset(timeout + 5*time.Second + appWorkerCompileLimit)
			if err := send(true); err != nil {
				conn.broken = true
				return nil, fmt.Errorf("%w: %w", errAppWorkerStopped, err)
			}
		case message.Op == "result" && message.ID == id:
			if message.Error != "" && message.Refusal {
				return nil, appWorkerRefusal{Reason: message.Error}
			}
			if message.Error != "" && message.PastLimit {
				return nil, appWorkerPastLimit{Text: message.Error}
			}
			if message.Error != "" {
				return nil, errors.New(message.Error)
			}
			return message.Result, nil
		default:
			conn.broken = true
			return nil, errors.New("the worker answered out of turn")
		}
	}
}

// endIdle ends workers idle for five minutes, and all of an app's workers
// when it stops.
func (pool *appWorkerPool) endIdle(now time.Time, stopped func(string) bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for installID, idle := range pool.idle {
		kept := idle[:0]
		for _, conn := range idle {
			if now.Sub(conn.used) > appWorkerIdle || stopped(installID) {
				go conn.stop()
				continue
			}
			kept = append(kept, conn)
		}
		pool.idle[installID] = kept
	}
}
