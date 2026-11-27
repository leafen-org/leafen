package app

import (
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// When an agent's connection or key was last used, for the person's list of
// connected agents. It is written at most once a minute per connection, so a
// busy agent doesn't write on every call, and apart from the call: the call
// only notes it and goes on, and the note is written after, so a read waits
// for no write (2026-11-16: while a Notion migration's last steps held the
// one write connection for seconds, an agent's read that fell on the minute
// waited with them to note its use).

const (
	mcpConnectionLastUsedResolutionMilliseconds = int64(time.Minute / time.Millisecond)
	mcpConnectionUseNotesKey                    = "leafenMCPConnectionUseNotes"
)

// mcpConnectionUseNotes are the uses noted and not written yet, one a
// connection, written by one writer at a time.
type mcpConnectionUseNotes struct {
	app     core.App
	mu      sync.Mutex
	pending map[string]int64
	writing bool
}

// noteMCPConnectionUse records a use when the last one recorded is a minute
// old or more, written apart from the call (inside a transaction, with it).
// A failure to record it doesn't refuse the call; a server that stops before
// it is written loses that use, at most a minute's.
func noteMCPConnectionUse(app core.App, connectionID string, lastUsedAt int64) {
	now := time.Now().UnixMilli()
	if connectionID == "" || now-lastUsedAt < mcpConnectionLastUsedResolutionMilliseconds {
		return
	}
	if app.IsTransactional() {
		writeMCPConnectionUse(app, connectionID, now)
		return
	}
	// Written later as the server, not as the call that first noted one (server_own_app.go).
	server := serverAppOf(app)
	notes := app.Store().GetOrSet(mcpConnectionUseNotesKey, func() any {
		return &mcpConnectionUseNotes{app: server, pending: map[string]int64{}}
	}).(*mcpConnectionUseNotes)
	notes.mu.Lock()
	notes.pending[connectionID] = max(notes.pending[connectionID], now)
	start := !notes.writing
	notes.writing = true
	notes.mu.Unlock()
	// Written under the server's own background, waited for as it stops
	// (server_background.go); a stopping server's uses go unwritten.
	if start && !serverBackgroundOf(server).behind("agent key uses", notes.write) {
		notes.mu.Lock()
		notes.writing = false
		notes.mu.Unlock()
	}
}

// write writes the uses noted until none are left. A use stays noted until
// its write is done, so whether a key was ever used is never asked in
// between (mcpConnectionUseNoted).
func (notes *mcpConnectionUseNotes) write() {
	for {
		notes.mu.Lock()
		taken := make(map[string]int64, len(notes.pending))
		for connectionID, at := range notes.pending {
			taken[connectionID] = at
		}
		if len(taken) == 0 {
			notes.writing = false
			notes.mu.Unlock()
			return
		}
		notes.mu.Unlock()
		for connectionID, at := range taken {
			writeMCPConnectionUse(notes.app, connectionID, at)
			notes.mu.Lock()
			if notes.pending[connectionID] == at {
				delete(notes.pending, connectionID)
			}
			notes.mu.Unlock()
		}
	}
}

// mcpConnectionUseNoted says whether a use of the connection is noted and
// not written yet.
func mcpConnectionUseNoted(app core.App, connectionID string) bool {
	notes, ok := app.Store().Get(mcpConnectionUseNotesKey).(*mcpConnectionUseNotes)
	if !ok {
		return false
	}
	notes.mu.Lock()
	defer notes.mu.Unlock()
	_, noted := notes.pending[connectionID]
	return noted
}

func writeMCPConnectionUse(app core.App, connectionID string, at int64) {
	_, _ = app.DB().NewQuery(`
		UPDATE {{leafen_mcp_connections}} SET [[last_used_at]] = {:now}
		WHERE [[id]] = {:id} AND [[last_used_at]] < {:now}
	`).Bind(dbx.Params{"id": connectionID, "now": at}).Execute()
}

// noteMCPConnectionReach records the address a connection came through
// (mcpReachedVia), "" when it came straight to the server, when that
// changed: a connection that came through a quick tunnel's address is cut
// off when the address does (server_tunnel_first_run.go), one that came
// through an earlier address goes on while it forwards.
func noteMCPConnectionReach(app core.App, connectionID, recorded, via string) {
	if connectionID == "" {
		return
	}
	// A key's own calls read it to write only a change (connection_reach.go).
	connectionReachNoted.Store("mcp:"+connectionID, via)
	if recorded == via {
		return
	}
	_, _ = app.DB().NewQuery(`UPDATE {{leafen_mcp_connections}} SET [[reached_via]] = {:via} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": connectionID, "via": via}).Execute()
}
