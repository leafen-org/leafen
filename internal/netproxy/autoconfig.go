package netproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"
)

// An automatic configuration script (PAC, or one the network gives by
// WPAD) says each address's proxy. The system's own evaluator reads it —
// CFNetwork on macOS, WinHTTP on Windows (autoconfig_*.go) — and its answers
// are kept a short while, by scheme and host.

// autoProxies is this platform's evaluator: the proxies the system's
// automatic configuration gives an address, in its order, nil for going
// directly. A build without one leaves it nil, and the setting unfollowed.
var autoProxies func(target *url.URL, script string, discover bool) ([]*url.URL, error)

const (
	// autoAnswerKept is how long an address's answer is kept.
	autoAnswerKept = time.Minute
	// autoScriptKept is how long a script fetched from its address is kept.
	autoScriptKept  = 5 * time.Minute
	autoScriptMost  = 1 << 20
	autoAnswersMost = 1024
)

type autoAnswer struct {
	proxy *url.URL
	ok    bool
	at    time.Time
}

var (
	autoMu      sync.Mutex
	autoAnswers = map[string]autoAnswer{}
	// autoNamed is the proxies the scripts named, which a client that
	// reaches only public addresses may still dial (IsProxy).
	autoNamed  = map[string]time.Time{}
	autoScript struct {
		address string
		text    string
		err     error
		at      time.Time
	}
)

// autoProxyFor is the proxy the system's automatic configuration gives an
// address: the first it names that Leafen can use, nil for directly; ok is
// false when it gives none, and the fixed proxies decide.
func autoProxyFor(target *url.URL, state inForce) (*url.URL, bool) {
	key := target.Scheme + "://" + strings.ToLower(target.Host)
	autoMu.Lock()
	kept, found := autoAnswers[key]
	autoMu.Unlock()
	if found && time.Since(kept.at) < autoAnswerKept {
		return kept.proxy, kept.ok
	}
	answer := autoAnswer{at: time.Now()}
	if proxies, err := autoProxies(target, state.autoConfig, state.autoDiscover); err == nil {
		for _, proxy := range proxies {
			if proxy == nil || proxy.Scheme == "http" || proxy.Scheme == "https" {
				answer.proxy, answer.ok = proxy, true
				break
			}
		}
	}
	autoMu.Lock()
	if len(autoAnswers) >= autoAnswersMost {
		autoAnswers = map[string]autoAnswer{}
	}
	autoAnswers[key] = answer
	if answer.proxy != nil {
		autoNamed[hostPort(answer.proxy)] = answer.at
	}
	autoMu.Unlock()
	return answer.proxy, answer.ok
}

// autoProxyNamed says whether a script named host:port as a proxy lately.
func autoProxyNamed(address string) bool {
	autoMu.Lock()
	defer autoMu.Unlock()
	for named, at := range autoNamed {
		if time.Since(at) > autoScriptKept {
			delete(autoNamed, named)
			continue
		}
		if sameAddress(named, address) {
			return true
		}
	}
	return false
}

// bypassed says whether an address is reached directly whatever the script
// says: the loopback, and the system's exceptions.
func bypassed(target *url.URL, noProxy string) bool {
	probe := httpproxy.Config{HTTPProxy: "http://probe.invalid:1", HTTPSProxy: "http://probe.invalid:1", NoProxy: noProxy}
	proxy, err := probe.ProxyFunc()(target)
	return err == nil && proxy == nil
}

var errAutoScript = errors.New("the automatic configuration script couldn't be read")

// fetchAutoScript is the script at an address — http(s):// fetched
// directly, never by a proxy, or file:// — kept a short while.
func fetchAutoScript(address string) (string, error) {
	autoMu.Lock()
	if autoScript.address == address && time.Since(autoScript.at) < autoScriptKept {
		text, err := autoScript.text, autoScript.err
		autoMu.Unlock()
		return text, err
	}
	autoMu.Unlock()
	text, err := readAutoScript(address)
	autoMu.Lock()
	autoScript.address, autoScript.text, autoScript.err, autoScript.at = address, text, err, time.Now()
	autoMu.Unlock()
	return text, err
}

func readAutoScript(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", errAutoScript
	}
	var body io.ReadCloser
	switch parsed.Scheme {
	case "file":
		file, err := os.Open(parsed.Path)
		if err != nil {
			return "", errAutoScript
		}
		body = file
	case "http", "https":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return "", errAutoScript
		}
		client := &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext}}
		response, err := client.Do(request)
		if err != nil {
			return "", errAutoScript
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return "", errAutoScript
		}
		body = response.Body
	default:
		return "", errAutoScript
	}
	defer body.Close()
	text, err := io.ReadAll(io.LimitReader(body, autoScriptMost+1))
	if err != nil || len(text) > autoScriptMost {
		return "", errAutoScript
	}
	return string(text), nil
}
