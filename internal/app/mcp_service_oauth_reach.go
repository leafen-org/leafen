package app

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/pocketbase/pocketbase/core"
)

// A service that signs Leafen in only by its client ID metadata document —
// it registers no clients, or refused this one — reads the document itself,
// from the internet, at this server's address. A Leafen opened at an address
// the internet can't reach (this computer, its local network, a tailnet name
// Funnel doesn't publish) is never signed in that way, and the person saw
// only the service's own "Invalid client_id" (2026-11-20, a synthetic
// service). So before sending them there the server says why and what to do
// instead — a client made on the service for Leafen, or Leafen opened at a
// public address — in settings as the address is typed (the probe's
// clientNeeded), when connecting or signing in again, and to an agent in the
// same words.
//
// It says "out of reach" only where it is sure: an http address, an address
// or name that is this computer's or its network's (hostNamedInNetwork), a
// name never in the public DNS (RFC 6761's test., invalid., example.), or a
// name its own name servers — asked directly, so no resolver of the local
// network answers for them — have nowhere or only at private addresses (a
// tailnet's name in ts.net is published only once Funnel opens it,
// server_tunnel_tailscale_published.go). Where they can't be asked, the
// document is tried as before.

const (
	mcpServiceReachAskFor = 3 * time.Second
	mcpServiceReachWait   = 8 * time.Second
)

var errMCPServiceAddressUnreachable = errors.New(errorCodeMCPServiceAddressUnreachable)

// mcpServiceUnreachable is errMCPServiceAddressUnreachable with the address
// the service couldn't reach and, where the server has one, its public
// address to connect from instead.
type mcpServiceUnreachable struct{ origin, public string }

func (failure *mcpServiceUnreachable) Error() string {
	return "the service can't reach this server at " + failure.origin
}

func (failure *mcpServiceUnreachable) Is(target error) bool {
	return target == errMCPServiceAddressUnreachable
}

// mcpServiceDocumentAt is the address a document client ID is served at.
func mcpServiceDocumentAt(document string) string {
	return strings.TrimSuffix(document, apiRoot+mcpServiceClientMetadataPath)
}

// mcpServiceOutOfReach says whether a service on the internet surely can't
// read this server's document at origin.
func mcpServiceOutOfReach(ctx context.Context, origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return true
	}
	// Leafen's own tunnel is public once it says it is on.
	if running := runningTunnelOrigin(); running != "" && strings.EqualFold(strings.TrimSuffix(running, "/"), origin) {
		return false
	}
	host, nearby := hostNamedInNetwork(parsed.Host)
	if nearby || mcpServiceReservedName(host) {
		return true
	}
	if net.ParseIP(host) != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, mcpServiceReachWait)
	defer cancel()
	return publicNameMissing(ctx, host)
}

// mcpServiceReservedName says whether a name is one the public DNS never has.
func mcpServiceReservedName(host string) bool {
	for _, reserved := range []string{"test", "invalid", "example"} {
		if host == reserved || strings.HasSuffix(host, "."+reserved) {
			return true
		}
	}
	return false
}

// mcpServicePublicOrigin is the server's public address other than origin,
// where it has one a service reaches: the address written or chosen for it,
// or its running tunnel's.
func mcpServicePublicOrigin(ctx context.Context, origin string) string {
	for _, candidate := range []string{configuredPublicOrigin(), runningTunnelOrigin()} {
		candidate = strings.TrimSuffix(candidate, "/")
		if candidate != "" && !strings.EqualFold(candidate, origin) && !mcpServiceOutOfReach(ctx, candidate) {
			return candidate
		}
	}
	return ""
}

// mcpServiceClientNeed is what the server needs to be the service's client
// when the person gives none, said before the sign-in: nil where it registers
// itself or the service reads its document at origin, else why not.
func mcpServiceClientNeed(ctx context.Context, meta *oauthex.AuthServerMeta, origin string) error {
	switch {
	case meta == nil || meta.RegistrationEndpoint != "":
		return nil
	case !meta.ClientIDMetadataDocumentSupported:
		return errMCPServiceNoRegistration
	case !mcpServiceOutOfReach(ctx, origin):
		return nil
	}
	return &mcpServiceUnreachable{origin: origin, public: mcpServicePublicOrigin(ctx, origin)}
}

// mcpServiceClientNeedText says it to the person's agent, the screen named in
// the person's language (empty: both); a refused registration is said with
// what the service answered (mcp_service_oauth_register.go).
func mcpServiceClientNeedText(err error, language string) string {
	unreachable, refused := &mcpServiceUnreachable{}, &mcpServiceRegistrationRefused{}
	if errors.As(err, &refused) {
		said := ""
		if refused.said != "" {
			said = " (it answered " + refused.said + ")"
		}
		return "The address is right: the service answered as an MCP server, but it refused to let Leafen register itself for its login" +
			said + ". Make an OAuth app on the service for Leafen (its redirect URI: " + refused.redirect +
			") and connect with its client ID (clientId, and clientSecret if it has one)."
	}
	if !errors.As(err, &unreachable) {
		return "The service doesn't let Leafen register for its login. Make an OAuth app on the service for Leafen and connect with its client ID, or with a token the service gives."
	}
	host := unreachable.origin
	if parsed, err := url.Parse(unreachable.origin); err == nil && parsed.Host != "" {
		host = parsed.Host
	}
	instead := "connect from Leafen opened at a public address, once a Leafen administrator turns one on in " +
		mcpScreen(language, "settings", "server", "serverRemoteAccess") + "."
	if unreachable.public != "" {
		instead = "connect from Leafen opened at its public address, " + unreachable.public + "."
	}
	return "To log in, this service connects to Leafen's address (" + host + ") itself, and that address isn't open to the internet, " +
		"so the service can't reach it. Make an OAuth app on the service for Leafen (its redirect URI: " + unreachable.origin + apiRoot +
		mcpServiceCallbackPath + ") and connect with its client ID (clientId, and clientSecret if it has one), or " + instead
}

// mcpServiceRequestLanguage is the language of the screens of the person
// asking (empty: not known).
func mcpServiceRequestLanguage(event *core.RequestEvent) string {
	if user, ok := authenticatedUser(event); ok {
		return mcpScreenLanguageIn(event.Request.Context(), event.App, user.Id)
	}
	return ""
}

// publicNameMissing says whether the name servers of the zone host is in
// answer it nowhere, or only at private addresses; false where none could be
// asked.
func publicNameMissing(ctx context.Context, host string) bool {
	servers := []string{}
	for zone := host; strings.Contains(zone, ".") && len(servers) == 0; zone = zone[strings.Index(zone, ".")+1:] {
		ask, cancel := context.WithTimeout(ctx, mcpServiceReachAskFor)
		records, err := net.DefaultResolver.LookupNS(ask, zone)
		cancel()
		for _, record := range records {
			if name := strings.TrimSuffix(record.Host, "."); name != "" && len(servers) < 3 {
				servers = append(servers, name)
			}
		}
		var dnsError *net.DNSError
		if err != nil && !(errors.As(err, &dnsError) && dnsError.IsNotFound) {
			return false
		}
	}
	for _, server := range servers {
		resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, net.JoinHostPort(server, "53"))
		}}
		ask, cancel := context.WithTimeout(ctx, mcpServiceReachAskFor)
		addresses, err := resolver.LookupIPAddr(ask, host+".")
		cancel()
		var dnsError *net.DNSError
		switch {
		case err == nil && len(addresses) > 0:
			for _, address := range addresses {
				if !addressInNetwork(address.IP) {
					return false
				}
			}
			return true
		case errors.As(err, &dnsError) && dnsError.IsNotFound:
			return true
		}
	}
	return false
}
