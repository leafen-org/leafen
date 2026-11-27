package netproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// A relay is a proxy on the loopback that goes on the way ForURL says: by
// the proxy in force, with its password, or directly where that one isn't
// used. Processes Leafen starts go through one when Leafen's own proxy has
// a password, so they never hold it. One may let through only some hosts.

// Relay is a relay serving on the loopback.
type Relay struct {
	// URL is the relay's address, http://127.0.0.1:port.
	URL      string
	allow    func(host string) bool
	server   *http.Server
	listener net.Listener
}

// StartRelay starts a relay; allow, when given, says which hosts it lets
// through.
func StartRelay(allow func(host string) bool) (*Relay, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	relay := &Relay{URL: "http://" + listener.Addr().String(), allow: allow, listener: listener}
	relay.server = &http.Server{Handler: relay, ReadHeaderTimeout: 30 * time.Second}
	go func() { _ = relay.server.Serve(listener) }()
	return relay, nil
}

// Close stops the relay and the tunnels through it.
func (relay *Relay) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = relay.server.Shutdown(ctx)
	_ = relay.server.Close()
}

// Env is the environment that sends a process through the relay.
func (relay *Relay) Env() []string {
	env := appendBoth(nil, "HTTP_PROXY", relay.URL)
	env = appendBoth(env, "HTTPS_PROXY", relay.URL)
	env = appendBoth(env, "NO_PROXY", withLoopback(""))
	return append(env, "NODE_USE_ENV_PROXY=1")
}

var (
	sharedOnce  sync.Once
	sharedRelay *Relay
	sharedErr   error
)

// relayAddress is the relay processes take for Leafen's own proxy's
// password, started the first time one needs it.
func relayAddress() (string, error) {
	sharedOnce.Do(func() { sharedRelay, sharedErr = StartRelay(nil) })
	if sharedErr != nil {
		return "", sharedErr
	}
	return sharedRelay.URL, nil
}

func (relay *Relay) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	target := request.URL
	if request.Method == http.MethodConnect {
		target = &url.URL{Scheme: "https", Host: request.Host}
	} else if !target.IsAbs() {
		http.Error(writer, "this is a proxy", http.StatusBadRequest)
		return
	}
	if relay.allow != nil && !relay.allow(strings.ToLower(target.Hostname())) {
		http.Error(writer, "this chat may not reach "+target.Hostname(), http.StatusForbidden)
		return
	}
	proxy, err := ForURL(target)
	if err != nil {
		http.Error(writer, "the proxy setting can't be read", http.StatusBadGateway)
		return
	}
	if request.Method == http.MethodConnect {
		relayTunnel(writer, request, proxy)
		return
	}
	transport := &http.Transport{Proxy: func(*http.Request) (*url.URL, error) { return proxy, nil }, ResponseHeaderTimeout: 10 * time.Minute}
	defer transport.CloseIdleConnections()
	outgoing := request.Clone(request.Context())
	outgoing.RequestURI = ""
	for _, name := range []string{"Proxy-Connection", "Proxy-Authorization", "Connection", "Keep-Alive", "Te", "Trailer", "Upgrade"} {
		outgoing.Header.Del(name)
	}
	response, err := transport.RoundTrip(outgoing)
	if err != nil {
		http.Error(writer, "couldn't reach "+target.Hostname(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for name, values := range response.Header {
		writer.Header()[name] = values
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
}

// relayTunnel opens the tunnel a CONNECT asks for — through the proxy, with
// its password, or directly — and joins the two.
func relayTunnel(writer http.ResponseWriter, request *http.Request, proxy *url.URL) {
	conn, reader, err := tunnel(request.Context(), request.Host, proxy)
	if err != nil {
		status := http.StatusBadGateway
		var refused refusedError
		if errors.As(err, &refused) {
			status = int(refused)
		}
		http.Error(writer, err.Error(), status)
		return
	}
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		conn.Close()
		http.Error(writer, "can't tunnel here", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		conn.Close()
		return
	}
	_, _ = io.WriteString(client, "HTTP/1.1 200 Connection established\r\n\r\n")
	go func() {
		// What the client sent past its CONNECT goes first.
		if waiting := buffered.Reader.Buffered(); waiting > 0 {
			_, _ = io.CopyN(conn, buffered, int64(waiting))
		}
		_, _ = io.Copy(conn, client)
		conn.Close()
	}()
	_, _ = io.Copy(client, reader)
	client.Close()
}

type refusedError int

func (status refusedError) Error() string { return fmt.Sprintf("the proxy refused (%d)", int(status)) }

// tunnel is a connection to host:port, through the proxy when there is one;
// reader holds what came past the proxy's answer.
func tunnel(ctx context.Context, host string, proxy *url.URL) (net.Conn, io.Reader, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second}
	if proxy == nil {
		conn, err := dialer.DialContext(ctx, "tcp", host)
		if err != nil {
			return nil, nil, fmt.Errorf("couldn't reach %s", host)
		}
		return conn, conn, nil
	}
	conn, err := dialer.DialContext(ctx, "tcp", hostPort(proxy))
	if err != nil {
		return nil, nil, errors.New("the proxy didn't answer")
	}
	if proxy.Scheme == "https" {
		secured := tls.Client(conn, &tls.Config{ServerName: proxy.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secured.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, nil, errors.New("the proxy didn't answer")
		}
		conn = secured
	}
	ask := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", host, host)
	if proxy.User != nil {
		password, _ := proxy.User.Password()
		ask += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(proxy.User.Username()+":"+password)) + "\r\n"
	}
	if _, err := io.WriteString(conn, ask+"\r\n"); err != nil {
		conn.Close()
		return nil, nil, errors.New("the proxy didn't answer")
	}
	reader := bufio.NewReader(conn)
	answer, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		conn.Close()
		return nil, nil, errors.New("the proxy didn't answer")
	}
	answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		conn.Close()
		return nil, nil, refusedError(answer.StatusCode)
	}
	return conn, reader, nil
}
