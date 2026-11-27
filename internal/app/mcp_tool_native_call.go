package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// A Notion tool for something Leafen's screen does through its own API runs
// that API's operation as the connection's person, as leafen_call does: the
// same scope, key rules, checks, saving and history as the screen, and the
// same asking of the person before an action its rules confirm.

// mcpNativeKey is the connection as the key a native operation runs under.
func mcpNativeKey(authority mcpAuthority) userKeyAuthority {
	return userKeyAuthority{
		ConnectionID: authority.ConnectionID, Name: authority.ClientName, OwnerID: authority.PrincipalID,
		Scope: authority.Scope, WorkspaceIDs: authority.WorkspaceIDs, AllWorkspaces: authority.AllWorkspaces, Agent: authority.Agent,
		Created: authority.Created, ExpiresAt: authority.CredentialExpiresAt, MCP: &userKeyMCPCall{Authority: authority},
	}
}

// mcpNativeCall runs one native operation for a tool call, its body sent as
// JSON, and answers with the operation's answer as it is — one that waits
// for the person's approval says what to ask them in the conversation.
func mcpNativeCall(ctx context.Context, method, path, query string, body any) (nativeOperationResult, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nativeOperationResult{}, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nativeOperationResult{}, failedBecause(errMCPToolFailed, err)
	}
	encoded, header := nativeJSONRequestBody(encoded)
	result, err := callNativeOperationWith(ctx, mcpNativeKey(authority), method, path, query, encoded, header, "")
	if err != nil {
		return nativeOperationResult{}, failedBecause(errMCPToolFailed, err)
	}
	return result, nil
}

// mcpNativeRefusal is a native operation's refusal as the tool's, in the
// operation's own words; nil when it went through.
func mcpNativeRefusal(result nativeOperationResult) error {
	return mcpNativeRefusalNamed(result, nil)
}

// mcpNativeRefusalNamed is mcpNativeRefusal for a tool whose inputs carry
// the operation's fields: each it names is said by the tool's input
// (mcp_tool_field_names.go).
func mcpNativeRefusalNamed(result nativeOperationResult, names mcpToolFieldNames) error {
	if result.Status >= 200 && result.Status < 300 {
		return nil
	}
	message := strings.TrimSpace(result.Text)
	encoded, _ := json.Marshal(result.Body)
	answer := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	if json.Unmarshal(encoded, &answer) == nil && strings.TrimSpace(answer.Error.Message) != "" {
		message = strings.TrimSpace(answer.Error.Message)
	}
	message = names.words(message)
	switch {
	case answer.Error.Code == errorCodeServerBusy, answer.Error.Code == errorCodeMemoryPressure,
		answer.Error.Code == errorCodeMemorySensorUnavailable:
		// The route was told busy (api_error_busy.go), or the server's memory
		// had the call wait before any of it ran (memory_pressure.go): so is
		// the agent, to call again shortly, as the app asks again.
		return errMCPServerBusy
	case result.Status >= 500:
		return errMCPToolFailed
	case message == "":
		return errMCPToolInvalid
	case result.Status == http.StatusUnauthorized || result.Status == http.StatusForbidden || result.Status == http.StatusNotFound:
		return mcpDenied("%s", message)
	}
	return mcpInvalid("%s", message)
}
