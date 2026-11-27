package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"leafen.org/leafen/internal/portmap"
)

// What the server measures of its network when Tailscale can't be used (the
// built-in tunnel decision's changes of 2026-10-19): whether it reaches the
// internet at all (a closed network uses a company address), whether it
// reaches Tailscale's control and why not, its public IPv4 and global IPv6,
// and whether its router opens ports by itself (only asked, nothing opened).
// The address choice changes its layout by these, and the text an agent is
// given carries them, so the agent starts from facts.

const (
	// reachInternetEnv and reachTailscaleEnv point the measures elsewhere,
	// to stand in a closed network or a blocked Tailscale in development.
	reachInternetEnv  = "LEAFEN_REACH_INTERNET_URLS"
	reachTailscaleEnv = "LEAFEN_REACH_TAILSCALE_URL"
	reachCacheFor     = time.Minute
	reachTimeout      = 6 * time.Second

	reachSituationClosed    = "closed"
	reachSituationTailscale = "tailscale"
	reachSituationOK        = "ok"
)

var defaultReachInternet = []string{"https://www.cloudflare.com/cdn-cgi/trace", "https://1.1.1.1/cdn-cgi/trace", "https://www.google.com/generate_204"}

const defaultReachTailscale = "https://controlplane.tailscale.com/key?v=88"

type serverReach struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Container  bool   `json:"container"`
	Internet   bool   `json:"internet"`
	PublicIPv4 string `json:"publicIPv4,omitempty"`
	IPv6       string `json:"ipv6,omitempty"`
	Tailscale  bool   `json:"tailscale"`
	// TailscaleProblem says why Tailscale can't be used: "dns", "timeout",
	// "intercepted" (someone in between answers TLS), "refused", "answer",
	// "unreachable", or the failure Tailscale's sign-in reported.
	TailscaleProblem string `json:"tailscaleProblem,omitempty"`
	NATPMP           bool   `json:"natpmp"`
	UPnP             bool   `json:"upnp"`
	Situation        string `json:"situation"`
	CheckedAt        int64  `json:"checkedAt"`
	// The server looks like a company's, which is offered its own address
	// first (server_tunnel_company.go); not part of the measure.
	Company bool `json:"company"`
}

var (
	reachMu   sync.Mutex
	reachLast *serverReach
)

func registerServerTunnelReachRoute(event *core.ServeEvent) {
	event.Router.GET(apiRoot+serverTunnelPath+"/reach", readServerReachRoute)
}

func readServerReachRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	reach := measureServerReach(event.Request.Context(), serverTunnelOf(event.App), event.Request.URL.Query().Get("fresh") == "1")
	if user, ok := authenticatedUser(event); ok {
		reach.Company = companyServer(event.App, user.GetString("email"))
	}
	return event.JSON(http.StatusOK, reach)
}

// measureServerReach measures, or answers the measure of the last minute.
func measureServerReach(ctx context.Context, tunnel *serverTunnel, fresh bool) serverReach {
	reachMu.Lock()
	defer reachMu.Unlock()
	if !fresh && reachLast != nil && time.Since(time.UnixMilli(reachLast.CheckedAt)) < reachCacheFor {
		return *reachLast
	}
	ctx, cancel := context.WithTimeout(ctx, reachTimeout)
	defer cancel()
	reach := serverReach{OS: runtime.GOOS, Arch: runtime.GOARCH, Container: runningInContainer(), IPv6: portmap.GlobalIPv6()}
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		reach.Internet, reach.PublicIPv4 = reachInternet(ctx)
	}()
	go func() {
		defer wait.Done()
		reach.Tailscale, reach.TailscaleProblem = reachTailscale(ctx)
	}()
	go func() {
		defer wait.Done()
		reach.NATPMP, reach.UPnP = portmap.PortMapper{}.Probe(ctx)
	}()
	wait.Wait()
	// Tailscale reached but its sign-in failing counts as not usable.
	if reach.Tailscale && tunnel != nil {
		tunnel.mu.Lock()
		failure, state := tunnel.failure.text, tunnel.state
		tunnel.mu.Unlock()
		if settings := serverTunnelChosen.Load(); settings != nil && settings.Mode == serverTunnelTailscale && state == serverTunnelStateRetrying && failure != "" {
			reach.Tailscale, reach.TailscaleProblem = false, failure
		}
	}
	switch {
	case !reach.Internet:
		reach.Situation = reachSituationClosed
	case !reach.Tailscale:
		reach.Situation = reachSituationTailscale
	default:
		reach.Situation = reachSituationOK
	}
	reach.CheckedAt = time.Now().UnixMilli()
	reachLast = &reach
	return reach
}

func runningInContainer() bool {
	for _, mark := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(mark); err == nil {
			return true
		}
	}
	return false
}

func reachURLs(env string, fallback []string) []string {
	urls := []string{}
	for _, part := range strings.Split(os.Getenv(env), ",") {
		if part = strings.TrimSpace(part); part != "" {
			urls = append(urls, part)
		}
	}
	if len(urls) == 0 {
		return fallback
	}
	return urls
}

// reachInternet says whether any of the well-known addresses answers over
// HTTPS, with the public IPv4 Cloudflare's trace names.
func reachInternet(ctx context.Context) (bool, string) {
	client := &http.Client{Timeout: reachTimeout}
	for _, address := range reachURLs(reachInternetEnv, defaultReachInternet) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			continue
		}
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		response.Body.Close()
		if response.StatusCode >= 500 {
			continue
		}
		ip := ""
		for _, line := range strings.Split(string(body), "\n") {
			if value, found := strings.CutPrefix(strings.TrimSpace(line), "ip="); found {
				if parsed := net.ParseIP(value); parsed != nil && parsed.To4() != nil {
					ip = parsed.String()
				}
			}
		}
		return true, ip
	}
	return false, ""
}

// reachTailscale says whether Tailscale's control answers, and why not.
func reachTailscale(ctx context.Context) (bool, string) {
	client := &http.Client{Timeout: reachTimeout}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(firstNonEmpty(os.Getenv(reachTailscaleEnv), defaultReachTailscale)), nil)
	if err != nil {
		return false, "unreachable"
	}
	response, err := client.Do(request)
	if err != nil {
		var dnsError *net.DNSError
		var unknownAuthority x509.UnknownAuthorityError
		var hostnameError x509.HostnameError
		var certificateError *tls.CertificateVerificationError
		var netError net.Error
		switch {
		case errors.As(err, &dnsError):
			return false, "dns"
		case errors.As(err, &unknownAuthority), errors.As(err, &hostnameError), errors.As(err, &certificateError):
			return false, "intercepted"
		case errors.As(err, &netError) && netError.Timeout():
			return false, "timeout"
		case strings.Contains(err.Error(), "refused") || strings.Contains(err.Error(), "reset"):
			return false, "refused"
		}
		return false, "unreachable"
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, "answer"
	}
	return true, ""
}
