package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"leafen.org/leafen/internal/netproxy"
)

// The relay (docs/app-platform.md 7, T3·T5): an app reaches an outside
// address through Leafen when the address needs a secret, is on the
// private network, or the server allows nothing else. The address must
// fall under one of the app's approved relay entries — same scheme, host
// and port, a path under the entry's — and so must every redirect. Each
// connection is checked by the address it actually reaches: public only,
// unless the entry was approved for the private network, which still never
// reaches loopback, link-local or metadata addresses or Leafen's own port.
// Secrets go only into headers by name, never into the address or body,
// and are masked if they come back.

var (
	errAppRelayDenied  = errors.New(errorCodeAppRelayDenied)
	errAppRelayFailed  = errors.New(errorCodeAppRelayFailed)
	errAppRelayLimited = errors.New(errorCodeAppRelayLimited)

	appRelaySecretPattern  = regexp.MustCompile(`\{\{secret:([A-Z][A-Z0-9_]{0,63})\}\}`)
	appRelayOAuthPattern   = regexp.MustCompile(`\{\{oauth:([a-z][a-zA-Z0-9_-]{0,47})\}\}`)
	appRelayMetadataV6     = netip.MustParseAddr("fd00:ec2::254")
	appRelayMethods        = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	appRelayDroppedHeaders = map[string]bool{
		"host": true, "connection": true, "content-length": true, "transfer-encoding": true, "upgrade": true,
		"keep-alive": true, "te": true, "trailer": true, "forwarded": true, "proxy-authorization": true,
		"proxy-connection": true, "set-cookie": true,
	}

	// The server's own port, which the relay never reaches on this machine.
	appRelayOwnPort atomic.Int32
	// Lookups and dials, replaceable where a check needs a synthetic network.
	appRelayLookup = net.DefaultResolver.LookupNetIP
	appRelayDial   = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	appRelayTLS    *tls.Config

	appRelayLimiter = newAppRelayLimits(appRelayPerMinute, appRelayConcurrent)
)

type appRelayWindow struct {
	started time.Time
	count   int
}

type appRelayLimits struct {
	mu                    sync.Mutex
	windows               map[string]*appRelayWindow
	running               map[string]int
	perMinute, concurrent int
}

func newAppRelayLimits(perMinute, concurrent int) *appRelayLimits {
	return &appRelayLimits{windows: map[string]*appRelayWindow{}, running: map[string]int{}, perMinute: perMinute, concurrent: concurrent}
}

// admit counts a call against the app's and person's minute and the app's
// calls under way; the returned release ends the call.
func (limits *appRelayLimits) admit(installID, personID string, now time.Time) (func(), bool) {
	limits.mu.Lock()
	defer limits.mu.Unlock()
	key := installID + "\x00" + personID
	window := limits.windows[key]
	if window == nil || now.Sub(window.started) >= time.Minute {
		window = &appRelayWindow{started: now}
		limits.windows[key] = window
	}
	if window.count >= limits.perMinute || limits.running[installID] >= limits.concurrent {
		return nil, false
	}
	window.count++
	limits.running[installID]++
	if len(limits.windows) > 10000 {
		for name, stale := range limits.windows {
			if now.Sub(stale.started) >= time.Minute {
				delete(limits.windows, name)
			}
		}
	}
	return func() {
		limits.mu.Lock()
		limits.running[installID]--
		if limits.running[installID] <= 0 {
			delete(limits.running, installID)
		}
		limits.mu.Unlock()
	}, true
}

type appRelayCall struct {
	App      core.App
	Install  storedAppInstall
	Manifest appManifest
	PersonID string
	Strict   bool
	// Tenancy is the organization the call is for, whose data rules its
	// outgoing body meets.
	Tenancy string
	// Fills the credentials of the person the call is for, if any.
	Credentials *appCredentialFiller
}

type appRelayLogEntry struct {
	method, host, path, outcome, requestType string
	status                                   int
	requestBytes, responseBytes              int
}

// runAppRelay sends one request for an app and returns the answer, masked.
func runAppRelay(ctx context.Context, call appRelayCall, input appRelayInput) (appRelayResponse, error) {
	started := time.Now()
	entry := appRelayLogEntry{method: strings.ToUpper(input.Method), outcome: "denied"}
	defer func() {
		_, _ = call.App.DB().Insert(appRelayLogTable, dbx.Params{
			"install_id": call.Install.ID, "person_id": call.PersonID, "method": entry.method, "host": entry.host,
			"path": entry.path, "status": entry.status, "outcome": entry.outcome, "request_bytes": entry.requestBytes,
			"request_type": entry.requestType, "response_bytes": entry.responseBytes,
			"duration_ms": time.Since(started).Milliseconds(), "created": time.Now().UnixMilli(),
		}).Execute()
	}()

	target, err := url.Parse(input.URL)
	if err != nil || !appRelayMethods[entry.method] {
		return appRelayResponse{}, fmt.Errorf("%w: a method and an address", errAppRelayDenied)
	}
	entry.host, entry.path = target.Host, target.EscapedPath()
	body := []byte(input.Body)
	if input.BodyBase64 {
		if body, err = base64.StdEncoding.DecodeString(input.Body); err != nil {
			return appRelayResponse{}, fmt.Errorf("%w: the body isn't base64", errAppRelayDenied)
		}
	}
	entry.requestBytes = len(body)
	if len(body) > appRelayMaxRequestBytes {
		return appRelayResponse{}, fmt.Errorf("%w: the body is larger than %d bytes", errAppRelayDenied, appRelayMaxRequestBytes)
	}
	if strings.Contains(input.URL, "{{") || bytes.Contains(body, []byte("{{secret:")) || bytes.Contains(body, []byte("{{oauth:")) ||
		bytes.Contains(body, []byte("{{credential:")) {
		return appRelayResponse{}, fmt.Errorf("%w: secrets go only into headers", errAppRelayDenied)
	}
	relay, ok := appRelayEntryFor(call.Manifest, target)
	if !ok {
		return appRelayResponse{}, fmt.Errorf("%w: %s isn't under the app's approved relay addresses", errAppRelayDenied, target.Redacted())
	}
	secrets, err := appSecretValues(call.App, call.Install.ID)
	if err != nil {
		return appRelayResponse{}, err
	}
	// Sign-in tokens the request carries are masked in the answer as secrets are.
	signIn := func(key string) (string, error) {
		token, err := appOAuthAccessToken(ctx, call.App, call.Install, call.Manifest, key, call.PersonID)
		if err == nil {
			secrets["oauth:"+key] = token
		}
		return token, err
	}
	// A person's credentials go only to an address that names them; what is
	// filled is masked in the answer as secrets are.
	raw := input.Headers
	filledCredential := map[string]bool{}
	for name, value := range input.Headers {
		if !appMentionsCredential(value) {
			continue
		}
		if len(filledCredential) == 0 {
			raw = maps.Clone(input.Headers)
		}
		filled, err := call.Credentials.fill(value, relay.Credentials)
		if err != nil {
			return appRelayResponse{}, fmt.Errorf("%w: %s", errAppRelayDenied, strings.TrimPrefix(err.Error(), errAppCredentialRefused.Error()+": "))
		}
		raw[name] = filled
		filledCredential[http.CanonicalHeaderKey(strings.ToLower(strings.TrimSpace(name)))] = true
	}
	for index, value := range call.Credentials.values() {
		secrets[fmt.Sprintf("credential:%d", index)] = value
	}
	headers, carrying, err := appRelayHeaders(raw, relay, secrets, signIn)
	if err != nil {
		return appRelayResponse{}, err
	}
	for name := range filledCredential {
		carrying[name] = true
	}
	if call.Strict {
		entry.requestType = strings.ToLower(strings.TrimSpace(strings.Split(headers.Get("Content-Type"), ";")[0]))
	}
	// What leaves meets the organization's data rules (DLP).
	if organizationID := appTenancyOrganization(call.Tenancy); organizationID != "" {
		if err := enforceOrganizationDLPContent(call.App, organizationID, string(body), target.RawQuery); err != nil {
			if errors.Is(err, errDLPPolicyBlocked) {
				entry.outcome = "dlp"
			}
			return appRelayResponse{}, err
		}
	}
	release, admitted := appRelayLimiter.admit(call.Install.ID, call.PersonID, time.Now())
	if !admitted {
		entry.outcome = "limited"
		return appRelayResponse{}, errAppRelayLimited
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, appRelayTimeoutSeconds*time.Second)
	defer cancel()
	method := entry.method
	for redirects := 0; ; redirects++ {
		response, err := appRelaySend(ctx, relay, method, target, headers, body)
		if err != nil {
			entry.outcome = "failed"
			if errors.Is(err, errAppRelayDenied) {
				entry.outcome = "denied"
			}
			return appRelayResponse{}, err
		}
		location := response.Header.Get("Location")
		if response.StatusCode < 300 || response.StatusCode > 399 || location == "" {
			defer response.Body.Close()
			return appRelayAnswer(response, secrets, &entry)
		}
		_ = response.Body.Close()
		if redirects >= appRelayMaxRedirects {
			entry.outcome, entry.status = "failed", response.StatusCode
			return appRelayResponse{}, fmt.Errorf("%w: more than %d redirects", errAppRelayFailed, appRelayMaxRedirects)
		}
		next, err := target.Parse(location)
		if err != nil {
			entry.outcome = "failed"
			return appRelayResponse{}, fmt.Errorf("%w: a redirect without an address", errAppRelayFailed)
		}
		nextRelay, ok := appRelayEntryFor(call.Manifest, next)
		if !ok {
			return appRelayResponse{}, fmt.Errorf("%w: a redirect to %s, outside the approved addresses", errAppRelayDenied, next.Redacted())
		}
		// Filled secrets stay with the address they were meant for.
		if next.Scheme != target.Scheme || next.Host != target.Host {
			for name := range carrying {
				headers.Del(name)
			}
		}
		if response.StatusCode == http.StatusSeeOther || method == "POST" &&
			(response.StatusCode == http.StatusMovedPermanently || response.StatusCode == http.StatusFound) {
			method, body = "GET", nil
		}
		target, relay = next, nextRelay
		entry.host, entry.path = target.Host, target.EscapedPath()
	}
}

// appRelayEntryFor finds the approved entry an address falls under.
func appRelayEntryFor(manifest appManifest, target *url.URL) (appManifestRelay, bool) {
	if target.User != nil || target.Fragment != "" || target.Host == "" || target.Opaque != "" {
		return appManifestRelay{}, false
	}
	clean := target.EscapedPath()
	if clean == "" {
		clean = "/"
	}
	if decoded, err := url.PathUnescape(clean); err != nil || path.Clean(decoded) != strings.TrimSuffix(decoded, "/") && path.Clean(decoded) != decoded {
		return appManifestRelay{}, false
	}
	for _, relay := range manifest.Network.Relay {
		approved, err := url.Parse(relay.URL)
		if err != nil || !strings.EqualFold(approved.Scheme, target.Scheme) ||
			!appRelayHostMatches(approved.Hostname(), target.Hostname()) || appRelayPort(approved) != appRelayPort(target) {
			continue
		}
		prefix := strings.TrimSuffix(approved.EscapedPath(), "/")
		if prefix == "" || clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return relay, true
		}
	}
	return appManifestRelay{}, false
}

// appRelayHostMatches reports whether a host is the approved one, or — for
// an approved "*.example.com", as a service gives each customer a site of
// its own — one name more under it: "acme.example.com", not "example.com"
// or "a.b.example.com".
func appRelayHostMatches(approved, host string) bool {
	approved, host = strings.ToLower(approved), strings.ToLower(host)
	if domain, wildcard := strings.CutPrefix(approved, "*."); wildcard {
		name, under := strings.CutSuffix(host, "."+domain)
		return under && name != "" && !strings.Contains(name, ".")
	}
	return approved == host
}

func appRelayPort(address *url.URL) string {
	if port := address.Port(); port != "" {
		return port
	}
	if strings.EqualFold(address.Scheme, "http") {
		return "80"
	}
	return "443"
}

// appRelayHeaders keeps the app's headers but those a relay mustn't pass,
// filling each {{secret:NAME}} the entry declares; carrying names the
// headers a secret went into.
func appRelayHeaders(raw map[string]string, relay appManifestRelay, secrets map[string]string,
	signIn func(string) (string, error)) (http.Header, map[string]bool, error) {
	headers, carrying := http.Header{}, map[string]bool{}
	if len(raw) > 50 {
		return nil, nil, fmt.Errorf("%w: at most 50 headers", errAppRelayDenied)
	}
	for name, value := range raw {
		lower := strings.ToLower(strings.TrimSpace(name))
		if lower == "" || appRelayDroppedHeaders[lower] || strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "x-forwarded-") {
			continue
		}
		if len(value) > 8192 || strings.ContainsAny(value, "\r\n") {
			return nil, nil, fmt.Errorf("%w: header %s is too long or has line breaks", errAppRelayDenied, name)
		}
		var missing error
		value = appRelayOAuthPattern.ReplaceAllStringFunc(value, func(match string) string {
			key := appRelayOAuthPattern.FindStringSubmatch(match)[1]
			if !appOneOf(key, relay.OAuth) {
				missing = fmt.Errorf("%w: this address doesn't take the %s login", errAppRelayDenied, key)
				return ""
			}
			token, err := signIn(key)
			if err != nil {
				missing = err
				return ""
			}
			carrying[http.CanonicalHeaderKey(lower)] = true
			return token
		})
		if missing != nil {
			return nil, nil, missing
		}
		filled := appRelaySecretPattern.ReplaceAllStringFunc(value, func(match string) string {
			secretName := appRelaySecretPattern.FindStringSubmatch(match)[1]
			secret, set := secrets[secretName]
			if !appOneOf(secretName, relay.Secrets) || !set {
				missing = fmt.Errorf("%w: secret %s isn't set for this address", errAppRelayDenied, secretName)
				return ""
			}
			carrying[http.CanonicalHeaderKey(lower)] = true
			return secret
		})
		if missing != nil {
			return nil, nil, missing
		}
		if strings.Contains(filled, "{{secret:") || strings.Contains(filled, "{{oauth:") {
			return nil, nil, fmt.Errorf("%w: header %s names a secret or login in the wrong form", errAppRelayDenied, name)
		}
		headers.Set(name, filled)
	}
	return headers, carrying, nil
}

// appRelaySend makes one request, on a connection that reaches only what
// the entry allows.
func appRelaySend(ctx context.Context, relay appManifestRelay, method string, target *url.URL, headers http.Header, body []byte) (*http.Response, error) {
	transport := &http.Transport{
		// An entry that may reach a private network goes there directly;
		// the rest go by the proxy in force, as other public requests.
		Proxy: func(request *http.Request) (*url.URL, error) {
			if relay.PrivateNetwork {
				return nil, nil
			}
			return outboundPublicProxy(request)
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: appRelayTimeoutSeconds * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		TLSClientConfig:       appRelayTLS,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return appRelayDialContext(ctx, network, address, relay.PrivateNetwork)
		},
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errAppRelayDenied, err)
	}
	request.Header = headers.Clone()
	request.Header.Set("User-Agent", "Leafen-App-Relay")
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, errOutboundUnsafeDestination) {
			reason := "a place apps may not reach"
			if refused := new(appUnreachableError); errors.As(err, &refused) {
				reason = refused.reason
			}
			return nil, fmt.Errorf("%w: the address reaches %s", errAppRelayDenied, reason)
		}
		return nil, fmt.Errorf("%w: %v", errAppRelayFailed, errors.Unwrap(err))
	}
	return response, nil
}

// appRelayDialContext connects only to addresses the entry may reach,
// checked after the name resolves.
func appRelayDialContext(ctx context.Context, network, address string, private bool) (net.Conn, error) {
	// The proxy in force, the destination checked first (outboundPublicProxy).
	if !private && netproxy.IsProxy(address) {
		return appRelayDial(ctx, network, address)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errOutboundUnsafeDestination
	}
	addresses := []netip.Addr{}
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		addresses = append(addresses, literal)
	} else if addresses, err = appRelayLookup(ctx, "ip", host); err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errOutboundUnsafeDestination
	}
	for _, candidate := range addresses {
		if !appRelayAddressAllowed(candidate, port, private) {
			return nil, &appUnreachableError{reason: appRelayRefusal(candidate, port, private)}
		}
	}
	var failures []error
	for _, candidate := range addresses {
		connection, dialErr := appRelayDial(ctx, network, net.JoinHostPort(candidate.Unmap().String(), port))
		if dialErr == nil {
			return connection, nil
		}
		failures = append(failures, dialErr)
	}
	return nil, errors.Join(failures...)
}

// appRelayIsOwnPort reports an address of this machine at Leafen's own port.
func appRelayIsOwnPort(address netip.Addr, port string) bool {
	own := appRelayOwnPort.Load()
	if own == 0 || port != strconv.Itoa(int(own)) {
		return false
	}
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return true
	}
	for _, local := range interfaces {
		if prefix, err := netip.ParsePrefix(local.String()); err == nil && prefix.Addr().Unmap() == address {
			return true
		}
	}
	return false
}

// appRelayAnswer reads at most the response limit and masks any of the
// app's secrets it holds.
func appRelayAnswer(response *http.Response, secrets map[string]string, entry *appRelayLogEntry) (appRelayResponse, error) {
	entry.status = response.StatusCode
	body, err := io.ReadAll(io.LimitReader(response.Body, appRelayMaxResponseBytes+1))
	if err != nil {
		entry.outcome = "failed"
		return appRelayResponse{}, fmt.Errorf("%w: the answer broke off", errAppRelayFailed)
	}
	entry.responseBytes = len(body)
	if len(body) > appRelayMaxResponseBytes {
		entry.outcome = "failed"
		return appRelayResponse{}, fmt.Errorf("%w: the answer is larger than %d bytes", errAppRelayFailed, appRelayMaxResponseBytes)
	}
	mask := func(text []byte) []byte {
		for _, secret := range secrets {
			if len(secret) >= 4 {
				text = bytes.ReplaceAll(text, []byte(secret), []byte("[secret]"))
			}
		}
		return text
	}
	headers := map[string]string{}
	for name, values := range response.Header {
		lower := strings.ToLower(name)
		if appRelayDroppedHeaders[lower] {
			continue
		}
		headers[lower] = string(mask([]byte(strings.Join(values, ", "))))
	}
	body = mask(body)
	answer := appRelayResponse{Status: response.StatusCode, Headers: headers}
	if utf8.Valid(body) {
		answer.Body = string(body)
	} else {
		answer.Body, answer.BodyBase64 = base64.StdEncoding.EncodeToString(body), true
	}
	entry.outcome = "ok"
	return answer, nil
}

// pruneAppRelayLog keeps thirty days of relay records.
func pruneAppRelayLog(app core.App, now time.Time) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_app_relay_log}} WHERE [[created]] < {:before}`).
		Bind(dbx.Params{"before": now.Add(-30 * 24 * time.Hour).UnixMilli()}).Execute()
	return err
}
