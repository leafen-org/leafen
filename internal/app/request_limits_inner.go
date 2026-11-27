package app

import (
	"context"
	"net/http"
	"strings"
)

// The request limits — an address's or a person's (request_limits.go) and a
// connection's window (request_window_limits.go) — guard the server against
// its callers. What this process sends its own router (withServerInnerRequest)
// they count by who chose it.
//
// A request a caller chose — an agent's leafen_call, an app's call, a
// person's step, a batch's reads — counts as that caller's own, as if it had
// come alone, so no request from outside goes further for passing through
// the server: the inner mark only lets it past the desktop server's address
// guard (LocalOwner.guard).
//
// What the server reads by itself is no caller's: a migration's scan and run
// reading a bundle another tool made, staged in a workspace of its own
// (notionBundleTransport), at the pace of the migration's own steps. Counted,
// it waited on the limits as an outside caller would (2026-11-25: a scan of
// 300 pages, 612 reads, filled its connection's 600 a minute and took 65 s;
// uncounted, 4–6 s), and it counted as the address 127.0.0.1, the one the
// desktop app's own window comes from (three such scans at once refused that
// window's page 105 times of 135, and a bundle's file refused would be taken
// for one that's gone). Of it, the reads the transport makes — the Notion
// API's paths and the bundle's files — are not counted; a request the staged
// content names elsewhere still is.

// serverOwnReadKey marks a request the server sends its own router for its
// own reading, which no request off the network can carry.
type serverOwnReadKey struct{}

func withServerOwnRead(ctx context.Context) context.Context {
	return context.WithValue(ctx, serverOwnReadKey{}, true)
}

// requestLimitsCount says whether the request limits count a request: every
// one but the server's own reading.
func requestLimitsCount(request *http.Request) bool {
	own, _ := request.Context().Value(serverOwnReadKey{}).(bool)
	if !own || !isServerInnerRequest(request) {
		return true
	}
	path := request.URL.Path
	return path != notionAPIRoot && !strings.HasPrefix(path, notionAPIRoot+"/") &&
		!strings.HasPrefix(path, apiRoot+strings.TrimSuffix(fileTransferDownloadCapabilityPath, "{token}"))
}
