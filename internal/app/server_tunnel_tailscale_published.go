//go:build !ios && !android

package app

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"time"
)

// Tailscale puts a device's public name (name.tailnet.ts.net) in the public
// DNS only after its Funnel opens, and the name's own name servers answered
// it five minutes later (2026-11-11). A browser or phone that looked the name
// up meanwhile kept "no such name" for five minutes more (the zone's negative
// time to live), so a done screen and a phone's QR shown at once opened
// nothing for minutes. The address is said to be on only once the name's own
// name servers answer it — asked directly, so no resolver on the way learns
// "none" — and meanwhile the screen says the name is being published.
// Nothing is waited for where those name servers can't be asked (outgoing
// DNS closed), for a tailnet a control server of its own runs (TS_CONTROL_URL:
// its names aren't public), nor for longer than tailscalePublishWait.

const (
	tailscalePublishCheckEvery = 5 * time.Second
	tailscalePublishWait       = 10 * time.Minute
	tailscalePublishAskFor     = 4 * time.Second
	// tailscalePublicZone is where Tailscale publishes Funnel names; its name
	// servers are asked, never a tailnet's own name, which may not exist yet.
	tailscalePublicZone = "ts.net"
)

type tailscaleNameAnswer int

const (
	// No name server answered: the wait can't tell, so it doesn't wait.
	tailscaleNameUnasked tailscaleNameAnswer = iota
	tailscaleNameMissing
	tailscaleNamePublished
)

// waitTailscalePublished waits until the address's own name servers answer
// it, saying so on the screen meanwhile; false once the tunnel ends.
func (tunnel *serverTunnel) waitTailscalePublished(ctx context.Context, address string) bool {
	if os.Getenv("TS_CONTROL_URL") != "" || !strings.HasSuffix(address, "."+tailscalePublicZone) {
		return ctx.Err() == nil
	}
	defer tunnel.setTailscalePublishing(false)
	servers := tailscalePublicNameServers(ctx)
	deadline := time.Now().Add(tailscalePublishWait)
	for {
		if askTailscaleName(ctx, servers, address) != tailscaleNameMissing || time.Now().After(deadline) {
			return ctx.Err() == nil
		}
		tunnel.setTailscalePublishing(true)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(tailscalePublishCheckEvery):
		}
	}
}

func (tunnel *serverTunnel) setTailscalePublishing(publishing bool) {
	tunnel.mu.Lock()
	changed := tunnel.tailscalePublishing != publishing
	tunnel.tailscalePublishing = publishing
	tunnel.mu.Unlock()
	if changed && publishing {
		tunnel.writeTailscalePublishing()
	}
}

// tailscalePublicNameServers are the name servers of the zone Funnel names
// are published in, by the system's resolver.
func tailscalePublicNameServers(ctx context.Context) []string {
	ask, cancel := context.WithTimeout(ctx, tailscalePublishAskFor)
	defer cancel()
	records, err := net.DefaultResolver.LookupNS(ask, tailscalePublicZone)
	if err != nil {
		return nil
	}
	servers := []string{}
	for _, record := range records {
		if host := strings.TrimSuffix(record.Host, "."); host != "" && len(servers) < 3 {
			servers = append(servers, host)
		}
	}
	return servers
}

// askTailscaleName asks the zone's name servers themselves for the address,
// until one answers.
func askTailscaleName(ctx context.Context, servers []string, address string) tailscaleNameAnswer {
	for _, server := range servers {
		resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, net.JoinHostPort(server, "53"))
		}}
		ask, cancel := context.WithTimeout(ctx, tailscalePublishAskFor)
		addresses, err := resolver.LookupHost(ask, address+".")
		cancel()
		var dnsError *net.DNSError
		switch {
		case err == nil && len(addresses) > 0:
			return tailscaleNamePublished
		case errors.As(err, &dnsError) && dnsError.IsNotFound:
			return tailscaleNameMissing
		}
	}
	return tailscaleNameUnasked
}
