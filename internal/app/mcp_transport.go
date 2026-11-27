package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The hosted MCP server answers at mcpRoutePath, and over server-sent
// events at mcpSSERoutePath.
const (
	mcpRoutePath    = "/mcp"
	mcpSSERoutePath = "/sse"
)

const (
	errorCodeMCPUnauthorized           = "LEAFEN_MCP_UNAUTHORIZED"
	errorCodeMCPOriginDenied           = "LEAFEN_MCP_ORIGIN_DENIED"
	errorCodeMCPProtocolParse          = "LEAFEN_MCP_PROTOCOL_PARSE_ERROR"
	errorCodeMCPProtocolInvalidRequest = "LEAFEN_MCP_PROTOCOL_INVALID_REQUEST"
	errorCodeMCPProtocolMethodMissing  = "LEAFEN_MCP_PROTOCOL_METHOD_NOT_FOUND"
	errorCodeMCPProtocolInvalidParams  = "LEAFEN_MCP_PROTOCOL_INVALID_PARAMS"
	errorCodeMCPProtocolInternal       = "LEAFEN_MCP_PROTOCOL_INTERNAL_ERROR"
	errorCodeMCPProtocolVersion        = "LEAFEN_MCP_PROTOCOL_VERSION_MISMATCH"

	mcpProtocolCaptureLimit = 64 << 10
	nativeMCPPath           = "/api/leafen/mcp"
)

const (
	mcpJSONRPCParseError     = -32700
	mcpJSONRPCInvalidRequest = -32600
	mcpJSONRPCMethodMissing  = -32601
	mcpJSONRPCInvalidParams  = -32602
	mcpJSONRPCInternalError  = -32603
	mcpJSONRPCVersionError   = -32022
)

var errMCPProtocolRejected = errors.New("MCP SDK rejected the protocol request")

type mcpAuthorityContextKey struct{}

func registerMCPRoutes(event *core.ServeEvent, sseSessions *mcpSSESessionRuntime) {
	credentialCompletion := &mcpCredentialCompletionRuntime{}
	oauthRuntime := &mcpOAuthRuntime{credentialCompletion: credentialCompletion}
	event.Router.Route(mcpConnectionCreateMethod, apiRoot+mcpConnectionCreatePath, func(request *core.RequestEvent) error {
		return createMCPConnection(request, credentialCompletion)
	})
	event.Router.Route(mcpConnectionListMethod, apiRoot+mcpConnectionListPath, readMCPConnectionList)
	event.Router.Route(mcpConnectionRevokeMethod, apiRoot+mcpConnectionRevokePath, revokeMCPConnection)
	registerMCPTokenSettingsRoutes(event)
	event.Router.Route(mcpConnectionCodeCreateMethod, apiRoot+mcpConnectionCodeCreatePath, createMCPConnectionCode)
	event.Router.Route(mcpConnectionCodeExchangeMethod, apiRoot+mcpConnectionCodeExchangePath, exchangeMCPConnectionCode).
		Bind(apis.BodyLimit(maxMCPOAuthRequestBytes))
	registerMCPDeviceConnectionRoutes(event)
	event.Router.Route(mcpOAuthReadConsentMethod, apiRoot+mcpOAuthReadConsentPath, readMCPOAuthConsent)
	event.Router.Route(mcpOAuthApproveConsentMethod, apiRoot+mcpOAuthApproveConsentPath, approveMCPOAuthConsent)
	event.Router.Route(mcpOAuthDenyConsentMethod, apiRoot+mcpOAuthDenyConsentPath, denyMCPOAuthConsent)
	event.Router.GET("/.well-known/oauth-protected-resource/mcp", serveMCPProtectedResourceMetadata)
	event.Router.OPTIONS("/.well-known/oauth-protected-resource/mcp", serveMCPProtectedResourceMetadata)
	event.Router.GET(mcpProtectedResourceMetadataPath+nativeMCPPath, serveMCPProtectedResourceMetadata)
	event.Router.OPTIONS(mcpProtectedResourceMetadataPath+nativeMCPPath, serveMCPProtectedResourceMetadata)
	event.Router.GET("/.well-known/oauth-authorization-server", serveMCPAuthorizationServerMetadata)
	event.Router.OPTIONS("/.well-known/oauth-authorization-server", serveMCPAuthorizationServerMetadata)
	event.Router.POST("/oauth/register", registerMCPOAuthClient).Bind(apis.BodyLimit(64 << 10))
	event.Router.GET("/oauth/authorize", beginMCPOAuthAuthorization)
	event.Router.POST("/oauth/token", func(request *core.RequestEvent) error {
		return exchangeMCPOAuthToken(request, oauthRuntime)
	}).Bind(apis.BodyLimit(64 << 10))
	event.Router.POST("/oauth/revoke", revokeMCPOAuthToken).Bind(apis.BodyLimit(64 << 10))
	registerMCPFileUploadRoutes(event)
	strictHTTP := newMCPHTTPHandler(event.App, mcpToolSurfaceStrict)
	strictSSE := newMCPSSEHandler(event.App, mcpToolSurfaceStrict, sseSessions)
	nativeHTTP := newMCPHTTPHandler(event.App, mcpToolSurfaceNative)
	registerMCPRoute(event, mcpRoutePath, strictHTTP, true)
	registerMCPRoute(event, mcpSSERoutePath, strictSSE, false)
	registerMCPRoute(event, nativeMCPPath, nativeHTTP, true)
}

func registerMCPRoute(event *core.ServeEvent, path string, handler http.Handler, allowDelete bool) {
	serve := func(request *core.RequestEvent) error {
		// Served over the request's own app (mcp_request_app.go).
		return serveMCPRequest(request, handler)
	}
	event.Router.POST(path, serve)
	event.Router.GET(path, serve)
	if allowDelete {
		event.Router.DELETE(path, serve)
	}
}

func newMCPServer(app core.App, surface mcpToolSurface) *mcp.Server {
	options := &mcp.ServerOptions{SchemaCache: mcpSchemaCache}
	if surface == mcpToolSurfaceNative {
		options.Instructions = mcpNativeInstructionsIn(accountLanguageEn)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "leafen", Version: "2"}, options)
	// Middleware added later wraps what came before: the tools' own come
	// first, so the request windows and the scope guard hold every call.
	registerMCPTools(server, app, surface)
	// Each answer's keys in its type's order, as Notion's keep theirs, and its text unescaped (mcp_answer_order.go);
	// innermost, a tool's arguments its schema refuses said with those it takes (mcp_tool_argument_refusals.go).
	server.AddReceivingMiddleware(mcpAnswersInOrder(), mcpToolArgumentRefusals())
	// One argument, one name: a tool that breaks it stops the server as it
	// starts, a call under another name is read by it, and the connection's
	// workspace given to a tool that takes none is taken (mcp_tool_arguments.go).
	server.AddReceivingMiddleware(mcpSharedArguments(checkedMCPToolArguments(server, surface)))
	// A refusal that came bare gets its kind's reason (mcp_tool_errors.go).
	server.AddReceivingMiddleware(mcpToolReasons(app))
	server.AddReceivingMiddleware(mcpToolRequestWindows(app), mcpToolScopeGuard(app))
	// The native endpoint's instructions also say which server this is and
	// the outside services it reaches (mcp_session_instructions.go).
	if surface == mcpToolSurfaceNative {
		server.AddReceivingMiddleware(mcpPersonalInstructions(app))
	}
	// Every tool listed, apps' too, in schemas every model's engine takes
	// (mcp_tool_schemas.go); what a client may cache, cached as its own (mcp_answer_cache_scope.go).
	server.AddReceivingMiddleware(mcpPlainToolSchemas(), mcpAnswersCachedPrivately())
	// The tools Leafen doesn't have come last (mcp_tool_unsupported.go).
	server.AddReceivingMiddleware(mcpUnsupportedToolsLast())
	// leafen_call runs a tool by name through the checks above
	// (mcp_named_tools.go); the list shows the core tools (mcp_core_tools.go).
	if surface == mcpToolSurfaceNative {
		server.AddReceivingMiddleware(mcpCoreToolList(app), mcpNamedTools(app))
		// An answer, leafen_call's of a tool too, carries its key's end as it nears (mcp_token_expiry_notice.go), never the person's notices.
		server.AddReceivingMiddleware(mcpKeyEndInResponses(app))
	}
	// Outermost: a call that panics ends with an error, not the server (panic_guard.go); within it, each answer, leafen_call's
	// of a tool too, names the workspaces it worked in (mcp_answer_workspaces.go), inside the answer (mcp_answer_notes.go).
	server.AddReceivingMiddleware(mcpPanicGuard(app), mcpAnswerWorkspaces())
	return server
}

// newMCPHTTPHandler serves a call that may write with a kept server serving
// its request's app (mcpServerForRequest), the rest with the one made here.
func newMCPHTTPHandler(app core.App, surface mcpToolSurface) http.Handler {
	server := newMCPServer(app, surface)
	if surface == mcpToolSurfaceNative {
		mcpAppListServers.Store(server)
	}
	writes := newMCPWriteServers(app, surface, server)
	streamable := mcp.NewStreamableHTTPHandler(
		func(request *http.Request) *mcp.Server {
			return mcpServerForRequest(request, server, writes)
		},
		// The SDK's DNS rebinding guard is kept in newMCPAuthenticatedHandler,
		// which also knows the server's own public address.
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true},
	)
	return newMCPAuthenticatedHandler(app, mcpEventStreamsKeptAlive(streamable))
}

func newMCPSSEHandler(
	app core.App,
	surface mcpToolSurface,
	sessions *mcpSSESessionRuntime,
) http.Handler {
	return newMCPAuthenticatedHandler(app, mcpEventStreamsKeptAlive(sessions.handler(app, surface)))
}

func newMCPAuthenticatedHandler(app core.App, next http.Handler) http.Handler {
	protocol := newMCPProtocolDiagnosticHandler(app, next)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !mcpHostAllowed(request) {
			http.Error(response, "Forbidden: invalid Host header", http.StatusForbidden)
			return
		}
		if !sameOriginOrNoOrigin(request) {
			writeMCPHTTPError(response, http.StatusForbidden, errorCodeMCPOriginDenied)
			return
		}
		authority, ok, err := authenticateMCPRequest(app, request)
		if err != nil {
			code := errorCodeMCPConnectionFailed
			if errors.Is(err, errMCPGovernanceUnavailable) || errors.Is(err, errMCPGovernanceInvalid) {
				code = errorCodeMCPGovernanceUnavailable
			}
			writeMCPHTTPDiagnosticError(
				app, response, http.StatusInternalServerError, code,
				"request-authentication", err,
			)
			return
		}
		if !ok {
			if metadataURL, metadataErr := mcpProtectedResourceMetadataURL(request); metadataErr == nil {
				response.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+metadataURL+`", scope="mcp"`)
			}
			writeMCPHTTPError(response, http.StatusUnauthorized, errorCodeMCPUnauthorized)
			return
		}
		ctx := context.WithValue(request.Context(), mcpAuthorityContextKey{}, authority)
		ctx = withRequestOrigins(ctx, request)
		// What one call writes is one moment in its pages' histories.
		ctx = withPageHistoryCallMoment(ctx)
		protocol.ServeHTTP(response, request.WithContext(ctx))
	})
}

type mcpProtocolRequestCapture struct {
	io.ReadCloser
	body     bytes.Buffer
	overflow bool
}

func (capture *mcpProtocolRequestCapture) Read(target []byte) (int, error) {
	count, err := capture.ReadCloser.Read(target)
	remaining := mcpProtocolCaptureLimit - capture.body.Len()
	if remaining > 0 {
		copied := min(count, remaining)
		_, _ = capture.body.Write(target[:copied])
		if copied < count {
			capture.overflow = true
		}
	} else if count > 0 {
		capture.overflow = true
	}
	return count, err
}

type mcpProtocolDiagnosticWriter struct {
	target    http.ResponseWriter
	status    int
	forwarded bool
	errorBody bytes.Buffer
	overflow  bool
}

func (writer *mcpProtocolDiagnosticWriter) Header() http.Header {
	return writer.target.Header()
}

func (writer *mcpProtocolDiagnosticWriter) WriteHeader(status int) {
	if writer.forwarded || writer.status != 0 {
		return
	}
	writer.status = status
	if status < http.StatusBadRequest {
		writer.forwarded = true
		writer.target.WriteHeader(status)
	}
}

func (writer *mcpProtocolDiagnosticWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if writer.forwarded {
		return writer.target.Write(body)
	}
	remaining := mcpProtocolCaptureLimit - writer.errorBody.Len()
	if remaining > 0 {
		copied := min(len(body), remaining)
		_, _ = writer.errorBody.Write(body[:copied])
		if copied < len(body) {
			writer.overflow = true
		}
	} else if len(body) > 0 {
		writer.overflow = true
	}
	return len(body), nil
}

func (writer *mcpProtocolDiagnosticWriter) Flush() {
	if writer.status >= http.StatusBadRequest {
		return
	}
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if flusher, ok := writer.target.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (writer *mcpProtocolDiagnosticWriter) Unwrap() http.ResponseWriter {
	return writer.target
}

type mcpProtocolRequestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
}

type mcpProtocolErrorEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error"`
}

type mcpProtocolFailure struct {
	code    int
	message string
	id      json.RawMessage
}

func newMCPProtocolDiagnosticHandler(app core.App, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		capture := &mcpProtocolRequestCapture{ReadCloser: request.Body}
		if request.Body != nil {
			request.Body = capture
		}
		writer := &mcpProtocolDiagnosticWriter{target: response}
		next.ServeHTTP(writer, request)
		writer.finish(app, request, capture)
	})
}

func (writer *mcpProtocolDiagnosticWriter) finish(app core.App, request *http.Request, capture *mcpProtocolRequestCapture) {
	if writer.forwarded || writer.status < http.StatusBadRequest {
		return
	}
	failure := classifyMCPProtocolFailure(writer, capture)
	code := mcpProtocolDiagnosticCode(failure.code)
	reference := newDiagnosticReference()
	// The standalone stream's 405 isn't a failure (mcp_standalone_get.go).
	logMCPProtocolResponse(app, request, writer.status, code, reference)
	header := writer.target.Header()
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "no-store")
	header.Del("Content-Length")
	writer.target.WriteHeader(writer.status)
	var id any
	if len(failure.id) > 0 {
		_ = json.Unmarshal(failure.id, &id)
	}
	_ = json.NewEncoder(writer.target).Encode(map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    failure.code,
			"message": failure.message,
			"data":    map[string]string{"reference": reference},
		},
		"id": id,
	})
}

func classifyMCPProtocolFailure(
	writer *mcpProtocolDiagnosticWriter,
	capture *mcpProtocolRequestCapture,
) mcpProtocolFailure {
	if !writer.overflow {
		var existing mcpProtocolErrorEnvelope
		if json.Unmarshal(writer.errorBody.Bytes(), &existing) == nil && existing.Error != nil {
			return mcpProtocolFailure{
				code: existing.Error.Code, message: mcpProtocolMessage(existing.Error.Code), id: existing.ID,
			}
		}
	}
	if capture == nil || capture.overflow {
		return mcpProtocolFailure{code: mcpJSONRPCInvalidRequest, message: "Invalid Request"}
	}
	if capture.body.Len() == 0 {
		return mcpProtocolFailure{code: mcpJSONRPCInvalidRequest, message: "Invalid Request"}
	}
	var request mcpProtocolRequestEnvelope
	if json.Unmarshal(capture.body.Bytes(), &request) != nil {
		return mcpProtocolFailure{code: mcpJSONRPCParseError, message: "Parse error"}
	}
	if request.Method != "" && !isMCPProtocolRequestMethod(request.Method) {
		return mcpProtocolFailure{
			code: mcpJSONRPCMethodMissing, message: "Method not found", id: request.ID,
		}
	}
	return mcpProtocolFailure{
		code: mcpJSONRPCInvalidRequest, message: "Invalid Request", id: request.ID,
	}
}

func isMCPProtocolRequestMethod(method string) bool {
	// The pinned SDK does not expose its pre-dispatch method registry or typed
	// failures for legacy protocol versions. This mirror classifies only the
	// SDK's already-rejected response; the SDK remains the dispatch authority.
	// Recheck it whenever the pinned MCP SDK or Leafen custom methods change.
	switch method {
	case "completion/complete", "initialize", "logging/setLevel", "notifications/cancelled",
		"notifications/initialized", "notifications/progress", "notifications/roots/list_changed",
		"ping", "prompts/get", "prompts/list", "resources/list", "resources/read",
		"resources/subscribe", "resources/templates/list", "resources/unsubscribe",
		"server/discover", "subscriptions/listen", "tools/call", "tools/list":
		return true
	default:
		return false
	}
}

func mcpProtocolMessage(code int) string {
	switch code {
	case mcpJSONRPCParseError:
		return "Parse error"
	case mcpJSONRPCInvalidRequest:
		return "Invalid Request"
	case mcpJSONRPCMethodMissing:
		return "Method not found"
	case mcpJSONRPCInvalidParams:
		return "Invalid params"
	case mcpJSONRPCInternalError:
		return "Internal error"
	case mcpJSONRPCVersionError:
		return "Protocol version mismatch"
	default:
		return "Protocol error"
	}
}

func mcpProtocolDiagnosticCode(code int) string {
	switch code {
	case mcpJSONRPCParseError:
		return errorCodeMCPProtocolParse
	case mcpJSONRPCMethodMissing:
		return errorCodeMCPProtocolMethodMissing
	case mcpJSONRPCInvalidParams:
		return errorCodeMCPProtocolInvalidParams
	case mcpJSONRPCInternalError:
		return errorCodeMCPProtocolInternal
	case mcpJSONRPCVersionError:
		return errorCodeMCPProtocolVersion
	default:
		return errorCodeMCPProtocolInvalidRequest
	}
}

// sameOriginOrNoOrigin keeps other pages from calling Leafen's MCP from a
// person's browser. The packaged client's own page (packaged_client.go) —
// the phone app's agent on that device — comes as the rest of the API takes
// it, with a bearer key and never a cookie.
func sameOriginOrNoOrigin(request *http.Request) bool {
	raw := strings.TrimSpace(request.Header.Get("Origin"))
	if raw == "" || isPackagedClientOrigin(raw) {
		return true
	}
	origin, err := url.Parse(raw)
	if err != nil || origin.Scheme == "" || origin.Host == "" {
		return false
	}
	return strings.EqualFold(origin.Host, request.Host)
}

func writeMCPHTTPError(response http.ResponseWriter, status int, code string) {
	writeMCPHTTPErrorWithReference(response, status, code, "")
}

func writeMCPHTTPErrorWithReference(
	response http.ResponseWriter,
	status int,
	code string,
	reference string,
) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	message := code
	if code == errorCodeMCPUnauthorized {
		// What an agent tells the person, whose connection this was.
		message += ": this key or login no longer works. To connect again without the person opening Leafen, POST " + apiRoot +
			mcpDeviceConnectionPath + " on this server with {\"name\": \"<your program's name>\"} and do what it says; or ask them to connect " +
			"you in Leafen: " + mcpScreen("", "connectAgent") + " in the workspace menu, or " + mcpScreen("", "settings", "features", "agents") + "."
	}
	errorBody := map[string]any{"code": -32001, "message": message}
	if reference != "" {
		errorBody["data"] = map[string]string{"reference": reference}
	}
	_ = json.NewEncoder(response).Encode(map[string]any{
		"jsonrpc": "2.0",
		"error":   errorBody,
		"id":      nil,
	})
}

// mcpHostAllowed keeps the MCP SDK's DNS rebinding protection: a request that
// reached this computer's loopback address must name a loopback host — but
// for the server's own public address, the one written or chosen (a running
// tunnel's, the administrator's own), which arrives that way through the
// tunnel or a reverse proxy on this computer.
func mcpHostAllowed(request *http.Request) bool {
	local, ok := request.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil || !mcpLoopbackHost(local.String()) || mcpLoopbackHost(request.Host) {
		return true
	}
	// The chosen address, or an earlier one that forwards here (mcp_earlier_address.go).
	if requestChosenOrigin(request) != "" || EarlierAddressOf(request) != "" {
		return true
	}
	written, err := url.Parse(configuredServerAddress())
	return err == nil && written.Host != "" && strings.EqualFold(written.Host, request.Host)
}
