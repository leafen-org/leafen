package app

import (
	"log"
	"maps"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A key made with the read scope reads: what it calls changes no page, row,
// comment, reaction, setting or anyone else's state (2026-11-16: a page's
// reactions resolved through a read key were gone, and the audit log, which
// names a key's changes only, didn't name it). A GET reads by its method. A
// write route a rule gives the read scope keeps it only when it is named
// here with why; any other needs content, and the server names each such
// route as it starts (logReadKeyWritesHeld). This is the one place that
// decides it, for the key's check and for the operations a key lists, both
// of which meet a route's rule through userKeyRuleFor.

// userKeyReadingWrites are the write routes a read key calls, each as its
// rule names it ("METHOD path"): those that read (userKeyWritesThatRead)
// and those that change only what is the person's, the app's or the key's
// own (userKeyOwnWrites).
var userKeyReadingWrites = func() map[string]bool {
	writes := maps.Clone(userKeyWritesThatRead)
	maps.Copy(writes, userKeyOwnWrites)
	return writes
}()

// userKeyWritesThatRead are the write routes a read key calls that read.
var userKeyWritesThatRead = map[string]bool{
	// Reads sent as a write, for their body or their size: queries,
	// searches, previews, reports and counts.
	"POST /data-sources/{dataSourceId}/properties/{propertyId}/access/preview": true,
	"POST /forms/places/search":                        true,
	"POST /page-analytics":                             true,
	"POST /page-past-viewers":                          true,
	"POST /pages/{id}/link-targets":                    true,
	"POST /pages/{id}/copy-mentions":                   true,
	"POST /pages/{id}/text-stored":                     true,
	"POST /pages/{pageId}/database-dependency-graph":   true,
	"POST /pages/{pageId}/database-query":              true,
	"POST /pages/{pageId}/places/search":               true,
	"POST /pages/{pageId}/places/reverse":              true,
	"POST /url-metadata":                               true,
	"POST /url-metadata/pdf":                           true,
	"POST /workspaces/{workspaceId}/change-probe":      true,
	"POST /workspaces/{workspaceId}/file-usage-report": true,
	"POST /workspaces/{workspaceId}/library-pages":     true,
	"POST /workspaces/{workspaceId}/people/lookup":     true,
	"POST /collaboration-state/{room}/lacks":           true,
	"POST /collaboration-state/{room}/missing":         true,
	"POST /offline-page-marks":                         true,
	"POST /deadline-notices/states":                    true,
	// A way to read what the key reads, made for it and ended by it: a
	// file's address, a frame, a view link, an export of pages to a file.
	"POST /files/{id}/capability":                           true,
	"POST /files/{id}/html-frame":                           true,
	"POST /pages/{pageId}/view-links":                       true,
	"DELETE /view-links/{viewLinkId}":                       true,
	"POST /workspaces/{workspaceId}/exports":                true,
	"POST /workspaces/{workspaceId}/exports/{jobId}/cancel": true,
	"POST /workspaces/{workspaceId}/exports/{jobId}/retry":  true,
}

// userKeyOwnWrites are the write routes a read key calls that change only
// what is its person's, its app's or its own.
var userKeyOwnWrites = map[string]bool{
	// What the person's reading leaves on their own history: a page visited
	// and left, a search made, visits taken off their own list.
	"POST /pages/{pageId}/visit":                        true,
	"POST /page-left":                                   true,
	"POST /workspaces/{workspaceId}/search/events":      true,
	"POST /workspaces/{workspaceId}/page-visits/remove": true,
	// An app's own: the route takes only that app's token, whose own grants
	// hold it.
	"POST /apps/{installId}/relay":                   true,
	"POST /apps/{installId}/oauth/{provider}/start":  true,
	"DELETE /apps/{installId}/oauth/{provider}":      true,
	"POST /apps/{installId}/actions/{action}":        true,
	"POST /apps/{installId}/kv":                      true,
	"POST /apps/{installId}/storage":                 true,
	"POST /app-storage":                              true,
	"POST /apps/{installId}/rooms/{room}/ticket":     true,
	"POST /apps/{installId}/guest-links":             true,
	"POST /apps/{installId}/guests/{guestId}/report": true,
	// The key's own token's "모두 맡기기": turning it on asks its person
	// (mcp_token_hand_over.go); turning it off narrows the key.
	"PUT " + mcpHandsAllOverPath: true,
	// The key's own connection's list naming Notion's tools: it reaches
	// nothing the key couldn't call already (mcp_token_notion_tools.go).
	"PUT " + mcpNotionToolsPath: true,
	// Its own connection kept in place of the code's connection
	// (mcp_connection_kept_code.go).
	"POST " + mcpOwnCodePath: true,
	// The key's own connection disconnected: it narrows only itself.
	"DELETE " + mcpOwnConnectionPath: true,
	// The person's own step an agent left them, cancelled by any of their
	// keys as the step's rule has it: it ends only that the step waits —
	// what was asked never ran, and nothing was typed (person_steps.go).
	"DELETE /person-steps/{stepId}": true,
	// A key's request to connect, told it needs no more, and the poll
	// (mcp_device_connection_widen.go).
	"POST " + mcpDeviceConnectionPath:      true,
	"POST " + mcpDeviceConnectionTokenPath: true,
}

// userKeyCodeRoutes take only their own code (userKeyCodeRules): a key adds
// nothing to them, so its scope doesn't either.
var userKeyCodeRoutes = func() map[string]bool {
	routes := map[string]bool{}
	for _, rule := range userKeyCodeRules {
		routes[rule.path] = true
	}
	return routes
}()

// userKeyHeldToReading is the rule as a request by this method meets it: a
// write by a read rule not named above needs content.
func userKeyHeldToReading(rule userKeyRule, method string) userKeyRule {
	if !userKeyReadRuleWrites(rule, method) {
		return rule
	}
	rule.scope = userKeyScopeContent
	return rule
}

func userKeyReadRuleWrites(rule userKeyRule, method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return rule.scope == userKeyScopeRead && rule.place != userKeyPlacePublic &&
		!userKeyReadingWrites[method+" "+rule.path] && !userKeyCodeRoutes[rule.path]
}

// logReadKeyWritesHeld names, as the server starts, each registered write
// route a read rule covers that isn't named above: a read key can't call it,
// which shows the moment the server is first run.
func logReadKeyWritesHeld(app core.App, group any) {
	held := []string{}
	for _, route := range nativeRoutes(group) {
		for _, rule := range userKeyRules {
			if rule.allows(route.Method) && rule.pattern.MatchString(route.Path) {
				if userKeyReadRuleWrites(rule, route.Method) {
					held = append(held, route.Method+" "+route.Path)
				}
				break
			}
		}
	}
	if len(held) == 0 {
		return
	}
	log.Printf("leafen: %d write routes classified for read keys need content, as none says it changes nothing: %s",
		len(held), strings.Join(held, ", "))
	app.Logger().Warn("leafen write routes held from read keys", "count", len(held), "routes", held)
}
