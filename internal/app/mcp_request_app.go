package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// An MCP call that may write writes through its request's own app, as every
// other request's handler does (request_write_stop.go): its transactions are
// the request's, which a closing server lets end until half its time and
// then stops, rolled back whole, the request told busy. Tools take the app
// of the server they are added to, so such a call, once signed in, is
// served by a server whose tools are added over a call app (mcpCallApp)
// that writes through the app of the call it serves: one call at a time,
// kept for the next call once the library's session for it ended — which
// waits for its handlers (mcpWriteServers). Made once over the server's own
// app as it started, they wrote outside that stop, held the write
// connection through it, and the call's client was told nothing but a cut
// connection (2026-11-18). Made again for each call, adding the tools took
// 4–9 ms of each call, 20–40 ms on a NAS-class server — the library reads
// each tool's schema again as it is added (2026-11-18). A call only of tools
// marked read-only, as a GET, and the rest of the protocol (lists, a
// client listening for changes) are served by the server made as the
// server started.
//
// Work a call leaves running after it answered keeps the call app: its
// transactions are then the server's own, or, once the server serves
// another call, that call's, which the stop ends with it.
//
// A session over server-sent events is one server from the stream's opening
// to its end, its calls posted apart from the stream and answered on it: it
// writes through an app whose transactions are one request's while the
// stream is open (mcpSessionWriteApp).

// mcpSchemaCache keeps the tools' schemas for the servers made after the
// first.
var mcpSchemaCache = mcp.NewSchemaCache()

type mcpRequestServerKey struct{}

// mcpRequestServer is the server one MCP request is served by.
type mcpRequestServer struct {
	app     core.App
	server  *mcp.Server
	release func()
	// busy: a stopping server turned it away before any of it ran
	// (turnMCPRequestAwayBusy).
	busy bool
}

// turnMCPRequestAwayBusy has an MCP request a stopping server won't take
// answered busy, as one that came once it was stopping
// (registerServerStoppingAnswer), in place of an answer of the library's.
// It is false for one served apart from serveMCPRequest, which then answers
// on its own.
func turnMCPRequestAwayBusy(request *http.Request) bool {
	held, ok := request.Context().Value(mcpRequestServerKey{}).(*mcpRequestServer)
	if ok {
		held.busy = true
	}
	return ok
}

// serveMCPRequest serves an MCP request over its own app, and answers it
// busy, as any request is when the server's stop ended its write with none
// of it kept (registerRequestWriteStop), in place of the answer its tools
// gave then — and when the stop ended it before anything of its answer was
// written (answerMCPRequestCutByStop), or a stopping server would not take
// it (turnMCPRequestAwayBusy).
func serveMCPRequest(request *core.RequestEvent, handler http.Handler) error {
	held := &mcpRequestServer{app: request.App}
	defer func() {
		if held.release != nil {
			held.release()
		}
	}()
	response := &mcpStoppedWriteResponse{
		ResponseWriter: request.Response,
		stopped:        func() bool { return requestWriteStopped(request) },
	}
	ctx := context.WithValue(request.Request.Context(), mcpRequestServerKey{}, held)
	handler.ServeHTTP(response, request.Request.WithContext(ctx))
	switch {
	case held.busy && !response.started:
		request.Set(serverStoppingTurnedAwayKey, true)
		request.Response.Header().Set("Connection", "close")
		return serverBusy(request)
	case response.dropped:
		return serverBusy(request)
	case !response.started && request.Request.Context().Err() != nil && serverStopping(request.App):
		return answerMCPRequestCutByStop(request)
	}
	return nil
}

const (
	errorCodeMCPAnswerLost = "LEAFEN_MCP_ANSWER_LOST"
	mcpAnswerLostMessage   = "The server stopped before answering this call, after some of what it changed was saved. " +
		"Read what it changed before sending it again."
)

// answerMCPRequestCutByStop answers a request the server's stop ended
// before anything of its answer was written. PocketBase ends the context of
// every request under way as it closes its listener, the parts stopped
// before, and the library leaves an answer it has not begun once the
// request's context ends: a call under way then was answered 200 with
// nothing at all (2026-11-18). One its tools had not yet been handed was
// dropped as its session closed; one they had begun went on — their
// context is the library's own, and the library waits for them before it
// returns — keeping what they wrote or failing at a closing server. None of
// it kept, the request is told busy, as one whose write the stop ended, and
// the client sends it again once the server is back; some of it kept, it is
// told its answer was lost, for its client to read what it changed rather
// than make it twice. An answer begun — a stream's first event, a call
// answered — goes on as it was.
func answerMCPRequestCutByStop(request *core.RequestEvent) error {
	request.Response.Header().Set("Connection", "close")
	if mcpRequestCutByStop(request) {
		return serverBusy(request)
	}
	return apiError(request, http.StatusServiceUnavailable, errorCodeMCPAnswerLost, mcpAnswerLostMessage)
}

// mcpRequestCutByStop notes a request the server's stop ended before it
// answered as one whose write the stop ended (requestWriteStopped), and is
// whether none of it was kept: none of its transactions ended having changed
// a row — a request that may not write keeps nothing.
func mcpRequestCutByStop(request *core.RequestEvent) bool {
	note, _ := request.Get(requestWriteNoteKey).(*requestWriteNote)
	if note == nil {
		return true
	}
	note.stopped.Store(true)
	return !note.committed.Load()
}

// mcpServerForRequest is the server a signed-in request is served by: a
// kept one serving its app for a call that may write, else the shared one.
func mcpServerForRequest(request *http.Request, shared *mcp.Server, writes *mcpWriteServers) *mcp.Server {
	held, ok := request.Context().Value(mcpRequestServerKey{}).(*mcpRequestServer)
	if !ok || held.app == nil || !mcpCallMayWrite(request, writes.readOnly) {
		return shared
	}
	if held.server == nil {
		held.server, held.release = writes.take(held.app)
	}
	return held.server
}

// mcpIdleWriteServers is how many servers for calls that may write a
// surface keeps between calls; more calls at once are served by servers
// made for them and let go.
const mcpIdleWriteServers = 8

// mcpWriteServers are a surface's servers for calls that may write.
type mcpWriteServers struct {
	app      core.App
	surface  mcpToolSurface
	readOnly map[string]bool
	mu       sync.Mutex
	idle     []*mcpWriteServer
}

type mcpWriteServer struct {
	server *mcp.Server
	app    *mcpCallApp
}

// newMCPWriteServers keeps a surface's servers for calls that may write,
// telling those calls by the tools its shared server marks read-only.
func newMCPWriteServers(app core.App, surface mcpToolSurface, shared *mcp.Server) *mcpWriteServers {
	return &mcpWriteServers{app: app, surface: surface, readOnly: mcpReadOnlyTools(shared)}
}

// take is a server serving one call over callApp until release is called.
func (servers *mcpWriteServers) take(callApp core.App) (*mcp.Server, func()) {
	servers.mu.Lock()
	var kept *mcpWriteServer
	if last := len(servers.idle) - 1; last >= 0 {
		kept, servers.idle = servers.idle[last], servers.idle[:last]
	}
	servers.mu.Unlock()
	if kept == nil {
		app := &mcpCallApp{App: servers.app}
		kept = &mcpWriteServer{server: newMCPServer(app, servers.surface), app: app}
	}
	kept.app.call.Store(&mcpCall{app: callApp})
	return kept.server, func() {
		kept.app.call.Store(nil)
		servers.mu.Lock()
		defer servers.mu.Unlock()
		if len(servers.idle) < mcpIdleWriteServers {
			servers.idle = append(servers.idle, kept)
		}
	}
}

// mcpCallApp is the app a kept server's tools are added over: the server's
// own app, but for its transactions, which run through the app of the call
// the server serves — a request's app differs from the server's in its
// transactions alone (requestWriteApp) — and between calls through the
// server's own.
type mcpCallApp struct {
	core.App
	call atomic.Pointer[mcpCall]
}

type mcpCall struct{ app core.App }

func (app *mcpCallApp) RunInTransaction(fn func(txApp core.App) error) error {
	if call := app.call.Load(); call != nil {
		return call.app.RunInTransaction(fn)
	}
	return app.App.RunInTransaction(fn)
}

// mcpCallMayWrite is whether an MCP request calls a tool not marked
// read-only. Its body, read here, is put back for the library to read, with
// the error reading it ended in, which the library answers.
func mcpCallMayWrite(request *http.Request, readOnly map[string]bool) bool {
	if request.Body == nil {
		return false
	}
	body, err := io.ReadAll(request.Body)
	readers := []io.Reader{bytes.NewReader(body)}
	if err != nil {
		readers = append(readers, mcpBodyError{err})
	}
	request.Body = io.NopCloser(io.MultiReader(readers...))
	type message struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	messages := []message{}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		_ = json.Unmarshal(trimmed, &messages)
	} else {
		one := message{}
		_ = json.Unmarshal(trimmed, &one)
		messages = append(messages, one)
	}
	for _, each := range messages {
		if each.Method != "tools/call" {
			continue
		}
		call := struct {
			Name string `json:"name"`
		}{}
		if json.Unmarshal(each.Params, &call) != nil || !readOnly[call.Name] {
			return true
		}
	}
	return false
}

// mcpBodyError is the error a request's body ended in, read again.
type mcpBodyError struct{ err error }

func (body mcpBodyError) Read([]byte) (int, error) { return 0, body.err }

// mcpReadOnlyTools are a server's own tools marked read-only.
func mcpReadOnlyTools(server *mcp.Server) map[string]bool {
	names := map[string]bool{}
	for _, tool := range listRegisteredMCPTools(server) {
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			names[tool.Name] = true
		}
	}
	return names
}

// mcpStoppedWriteResponse passes an MCP answer on, but for one given once
// the server's stop ended its request's write with none of it kept: that
// one is dropped, for the request to be answered busy.
type mcpStoppedWriteResponse struct {
	http.ResponseWriter
	stopped          func() bool
	started, dropped bool
}

// start decides at the answer's first byte, once its tools have answered,
// whether it goes out.
func (response *mcpStoppedWriteResponse) start() bool {
	if !response.started {
		response.started = true
		response.dropped = response.stopped()
	}
	return !response.dropped
}

func (response *mcpStoppedWriteResponse) WriteHeader(status int) {
	if response.start() {
		response.ResponseWriter.WriteHeader(status)
	}
}

func (response *mcpStoppedWriteResponse) Write(body []byte) (int, error) {
	if !response.start() {
		return len(body), nil
	}
	return response.ResponseWriter.Write(body)
}

func (response *mcpStoppedWriteResponse) Flush() {
	if !response.start() {
		return
	}
	if flusher, ok := response.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (response *mcpStoppedWriteResponse) Unwrap() http.ResponseWriter {
	return response.ResponseWriter
}

// mcpSessionWriteApp is the app a session over server-sent events writes
// through, its transactions one request's until end is called as the
// stream ends.
func mcpSessionWriteApp(app core.App) (core.App, func()) {
	note := &requestWriteNote{}
	return requestWriteApp{App: app, writes: requestWritesOf(app), note: note}, func() { note.answered.Store(true) }
}

// mcpAppListServers is the native surface's shared server, where apps'
// tools, prompts and resources are listed.
var mcpAppListServers atomic.Pointer[mcp.Server]

// mcpAppsChangedMarker is a tool, prompt and resource the native server takes
// and drops at once when apps change: the MCP library announces only changes
// to a server's own, and apps' are listed around them. Clients listening for
// changes (subscriptions/listen) then read their lists again; the app
// middleware keeps the marker out of lists.
const mcpAppsChangedMarker = "leafen_apps_changed"

// mcpAppsChanged tells listening clients that apps' tools, prompts and
// resources may have changed.
func mcpAppsChanged() {
	server := mcpAppListServers.Load()
	if server == nil {
		return
	}
	server.AddTool(&mcp.Tool{Name: mcpAppsChangedMarker, InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, errors.New("no such tool")
		})
	server.RemoveTools(mcpAppsChangedMarker)
	server.AddPrompt(&mcp.Prompt{Name: mcpAppsChangedMarker}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return nil, errors.New("no such prompt")
	})
	server.RemovePrompts(mcpAppsChangedMarker)
	uri := "leafen-internal://" + mcpAppsChangedMarker
	server.AddResource(&mcp.Resource{URI: uri, Name: mcpAppsChangedMarker}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return nil, mcp.ResourceNotFoundError(uri)
	})
	server.RemoveResources(uri)
}
