package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// Talking to a connected service's MCP server: only public addresses (or
// the loopback address allowed for checks), the person's sign-in put in
// only for the service's own host, and its answer that asks to sign in kept
// so the server can begin one.

const (
	mcpServiceAllowedLoopbackEnv = "LEAFEN_MCP_SERVICE_ALLOWED_LOOPBACK"
	mcpServiceReadSeconds        = 30
)

var errMCPServiceSignIn = errors.New("the service asks to log in")

func mcpServiceAllowedLoopback() string {
	return strings.TrimSpace(os.Getenv(mcpServiceAllowedLoopbackEnv))
}

func parseMCPServiceLoopbackURL(raw, loopback string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host != loopback || parsed.User != nil {
		return "", errMCPServiceInvalid
	}
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

// newMCPServiceHTTPClient reaches public addresses only, follows a few
// redirects that stay on https, and leaves time limits to each request.
func newMCPServiceHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	transport.MaxResponseHeaderBytes = 64 << 10
	loopback := mcpServiceAllowedLoopback()
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: transport.IdleConnTimeout}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(ctx, network, address, loopback, net.DefaultResolver.LookupNetIP, dialer.DialContext)
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, prior []*http.Request) error {
			if len(prior) >= 3 || request.URL.Scheme != "https" && (loopback == "" || request.URL.Host != loopback) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// mcpServiceSignInTransport puts the sign-in's headers in requests to the
// service's host and keeps the challenge of an answer asking to sign in.
type mcpServiceSignInTransport struct {
	base    http.RoundTripper
	host    string
	headers [][2]string

	mu         sync.Mutex
	challenged bool
	challenges []string
}

func (transport *mcpServiceSignInTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if len(transport.headers) > 0 && strings.EqualFold(request.URL.Host, transport.host) {
		request = request.Clone(request.Context())
		for _, header := range transport.headers {
			request.Header.Set(header[0], header[1])
		}
	}
	response, err := transport.base.RoundTrip(request)
	if err == nil && response.StatusCode == http.StatusUnauthorized {
		transport.mu.Lock()
		transport.challenged = true
		transport.challenges = response.Header.Values("WWW-Authenticate")
		transport.mu.Unlock()
	}
	return response, err
}

type mcpServiceReading struct {
	Name    string
	Version string
	Tools   []MCPServiceTool
}

// mcpServiceSession is a session with the service as one sign-in.
type mcpServiceSession struct {
	*mcp.ClientSession
	signIn *mcpServiceSignInTransport
}

// failure is err as the person reads it: an answer asking to sign in is
// errMCPServiceSignIn, with its challenges.
func (signIn *mcpServiceSignInTransport) failure(err error) ([]oauthex.Challenge, error) {
	signIn.mu.Lock()
	challenged, headers := signIn.challenged, signIn.challenges
	signIn.mu.Unlock()
	switch {
	case challenged:
		challenges, _ := oauthex.ParseWWWAuthenticate(headers)
		return challenges, errMCPServiceSignIn
	case errors.Is(err, errOutboundUnsafeDestination):
		return nil, errOutboundUnsafeDestination
	}
	return nil, fmt.Errorf("%w: %v", errMCPServiceFailed, err)
}

// openMCPServiceSession opens a session with the service, sending it the
// sign-in's headers.
func openMCPServiceSession(ctx context.Context, serviceURL string, headers [][2]string) (mcpServiceSession, []oauthex.Challenge, error) {
	target, err := url.Parse(serviceURL)
	if err != nil {
		return mcpServiceSession{}, nil, errMCPServiceInvalid
	}
	base := newMCPServiceHTTPClient()
	signIn := &mcpServiceSignInTransport{base: base.Transport, host: target.Host, headers: headers}
	client := &http.Client{Transport: signIn, CheckRedirect: base.CheckRedirect}
	var transport mcp.Transport = &mcp.StreamableClientTransport{
		Endpoint: serviceURL, HTTPClient: client, MaxRetries: -1, DisableStandaloneSSE: true,
	}
	// Servers of the older protocol answer at an address ending in /sse.
	if strings.HasSuffix(strings.TrimRight(target.Path, "/"), "/sse") {
		transport = &mcp.SSEClientTransport{Endpoint: serviceURL, HTTPClient: client}
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "leafen", Version: "2"}, nil).Connect(ctx, transport, nil)
	if err != nil {
		challenges, err := signIn.failure(err)
		return mcpServiceSession{}, challenges, err
	}
	return mcpServiceSession{ClientSession: session, signIn: signIn}, nil, nil
}

// readMCPService reads the service's name and its tools.
func readMCPService(ctx context.Context, serviceURL string, headers [][2]string) (mcpServiceReading, []oauthex.Challenge, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpServiceReadSeconds*time.Second)
	defer cancel()
	session, challenges, err := openMCPServiceSession(ctx, serviceURL, headers)
	if err != nil {
		return mcpServiceReading{Tools: []MCPServiceTool{}}, challenges, err
	}
	defer session.Close()
	reading, err := session.read(ctx)
	return reading, nil, err
}

func (session mcpServiceSession) read(ctx context.Context) (mcpServiceReading, error) {
	reading := mcpServiceReading{Tools: []MCPServiceTool{}}
	if initialized := session.InitializeResult(); initialized != nil && initialized.ServerInfo != nil {
		reading.Name = firstNonempty(initialized.ServerInfo.Title, initialized.ServerInfo.Name)
		reading.Version = initialized.ServerInfo.Version
		if initialized.Capabilities == nil || initialized.Capabilities.Tools == nil {
			return reading, nil
		}
	}
	// Every tool is kept; past the safety bound the reading fails, saying
	// why, rather than keeping some.
	size := 0
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			_, err = session.signIn.failure(err)
			return reading, err
		}
		kept := mcpServiceToolOf(tool)
		encoded, _ := json.Marshal(kept)
		size += len(encoded)
		if len(reading.Tools) >= mcpServiceToolsSafety || size > mcpServiceToolsSafetyBytes {
			return mcpServiceReading{Tools: []MCPServiceTool{}}, errMCPServiceTooMany
		}
		reading.Tools = append(reading.Tools, kept)
	}
	return reading, nil
}

func mcpServiceToolOf(tool *mcp.Tool) MCPServiceTool {
	kept := MCPServiceTool{Name: tool.Name, Title: tool.Title, Description: tool.Description}
	if tool.Annotations != nil {
		kept.Title = firstNonempty(kept.Title, tool.Annotations.Title)
		kept.ReadOnly = tool.Annotations.ReadOnlyHint
		// Unsaid, a tool that changes things may destroy them (MCP's default).
		kept.Destructive = !kept.ReadOnly && (tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint)
	} else {
		kept.Destructive = true
	}
	if tool.InputSchema != nil {
		kept.InputSchema, _ = json.Marshal(tool.InputSchema)
	}
	if tool.OutputSchema != nil {
		kept.OutputSchema, _ = json.Marshal(tool.OutputSchema)
	}
	return kept
}
