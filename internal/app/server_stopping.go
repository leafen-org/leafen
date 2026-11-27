package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// A request's context ends for two reasons that call for different
// answers. Its client left — a newer read of the page list retires the one
// under way, a tab closes — and no one reads what it is told: it is noted as
// the client's (499), not as a server failure. Or the server is stopping:
// PocketBase ends every request under way as it closes its listener, while
// their clients still wait for an answer; they are told the server can't
// serve now (503), which a screen takes as the server away and asks again
// once it is back (web apiTransport.ts statusSaysServerAway), and the
// failure is logged as one (2026-11-17: a health read and a page write held
// across a restart were told 499, and the screen didn't ask again).
//
// The server is marked stopping before its shutdown ends anything
// (app.go leafenProductShutdown), so every request ended after that is the
// server's — and so is every failure of a request under way: the server
// stops its parts beneath it, its writes' waits for the write connection end
// and its pools close, and whichever part it failed in, it is told the
// server is away (requestFailureStatus; 2026-11-17: a page made and a
// workspace read under way at a restart were told 500).
//
// A request that comes once the server is stopping is told so before any of
// its work (registerServerStoppingAnswer): the server stops its parts — the
// work requests leave to do after them, its runners, its graph reads' pool,
// its rooms — for up to its five seconds before PocketBase closes its
// listener, and a request let in meanwhile reached parts already stopped
// (2026-11-17, a stop with a write under way: pages made and workspaces read
// in the last few hundred milliseconds before the listener closed were told
// 500 within milliseconds, a page was made after those parts had stopped,
// and writes let in took the write connection from the server's own last
// ones, which then failed and the process ended as failed). It is told busy,
// as one the server had no turn for (serverBusy), which the app sends again
// once the server answers (apiTransport.ts), its connection closes with the
// answer, and it is noted as turned away, not as a failure: none of it ran.
// The app's own pages and build files, which no part serves, are still
// served.

const serverStoppingStoreKey = "leafen.serverStopping"

func serverStoppingFlag(app core.App) *atomic.Bool {
	flag, _ := app.Store().GetOrSet(serverStoppingStoreKey, func() any {
		return &atomic.Bool{}
	}).(*atomic.Bool)
	return flag
}

// markServerStopping notes that the server has begun to stop.
func markServerStopping(app core.App) {
	serverStoppingFlag(app).Store(true)
}

// serverStopping is whether the server has begun to stop.
func serverStopping(app core.App) bool {
	return app != nil && serverStoppingFlag(app).Load()
}

// requestClientLeft is whether a request ended because its client left: its
// context was canceled while the server was not stopping.
func requestClientLeft(event *core.RequestEvent) bool {
	if event == nil || event.Request == nil ||
		!errors.Is(event.Request.Context().Err(), context.Canceled) {
		return false
	}
	return !serverStopping(event.App)
}

// requestFailureStatus is the status a request's server failure is answered
// with (writeAPIErrorWithReference): the client's (499) when its client
// left, the server away (503) while the server stops, else its own.
func requestFailureStatus(event *core.RequestEvent, status int) int {
	switch {
	case status < http.StatusInternalServerError:
		return status
	case requestClientLeft(event):
		return statusClientClosedRequest
	case event != nil && serverStopping(event.App):
		return http.StatusServiceUnavailable
	}
	return status
}

// registerServerStoppingAnswer has a request that comes once the server is
// stopping told busy before any of its work, but for the app's own files.
func registerServerStoppingAnswer(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		// After the request log and the panic's answer, before anything else.
		Id: "leafenServerStopping", Priority: apis.DefaultPanicRecoverMiddlewarePriority + 1,
		Func: func(request *core.RequestEvent) error {
			if !serverStopping(request.App) || webAppFileRequest(request.Request) {
				return request.Next()
			}
			request.Set(serverStoppingTurnedAwayKey, true)
			request.Response.Header().Set("Connection", "close")
			return serverBusy(request)
		},
	})
}

const serverStoppingTurnedAwayKey = "leafen.serverStoppingTurnedAway"

// requestTurnedAwayStopping is whether a stopping server turned a request
// away before any of its work, as no failure (logAPIDiagnostic).
func requestTurnedAwayStopping(event *core.RequestEvent) bool {
	turned, _ := event.Get(serverStoppingTurnedAwayKey).(bool)
	return turned
}

// webAppFileRequest is whether a request asks for one of the app's own pages
// or build files, which the app's route serves from the build alone.
func webAppFileRequest(request *http.Request) bool {
	return request.Pattern == http.MethodGet+" "+webAppPath && !strings.HasPrefix(request.URL.Path, apiRoot+"/")
}

// requestPause waits d, true when the request ended first.
func requestPause(event *core.RequestEvent, d time.Duration) bool {
	select {
	case <-event.Request.Context().Done():
		return true
	case <-time.After(d):
		return false
	}
}
