package app

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A link in a mail is read by someone other than whoever asked for the mail, and
// anyone can write any Host on a request: a reset asked for with a Host of the
// asker's own would mail the person's token to the asker's server. So a mail
// takes an address the server knows is its own, and an answer to the asker
// (outwardServerOrigin) may stay as the request sees it. Nothing new to set up:
// the server knows the address written or chosen, the address a connection really
// came in on, and the addresses its administrators' browsers have opened it at,
// which is what tells it the address behind a proxy or a container's port.

const (
	serverAdministratorAddressesKept  = 16
	serverAdministratorAddressRefresh = time.Hour
)

var errServerAddressUnknown = errors.New("the server has no known address for a mail's link: an administrator opens it once, or one is set in the settings")

// mailServerOrigin is the address for a link in a mail: the one written or
// chosen, else the request's when the server knows it is its own, else the one an
// administrator used last, one people elsewhere can reach before this computer's
// own. "" when it knows none, and a mail then carries no link.
func mailServerOrigin(app core.App, request *http.Request) string {
	if origin := configuredPublicOrigin(); origin != "" {
		return origin
	}
	if requestHostIsConnection(request) {
		if request.TLS != nil {
			return "https://" + request.Host
		}
		return "http://" + request.Host
	}
	used := []string{}
	if err := app.DB().NewQuery(`SELECT [[origin]] FROM {{leafen_server_administrator_addresses}}
		ORDER BY [[used_at]] DESC, [[origin]]`).Column(&used); err != nil {
		return ""
	}
	loopback := ""
	reachable := ""
	for _, origin := range used {
		parsed, err := url.Parse(origin)
		if err != nil {
			continue
		}
		if strings.EqualFold(parsed.Host, request.Host) {
			return origin
		}
		switch isLoopback := mcpLoopbackHost(parsed.Host); {
		case isLoopback && loopback == "":
			loopback = origin
		case !isLoopback && reachable == "":
			reachable = origin
		}
	}
	if reachable != "" {
		return reachable
	}
	return loopback
}

// requestHostIsConnection is whether the request's Host names the address its
// connection came in on, or this computer by name at that port: writing it gets
// no one anywhere but this server.
func requestHostIsConnection(request *http.Request) bool {
	local, ok := request.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil {
		return false
	}
	localHost, localPort, err := net.SplitHostPort(local.String())
	if err != nil {
		return false
	}
	host, port, err := net.SplitHostPort(request.Host)
	if err != nil {
		host, port = request.Host, "80"
		if request.TLS != nil {
			port = "443"
		}
	}
	if port != localPort {
		return false
	}
	host = strings.Trim(host, "[]")
	localIP := net.ParseIP(localHost)
	if ip := net.ParseIP(host); ip != nil {
		return localIP != nil && ip.Equal(localIP)
	}
	// A name under localhost is this computer too, wherever it is opened:
	// browsers send no such name elsewhere (RFC 6761), so a page opened as
	// g.localhost gets its mails' links there rather than at another name an
	// administrator used (2026-11-06).
	if lower := strings.ToLower(host); lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return localIP != nil && localIP.IsLoopback()
	}
	machine, err := os.Hostname()
	if err != nil || machine == "" {
		return false
	}
	short, _, _ := strings.Cut(machine, ".")
	return strings.EqualFold(host, machine) || strings.EqualFold(host, short) || strings.EqualFold(host, short+".local")
}

var serverAdministratorAddressesNoted sync.Map

// noteAdministratorAddress keeps the address an administrator's browser has the
// server open at. Only a browser's sign-in counts: a browser can't write its Host,
// and a member or a key can't make an address known. The written and chosen
// addresses are known already, and a tunnel's goes with the tunnel.
func noteAdministratorAddress(event *core.RequestEvent, userID string) {
	request := event.Request
	if isServerInnerRequest(request) || currentBrowserSessionID(event) == "" || requestChosenOrigin(request) != "" {
		return
	}
	hostURL, err := url.Parse("http://" + request.Host)
	if err != nil || hostURL.Host == "" || hostURL.User != nil || hostURL.Path != "" {
		return
	}
	origin := requestSchemeSeen(request) + "://" + hostURL.Host
	now := time.Now()
	if noted, seen := serverAdministratorAddressesNoted.Load(origin); seen && now.Sub(noted.(time.Time)) < serverAdministratorAddressRefresh {
		return
	}
	if administrator, err := isAdministrator(event.App, userID); err != nil || !administrator {
		return
	}
	if _, err := event.App.DB().NewQuery(`
		INSERT INTO {{leafen_server_administrator_addresses}} ([[origin]], [[used_at]]) VALUES ({:origin}, {:now})
		ON CONFLICT ([[origin]]) DO UPDATE SET [[used_at]] = excluded.[[used_at]]
	`).Bind(dbx.Params{"origin": origin, "now": now.UnixMilli()}).Execute(); err != nil {
		return
	}
	serverAdministratorAddressesNoted.Store(origin, now)
	noteHTTPSOrigin(origin)
	_, _ = event.App.DB().NewQuery(`DELETE FROM {{leafen_server_administrator_addresses}} WHERE [[origin]] NOT IN (
		SELECT [[origin]] FROM {{leafen_server_administrator_addresses}} ORDER BY [[used_at]] DESC LIMIT {:kept})`).
		Bind(dbx.Params{"kept": serverAdministratorAddressesKept}).Execute()
}

// requestSchemeSeen says how an administrator's browser reached the server,
// by the one rule (request_scheme.go).
func requestSchemeSeen(request *http.Request) string {
	return requestScheme(request)
}
