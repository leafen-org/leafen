package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"leafen.org/leafen/internal/netproxy"
)

var (
	errOutboundInvalidURL        = errors.New("outbound URL is invalid")
	errOutboundUnsafeDestination = errors.New("outbound destination is not public")
	outboundSharedAddressPrefix  = netip.MustParsePrefix("100.64.0.0/10")
)

func normalizeOutboundPublicURL(raw string, maxRunes int, stripFragment bool) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", refuse(errOutboundInvalidURL, "the URL is empty")
	}
	if maxRunes > 0 && utf8.RuneCountInString(trimmed) > maxRunes {
		return "", refuse(errOutboundInvalidURL, "the URL is over %d characters", maxRunes)
	}
	parsed, err := url.Parse(trimmed)
	switch {
	case err != nil || parsed.Opaque != "" || parsed.Host == "" || parsed.Hostname() == "":
		return "", refuse(errOutboundInvalidURL, "the URL isn't a web address with a host, such as https://example.com/")
	case parsed.User != nil:
		return "", refuse(errOutboundInvalidURL, "the URL holds a user name or password before its host")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", refuse(errOutboundInvalidURL, "the URL starts with https:// or http://, not %s:", parsed.Scheme)
	}
	if parsed.Fragment != "" && !stripFragment {
		return "", refuse(errOutboundInvalidURL, "the URL holds a #fragment")
	}
	parsed.Fragment = ""
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return "", refuse(errOutboundUnsafeDestination, "its host is this machine")
	}
	if address, parseErr := netip.ParseAddr(hostname); parseErr == nil && !outboundAddressIsPublic(address) {
		return "", refuse(errOutboundUnsafeDestination, "its host %s is this machine or a private network's address", hostname)
	}
	port := parsed.Port()
	if port != "" {
		number, parseErr := strconv.Atoi(port)
		if parseErr != nil || number < 1 || number > 65535 {
			return "", refuse(errOutboundInvalidURL, "the URL's port is from 1 to 65535, not %s", port)
		}
		if parsed.Scheme == "http" && number == 80 || parsed.Scheme == "https" && number == 443 {
			port = ""
		}
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

// outboundAddressIsPublic says whether the server may fetch from an address
// for a link someone gave it: a global unicast one outside the private and
// shared (100.64.0.0/10) ranges. It is a safety rule against reaching the
// server's own network from a link, not the network reach table's question
// of who opens an address (network_reach.go): anything not plainly public
// stays refused whatever that table holds.
func outboundAddressIsPublic(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsValid() &&
		address.IsGlobalUnicast() &&
		!address.IsPrivate() &&
		!outboundSharedAddressPrefix.Contains(address)
}

func outboundPublicDialContext(
	ctx context.Context,
	network, address, allowedLoopbackHost string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	// The proxy in force is the administrator's, the environment's or the
	// system's, and is dialed as it is: outboundPublicProxy checked the
	// destination first.
	if netproxy.IsProxy(address) {
		return dial(ctx, network, address)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid address", errOutboundUnsafeDestination)
	}
	addresses := []netip.Addr{}
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		addresses = append(addresses, literal)
	} else {
		addresses, err = lookup(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%w: no resolved address", errOutboundUnsafeDestination)
	}
	allowLoopback := address == allowedLoopbackHost
	for _, candidate := range addresses {
		candidate = candidate.Unmap()
		if !(outboundAddressIsPublic(candidate) || allowLoopback && candidate.IsLoopback()) {
			return nil, fmt.Errorf("%w: rejected address", errOutboundUnsafeDestination)
		}
	}
	var dialErrors []error
	for _, candidate := range addresses {
		connection, dialErr := dial(ctx, network, net.JoinHostPort(candidate.Unmap().String(), port))
		if dialErr == nil {
			return connection, nil
		}
		dialErrors = append(dialErrors, dialErr)
	}
	return nil, errors.Join(dialErrors...)
}

// outboundPublicProxy is a public-only client's proxy (netproxy): the proxy
// in force for a destination that resolves here to public addresses, or
// doesn't resolve here at all — a company network may leave names out to
// its proxy alone. A name that resolves here to another address is
// refused, and so is the proxy itself as a destination.
func outboundPublicProxy(request *http.Request) (*url.URL, error) {
	proxy, err := netproxy.ForRequest(request)
	host := request.URL.Hostname()
	if err != nil || proxy == nil {
		port := request.URL.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443"}[request.URL.Scheme]
		}
		if netproxy.IsProxy(net.JoinHostPort(host, port)) {
			return nil, fmt.Errorf("%w: the proxy", errOutboundUnsafeDestination)
		}
		return nil, err
	}
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		if !outboundAddressIsPublic(literal.Unmap()) {
			return nil, fmt.Errorf("%w: rejected address", errOutboundUnsafeDestination)
		}
		return proxy, nil
	}
	if addresses, lookupErr := net.DefaultResolver.LookupNetIP(request.Context(), "ip", host); lookupErr == nil {
		for _, address := range addresses {
			if !outboundAddressIsPublic(address.Unmap()) {
				return nil, fmt.Errorf("%w: rejected address", errOutboundUnsafeDestination)
			}
		}
	}
	return proxy, nil
}
