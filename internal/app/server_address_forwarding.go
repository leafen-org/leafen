package app

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// An address changed is forwarded for a while (the built-in tunnel
// decision's changes of 2026-10-19): when the address the administrator
// chose becomes another — Tailscale's to their own domain, one domain to
// another — the old one answers every request with a redirect to the same
// place at the new one for 90 days, so shared links, bookmarks and connected
// agents keep working while people move. The server keeps a way in at the
// old address where it held it: the old Tailscale device's Funnel, or the
// old certificate it served itself. An address behind the administrator's
// own proxy or Cloudflare tunnel forwards only while that still sends it
// here. A quick tunnel's address was temporary and isn't forwarded. Agents
// connected at the old address go on there (mcp_earlier_address.go).
//
// An old address still in use forwards on (2026-10-20): each request there
// keeps it going 30 days past that request, up to a year from the change, so
// a link or an agent used monthly isn't cut off at 90 days, while an address
// nobody uses still ends. What still comes — agents, apps, links, sign-in
// providers — is kept to show on the remote access screen.

const (
	addressForwardingFile = "forwarding.json"
	// addressForwardForEnv shortens the 90 days, for development.
	addressForwardForEnv = "LEAFEN_ADDRESS_FORWARD_FOR"

	forwardKindTailscale = "tailscale"
	forwardKindTLS       = "tls"
	forwardKindProxy     = "proxy"
	// forwardUseSaveEvery is how often a forward's use is written down.
	forwardUseSaveEvery = time.Minute
)

func addressForwardFor() time.Duration {
	if span, err := time.ParseDuration(strings.TrimSpace(os.Getenv(addressForwardForEnv))); err == nil && span > 0 {
		return span
	}
	return 90 * 24 * time.Hour
}

// addressForwardAfterUse is how long a forward lasts past a request at its
// old address (30 days), and addressForwardLongest how long past the change
// at most (a year): shares of the period, so a shortened one stays short.
func addressForwardAfterUse() time.Duration { return addressForwardFor() / 3 }
func addressForwardLongest() time.Duration  { return addressForwardFor() * 365 / 90 }

// addressForward is an old address forwarding to a new one until a time.
type addressForward struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Until int64  `json:"until"`
	Kind  string `json:"kind"`
	Port  int    `json:"port,omitzero"`
	// Since is when the address changed; LastSeen the last request at the
	// old address, and Seen what made such requests.
	Since    int64    `json:"since,omitempty"`
	LastSeen int64    `json:"lastSeen,omitzero"`
	Seen     []string `json:"seen,omitempty"`
	// SignedOut says, on the remote access screen, that the earlier
	// Tailscale device asks to be signed in again, so the address doesn't
	// forward now (forwardSignedOut); it isn't written down.
	SignedOut bool `json:"signedOut,omitzero"`
	// Its connections were told it ends soon, and that it ended (server_address_forwarding_end_notice.go).
	SoonNoticed bool `json:"soonNoticed,omitzero"`
	EndNoticed  bool `json:"endNoticed,omitzero"`
}

// addressCanonical is an address the server was reached at, and how.
type addressCanonical struct {
	Origin string `json:"origin"`
	Kind   string `json:"kind"`
	Port   int    `json:"port,omitzero"`
	// SignedOut: the Tailscale device that held the address was signed out
	// of the administrator's account ("Tailscale에서 로그아웃"). Its address
	// went with it — the device can't open it again — so another address
	// chosen after doesn't forward from it.
	SignedOut bool `json:"signedOut,omitzero"`
}

type addressForwarding struct {
	// Last is the address chosen last, so one chosen after remote access was
	// off for a while still forwards from it.
	Last     *addressCanonical `json:"last,omitempty"`
	Forwards []addressForward  `json:"forwards"`
}

var (
	forwardingMu     sync.Mutex
	forwardingActive atomic.Pointer[[]addressForward]
)

func addressForwardingPath(app core.App) string {
	return filepath.Join(app.DataDir(), addressWatchFolder, addressForwardingFile)
}

func loadAddressForwarding(app core.App) addressForwarding {
	forwarding := addressForwarding{}
	if data, err := os.ReadFile(addressForwardingPath(app)); err == nil {
		_ = json.Unmarshal(data, &forwarding)
	}
	return forwarding
}

func saveAddressForwarding(app core.App, forwarding addressForwarding) {
	if os.MkdirAll(filepath.Dir(addressForwardingPath(app)), 0o700) != nil {
		return
	}
	data, _ := json.Marshal(forwarding)
	_ = os.WriteFile(addressForwardingPath(app), data, 0o600)
}

// activeForwards are the forwards not yet over, kept for the request hook.
func activeForwards(forwarding addressForwarding, now time.Time) []addressForward {
	active := []addressForward{}
	for _, forward := range forwarding.Forwards {
		if forward.Until > now.UnixMilli() {
			active = append(active, forward)
		}
	}
	return active
}

// canonicalOf is the address a setting gives and how the server is reached
// there (with none chosen, the start settings', startAddressCanonical); none for a quick tunnel.
func canonicalOf(app core.App, settings serverTunnelSettings) addressCanonical {
	switch settings.Mode {
	case serverTunnelTailscale:
		if settings.TailscaleAddress != "" {
			return addressCanonical{Origin: "https://" + settings.TailscaleAddress, Kind: forwardKindTailscale, Port: 443}
		}
	case serverTunnelOwnAddress:
		if port := addressTLSPort(app, settings.Address); port > 0 {
			return addressCanonical{Origin: settings.Address, Kind: forwardKindTLS, Port: port}
		}
		return addressCanonical{Origin: settings.Address, Kind: forwardKindProxy}
	case serverTunnelToken:
		return addressCanonical{Origin: settings.Address, Kind: forwardKindProxy}
	}
	return startAddressCanonical(settings)
}

// noteCanonicalChange records the chosen address and, when it replaced
// another, the forward from that one, telling whose agents came through it.
func noteCanonicalChange(app core.App, settings serverTunnelSettings) {
	if app == nil {
		return
	}
	current := canonicalOf(app, settings)
	forwardingMu.Lock()
	defer forwardingMu.Unlock()
	forwarding := loadAddressForwarding(app)
	now := time.Now()
	forwarding.Forwards = activeForwards(forwarding, now)
	changed := false
	if current.Origin != "" {
		if forwarding.Last != nil && forwarding.Last.Origin != "" && forwarding.Last.Origin != current.Origin {
			kept := []addressForward{}
			for _, forward := range forwarding.Forwards {
				// The address chosen again stops being forwarded; every
				// earlier one goes straight to the new address, no chain.
				if forward.From != current.Origin && forward.From != forwarding.Last.Origin {
					forward.To = current.Origin
					kept = append(kept, forward)
				}
			}
			forwarding.Forwards = kept
			if !forwarding.Last.SignedOut {
				forwarding.Forwards = append(kept, addressForward{From: forwarding.Last.Origin, To: current.Origin,
					Until: now.Add(addressForwardFor()).UnixMilli(), Kind: forwarding.Last.Kind, Port: forwarding.Last.Port,
					Since: now.UnixMilli()})
			}
			tellAddressChange(app, forwarding.Last.Origin, current.Origin)
			changed = true
		}
		if forwarding.Last == nil || *forwarding.Last != current {
			forwarding.Last = &current
			changed = true
		}
	}
	if changed {
		saveAddressForwarding(app, forwarding)
	}
	active := activeForwards(forwarding, now)
	forwardingActive.Store(&active)
}

// noteTailscaleSignedOut lets the Tailscale addresses go once the device was
// signed out of the administrator's account: the address it held, chosen
// last, forwards from nowhere when another is chosen, and the earlier
// Tailscale addresses it kept forwarding stop, as the device can open none of
// them again (2026-11-11: the screen said an old address forwarded that the
// signed-out device couldn't open). The people whose agents came through it
// are still told to connect them again when the next address is chosen.
func noteTailscaleSignedOut(app core.App) {
	if app == nil {
		return
	}
	forwardingMu.Lock()
	defer forwardingMu.Unlock()
	forwarding := loadAddressForwarding(app)
	now := time.Now()
	kept := []addressForward{}
	for _, forward := range activeForwards(forwarding, now) {
		if forward.Kind != forwardKindTailscale {
			kept = append(kept, forward)
		}
	}
	forwarding.Forwards = kept
	if forwarding.Last != nil && forwarding.Last.Kind == forwardKindTailscale {
		forwarding.Last.SignedOut = true
	}
	saveAddressForwarding(app, forwarding)
	active := activeForwards(forwarding, now)
	forwardingActive.Store(&active)
}

// forwardSignedOut are the earlier Tailscale addresses whose device asked to
// be signed in again when their way last tried (runForwardingWay): they
// don't forward until it is.
var forwardSignedOut sync.Map

// forwardedOrigin is the address an earlier one forwards to now, "" when it
// doesn't (2026-10-20: what was handed out at the old address — an agent's
// token, a link in a page — is this server's while it forwards).
func forwardedOrigin(origin string) string {
	active := forwardingActive.Load()
	if active == nil || origin == "" {
		return ""
	}
	origin = strings.TrimRight(origin, "/")
	now := time.Now().UnixMilli()
	for _, forward := range *active {
		if forward.Until > now && strings.EqualFold(strings.TrimRight(forward.From, "/"), origin) {
			return strings.TrimRight(forward.To, "/")
		}
	}
	return ""
}

// movedResource is an address at an earlier address that forwards, as it is
// at the address it forwards to; "" for any other.
func movedResource(resource string) string {
	parsed, err := url.Parse(resource)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	to := forwardedOrigin(parsed.Scheme + "://" + parsed.Host)
	if to == "" {
		return ""
	}
	return to + parsed.EscapedPath()
}

// forwardFor is the forward a request at an earlier address takes.
func forwardFor(request *http.Request) (addressForward, bool) {
	active := forwardingActive.Load()
	if active == nil || request == nil {
		return addressForward{}, false
	}
	host := request.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	now := time.Now().UnixMilli()
	for _, forward := range *active {
		if forward.Until > now && strings.EqualFold(hostOf(forward.From), host) && forward.From != chosenServerOrigin() {
			return forward, true
		}
	}
	return addressForward{}, false
}

// programServedInPlace says whether a request at an earlier address is an
// outside program's call answered there rather than redirected: an app's
// webhooks and endpoints, whose callers mostly don't follow a
// redirect (2026-10-20: GitHub's and Stripe's webhooks don't), so a
// redirect would lose them while the address still forwards; and a SAML
// sign-in's metadata and the service hub's client metadata document, which
// say the values of the address they are read at.
func programServedInPlace(path string) bool {
	// The page carrying a sign-in over asks for its pass where the sign-in is (address_handoff.go).
	if strings.HasPrefix(path, appEndpointAddressPrefix) || path == apiRoot+mcpServiceClientMetadataPath || path == apiRoot+addressHandoffIssuePath {
		return true
	}
	if saml, ok := strings.CutPrefix(path, apiRoot+"/auth/saml/"); ok {
		provider, metadata := strings.CutSuffix(saml, "/metadata")
		return metadata && provider != "" && !strings.Contains(provider, "/")
	}
	rest, app := strings.CutPrefix(path, apiRoot+"/apps/")
	if !app {
		return false
	}
	parts := strings.Split(rest, "/")
	return len(parts) == 3 && parts[0] != "" && parts[1] == "hooks" && parts[2] != ""
}

// forwardUseOf names what made a request at an earlier address.
func forwardUseOf(request *http.Request) string {
	path := request.URL.Path
	switch {
	case strings.Contains(path, "/auth/providers/") || strings.Contains(path, "/auth/saml/"):
		return "signIn"
	case programServedInPlace(path):
		return "programs"
	case mcpAgentRequestPath(path):
		return "agents"
	case keyProgramRequest(request):
		return "keys"
	case strings.HasPrefix(path, "/api/"):
		return "apps"
	default:
		return "links"
	}
}

type forwardUse struct {
	last  int64
	seen  map[string]bool
	saved int64
}

var (
	forwardUsesMu sync.Mutex
	forwardUses   = map[string]*forwardUse{}
)

// noteForwardUse keeps that an earlier address was just used, writing it
// down (and the longer forward it earns) at most once a minute an address.
func noteForwardUse(app core.App, from, use string) {
	now := time.Now().UnixMilli()
	forwardUsesMu.Lock()
	entry := forwardUses[from]
	if entry == nil {
		entry = &forwardUse{seen: map[string]bool{}}
		forwardUses[from] = entry
	}
	entry.last = now
	fresh := !entry.seen[use]
	entry.seen[use] = true
	due := fresh || now-entry.saved >= forwardUseSaveEvery.Milliseconds()
	if due {
		entry.saved = now
	}
	forwardUsesMu.Unlock()
	if due && app != nil {
		serverBackgroundOf(app).behind("earlier address use", func() { saveForwardUse(app, from) })
	}
}

// saveForwardUse writes an earlier address's last use down and moves its end
// to addressForwardAfterUse past it, never beyond addressForwardLongest past
// the change.
func saveForwardUse(app core.App, from string) {
	forwardUsesMu.Lock()
	entry := forwardUses[from]
	last, seen := int64(0), []string{}
	if entry != nil {
		last = entry.last
		for use := range entry.seen {
			seen = append(seen, use)
		}
	}
	forwardUsesMu.Unlock()
	if last == 0 {
		return
	}
	forwardingMu.Lock()
	defer forwardingMu.Unlock()
	forwarding := loadAddressForwarding(app)
	changed := false
	for index := range forwarding.Forwards {
		forward := &forwarding.Forwards[index]
		if forward.From != from {
			continue
		}
		since := forward.Since
		if since == 0 {
			since = forward.Until - addressForwardFor().Milliseconds()
		}
		if last > forward.LastSeen {
			forward.LastSeen, changed = last, true
		}
		if until := min(last+addressForwardAfterUse().Milliseconds(), since+addressForwardLongest().Milliseconds()); until > forward.Until {
			forward.Until, changed = until, true
		}
		for _, use := range seen {
			if !slices.Contains(forward.Seen, use) {
				forward.Seen, changed = append(forward.Seen, use), true
			}
		}
		slices.Sort(forward.Seen)
	}
	if !changed {
		return
	}
	saveAddressForwarding(app, forwarding)
	active := activeForwards(forwarding, time.Now())
	forwardingActive.Store(&active)
}

// registerAddressForwardingHook sends a request at an earlier address that
// forwards to the new one, before anything else answers it.
func registerAddressForwardingHook(event *core.ServeEvent) {
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenAddressForwarding", Priority: -100001,
		Func: func(e *core.RequestEvent) error {
			if forwardRequest(e) {
				return nil
			}
			return e.Next()
		},
	})
}

// forwardRequest answers a request made at a forwarded address with a
// redirect keeping its method and body, or lets an outside program's call
// be answered there.
func forwardRequest(event *core.RequestEvent) bool {
	forward, ok := forwardFor(event.Request)
	if !ok {
		return false
	}
	noteForwardUse(event.App, forward.From, forwardUseOf(event.Request))
	if forwardServedInPlace(event.Request) {
		return false
	}
	// A page opened there carries the browser's sign-in over (address_handoff.go).
	if to := handoffTarget(forward); to != "" && addressHandoffNavigation(event.Request) {
		serveAddressHandoffPage(event.Response, event.Request, to)
		return true
	}
	allowForwardAcrossOrigins(event.Response, event.Request)
	http.Redirect(event.Response, event.Request, strings.TrimRight(forward.To, "/")+event.Request.URL.RequestURI(), http.StatusPermanentRedirect)
	return true
}

// allowForwardAcrossOrigins lets a page on another origin — the apps'
// window — follow the redirect, which answers before the CORS middleware
// would: a cross-origin redirect without it is refused, and the app never
// learns where its server went (2026-10-20).
func allowForwardAcrossOrigins(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Origin") != "" {
		writer.Header().Set("Access-Control-Allow-Origin", "*")
	}
}

// EarlierAddressOf is the earlier address a request came to and is answered
// at (forwardServedInPlace), "" for one that came to the current address.
func EarlierAddressOf(request *http.Request) string {
	if forward, ok := forwardFor(request); ok {
		return strings.TrimRight(forward.From, "/")
	}
	return ""
}

// currentForwards are the earlier addresses forwarding now, for the screen:
// not one chosen again, which is the server's while it opens (forwardFor).
func currentForwards() []addressForward {
	active := forwardingActive.Load()
	if active == nil {
		return nil
	}
	now := time.Now().UnixMilli()
	chosen := chosenServerOrigin()
	forwards := []addressForward{}
	for _, forward := range *active {
		if forward.Until > now && forward.From != chosen {
			_, forward.SignedOut = forwardSignedOut.Load(forward.From)
			forwards = append(forwards, forward)
		}
	}
	return forwards
}

// earlierServerAddresses are the addresses that forward here now, for the
// client to know a link at one as this server's (2026-10-20).
func earlierServerAddresses() []string {
	addresses := []string{}
	for _, forward := range currentForwards() {
		addresses = append(addresses, strings.TrimRight(forward.From, "/"))
	}
	return addresses
}
