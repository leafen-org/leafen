package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type mcpSSESession struct {
	generation     uint64
	credentialHash string
	handler        http.Handler
	cancel         context.CancelFunc
	calls          *mcpSSECalls // those it took and has not answered (mcp_sse_session_calls.go)
}

type mcpSSESessionRuntime struct {
	app core.App

	mu             sync.Mutex
	nextGeneration uint64
	closed         bool
	sessions       map[string]mcpSSESession
	wait           sync.WaitGroup
}

type storedMCPActiveSSECredential struct {
	ID        string `db:"id"`
	TokenHash string `db:"token_hash"`
}

func newMCPSSESessionRuntime(app core.App) *mcpSSESessionRuntime {
	return &mcpSSESessionRuntime{
		app:      app,
		sessions: make(map[string]mcpSSESession),
	}
}

func (runtime *mcpSSESessionRuntime) handler(app core.App, surface mcpToolSurface) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authority, ok := request.Context().Value(mcpAuthorityContextKey{}).(mcpAuthority)
		if !ok || authority.ConnectionID == "" || authority.CredentialHash == "" {
			writeMCPHTTPError(response, http.StatusUnauthorized, errorCodeMCPUnauthorized)
			return
		}

		switch request.Method {
		case http.MethodGet:
			runtime.serveGET(app, surface, authority, response, request)
		case http.MethodPost:
			runtime.servePOST(response, request, authority)
		default:
			response.Header().Set("Allow", "GET, POST")
			http.Error(response, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})
}

func (runtime *mcpSSESessionRuntime) serveGET(
	app core.App,
	surface mcpToolSurface,
	authority mcpAuthority,
	response http.ResponseWriter,
	request *http.Request,
) {
	// Its tools write as the request the open stream is (mcp_request_app.go).
	sessionApp, ended := mcpSessionWriteApp(app)
	defer ended()
	server := newMCPServer(sessionApp, surface)
	// The DNS rebinding guard is newMCPAuthenticatedHandler's (mcpHostAllowed).
	handler := mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return server }, &mcp.SSEOptions{DisableLocalhostProtection: true})
	ctx, cancel := mcpSSESessionContext(request.Context(), authority.CredentialExpiresAt)
	calls := newMCPSSECalls()
	generation, admitted := runtime.replace(authority, handler, cancel, calls)
	if !admitted {
		cancel()
		writeMCPHTTPError(response, http.StatusServiceUnavailable, errorCodeMCPConnectionFailed)
		return
	}
	defer func() {
		calls.end()
		runtime.finish(authority.ConnectionID, generation)
		cancel()
	}()

	current, authenticated, err := authenticateMCPRequest(app, request.WithContext(ctx))
	if err != nil {
		writeMCPHTTPDiagnosticError(
			app, response, http.StatusInternalServerError, errorCodeMCPConnectionFailed,
			"sse-session-reauthentication", err,
		)
		return
	}
	if !authenticated || current.ConnectionID != authority.ConnectionID ||
		current.CredentialHash != authority.CredentialHash {
		writeMCPHTTPError(response, http.StatusUnauthorized, errorCodeMCPUnauthorized)
		return
	}
	handler.ServeHTTP(&mcpSSEStream{ResponseWriter: response, calls: calls}, request.WithContext(ctx))
}

func mcpSSESessionContext(parent context.Context, expiresAt int64) (context.Context, context.CancelFunc) {
	if expiresAt > 0 {
		return context.WithDeadline(parent, time.UnixMilli(expiresAt))
	}
	return context.WithCancel(parent)
}

func (runtime *mcpSSESessionRuntime) replace(
	authority mcpAuthority,
	handler http.Handler,
	cancel context.CancelFunc,
	calls *mcpSSECalls,
) (uint64, bool) {
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return 0, false
	}
	runtime.nextGeneration++
	generation := runtime.nextGeneration
	previous := runtime.sessions[authority.ConnectionID]
	runtime.sessions[authority.ConnectionID] = mcpSSESession{
		generation: generation, credentialHash: authority.CredentialHash,
		handler: handler, cancel: cancel, calls: calls,
	}
	runtime.wait.Add(1)
	runtime.mu.Unlock()

	if previous.cancel != nil {
		previous.cancel()
	}
	return generation, true
}

func (runtime *mcpSSESessionRuntime) finish(connectionID string, generation uint64) {
	runtime.mu.Lock()
	if session, ok := runtime.sessions[connectionID]; ok && session.generation == generation {
		delete(runtime.sessions, connectionID)
	}
	runtime.mu.Unlock()
	runtime.wait.Done()
}

func (runtime *mcpSSESessionRuntime) reconcileCredentials() {
	snapshot := runtime.snapshotCredentials()
	if len(snapshot) == 0 {
		return
	}
	active, err := runtime.loadActiveCredentials(snapshot)
	if err != nil {
		logDiagnostic(
			runtime.app, http.StatusInternalServerError, errorCodeMCPConnectionFailed,
			newDiagnosticReference(), "mcp.sse", "credential-reconciliation",
			errorCodeMCPConnectionFailed, err, diagnosticSourcePosition(1),
		)
		active = map[string]string{}
	}
	runtime.cancelMismatched(active)
}

func (runtime *mcpSSESessionRuntime) snapshotCredentials() map[string]string {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	snapshot := make(map[string]string, len(runtime.sessions))
	for connectionID, session := range runtime.sessions {
		snapshot[connectionID] = session.credentialHash
	}
	return snapshot
}

func (runtime *mcpSSESessionRuntime) loadActiveCredentials(
	snapshot map[string]string,
) (map[string]string, error) {
	placeholders := make([]string, 0, len(snapshot))
	params := dbx.Params{"now": time.Now().UnixMilli()}
	index := 0
	for connectionID := range snapshot {
		name := fmt.Sprintf("connection%d", index)
		placeholders = append(placeholders, "{:"+name+"}")
		params[name] = connectionID
		index++
	}
	rows := []storedMCPActiveSSECredential{}
	err := runtime.app.DB().NewQuery(`
		SELECT [[id]], [[token_hash]] FROM {{leafen_mcp_connections}}
		WHERE [[id]] IN (` + strings.Join(placeholders, ", ") + `)
			AND [[revoked_at]] = 0
			AND ([[token_expires_at]] = 0 OR [[token_expires_at]] > {:now})
	`).Bind(params).All(&rows)
	active := make(map[string]string, len(rows))
	for _, row := range rows {
		active[row.ID] = row.TokenHash
	}
	return active, err
}

func (runtime *mcpSSESessionRuntime) cancelMismatched(active map[string]string) {
	cancellations := []context.CancelFunc{}
	runtime.mu.Lock()
	for connectionID, session := range runtime.sessions {
		if credentialHash, ok := active[connectionID]; !ok || credentialHash != session.credentialHash {
			delete(runtime.sessions, connectionID)
			cancellations = append(cancellations, session.cancel)
		}
	}
	runtime.mu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

// Shutdown closes each stream once the calls its session took are answered
// on it, or ctx ends (mcp_sse_session_calls.go); no call is taken any more.
func (runtime *mcpSSESessionRuntime) Shutdown(ctx context.Context) error {
	runtime.mu.Lock()
	runtime.closed = true
	closing := make([]mcpSSESession, 0, len(runtime.sessions))
	for connectionID, session := range runtime.sessions {
		delete(runtime.sessions, connectionID)
		closing = append(closing, session)
	}
	runtime.mu.Unlock()
	for _, session := range closing {
		go func(session mcpSSESession) {
			select {
			case <-session.calls.settled():
			case <-ctx.Done():
			}
			session.cancel()
		}(session)
	}

	done := make(chan struct{})
	go func() {
		runtime.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", errorCodeMCPConnectionFailed, ctx.Err())
	}
}
