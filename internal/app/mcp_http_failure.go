package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// writeMCPHTTPDiagnosticError answers an MCP request over HTTP that failed
// before the protocol took it — signing it in, which only reads — logged
// with its cause. One whose database stayed busy is told busy as every
// failure's answer is (busyAnswer): 503 with Retry-After and the shared busy
// code, so the agent's client asks again rather than give up on a 500.
func writeMCPHTTPDiagnosticError(
	app core.App,
	response http.ResponseWriter,
	status int,
	code string,
	stage string,
	cause error,
) {
	reference := newDiagnosticReference()
	answered, answeredCode, _ := busyAnswer(response.Header(), status, code, "", cause)
	logDiagnostic(
		app, answered, answeredCode, reference, "mcp.http", stage, code, cause,
		diagnosticSourcePosition(1),
	)
	writeMCPHTTPErrorWithReference(response, answered, answeredCode, reference)
}
