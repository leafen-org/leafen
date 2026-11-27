package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The tunnels' door (2026-10-18): the address a person came from is taken
// from a tunnel's header only when the request came through a way no one
// else's request can — not from any request that merely comes from this
// computer, as a reverse proxy beside the server (Nginx, Caddy) passes on
// whatever header a person sends. The proxies for Tailscale and for the
// administrator's own certificate run in this process, and cloudflared
// reaches the server at a door of its own, a listener on this computer's
// loopback that no other path uses; all hand the address on with a secret
// this process made at start. Every other request loses the headers.

const (
	tunnelDoorHeader = "Leafen-Tunnel-Door"
	// tunnelClientAddressHeader carries the address a person came from, set
	// by this process's own proxies.
	tunnelClientAddressHeader = "Leafen-Tunnel-Client-Ip"
	tunnelDoorPortEnv         = "LEAFEN_TUNNEL_DOOR_PORT"
	// A tunnel of the administrator's own still reaching the server's port
	// rather than its door is shown for this long after.
	tunnelDoorMissedShown = 10 * time.Minute
)

// tunnelDoorSecret is this process's: only its own paths know it.
var tunnelDoorSecret = func() string {
	secret, err := randomHex(32)
	if err != nil {
		panic("leafen: no randomness for the tunnel's door")
	}
	return secret
}()

const tunnelDoorStoreKey = "leafenTunnelDoor"

var errTunnelDoorServerStopping = errors.New("the server is stopping")

type tunnelDoor struct {
	once sync.Once
	url  string
	err  error
}

// tunnelDoorOf opens the door once for the server at localURL, answering its
// address: the port after the server's own where it is free (one a
// tunnel of the administrator's own can name for good), or any. It stays
// open until the server stops.
func tunnelDoorOf(app core.App, localURL string) (string, error) {
	door, _ := app.Store().GetOrSet(tunnelDoorStoreKey, func() any { return &tunnelDoor{} }).(*tunnelDoor)
	door.once.Do(func() {
		target, err := url.Parse(localURL)
		if err != nil {
			door.err = err
			return
		}
		listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(tunnelDoorPort(target))))
		if err != nil {
			listener, err = net.Listen("tcp", "127.0.0.1:0")
		}
		if err != nil {
			door.err = err
			return
		}
		proxy := &httputil.ReverseProxy{FlushInterval: -1, Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Host = request.In.Host
			client := net.ParseIP(strings.TrimSpace(request.In.Header.Get("Cf-Connecting-Ip")))
			for _, header := range []string{"Cf-Connecting-Ip", tunnelClientAddressHeader, tunnelDoorHeader} {
				request.Out.Header.Del(header)
			}
			if client != nil {
				request.Out.Header.Set(tunnelClientAddressHeader, client.String())
			}
			request.Out.Header.Set(tunnelDoorHeader, tunnelDoorSecret)
		}}
		server := &http.Server{Handler: proxy, ReadHeaderTimeout: 30 * time.Second}
		// The door closes as the server stops, letting go of its port for
		// a server started again in the same process (server_background.go);
		// one stopping opens none.
		if !serverBackgroundOf(app).run("tunnels' door", func(ctx context.Context) {
			stop := context.AfterFunc(ctx, func() { _ = server.Close() })
			defer stop()
			_ = server.Serve(listener)
		}) {
			_ = listener.Close()
			door.err = errTunnelDoorServerStopping
			return
		}
		door.url = "http://" + listener.Addr().String()
	})
	return door.url, door.err
}

func tunnelDoorPort(target *url.URL) int {
	if port, err := strconv.Atoi(strings.TrimSpace(os.Getenv(tunnelDoorPortEnv))); err == nil && port > 0 && port < 65536 {
		return port
	}
	if port, err := strconv.Atoi(target.Port()); err == nil && port > 0 && port < 65535 {
		return port + 1
	}
	return 0
}

// handTunnelAddress takes a person's address from a request that came
// through the door or this process's own proxies, and drops the headers
// from every request; a request from this computer carrying cloudflared's
// header without the door is a tunnel pointed at the server's own port,
// which the screen asks to point at the door.
func handTunnelAddress(e *core.RequestEvent) {
	host, _, _ := net.SplitHostPort(e.Request.RemoteAddr)
	peer := net.ParseIP(host)
	loopback := peer != nil && peer.IsLoopback()
	door := e.Request.Header.Get(tunnelDoorHeader)
	switch {
	case loopback && door != "" && subtle.ConstantTimeCompare([]byte(door), []byte(tunnelDoorSecret)) == 1:
		if client := net.ParseIP(strings.TrimSpace(e.Request.Header.Get(tunnelClientAddressHeader))); client != nil {
			e.Request.RemoteAddr = net.JoinHostPort(client.String(), "0")
		} else {
			// From outside all the same, though its loopback peer says this computer (setup_same_network.go).
			markTunnelClientUnknown(e)
		}
	case loopback && e.Request.Header.Get("Cf-Connecting-Ip") != "" && runningTunnelOrigin() != "":
		serverTunnelOf(e.App).noteDoorMissed()
	}
	for _, header := range []string{"Cf-Connecting-Ip", tunnelClientAddressHeader, tunnelDoorHeader} {
		e.Request.Header.Del(header)
	}
}

// noteDoorMissed keeps when a tunnel last reached the server's own port.
func (tunnel *serverTunnel) noteDoorMissed() {
	tunnel.mu.Lock()
	defer tunnel.mu.Unlock()
	tunnel.doorMissed = time.Now()
}
