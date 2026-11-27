package app

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// Notion counts Public API requests per connection and MCP tool calls per
// person, in fixed windows, with the numbers of its Business and Enterprise
// plans: 600 requests a minute for a connection, 600 tool calls a minute for
// a person, and 20 calls in 10 seconds for each search and data source query
// tool. An address's own limit (request_limits.go) still guards the server.
const (
	requestWindowStoreKey          = "leafenRequestWindows"
	requestWindowConnectionMax     = 600
	requestWindowPersonToolMax     = 600
	requestWindowSearchToolMax     = 20
	requestWindowMinute            = time.Minute
	requestWindowSearchToolSpan    = 10 * time.Second
	requestWindowRetryAtOnceWithin = 2 * time.Second
)

// requestWindowSearchTools are the tools Notion limits on their own, and
// Leafen's native search beside them.
var requestWindowSearchTools = map[string]bool{
	"notion-search": true, "notion-query-data-sources": true, "leafen_search": true,
}

type requestWindow struct {
	start time.Time
	count int
}

// requestWindowLimiter counts requests by key in fixed windows. A window
// starts at its key's first request and holds limit requests.
type requestWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	span    time.Duration
	windows map[string]*requestWindow
	swept   time.Time
}

func newRequestWindowLimiter(limit int, span time.Duration) *requestWindowLimiter {
	return &requestWindowLimiter{limit: limit, span: span, windows: map[string]*requestWindow{}}
}

// admit counts one request for key at now, or says how long until the key's
// window takes another.
func (limiter *requestWindowLimiter) admit(key string, now time.Time) (bool, time.Duration) {
	return limiter.admitN(key, 1, now)
}

// admitN counts a request of weight n for key at now.
func (limiter *requestWindowLimiter) admitN(key string, n int, now time.Time) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	// Ended windows go at most once a span, so the map holds only the keys
	// seen in the last span or two.
	if now.Sub(limiter.swept) >= limiter.span {
		for stale, window := range limiter.windows {
			if now.Sub(window.start) >= limiter.span {
				delete(limiter.windows, stale)
			}
		}
		limiter.swept = now
	}
	window := limiter.windows[key]
	if window == nil || now.Sub(window.start) >= limiter.span {
		window = &requestWindow{start: now}
		limiter.windows[key] = window
	}
	if window.count+n > limiter.limit {
		return false, window.start.Add(limiter.span).Sub(now)
	}
	window.count += n
	return true, 0
}

// refund gives back a request counted for key that was refused elsewhere.
func (limiter *requestWindowLimiter) refund(key string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if window := limiter.windows[key]; window != nil && window.count > 0 {
		window.count--
	}
}

type requestWindows struct {
	connections *requestWindowLimiter
	personTools *requestWindowLimiter
	searchTools *requestWindowLimiter
}

// registerRequestWindows keeps the windows with the serving app; a
// development or test profile lifts their numbers as it lifts the address
// limits.
func registerRequestWindows(app core.App, development bool) {
	connections, personTools, searchTools := requestWindowConnectionMax, requestWindowPersonToolMax, requestWindowSearchToolMax
	if development {
		connections, personTools, searchTools = requestLimitDevelopmentMax, requestLimitDevelopmentMax, requestLimitDevelopmentMax
	}
	app.Store().Set(requestWindowStoreKey, &requestWindows{
		connections: newRequestWindowLimiter(connections, requestWindowMinute),
		personTools: newRequestWindowLimiter(personTools, requestWindowMinute),
		searchTools: newRequestWindowLimiter(searchTools, requestWindowSearchToolSpan),
	})
}

func requestWindowsFor(app core.App) *requestWindows {
	windows, _ := app.Store().Get(requestWindowStoreKey).(*requestWindows)
	return windows
}

// requestWindowRetryAfter is a wait in whole seconds, as Retry-After gives it.
func requestWindowRetryAfter(wait time.Duration) int {
	return max(1, int(math.Ceil(wait.Seconds())))
}

// admitNotionConnectionRequest counts a Public API request against its
// connection's window. A request over it is answered as Notion answers one:
// 429 with the wait left. A request without a connection goes on to be
// refused as before; the server's own reading isn't counted
// (request_limits_inner.go).
func admitNotionConnectionRequest(event *core.RequestEvent) (bool, error) {
	windows := requestWindowsFor(event.App)
	if windows == nil || !requestLimitsCount(event.Request) {
		return false, nil
	}
	identity, ok, err := notionPublicAPIBearerFromRequest(event)
	if err != nil || !ok {
		return false, nil
	}
	allowed, wait := windows.connections.admit(notionAsyncConnectionID(identity), time.Now())
	if allowed {
		return false, nil
	}
	retryAfter := strconv.Itoa(requestWindowRetryAfter(wait))
	event.Response.Header().Set("Retry-After", retryAfter)
	return true, notionRateLimitError(event, "public_api_request_rate_limit", retryAfter, nil)
}

// mcpToolRequestWindows counts MCP tool calls against their person's windows,
// as Notion's MCP does: a wait of two seconds or less is waited out and the
// call tried once more; a longer one is the call's tool error at once, with
// its code, wait and reason.
func mcpToolRequestWindows(app core.App) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			call, isCall := request.(*mcp.CallToolRequest)
			windows := requestWindowsFor(app)
			if method != "tools/call" || !isCall || call.Params == nil || windows == nil {
				return next(ctx, method, request)
			}
			authority, err := mcpAuthorityFromContext(ctx)
			if err != nil || authority.PrincipalID == "" {
				return next(ctx, method, request)
			}
			for attempt := 0; ; attempt++ {
				reason, wait := windows.admitTool(authority.PrincipalID, call.Params.Name, time.Now())
				if reason == "" {
					return next(ctx, method, request)
				}
				if attempt == 0 && wait <= requestWindowRetryAtOnceWithin {
					timer := time.NewTimer(wait)
					select {
					case <-ctx.Done():
						timer.Stop()
						return nil, ctx.Err()
					case <-timer.C:
					}
					continue
				}
				return mcpRateLimitedResult(reason, wait), nil
			}
		}
	}
}

// admitTool counts a tool call against its tool's own window, when it has
// one, and its person's; it names the limit a refused call hit.
func (windows *requestWindows) admitTool(personID, tool string, now time.Time) (string, time.Duration) {
	searchKey := personID + "\x00" + tool
	searched := requestWindowSearchTools[tool]
	if searched {
		if allowed, wait := windows.searchTools.admit(searchKey, now); !allowed {
			return "public_api_endpoint_rate_limit", wait
		}
	}
	if allowed, wait := windows.personTools.admit(personID, now); !allowed {
		if searched {
			windows.searchTools.refund(searchKey)
		}
		return "public_api_request_rate_limit", wait
	}
	return "", 0
}

// mcpRateLimitedResult is a tool call over its limit as Notion's MCP gives
// it: a tool error whose structured content and text carry the error.
func mcpRateLimitedResult(reason string, wait time.Duration) *mcp.CallToolResult {
	detail := map[string]any{
		"code": "rate_limited", "message": "Too many requests for now; try again in a little while.",
		"retry_after_seconds": requestWindowRetryAfter(wait), "rate_limit_reason": reason,
	}
	structured := map[string]any{"error": detail}
	body, _ := json.Marshal(structured)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}, IsError: true, StructuredContent: structured,
	}
}
