package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

// When more requests come than the server can work on at once, they wait
// their turn instead of all working at once: thousands at once fought for
// the one write connection and for SQLite's memory, shared by every
// connection, and the server did less the more it was asked (2026-10-15
// load measurement: 3,000 people at a real pace, writes waiting 16 s and
// failing; 5,000, reads failing too). A request waits at most
// requestAdmissionWait for its turn, and is then told the server is busy
// and to come back — before anything of it ran, so the app sends it again
// (apiTransport.ts). Streams and sockets, which stay open, are let through.
//
// Few at a time do more than many: at four turns a processor, 3,000 people
// waited 1.5 s; at one, 0.6 s (2026-10-15). Reads and writes share the
// turns: with turns of their own, or writes held to half, more ran at once
// or writes waited longer, and 3,000 people waited 2.8 s and 4 s.
//
// Searches, which can take a second, have turns of their own, half as
// many: in the shared turns they kept everyone else waiting a second while
// 300 people searched 50,000 rows just imported, and with as many turns as
// the rest they took the processors from everyone (2026-10-15).
//
// Writes may hold all the shared turns but requestAdmissionReadReserve:
// they wait for the one write connection, and holding every turn while they
// waited kept a 1 ms read waiting seconds behind them (2026-10-16, a
// ten-hour run: page reads p95 4 s during writes, 1–2 ms after). A write
// takes its write turn before a shared one, so one waiting for its write
// turn holds no shared turn.

const (
	requestAdmissionWait       = 10 * time.Second
	requestAdmissionRetryAfter = "2"
	errorCodeServerBusy        = "LEAFEN_SERVER_BUSY"
	serverBusyMessage          = "The server is busy. Try again in a moment."
)

const requestAdmissionReadReserve = 2

var (
	requestAdmissionSlots    = make(chan struct{}, max(8, runtime.GOMAXPROCS(0)))
	requestAdmissionWrites   = make(chan struct{}, max(8, runtime.GOMAXPROCS(0))-requestAdmissionReadReserve)
	requestAdmissionSearches = make(chan struct{}, max(2, runtime.GOMAXPROCS(0)/2))
)

// requestAdmissionWrite is whether a request may write.
func requestAdmissionWrite(request *http.Request) bool {
	return request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodOptions
}

// requestTurns are the turns a request holds, kept on its context so a
// handler that waits for something to happen gives them back while it waits
// (whileWaiting) and takes them again to answer.
type requestTurns struct {
	slots chan struct{}
	write bool
	held  bool
}

type requestTurnsKey struct{}

func (turns *requestTurns) give() {
	if !turns.held {
		return
	}
	turns.held = false
	<-turns.slots
	if turns.write {
		<-requestAdmissionWrites
	}
}

// take takes the turns again by a new deadline, as the request first did.
func (turns *requestTurns) take(request *http.Request) bool {
	deadline := time.Now().Add(requestAdmissionWait)
	if turns.write {
		if taken, err := takeRequestTurn(request, requestAdmissionWrites, deadline); err != nil || !taken {
			return false
		}
	}
	if taken, err := takeRequestTurn(request, turns.slots, deadline); err != nil || !taken {
		if turns.write {
			<-requestAdmissionWrites
		}
		return false
	}
	turns.held = true
	return true
}

// whileWaiting gives a request's turns back while it waits for something to
// happen — a change check, a messenger's list — and takes them again to
// answer, so its work waits its turn as any other's and its waiting holds
// none (2026-10-28: ten open windows, each waiting, held every turn of a
// ten-processor server, and every other request waited ten seconds and was
// refused). False when the turns could not be had again; the request is
// answered busy then (serverBusy).
func whileWaiting(request *http.Request, wait func()) bool {
	turns, _ := request.Context().Value(requestTurnsKey{}).(*requestTurns)
	if turns == nil || !turns.held {
		wait()
		return true
	}
	turns.give()
	wait()
	return turns.take(request)
}

// serverBusy answers a request the server had no turn for. One whose
// context ended while it waited is told so as any failure is: busy when the
// server is stopping, its client still waiting, and as the client's (499)
// when the client left (server_stopping.go) — not as PocketBase's refusal of
// a bad request (400), which a screen doesn't ask again.
func serverBusy(request *core.RequestEvent) error {
	request.Response.Header().Set("Retry-After", requestAdmissionRetryAfter)
	return apiError(request, http.StatusServiceUnavailable, errorCodeServerBusy, serverBusyMessage)
}

// takeRequestTurn takes a turn from slots by the deadline, false when the
// time runs out or the request ends first.
func takeRequestTurn(request *http.Request, slots chan struct{}, deadline time.Time) (bool, error) {
	select {
	case slots <- struct{}{}:
		return true, nil
	default:
	}
	wait := time.NewTimer(time.Until(deadline))
	defer wait.Stop()
	select {
	case slots <- struct{}{}:
		return true, nil
	case <-wait.C:
		return false, nil
	case <-request.Context().Done():
		return false, request.Context().Err()
	}
}

// requestAdmissionSearch is whether a request is a search of a workspace.
func requestAdmissionSearch(request *http.Request) bool {
	return request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/search") &&
		strings.HasPrefix(request.URL.Path, apiRoot+"/workspaces/")
}

// requestAdmissionPassThrough are requests that stay open or carry files:
// sockets, file bytes, uploads and downloads.
var requestAdmissionPassThrough = []string{"/collaboration/", "/rooms/", "/files", "/uploads", "/download", "/html-frame"}

func requestAdmitted(request *http.Request) bool {
	path := request.URL.Path
	if !strings.HasPrefix(path, apiRoot+"/") || strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
		return false
	}
	// A read batch does none of the work: each read it names takes its own
	// turn (read_batch.go), and the batch holding one would keep them waiting.
	if path == apiRoot+readBatchPath {
		return false
	}
	for _, through := range requestAdmissionPassThrough {
		if strings.Contains(path, through) {
			return false
		}
	}
	return true
}

// A write let in is answered when it is done, however long it holds the
// one write connection (it waits for the connection itself at most
// writeConnectionWait, sqlite_write_gate.go): the server's limits of five
// minutes to read a request and to answer it end for a write once its body
// is read. At the read limit the server ends a request's context as if its
// sender had gone, and at the answer limit it cuts the connection: queued
// behind a transaction that held the connection longer (2026-11-08: a
// 466-second read), an app's change and an agent's were kept, and their
// senders were told the connection failed. What a write is told — done, or
// refused as any refusal — is now what happened. Its body is still read
// within the limit.
func answerWritesWhenDone(request *core.RequestEvent) error {
	if !requestAdmissionWrite(request.Request) {
		return request.Next()
	}
	control := http.NewResponseController(request.Response)
	_ = control.SetWriteDeadline(time.Time{})
	unbounded := func() { _ = control.SetReadDeadline(time.Time{}) }
	body, rereadable := request.Request.Body.(*router.RereadableReadCloser)
	switch {
	case request.Request.Body == nil || request.Request.Body == http.NoBody:
		unbounded()
	case rereadable:
		body.ReadCloser = &readUntilEnd{ReadCloser: body.ReadCloser, end: unbounded}
	}
	return request.Next()
}

// readUntilEnd is a request's body that says when it was read to its end.
type readUntilEnd struct {
	io.ReadCloser
	end func()
}

func (body *readUntilEnd) Read(p []byte) (int, error) {
	n, err := body.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) && body.end != nil {
		body.end()
		body.end = nil
	}
	return n, err
}

// registerRequestAdmission has the product's requests take a turn before
// any of their work, signing in included, and its writes answered when done.
func registerRequestAdmission(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenWriteAnswers", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority - 2,
		Func: answerWritesWhenDone,
	})
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenRequestAdmission", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority - 1,
		Func: func(request *core.RequestEvent) error {
			if !requestAdmitted(request.Request) {
				return request.Next()
			}
			slots := requestAdmissionSlots
			if requestAdmissionSearch(request.Request) {
				slots = requestAdmissionSearches
			}
			deadline := time.Now().Add(requestAdmissionWait)
			turns := &requestTurns{slots: slots, write: slots == requestAdmissionSlots && requestAdmissionWrite(request.Request)}
			// A request that ended while it waited is answered as one
			// not let in (serverBusy).
			if turns.write {
				if taken, _ := takeRequestTurn(request.Request, requestAdmissionWrites, deadline); !taken {
					return serverBusy(request)
				}
			}
			if taken, _ := takeRequestTurn(request.Request, slots, deadline); !taken {
				if turns.write {
					<-requestAdmissionWrites
				}
				return serverBusy(request)
			}
			turns.held = true
			defer turns.give()
			request.Request = request.Request.WithContext(context.WithValue(request.Request.Context(), requestTurnsKey{}, turns))
			return request.Next()
		},
	})
}
