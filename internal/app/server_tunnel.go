package app

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// Leafen's built-in tunnel (decided 2026-10-13): an administrator lets people
// and agents outside reach the server with no domain, port forwarding or
// certificate, through their own free Cloudflare service — a quick tunnel,
// which needs no account and gets a new address each time it starts, or a
// tunnel of their own Cloudflare account by its token, which serves their own
// address and comes back by itself when the server starts again. Traffic
// passes Cloudflare and is decrypted there. Leafen runs no relay of its own.
//
// cloudflared is found on the PATH, then in the data folder's bin/, and else
// fetched from Cloudflare's official GitHub release, its published SHA-256
// checked before it runs.

const (
	serverTunnelOff   = "off"
	serverTunnelQuick = "quick"
	serverTunnelToken = "token"
	// The administrator's own Tailscale, its public address by Funnel
	// (server_tunnel_tailscale.go).
	serverTunnelTailscale = "tailscale"
	// The administrator's own address, served by their own proxy or port
	// forwarding: no tunnel runs (server_tunnel_chosen.go).
	serverTunnelOwnAddress = "address"

	serverTunnelPath           = "/admin/tunnel"
	serverTunnelTokenMaxBytes  = 4096
	serverTunnelStoreKey       = "leafenServerTunnel"
	serverTunnelBinaryMaxBytes = 256 << 20

	errorCodeServerTunnelInvalid = "LEAFEN_SERVER_TUNNEL_INVALID"
	errorCodeServerTunnelFailed  = "LEAFEN_SERVER_TUNNEL_FAILED"

	serverTunnelStateOff      = "off"
	serverTunnelStateStarting = "starting"
	serverTunnelStateOn       = "on"
	serverTunnelStateRetrying = "retrying"
)

var (
	// serverTunnelOrigin is the running tunnel's https origin, "" with none.
	serverTunnelOrigin atomic.Value
	quickTunnelAddress = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
)

func runningTunnelOrigin() string {
	origin, _ := serverTunnelOrigin.Load().(string)
	return origin
}

// requestTunnelOrigin is the running tunnel's origin for a request that
// came through it: cloudflared keeps the address the person used as Host.
func requestTunnelOrigin(request *http.Request) string {
	tunnel := runningTunnelOrigin()
	if tunnel == "" || request == nil {
		return ""
	}
	parsed, err := url.Parse(tunnel)
	if err != nil {
		return ""
	}
	host := request.Host
	if name, _, splitErr := net.SplitHostPort(host); splitErr == nil {
		host = name
	}
	if !strings.EqualFold(host, parsed.Hostname()) {
		return ""
	}
	return tunnel
}

type serverTunnelSettings struct {
	Mode        string `db:"mode"`
	TokenCipher string `db:"token_cipher"`
	Address     string `db:"address"`
	Updated     int64  `db:"updated"`
	// The address a quick tunnel came up at last (server_tunnel_first_run.go).
	QuickAddress string `db:"quick_address"`
	// The public address Tailscale gave the server last (server_tunnel_tailscale.go).
	TailscaleAddress string `db:"tailscale_address"`
	RestoreScoped    bool   `db:"-"` // a restore's own outside way, never saved (setup_restore_agent_outside.go)
}

func loadServerTunnelSettings(app core.App) (serverTunnelSettings, error) {
	settings := serverTunnelSettings{}
	err := app.DB().NewQuery(`
		SELECT [[mode]], [[token_cipher]], [[address]], [[updated]], [[quick_address]], [[tailscale_address]]
		FROM {{leafen_server_tunnel}} WHERE [[id]] = 'tunnel'
	`).One(&settings)
	if errors.Is(err, sql.ErrNoRows) {
		return serverTunnelSettings{Mode: serverTunnelOff}, nil
	}
	return settings, err
}

// serverTunnelStatus is what the administrator's screen shows.
type serverTunnelStatus struct {
	Mode     string `json:"mode"`
	Address  string `json:"address"`
	HasToken bool   `json:"hasToken"`
	State    string `json:"state"`
	URL      string `json:"url,omitempty"`
	Failure  string `json:"failure,omitempty"`
	// What kind the failure, or the hold, is, said in the screen's words (server_tunnel_failure.go).
	FailureKind string `json:"failureKind,omitempty"`
	Since       int64  `json:"since,omitempty"`
	// What it is waiting for now (server_tunnel_stage.go).
	Stage string `json:"stage"`
	// The address the start settings give (LEAFEN_PUBLIC_URL), and whether
	// it is the server's address still: it is until a way out is chosen here
	// (server_tunnel_start_address.go).
	Configured      string `json:"configured,omitempty"`
	ConfiguredInUse bool   `json:"configuredInUse,omitzero"`
	// The packaged app's own server, which takes only agents through the
	// tunnel (local_owner_tunnel.go).
	LocalOnly bool `json:"localOnly,omitzero"`
	// A certificate for the server's address it didn't get
	// (server_address_ct.go).
	CertificateAlert *addressCertificateAlert `json:"certificateAlert,omitempty"`
	// When the certificate logs were last read for the address, and last
	// couldn't be (server_address_ct_watch.go).
	CertificateWatch *addressCTWatchStatus `json:"certificateWatch,omitempty"`
	// The tunnels' door, which a tunnel of the administrator's own names as
	// its service, and whether one still reaches the server's own port
	// (server_tunnel_door.go).
	Door       string `json:"door,omitempty"`
	DoorMissed bool   `json:"doorMissed,omitzero"`
	// Tailscale: the public address it gave last, where the administrator
	// signs in or allows the public address while the server waits, and the
	// device name asked for when Tailscale gave another (an old device of
	// the same name is still there).
	TailscaleAddress string `json:"tailscaleAddress,omitempty"`
	TailscaleLogin   string `json:"tailscaleLogin,omitempty"`
	TailscaleAllow   string `json:"tailscaleAllow,omitempty"`
	TailscaleRenamed string `json:"tailscaleRenamed,omitempty"`
	// Tailscale's address is open but its name isn't in the public DNS yet
	// (server_tunnel_tailscale_published.go).
	TailscalePublishing bool `json:"tailscalePublishing,omitzero"`
	// Whether Tailscale is built into this server (not on phones).
	TailscaleAvailable bool `json:"tailscaleAvailable"`
	// The administrator's own address: the port the server takes HTTPS on
	// with its own certificate (0 behind their proxy), and what it found
	// there (server_address_tls.go).
	AddressTLSPort int                 `json:"addressTlsPort,omitzero"`
	AddressCheck   *serverAddressCheck `json:"addressCheck,omitempty"`
	// Earlier addresses forwarding to the current one, until when.
	Forwards []addressForward `json:"forwards,omitempty"`
	// While Leafen's own remote access is off, the outside addresses the
	// server was opened at all the same (server_reached_outside.go).
	ReachedFrom []serverReachedFrom `json:"reachedFrom,omitempty"`
}

type serverTunnelInput struct {
	Mode    string `json:"mode"`
	Token   string `json:"token,omitempty"`
	Address string `json:"address,omitempty"`
}

type serverTunnel struct {
	mu       sync.Mutex
	app      core.App
	localURL string
	cancel   context.CancelFunc
	done     chan struct{}
	state    string
	url      string
	failure  tunnelFailure
	since    int64
	// When a tunnel last reached the server's own port, not its door.
	doorMissed time.Time
	// Tailscale's sign-in and allow addresses while the server waits, the
	// device name it didn't get, and how to sign the running device out
	// (server_tunnel_tailscale.go).
	tailscaleLogin, tailscaleAllow, tailscaleRenamed string
	tailscalePublishing                              bool
	// Which person's wait the allow page is for, and whether the address's
	// first certificate is being fetched (server_tunnel_stage.go).
	tailscaleStep        string
	tailscaleCertificate bool
	tailscaleLogout      func(context.Context) error
	// The setting's change time applied last, and how to stop following
	// changes another process makes (the command line's `leafen tailscale`).
	appliedUpdated int64
	followCancel   context.CancelFunc
	// What the server found at its own address last (server_address_tls.go).
	addressCheck *serverAddressCheck
	// Says the setting changed to the loop keeping earlier addresses
	// reachable while they forward (server_address_forwarding.go).
	forwardingChanged chan struct{}
	restoreScoped     bool // running a restore's own outside way (setup_restore_agent_outside.go)
}

func serverTunnelOf(app core.App) *serverTunnel {
	if tunnel, ok := app.Store().Get(serverTunnelStoreKey).(*serverTunnel); ok {
		return tunnel
	}
	tunnel := &serverTunnel{app: app, state: serverTunnelStateOff, forwardingChanged: make(chan struct{}, 1)}
	app.Store().Set(serverTunnelStoreKey, tunnel)
	return tunnel
}

func (tunnel *serverTunnel) status(settings serverTunnelSettings) serverTunnelStatus {
	reached := tunnel.openedFromOutside(settings)
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	return serverTunnelStatus{ReachedFrom: reached,
		Mode: settings.Mode, Address: settings.Address, HasToken: settings.TokenCipher != "",
		State: tunnel.state, URL: tunnel.url, Failure: tunnel.failure.text, FailureKind: tunnel.failure.kind, Since: tunnel.since, Stage: tunnel.stageLocked(settings),
		Configured: configuredServerAddress(), ConfiguredInUse: startAddressStands(settings), LocalOnly: tunnel.app != nil && localOwnerOf(tunnel.app) != nil,
		CertificateAlert: tunnel.certificateAlert(settings), CertificateWatch: tunnel.certificateWatch(settings),
		Door: tunnel.doorURL(), DoorMissed: !tunnel.doorMissed.IsZero() && time.Since(tunnel.doorMissed) < tunnelDoorMissedShown,
		TailscaleAddress: settings.TailscaleAddress, TailscaleLogin: tunnel.tailscaleLogin, TailscaleAllow: tunnel.tailscaleAllow,
		TailscaleRenamed: tunnel.tailscaleRenamed, TailscalePublishing: tunnel.tailscalePublishing, TailscaleAvailable: tailscaleBuiltIn,
		AddressCheck: tunnel.addressCheckOf(settings), AddressTLSPort: tunnel.ownAddressTLSPort(settings), Forwards: currentForwards(),
	}
}

// doorURL is the tunnels' door, opened if it isn't yet; "" where it can't be.
func (tunnel *serverTunnel) doorURL() string {
	if tunnel.app == nil {
		return ""
	}
	door, _ := tunnelDoorOf(tunnel.app, tunnel.localURL)
	return door
}

func (tunnel *serverTunnel) set(state, address string, failure tunnelFailure) {
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	if state != tunnel.state || address != tunnel.url {
		tunnel.since = time.Now().UnixMilli()
	}
	tunnel.state, tunnel.url, tunnel.failure = state, address, failure
	if state == serverTunnelStateOn {
		serverTunnelOrigin.Store(address)
	} else {
		serverTunnelOrigin.Store("")
	}
}

// stop ends a running tunnel and waits for it.
func (tunnel *serverTunnel) stop() {
	tunnel.mu.Lock()
	cancel, done := tunnel.cancel, tunnel.done
	tunnel.cancel, tunnel.done = nil, nil
	tunnel.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	tunnel.set(serverTunnelStateOff, "", noTunnelFailure)
	tunnel.mu.Lock()
	tunnel.tailscaleLogin, tunnel.tailscaleAllow, tunnel.tailscaleRenamed = "", "", ""
	tunnel.tailscalePublishing, tunnel.tailscaleStep, tunnel.tailscaleCertificate = false, "", false
	tunnel.mu.Unlock()
}

// apply runs the tunnel the settings ask for, ending any other first.
func (tunnel *serverTunnel) apply(settings serverTunnelSettings) error {
	tunnel.stop()
	tunnel.mu.Lock()
	tunnel.appliedUpdated, tunnel.restoreScoped = settings.Updated, settings.RestoreScoped
	tunnel.mu.Unlock()
	tunnel.noteSettingsChosen(settings)
	if settings.Mode == serverTunnelOwnAddress {
		tunnel.mu.Lock()
		tunnel.addressCheck = nil
		tunnel.mu.Unlock()
		tunnel.checkAddressSoon(settings.Address)
	}
	if settings.Mode == serverTunnelOff || settings.Mode == "" ||
		(settings.Mode == serverTunnelOwnAddress && (tunnel.app == nil || addressTLSPort(tunnel.app, settings.Address) == 0)) {
		return nil
	}
	// A server whose data moved elsewhere, or a paused primary that let its
	// address go, runs no remote access: the same device and keys answer at
	// the server that took over (replica_remote_access.go).
	if tunnel.app != nil && remoteAccessHeldBack(tunnel.app) {
		tunnel.set(serverTunnelStateOff, "", remoteAccessHeld(tunnel.app))
		return nil
	}
	token := ""
	if settings.Mode == serverTunnelToken {
		var err error
		if token, err = decryptAdministratorSetting(tunnel.app, settings.TokenCipher); err != nil || token == "" {
			tunnel.set(serverTunnelStateRetrying, "", failedTunnel(tunnelFailureToken, "The tunnel's token can't be read; enter it again."))
			return errors.New(errorCodeServerTunnelFailed)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	tunnel.mu.Lock()
	tunnel.cancel, tunnel.done = cancel, done
	tunnel.mu.Unlock()
	tunnel.set(serverTunnelStateStarting, "", noTunnelFailure)
	go func() {
		defer close(done)
		tunnel.supervise(ctx, settings, token)
	}()
	return nil
}

// supervise keeps cloudflared running until the tunnel is ended, starting it
// again after it stops, waiting longer each time it fails quickly.
func (tunnel *serverTunnel) supervise(ctx context.Context, settings serverTunnelSettings, token string) {
	wait := 2 * time.Second
	for ctx.Err() == nil {
		started := time.Now()
		failure := tunnel.runOnce(ctx, settings, token)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			wait = 2 * time.Second
		}
		tunnel.set(serverTunnelStateRetrying, "", failure)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, time.Minute)
	}
}

// startServerTunnel opens the tunnel the server was left with, once it serves.
func startServerTunnel(event *core.ServeEvent) {
	tunnel := serverTunnelOf(event.App)
	tunnel.localURL = "http://127.0.0.1:8090"
	if event.Server != nil {
		if host, port, err := net.SplitHostPort(event.Server.Addr); err == nil && port != "" {
			if host == "" || host == "0.0.0.0" || host == "::" {
				host = "127.0.0.1"
			}
			tunnel.localURL = "http://" + net.JoinHostPort(host, port)
		}
	}
	// Ended as the tunnel closes and waited for as the server stops (server_background.go).
	background := serverBackgroundOf(event.App)
	ctx, cancel := context.WithCancel(background.ctx)
	tunnel.mu.Lock()
	tunnel.followCancel = cancel
	tunnel.mu.Unlock()
	background.run("tunnel settings follow", func(context.Context) { tunnel.followSettings(ctx) })
	background.run("address forwarding", func(context.Context) { tunnel.keepForwardingWays(ctx, tunnel.forwardingChanged) })
	settings, err := loadServerTunnelSettings(event.App)
	if err != nil {
		return
	}
	noteChosenTunnelSettings(settings)
	tunnel.noteAddressChange(settings)
	if settings.Mode == serverTunnelOff {
		return
	}
	holdUntilOwnershipConfirmed(event.App, settings)
	_ = tunnel.apply(settings)
}

func registerServerTunnelRoutes(event *core.ServeEvent) {
	// The address a person came from is taken only through the tunnels'
	// door (server_tunnel_door.go); elsewhere the headers are anyone's claim and are dropped.
	registerAddressForwardingHook(event)
	event.Router.Bind(&hook.Handler[*core.RequestEvent]{
		Id: "leafenTunnelClientAddress", Priority: -100000,
		Func: func(e *core.RequestEvent) error {
			handTunnelAddress(e)
			return e.Next()
		},
	})
	// The hosts the server was opened at over https before (request_scheme.go).
	loadHTTPSHosts(event.App)
	event.Router.GET(apiRoot+serverTunnelPath, readServerTunnelRoute)
	event.Router.PUT(apiRoot+serverTunnelPath, changeServerTunnelRoute).Bind(apis.BodyLimit(serverTunnelTokenMaxBytes * 2))
	event.Router.POST(apiRoot+serverTunnelCertificateAlertPath+"/clear", clearAddressCertificateAlertRoute)
	event.Router.POST(apiRoot+serverTunnelPath+"/tailscale/logout", logoutTailscaleRoute)
	registerServerTunnelReachRoute(event)
	startServerTunnel(event)
}

func readServerTunnelRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	settings, err := loadServerTunnelSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't read the tunnel.", err)
	}
	return event.JSON(http.StatusOK, serverTunnelOf(event.App).status(settings))
}

func changeServerTunnelRoute(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	input := serverTunnelInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeServerTunnelInvalid, "Check the tunnel.")
	}
	current, err := loadServerTunnelSettings(event.App)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't change the tunnel.", err)
	}
	next := serverTunnelSettings{Mode: strings.TrimSpace(input.Mode), TokenCipher: current.TokenCipher, Address: current.Address,
		TailscaleAddress: current.TailscaleAddress}
	switch next.Mode {
	case serverTunnelOff, serverTunnelQuick:
	case serverTunnelOwnAddress:
		address := strings.TrimRight(strings.TrimSpace(input.Address), "/")
		parsed, err := url.Parse(address)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.User != nil ||
			parsed.RawQuery != "" || parsed.Fragment != "" {
			return apiError(event, http.StatusBadRequest, errorCodeServerTunnelInvalid,
				"Give your address as https://your-domain, the one your own proxy or port forwarding serves.")
		}
		next.Address = address
	case serverTunnelTailscale:
		if !tailscaleBuiltIn {
			return apiError(event, http.StatusBadRequest, errorCodeServerTunnelInvalid, "Tailscale isn't built into Leafen on phones; use it on an always-on server.")
		}
	case serverTunnelToken:
		address := strings.TrimRight(strings.TrimSpace(input.Address), "/")
		parsed, err := url.Parse(address)
		if address != "" {
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.User != nil ||
				parsed.RawQuery != "" || parsed.Fragment != "" {
				return apiError(event, http.StatusBadRequest, errorCodeServerTunnelInvalid,
					"Give the tunnel's address as https://your-domain, the hostname its Cloudflare route serves.")
			}
			next.Address = address
		}
		if token := strings.TrimSpace(input.Token); token != "" {
			if len(token) > serverTunnelTokenMaxBytes || strings.ContainsAny(token, " \r\n\t") {
				return apiError(event, http.StatusBadRequest, errorCodeServerTunnelInvalid, "Paste the tunnel's token as Cloudflare gives it.")
			}
			if next.TokenCipher, err = encryptAdministratorSetting(event.App, cipherServerTunnel, token); err != nil {
				return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't keep the token.", err)
			}
		}
		if next.TokenCipher == "" || next.Address == "" {
			return apiError(event, http.StatusBadRequest, errorCodeServerTunnelInvalid,
				"A tunnel of your own needs its token and the address its Cloudflare route serves.")
		}
	default:
		return apiError(event, http.StatusBadRequest, errorCodeServerTunnelInvalid, "Choose off, Tailscale, your own address, a quick tunnel or your own tunnel.")
	}
	next.Updated = time.Now().UnixMilli()
	if _, err := event.App.DB().NewQuery(`
		INSERT INTO {{leafen_server_tunnel}} ([[id]], [[mode]], [[token_cipher]], [[address]], [[updated]])
		VALUES ('tunnel', {:mode}, {:token}, {:address}, {:updated})
		ON CONFLICT ([[id]]) DO UPDATE SET [[mode]] = excluded.[[mode]], [[token_cipher]] = excluded.[[token_cipher]],
			[[address]] = excluded.[[address]], [[updated]] = excluded.[[updated]]
	`).Bind(dbx.Params{"mode": next.Mode, "token": next.TokenCipher, "address": next.Address, "updated": next.Updated}).Execute(); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeServerTunnelFailed, "Couldn't change the tunnel.", err)
	}
	tunnel := serverTunnelOf(event.App)
	_ = tunnel.apply(next)
	return event.JSON(http.StatusOK, tunnel.status(next))
}
