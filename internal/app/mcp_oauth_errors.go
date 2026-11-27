package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// oauthBusy is RFC 6749's error for a server that can't take the request
// for now.
const oauthBusy = "temporarily_unavailable"

func oauthProtocolError(event *core.RequestEvent, status int, code string, description string) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(status, map[string]string{
		"error": code, "error_description": description,
	})
}

// oauthServerError answers an OAuth request that failed on the server's
// side as server_error, through the rules of every failure's answer
// (answeredFailure): logged with its cause, and, when the database stayed
// busy, temporarily_unavailable with the shared busy words, 503 with
// Retry-After, so the client asks again.
func oauthServerError(event *core.RequestEvent, description string, cause error) error {
	status, code, said := answeredFailure(event, http.StatusInternalServerError, errorCodeMCPOAuthFailed, description, cause,
		diagnosticSourcePosition(1))
	if answeredBusy(code) {
		return oauthProtocolError(event, status, oauthBusy, said)
	}
	return oauthProtocolError(event, status, "server_error", description)
}

func oauthProtocolErrorWithProductCode(
	event *core.RequestEvent,
	status int,
	protocolCode string,
	productCode string,
	description string,
) error {
	return writeOAuthProtocolError(event, status, protocolCode, productCode, description, nil, diagnosticSourcePosition(1))
}

// oauthProtocolErrorFrom is oauthProtocolErrorWithProductCode carrying what
// failed, so a database that stayed busy is told busy.
func oauthProtocolErrorFrom(
	event *core.RequestEvent, status int, protocolCode, productCode, description string, cause error,
) error {
	return writeOAuthProtocolError(event, status, protocolCode, productCode, description, cause, diagnosticSourcePosition(1))
}

// writeOAuthProtocolError answers an OAuth error with Leafen's code and
// reference beside the protocol's, through the rules of every failure's
// answer (failureAnswer): told busy, the protocol's error is
// temporarily_unavailable and Leafen's the shared busy code.
func writeOAuthProtocolError(
	event *core.RequestEvent, status int, protocolCode, productCode, description string, cause error, sourcePosition string,
) error {
	reference := newDiagnosticReference()
	answered, answeredCode, said := failureAnswer(event, status, productCode, description, cause)
	if answeredBusy(answeredCode) {
		protocolCode = oauthBusy
	}
	logAPIDiagnostic(
		event, answered, answeredCode, reference, diagnosticOperation(event),
		"oauth-protocol", productCode, cause, sourcePosition,
	)
	event.Response.Header().Set("Cache-Control", "no-store")
	event.Response.Header().Set("Pragma", "no-cache")
	return event.JSON(answered, map[string]string{
		"error": protocolCode, "error_description": said,
		"error_code": answeredCode, "diagnostic_reference": reference,
	})
}
