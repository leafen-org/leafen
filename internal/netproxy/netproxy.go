// Package netproxy is the proxy Leafen's requests out go through, so a
// company's network that lets out only its proxy works with nothing to set
// (the bots-as-accounts decision, 2026-10-21 18:00): Leafen's own setting
// where one is set — the desktop app's, or a server administrator's — else
// the environment's (HTTPS_PROXY, HTTP_PROXY, NO_PROXY), else the system's
// (macOS's and Windows' proxy settings, an automatic configuration script
// among them, which the system's own evaluator reads). The loopback is
// always reached directly. Processes Leafen starts — agents and their commands — take the
// same proxy by the environment; a proxy's password reaches them through a
// relay on the loopback, never in their environment.
package netproxy

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"
)

// Config is a proxy Leafen is given.
type Config struct {
	// URL is the proxy, http://host:port, with user:password@ where it
	// asks for them.
	URL string `json:"url"`
	// NoProxy names the hosts reached directly, as NO_PROXY does: names
	// (".corp.example" for its subdomains), addresses and CIDR ranges,
	// separated by commas.
	NoProxy string `json:"noProxy,omitempty"`
}

// Where the proxy in force comes from.
const (
	SourceNone        = ""
	SourceLeafen      = "leafen"
	SourceEnvironment = "environment"
	SourceSystem      = "system"
)

// State describes the proxy in force, with no password.
type State struct {
	Source string `json:"source"`
	// The proxies for http:// and https:// addresses, host:port.
	HTTP  string `json:"http,omitempty"`
	HTTPS string `json:"https,omitempty"`
	// NoProxy is reached directly.
	NoProxy string `json:"noProxy,omitempty"`
	// AutoConfig is the address of the system's automatic configuration
	// script Leafen follows; AutoDiscover, one the network gives (WPAD).
	AutoConfig   string `json:"autoConfig,omitempty"`
	AutoDiscover bool   `json:"autoDiscover,omitempty"`
	// Unfollowed names a system setting Leafen doesn't follow: an
	// automatic configuration script ("autoconfig") where this build reads
	// none, or a SOCKS proxy ("socks"), with no proxy Leafen follows beside
	// it.
	Unfollowed string `json:"unfollowed,omitempty"`
}

// systemSettings is what the system's settings say, as read on this
// platform (system_*.go).
type systemSettings struct {
	config httpproxy.Config
	// The system reaches names without a dot directly.
	simpleDirect bool
	// The automatic configuration script the system follows, and whether it
	// finds one on the network.
	autoConfig   string
	autoDiscover bool
	unfollowed   string
}

// How long the system's settings are read again after.
const systemRead = 30 * time.Second

var (
	mu       sync.Mutex
	own      *Config
	ownProxy *url.URL
	system   systemSettings
	readAt   time.Time
	// readSystem reads the system's settings; it is a variable so a
	// platform without any reads none.
	readSystem = platformSettings
)

var errProxy = errors.New("check the proxy: an http:// or https:// address with its port")

// Check reads a proxy a person gives.
func Check(config Config) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(config.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.Port() == "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errProxy
	}
	parsed.Path = ""
	return parsed, nil
}

// Set gives Leafen's own proxy; nil follows the environment and the system.
func Set(config *Config) error {
	var parsed *url.URL
	if config != nil {
		var err error
		if parsed, err = Check(*config); err != nil {
			return err
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if config == nil {
		own, ownProxy = nil, nil
	} else {
		kept := *config
		own, ownProxy = &kept, parsed
	}
	return nil
}

// inForce is the proxy rules in force and where they come from.
type inForce struct {
	config       httpproxy.Config
	simpleDirect bool
	source       string
	unfollowed   string
	// The system's automatic configuration, which decides each address's
	// proxy before the fixed ones in config (autoconfig.go).
	auto         bool
	autoConfig   string
	autoDiscover bool
}

// current is the proxy rules in force.
func current() inForce {
	mu.Lock()
	defer mu.Unlock()
	if own != nil {
		proxy := ownProxy.String()
		return inForce{config: httpproxy.Config{HTTPProxy: proxy, HTTPSProxy: proxy, NoProxy: own.NoProxy}, source: SourceLeafen}
	}
	if env := httpproxy.FromEnvironment(); env.HTTPProxy != "" || env.HTTPSProxy != "" {
		return inForce{config: *env, source: SourceEnvironment}
	}
	if time.Since(readAt) > systemRead {
		system, readAt = readSystem(), time.Now()
	}
	state := inForce{config: system.config, simpleDirect: system.simpleDirect, source: SourceSystem}
	if system.autoConfig != "" || system.autoDiscover {
		if autoProxies == nil {
			// This build has no evaluator: the fixed proxies, if any.
			state.unfollowed = "autoconfig"
		} else {
			state.auto, state.autoConfig, state.autoDiscover = true, system.autoConfig, system.autoDiscover
			return state
		}
	}
	if system.config.HTTPProxy != "" || system.config.HTTPSProxy != "" {
		state.unfollowed = ""
		return state
	}
	if state.unfollowed == "" {
		state.unfollowed = system.unfollowed
	}
	return inForce{source: SourceNone, unfollowed: state.unfollowed}
}

// ForURL is the proxy for an address, or nil to reach it directly.
func ForURL(target *url.URL) (*url.URL, error) {
	state := current()
	if state.simpleDirect && !strings.Contains(target.Hostname(), ".") {
		return nil, nil
	}
	if state.auto && !bypassed(target, state.config.NoProxy) {
		if proxy, ok := autoProxyFor(target, state); ok {
			return proxy, nil
		}
	}
	return state.config.ProxyFunc()(target)
}

// ForRequest is ForURL for an http.Transport's Proxy.
func ForRequest(request *http.Request) (*url.URL, error) { return ForURL(request.URL) }

// Install makes the proxy the default transport's, which http.Get and the
// clients cloned from it after use.
func Install() {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport.Proxy = ForRequest
	}
}

// Current describes the proxy in force.
func Current() State {
	state := current()
	return State{Source: state.source, HTTP: hostOf(state.config.HTTPProxy), HTTPS: hostOf(state.config.HTTPSProxy), NoProxy: state.config.NoProxy,
		AutoConfig: state.autoConfig, AutoDiscover: state.autoDiscover, Unfollowed: state.unfollowed}
}

// IsProxy says whether host:port is the address of a proxy in force, which
// a client that reaches only public addresses may still dial.
func IsProxy(address string) bool {
	if autoProxyNamed(address) {
		return true
	}
	config := current().config
	for _, proxy := range []string{config.HTTPProxy, config.HTTPSProxy} {
		if parsed, err := url.Parse(proxy); err == nil && parsed.Host != "" && sameAddress(hostPort(parsed), address) {
			return true
		}
	}
	return false
}

// Env is the environment a process Leafen starts takes to go out the same
// way: the proxy in force, with the loopback reached directly, which tools
// that read these variables don't all leave out by themselves. A proxy's
// password goes by the relay. With no proxy in force it is none.
func Env() []string {
	state := current()
	config, source := state.config, state.source
	if source == SourceNone {
		return nil
	}
	// The environment can't say a script's answers: they go by the relay,
	// which asks it for each address.
	if state.auto {
		relay, err := relayAddress()
		if err != nil {
			return nil
		}
		env := appendBoth(nil, "HTTP_PROXY", relay)
		env = appendBoth(env, "HTTPS_PROXY", relay)
		env = appendBoth(env, "NO_PROXY", withLoopback(""))
		return append(env, "NODE_USE_ENV_PROXY=1")
	}
	env := []string{}
	if source != SourceEnvironment {
		httpProxy, httpsProxy := config.HTTPProxy, config.HTTPSProxy
		if source == SourceLeafen && ownProxy.User != nil {
			relay, err := relayAddress()
			if err != nil {
				return nil
			}
			httpProxy, httpsProxy = relay, relay
		}
		env = appendBoth(env, "HTTP_PROXY", httpProxy)
		env = appendBoth(env, "HTTPS_PROXY", httpsProxy)
	}
	env = appendBoth(env, "NO_PROXY", withLoopback(config.NoProxy))
	// Node's own fetch follows these only when told (Node 24.5 and 22.21).
	return append(env, "NODE_USE_ENV_PROXY=1")
}

func appendBoth(env []string, name, value string) []string {
	if value == "" {
		return env
	}
	return append(env, name+"="+value, strings.ToLower(name)+"="+value)
}

func withLoopback(noProxy string) string {
	if strings.TrimSpace(noProxy) == "" {
		return "localhost,127.0.0.1,::1"
	}
	return noProxy + ",localhost,127.0.0.1,::1"
}

// WithEnv is env with the proxy's environment in place of what it had.
func WithEnv(env []string) []string { return ReplaceEnv(env, Env()) }

// ReplaceEnv is env with proxy's variables in place of the same ones it
// had, in either case.
func ReplaceEnv(env, proxy []string) []string {
	if len(proxy) == 0 {
		return env
	}
	named := map[string]bool{}
	for _, entry := range proxy {
		name, _, _ := strings.Cut(entry, "=")
		named[strings.ToUpper(name)] = true
	}
	out := make([]string, 0, len(env)+len(proxy))
	for _, entry := range env {
		if name, _, _ := strings.Cut(entry, "="); !named[strings.ToUpper(name)] {
			out = append(out, entry)
		}
	}
	return append(out, proxy...)
}

// InForce says whether a proxy is in force, from any source.
func InForce() bool {
	return current().source != SourceNone
}

func hostOf(proxy string) string {
	if parsed, err := url.Parse(proxy); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return proxy
}

// hostPort is a proxy's host:port, its scheme's port where it names none.
func hostPort(proxy *url.URL) string {
	if proxy.Port() != "" {
		return proxy.Host
	}
	port := "80"
	if proxy.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(proxy.Hostname(), port)
}

func sameAddress(a, b string) bool {
	hostA, portA, errA := net.SplitHostPort(a)
	hostB, portB, errB := net.SplitHostPort(b)
	return errA == nil && errB == nil && portA == portB && strings.EqualFold(hostA, hostB)
}
