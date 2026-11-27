package app

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"sync/atomic"
)

// A request the server makes to its own router (inner_request_target.go)
// arrives where the request it is made for arrived: at the same host, over
// the same scheme (2026-11-23, the gate's lap 9: built bare, it arrived at
// http://example.com, so on a server without a public address every
// address an answer built from it was example.com's, and an agent's
// leafen_call asking for more, or making a connection, was refused as a
// public address over http). An agent's call through MCP carries the address
// its request came in on (requestArrivalOrigin); a person's step carries
// theirs; a call made for no request, such as an app's run, the address the
// server serves at on this computer (setupServedAddress). Every reader of a
// request's address — publicServerOrigin, requestArrivalOrigin,
// answeredAtOrigin, outwardServerOrigin, requestScheme — then reads the
// inner request as the outer one.

type innerRequestArrivalKey struct{}

// innerRequestHome is the server's own address on this computer, as it serves.
var innerRequestHome atomic.Value

// withRequestOrigins carries a request's addresses to the work done for it
// with no request at hand: its public address (requestOriginFromContext) and
// the one it came in on, which an inner request arrives at.
func withRequestOrigins(ctx context.Context, request *http.Request) context.Context {
	ctx = context.WithValue(ctx, requestOriginContextKey{}, requestOrigin(request))
	if arrival, err := requestArrivalOrigin(request); err == nil {
		ctx = withInnerRequestArrival(ctx, arrival)
	}
	return ctx
}

// withInnerRequestArrival has the inner requests made with ctx arrive at origin.
func withInnerRequestArrival(ctx context.Context, origin string) context.Context {
	if origin == "" {
		return ctx
	}
	return context.WithValue(ctx, innerRequestArrivalKey{}, origin)
}

// innerRequestArrivalOf is where an inner request made with ctx arrives:
// its outer request's address, else the server's own.
func innerRequestArrivalOf(ctx context.Context) string {
	if origin, _ := ctx.Value(innerRequestArrivalKey{}).(string); origin != "" {
		return origin
	}
	home, _ := innerRequestHome.Load().(string)
	return home
}

// arriveInnerRequest gives an inner request the host and scheme of origin.
func arriveInnerRequest(request *http.Request, origin string) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return
	}
	// As a server's request has them: the host on Host, the scheme by TLS,
	// the URL only a path (read_batch.go's inner reads do the same).
	request.Host = parsed.Host
	if parsed.Scheme == "https" {
		request.TLS = &tls.ConnectionState{ServerName: parsed.Hostname()}
	} else {
		request.TLS = nil
	}
}
