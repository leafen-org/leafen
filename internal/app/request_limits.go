package app

import (
	"errors"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"github.com/pocketbase/pocketbase/tools/router"
)

const (
	requestLimitProfileEnvironment = "LEAFEN_RATE_LIMIT_PROFILE"
	requestLimitWindowSeconds      = 60
	requestLimitDevelopmentMax     = 10_000_000

	requestLimitGlobal            = 1_800
	requestLimitFunctions         = 1_200
	requestLimitStorage           = 300
	requestLimitAuth              = 120
	requestLimitSignIn            = 30
	requestLimitSignUp            = 12
	requestLimitEvents            = 2_400
	requestLimitMCPRegistration   = 10
	errorCodeRateLimited          = "LEAFEN_RATE_LIMITED"
	requestLimitResponseHandlerID = "leafenRequestLimitResponse"
	requestLimitHandlerID         = "leafenRequestLimits"

	requestLimitWeightHeavy   = 2
	requestLimitWeightOutside = 5
	requestLimitWeightJob     = 30
)

// A server moving here sends a snapshot of many files, and only with the
// receiving place's secret (server_move.go).
const requestLimitServerMove = 60_000

// requestLimitRetryAfter is the longest wait a limit gives: its window, which
// is also Notion's longest wait for a connection. A window over its limit
// says the wait it has left.
const requestLimitRetryAfter = "60"

type requestLimitDefinition struct {
	label string
	max   int
	// weighted limits count a heavy request as more than one; sign-in,
	// sign-up and the other narrow limits count each request once.
	weighted bool
}

var productionRequestLimits = []requestLimitDefinition{
	{label: "POST /api/leafen/v1/auth/register", max: requestLimitSignUp},
	{label: "POST /api/leafen/v1/auth/login", max: requestLimitSignIn},
	// Finding a company's sign-in by an email's domain is tried as sign-in is.
	{label: "POST /api/leafen/v1/auth/saml/find", max: requestLimitSignIn},
	{label: "/api/leafen/v1/auth/", max: requestLimitAuth},
	{label: "/api/leafen/v1/files/", max: requestLimitStorage},
	{label: "/api/leafen/v1/collaboration/", max: requestLimitEvents},
	{label: "/api/leafen/v1/agent-runner/", max: requestLimitEvents},
	{label: "/api/leafen/v1/server-move/in/", max: requestLimitServerMove},
	{label: "POST /oauth/register", max: requestLimitMCPRegistration},
	{label: "/oauth/", max: requestLimitAuth},
	{label: "/mcp", max: requestLimitFunctions},
	{label: "/api/leafen/v1/", max: requestLimitFunctions, weighted: true},
	{label: "/", max: requestLimitGlobal, weighted: true},
}

// registerRequestLimits counts requests in Leafen's own windows instead of
// PocketBase's, which count by the whole address string: a signed-in person
// is counted as themself wherever they connect from, so staff behind one
// company address don't share one count and a person changing addresses
// doesn't gain more; everything else, sign-in included, is counted by
// address, an IPv6 address by its /64 (2026-10-18).
func registerRequestLimits(event *core.ServeEvent) {
	development := requestLimitProfileIsDevelopment(os.Getenv(requestLimitProfileEnvironment))
	limits := make([]requestLimit, 0, len(productionRequestLimits))
	for _, definition := range productionRequestLimits {
		maximum := definition.max
		if development {
			maximum = requestLimitDevelopmentMax
		}
		limits = append(limits, requestLimit{
			definition: definition,
			windows:    newRequestWindowLimiter(maximum, requestLimitWindowSeconds*time.Second),
		})
	}

	registerRequestWindows(event.App, development)
	event.App.Settings().RateLimits.Enabled = false
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id:       requestLimitResponseHandlerID,
		Priority: apis.DefaultRateLimitMiddlewarePriority - 10,
		Func:     mapRequestLimitResponse,
	})
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id:       requestLimitHandlerID,
		Priority: apis.DefaultRateLimitMiddlewarePriority,
		Func:     func(request *core.RequestEvent) error { return admitRequestLimits(request, limits) },
	})
}

type requestLimit struct {
	definition requestLimitDefinition
	windows    *requestWindowLimiter
}

// admitRequestLimits counts a request, by its weight, against the first
// limit whose label it matches, as PocketBase matched them; the server's own
// reading isn't counted (request_limits_inner.go).
func admitRequestLimits(event *core.RequestEvent, limits []requestLimit) error {
	if event.HasSuperuserAuth() || !requestLimitsCount(event.Request) {
		return event.Next()
	}
	// A build's own files never change and cost little to answer: counted, a
	// browser opening many tabs for the first time refuses itself its screens
	// (2026-11-05: a sign-in landed on the error card over a 429 for a module).
	if path := event.Request.URL.Path; strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/fonts/") {
		return event.Next()
	}
	for _, limit := range limits {
		if !requestLimitMatches(limit.definition.label, event.Request) {
			continue
		}
		key := limit.definition.label + "\x00" + requestLimitHolder(event)
		weight := 1
		if limit.definition.weighted {
			weight = requestLimitWeight(event.Request)
		}
		allowed, wait := limit.windows.admitN(key, weight, time.Now())
		if !allowed {
			event.Response.Header().Set("Retry-After", strconv.Itoa(requestWindowRetryAfter(wait)))
			return router.NewTooManyRequestsError("", nil)
		}
		break
	}
	return event.Next()
}

func requestLimitMatches(label string, request *http.Request) bool {
	method, path, found := strings.Cut(label, " ")
	if !found {
		method, path = "", label
	}
	return (method == "" || method == request.Method) && strings.HasPrefix(request.URL.Path, path)
}

// requestLimitHolder is whom a request counts against: the signed-in person,
// or else the address, an IPv6 address by its /64, which one line can
// change at will.
func requestLimitHolder(event *core.RequestEvent) string {
	if user, ok := authenticatedUser(event); ok {
		return "person:" + user.Id
	}
	return "address:" + requestLimitAddress(event.RealIP())
}

func requestLimitAddress(raw string) string {
	address, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	address = address.Unmap()
	if address.Is4() {
		return address.String()
	}
	prefix, err := address.Prefix(64)
	if err != nil {
		return address.String()
	}
	return prefix.String()
}

// requestLimitWeight is what a request costs of its window. A heavy request
// costs more: reading outside addresses for a link or a cover image search,
// which spends the server's own quota there, starting an export or an
// import, a search, and a write. A person working normally stays well
// inside 1,200 a minute even so: search as they type is a few requests a
// second, typing itself travels on the page's live connection, and a page
// with ten linked PDFs costs 50.
func requestLimitWeight(request *http.Request) int {
	path := request.URL.Path
	switch {
	case strings.HasPrefix(path, apiRoot+urlMetadataPath), strings.HasPrefix(path, apiRoot+coverImageSearchPath):
		return requestLimitWeightOutside
	case request.Method == http.MethodPost && (strings.HasSuffix(path, "/exports") || strings.HasSuffix(path, "/imports") ||
		strings.HasSuffix(path, "/product-exports") || strings.HasSuffix(path, "/audit-exports") ||
		strings.HasSuffix(path, "/discovery-exports")):
		return requestLimitWeightJob
	case strings.Contains(path, "/search"):
		return requestLimitWeightHeavy
	case request.Method != http.MethodGet && request.Method != http.MethodHead && request.Method != http.MethodOptions:
		return requestLimitWeightHeavy
	}
	return 1
}

func requestLimitProfileIsDevelopment(profile string) bool {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "development", "test":
		return true
	default:
		return false
	}
}

func mapRequestLimitResponse(event *core.RequestEvent) error {
	err := event.Next()
	var apiErr *router.ApiError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		return err
	}

	if event.Response.Header().Get("Retry-After") == "" {
		event.Response.Header().Set("Retry-After", requestLimitRetryAfter)
	}
	retryAfter := event.Response.Header().Get("Retry-After")
	path := event.Request.URL.Path
	under := func(root string) bool { return path == root || strings.HasPrefix(path, root+"/") }
	switch {
	case under(notionAdminAPIRoot):
		return notionAdminAPIError(
			event,
			http.StatusTooManyRequests,
			notionAdminErrorRateLimited,
			notionAdminRateLimitedMessage,
			err,
		)
	case under(notionAPIRoot):
		return notionRateLimitError(event, "public_api_request_rate_limit", retryAfter, err)
	case under(mcpRoutePath), under(mcpSSERoutePath):
		return notionRateLimitError(event, "mcp_tool_rate_limit", retryAfter, err)
	}
	return apiErrorWithCause(
		event,
		http.StatusTooManyRequests,
		errorCodeRateLimited,
		"Too many requests. Try again shortly.",
		diagnosticOperation(event),
		"admission",
		"POCKETBASE_RATE_LIMIT",
		err,
	)
}

// notionRateLimitError answers a Public API or MCP request over its limit as
// Notion does: rate_limited, with the limit's reason and the wait in whole
// seconds, as Retry-After gives it, in additional_data.
func notionRateLimitError(event *core.RequestEvent, reason, retryAfter string, cause error) error {
	return notionCompatibilityErrorWithData(
		event, http.StatusTooManyRequests, "rate_limited", "Too many requests for now; try again in a little while.",
		map[string]any{"rate_limit_reason": reason, "retry_after": retryAfter}, cause,
	)
}
