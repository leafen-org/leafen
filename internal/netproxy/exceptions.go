package netproxy

import (
	"net"
	"strconv"
	"strings"
)

// noProxyOf turns a system's exceptions into NO_PROXY's form: "*.corp" and
// "*.corp.*"-like patterns to their name, "10.*" and "169.254/16" to CIDR
// ranges; what it can't read is left out.
func noProxyOf(exceptions []string) string {
	out := []string{}
	for _, entry := range exceptions {
		entry = strings.TrimSpace(entry)
		switch {
		case entry == "":
		case strings.Contains(entry, "/"):
			if cidr := cidrOf(entry); cidr != "" {
				out = append(out, cidr)
			}
		case strings.HasSuffix(entry, ".*") || entry == "*":
			if cidr := wildcardCIDR(entry); cidr != "" {
				out = append(out, cidr)
			}
		case strings.HasPrefix(entry, "*."):
			out = append(out, entry[1:])
		case strings.HasPrefix(entry, "*"):
			out = append(out, "."+strings.TrimLeft(entry, "*."))
		default:
			out = append(out, entry)
		}
	}
	return strings.Join(out, ",")
}

// cidrOf reads a range, its address shortened as macOS writes it
// ("169.254/16").
func cidrOf(entry string) string {
	address, bits, _ := strings.Cut(entry, "/")
	parts := strings.Split(address, ".")
	if strings.Contains(address, ":") {
		if _, network, err := net.ParseCIDR(entry); err == nil {
			return network.String()
		}
		return ""
	}
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	if _, network, err := net.ParseCIDR(strings.Join(parts, ".") + "/" + bits); err == nil {
		return network.String()
	}
	return ""
}

// wildcardCIDR reads "10.*" or "192.168.*" as the range it names.
func wildcardCIDR(entry string) string {
	parts := strings.Split(strings.TrimSuffix(entry, ".*"), ".")
	if entry == "*" || len(parts) > 3 {
		return ""
	}
	for _, part := range parts {
		if number, err := strconv.Atoi(part); err != nil || number < 0 || number > 255 {
			return ""
		}
	}
	bits := len(parts) * 8
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".") + "/" + strconv.Itoa(bits)
}

// joinPort is host:port, the given port or the fallback.
func joinPort(host, port, fallback string) string {
	if port == "" || port == "0" {
		port = fallback
	}
	return net.JoinHostPort(host, port)
}
