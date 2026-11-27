package app

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// The one write connection is shared by people's edits, the big writes of
// imports, and the work done behind them: the search index, reminders,
// notifications, compaction. Whoever asked for it next got it, at random,
// and work in the background took it as often as a person (2026-10-15
// load measurement: with 300 people at work, a CSV import's turns and the
// index between them held people's edits up to several seconds).
//
// People's edits come first: while one is under way, work in the
// background waits before its next write, at most writeLaneYieldMax, so it
// keeps a share of the connection however busy people are. A page's own
// changes keep their order; only work on other pages waits.
//
// A person's request that writes in bulk — an agent's change of hundreds of
// blocks — is a big write too: it no longer counts as an edit to wait for,
// and it waits for people's edits before it writes, the same way. Big
// writes go one at a time, so people's edits wait behind at most one (two
// agents' batches back to back held people's edits 1.9 s, 2026-10-15).

const (
	writeLaneYieldMax  = 250 * time.Millisecond
	writeLaneYieldPoll = 2 * time.Millisecond
	// writeLaneBulkOperations is how many operations make a change a big write.
	writeLaneBulkOperations = 100
)

type writeLaneEntryKey struct{}

// writeLaneEntry is a person's writing request as the lane counts it.
type writeLaneEntry struct {
	counted atomic.Bool
}

// peopleWriting counts people's requests that may write, under way now.
var peopleWriting atomic.Int64

// writeLaneReadPaths are requests that are sent as writes but only read.
var writeLaneReadPaths = []string{
	"/database-query", "/places/search", "/blueprints/preview", "/library-pages", "/people/lookup",
}

// writeLanePersonWrite is whether a request is a person's that may write:
// one of the product's API, other than a read sent as a write. Agents'
// calls (MCP) are big writes of their own and don't hold the work behind
// them back.
func writeLanePersonWrite(request *http.Request) bool {
	switch request.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	path := request.URL.Path
	return strings.HasPrefix(path, "/api/leafen/v1/") && !requestOnlyReads(path)
}

// requestOnlyReads is whether a request sent as a write only reads.
func requestOnlyReads(path string) bool {
	for _, read := range writeLaneReadPaths {
		if strings.HasSuffix(path, read) {
			return true
		}
	}
	return false
}

// registerWriteLane counts people's writing requests while they are under way.
func registerWriteLane(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenWriteLane", Priority: apis.DefaultLoadAuthTokenMiddlewarePriority + 10,
		Func: func(request *core.RequestEvent) error {
			if !writeLanePersonWrite(request.Request) {
				return request.Next()
			}
			entry := &writeLaneEntry{}
			entry.counted.Store(true)
			peopleWriting.Add(1)
			defer func() {
				if entry.counted.Swap(false) {
					peopleWriting.Add(-1)
				}
			}()
			request.Request = request.Request.WithContext(context.WithValue(request.Request.Context(), writeLaneEntryKey{}, entry))
			return request.Next()
		},
	})
}

// bulkWriting lets one big write go at a time.
var bulkWriting = make(chan struct{}, 1)

// writeInBulk makes a request's change a big write: it stops counting as
// an edit, takes its turn among big writes, and waits for people's edits
// before it writes. done gives the turn back once it has written.
func writeInBulk(ctx context.Context) (done func()) {
	if entry, ok := ctx.Value(writeLaneEntryKey{}).(*writeLaneEntry); ok && entry.counted.Swap(false) {
		peopleWriting.Add(-1)
	}
	select {
	case bulkWriting <- struct{}{}:
	case <-ctx.Done():
		return func() {}
	}
	yieldToPeople(ctx)
	return func() { <-bulkWriting }
}

// yieldToPeople waits, before a write of work in the background, while
// people's edits are under way: until none is, or writeLaneYieldMax.
func yieldToPeople(ctx context.Context) {
	if peopleWriting.Load() == 0 {
		return
	}
	deadline := time.Now().Add(writeLaneYieldMax)
	ticker := time.NewTicker(writeLaneYieldPoll)
	defer ticker.Stop()
	for peopleWriting.Load() > 0 && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
