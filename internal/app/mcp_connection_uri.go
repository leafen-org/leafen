package app

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const mcpConnectionURIVersion = "1"

var (
	errMCPConnectionURIInvalid                  = errors.New(errorCodeMCPStdioConfig)
	errMCPPublicDestinationConfirmationRequired = errors.New(
		"LEAFEN_MCP_PUBLIC_DESTINATION_CONFIRMATION_REQUIRED",
	)
	errMCPPublicDestinationHTTPSRequired = errors.New(
		"LEAFEN_MCP_PUBLIC_DESTINATION_HTTPS_REQUIRED",
	)
	errMCPStdioCrossOriginRedirect = errors.New("LEAFEN_MCP_CROSS_ORIGIN_REDIRECT_DENIED")
)

func newMCPConnectionURI(endpoint string, key string) (string, error) {
	parsed, err := parseMCPTransportEndpoint(endpoint)
	if err != nil || !validMCPManualKey(key) {
		return "", errMCPConnectionURIInvalid
	}
	if parsed.Scheme == "http" && !isPrivateMCPDestination(parsed.Hostname()) {
		return "", errMCPPublicDestinationHTTPSRequired
	}
	parsed.Scheme = "leafen-mcp+" + parsed.Scheme
	parsed.RawQuery = "v=" + mcpConnectionURIVersion
	parsed.Fragment = "key=" + key
	return parsed.String(), nil
}

func parseMCPConnectionURI(
	value string,
	confirmPublicDestination bool,
) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		parsed.Path != "/mcp" || parsed.RawPath != "" {
		return "", "", errMCPConnectionURIInvalid
	}
	transportScheme := ""
	switch parsed.Scheme {
	case "leafen-mcp+http":
		transportScheme = "http"
	case "leafen-mcp+https":
		transportScheme = "https"
	default:
		return "", "", errMCPConnectionURIInvalid
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(query) != 1 || len(query["v"]) != 1 ||
		query.Get("v") != mcpConnectionURIVersion {
		return "", "", errMCPConnectionURIInvalid
	}
	fragment, err := url.ParseQuery(parsed.Fragment)
	key := fragment.Get("key")
	if err != nil || len(fragment) != 1 || len(fragment["key"]) != 1 || !validMCPManualKey(key) {
		return "", "", errMCPConnectionURIInvalid
	}
	endpoint, err := parseMCPTransportEndpoint(transportScheme + "://" + parsed.Host + parsed.Path)
	if err != nil {
		return "", "", errMCPConnectionURIInvalid
	}
	isPrivate := isPrivateMCPDestination(endpoint.Hostname())
	if endpoint.Scheme == "http" && !isPrivate {
		return "", "", errMCPPublicDestinationHTTPSRequired
	}
	if !isPrivate && !confirmPublicDestination {
		return "", "", errMCPPublicDestinationConfirmationRequired
	}
	// The bridge speaks to the Leafen MCP endpoint, Leafen's own tools
	// (memory, migration, inbox) beside Notion's; the URI keeps naming /mcp,
	// its format since v1 (2026-10-16: an agent bridged with a key made in
	// the settings had no Leafen memory).
	native := *endpoint
	native.Path = nativeMCPPath
	return native.String(), key, nil
}

func parseMCPTransportEndpoint(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Path != "/mcp" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errMCPConnectionURIInvalid
	}
	return parsed, nil
}

func validMCPManualKey(key string) bool {
	if len(key) != len("lmc_")+43 || !strings.HasPrefix(key, "lmc_") {
		return false
	}
	for _, value := range key[len("lmc_"):] {
		if (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') ||
			(value >= '0' && value <= '9') || value == '-' || value == '_' {
			continue
		}
		return false
	}
	return true
}

// isPrivateMCPDestination says whether a host is this computer's or its
// network's (network_reach.go): a name of one label is a machine on this
// network — a compose service such as "leafen" — never one the internet
// resolves; so is a .local name, and a .localhost one is this computer
// (2026-11-05: http://c.localhost read "couldn't make the connection code").
func isPrivateMCPDestination(hostname string) bool {
	return placeOfHost(hostName(hostname)) != placeAnywhere
}

func newMCPStdioHTTPClient() *http.Client {
	return &http.Client{CheckRedirect: func(request *http.Request, prior []*http.Request) error {
		if len(prior) == 0 || !sameMCPTransportOrigin(prior[len(prior)-1].URL, request.URL) {
			return errMCPStdioCrossOriginRedirect
		}
		return nil
	}}
}

func sameMCPTransportOrigin(left *url.URL, right *url.URL) bool {
	return left != nil && right != nil && strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host)
}
