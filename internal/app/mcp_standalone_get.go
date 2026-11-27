package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A client of MCP 2025-11-25 opens a stream for what the server sends of its
// own with a GET to the endpoint, beside its calls, and the specification
// lets a server that keeps none answer 405 Method Not Allowed — the client
// then goes on without it. Leafen's stateless endpoints keep none, so the
// TypeScript SDK's clients (Cursor and others) got that 405 on every connect,
// and each was logged as a WARN "leafen request failed", hiding the real
// failures (2026-11-23). That answer is the endpoint working as the
// specification says: it is logged at debug level alone, its reference kept
// there. Every other refusal — a 405 to another method, a bad request — is
// logged as a failure, as before.

// logMCPProtocolResponse logs a refused MCP request, as a failure unless it
// is the standalone stream's GET answered as the specification lets it be.
func logMCPProtocolResponse(app core.App, request *http.Request, status int, code, reference string) {
	if mcpStandaloneStreamRefused(request, status) {
		if app != nil {
			app.Logger().Debug("mcp stream not kept: standalone GET answered 405 by the specification",
				"diagnosticReference", reference, "operation", "mcp.http", "status", status)
		}
		return
	}
	logDiagnostic(app, status, code, reference, "mcp.http", "protocol-response", code, errMCPProtocolRejected, diagnosticSourcePosition(2))
}

// mcpStandaloneStreamRefused says whether a refusal is the 405 a GET for the
// server's own stream gets where no stream is kept.
func mcpStandaloneStreamRefused(request *http.Request, status int) bool {
	return request != nil && request.Method == http.MethodGet && status == http.StatusMethodNotAllowed &&
		strings.Contains(strings.ToLower(request.Header.Get("Accept")), "text/event-stream")
}
