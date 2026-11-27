package app

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// Why an agent's request to connect can't be found or allowed
// (mcp_device_connections.go), in one table: each refusal's common cause,
// which Leafen's screen says in its person's language (AgentAllowByCode.tsx;
// 2026-11-23: every refusal there read "Couldn't check the code."), beside
// its code (api_error_because.go), and the words an agent hears.
const (
	mcpDeviceRefusedCodeUnknown          = "codeUnknown"
	mcpDeviceRefusedTooManyWrongCodes    = "tooManyWrongCodes"
	mcpDeviceRefusedNoWorkspace          = "noWorkspace"
	mcpDeviceRefusedDecidedMeanwhile     = "decidedMeanwhile"
	mcpDeviceRefusedOrganizationKeysOff  = "organizationKeysOff"
	mcpDeviceRefusedOrganizationKeyDays  = "organizationKeyDays"
	mcpDeviceRefusedAgentNotApproved     = "agentNotApproved"
	mcpDeviceRefusedConnectionsFull      = "connectionsFull"
	mcpDeviceRefusedHTTPSRequired        = "httpsRequired"
	mcpDeviceRefusedWorkspaceUnreachable = "workspaceUnreachable"
)

// mcpDeviceRefusalOf is why err refuses a request as planned — its cause and
// the words an agent hears — or none for a failure no refusal names.
func mcpDeviceRefusalOf(err error) (string, string) {
	var policyDenial userKeyPolicyError
	switch {
	case errors.As(err, &policyDenial):
		cause := mcpDeviceRefusedOrganizationKeyDays
		if policyDenial.reason == organizationKeysOffDenial {
			cause = mcpDeviceRefusedOrganizationKeysOff
		}
		return cause, "The organization doesn't allow this connection: " + policyDenial.reason
	case errors.Is(err, errMCPClientNotApproved):
		return mcpDeviceRefusedAgentNotApproved, "An administrator of the workspaces' organization has to approve agents connected by hand first."
	case errors.Is(err, errMCPConnectionCapacity):
		return mcpDeviceRefusedConnectionsFull,
			"The account has as many agent connections as it may; its person disconnects one they no longer use in Leafen's settings first."
	case errors.Is(err, errMCPPublicDestinationHTTPSRequired):
		return mcpDeviceRefusedHTTPSRequired, "Agents connect to a public server address only over HTTPS."
	case isPermissionDenied(err):
		return mcpDeviceRefusedWorkspaceUnreachable, "The person no longer reaches one of the workspaces they allowed."
	}
	return "", ""
}

// Every road an agent meets these on parts a refusal, which it tells its
// person, from the server's failure, which it tries again shortly — never a
// failure as a refusal or a wrong input (2026-11-23: a failure in the
// conversation read "LEAFEN_MCP_TOOL_INVALID: Couldn't create the MCP
// connection.", and a poll's "access_denied", which ends it).

// mcpDeviceToolAnswer is err met answering a request in a conversation
// (leafen_connection_requests): a refusal in its words; one the tool words
// itself (a code of its own: a wrong code, a workspace it doesn't reach) as
// it is; any other the server's failure, logged, said to try once more.
func mcpDeviceToolAnswer(err error) error {
	if err == nil {
		return nil
	}
	if _, said := mcpDeviceRefusalOf(err); said != "" {
		return fmt.Errorf("%w: %s", errMCPToolInvalid, said)
	}
	for _, worded := range []error{errMCPToolInvalid, errMCPScopeDenied, errMCPToolFailed, errMCPAuthorityMissing,
		errMCPWorkspaceRequired, errMCPConfirmationRequired} {
		if errors.Is(err, worded) {
			return err
		}
	}
	return failedBecause(errMCPToolFailed, err)
}

// mcpDevicePollRefusal answers an agent's poll for err met making the
// connection allowed: a refusal as access_denied in its words, which ends the
// poll; the server's failure as server_error, logged, which it polls again.
func mcpDevicePollRefusal(event *core.RequestEvent, err error) error {
	if _, said := mcpDeviceRefusalOf(err); said != "" {
		return oauthProtocolError(event, http.StatusBadRequest, "access_denied", said)
	}
	return oauthProtocolErrorFrom(event, http.StatusInternalServerError, "server_error", errorCodeMCPDeviceConnectionFailed,
		"Couldn't create the MCP connection. Poll again in a while.", err)
}

// mcpDeviceScreenRefused answers the screen with a refusal and its cause.
func mcpDeviceScreenRefused(event *core.RequestEvent, status int, cause, said string) error {
	apiErrorBecause(event, "", cause)
	return apiError(event, status, errorCodeMCPDeviceConnectionInvalid, said)
}

// mcpDeviceScreenRefusal answers the screen for err met allowing a request:
// its refusal, or the server's failure.
func mcpDeviceScreenRefusal(event *core.RequestEvent, err error) error {
	if cause, said := mcpDeviceRefusalOf(err); cause != "" {
		return mcpDeviceScreenRefused(event, http.StatusForbidden, cause, said)
	}
	return apiErrorFrom(event, http.StatusInternalServerError, errorCodeMCPDeviceConnectionFailed, "Couldn't check the request to connect.", err)
}
