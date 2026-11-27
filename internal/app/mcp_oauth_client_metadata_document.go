package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An agent can name itself by the address of its published metadata
// document instead of registering (OAuth Client ID Metadata Document,
// draft-ietf-oauth-client-id-metadata-document, which MCP's authorization
// takes up): Claude's connector does so by default ("Claude의 게시된 ID
// 사용"). The authorization request carries that https address as its
// client_id; the server reads the document there — public addresses only,
// no redirects, at most 5 KiB — checks that it names itself by the same
// address, and keeps it as a client under that address, so the token and
// refresh requests find it as they find a registered one. It is read again
// at the next authorization after a few minutes, so a changed document takes
// effect.

const (
	mcpOAuthClientMetadataMaxBytes  = 5 << 10
	mcpOAuthClientMetadataTimeout   = 5 * time.Second
	mcpOAuthClientMetadataFreshFor  = 5 * time.Minute
	mcpOAuthClientMetadataFailedFor = time.Minute
	// Reads at once for the whole server: the authorization address answers
	// anyone, so it never carries more than a few reads elsewhere.
	mcpOAuthClientMetadataReadsAtOnce = 4
)

var (
	errMCPOAuthClientMetadataBusy = errors.New("too many client metadata documents are being read; try again shortly")
	mcpOAuthClientMetadataReads   = make(chan struct{}, mcpOAuthClientMetadataReadsAtOnce)
	mcpOAuthClientMetadataSeen    = struct {
		sync.Mutex
		at map[string]mcpOAuthClientMetadataReading
	}{at: map[string]mcpOAuthClientMetadataReading{}}
	mcpOAuthClientMetadataHTTP = newMCPOAuthClientMetadataHTTPClient()
)

type mcpOAuthClientMetadataReading struct {
	at  time.Time
	err error
}

type mcpOAuthClientMetadataDocument struct {
	ClientID string `json:"client_id"`
	mcpOAuthClientRegistration
}

func newMCPOAuthClientMetadataHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = outboundPublicProxy
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return outboundPublicDialContext(ctx, network, address, "", net.DefaultResolver.LookupNetIP, dialer.DialContext)
	}
	return &http.Client{
		Transport: transport, Timeout: mcpOAuthClientMetadataTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// mcpOAuthClientIDIsMetadataAddress reports whether a client_id names a
// metadata document rather than a registered client.
func mcpOAuthClientIDIsMetadataAddress(clientID string) bool {
	return strings.HasPrefix(clientID, "https://")
}

// validMCPOAuthClientMetadataAddress checks a client_id address as the draft
// asks: https, a host, a path past "/", no fragment, no user, no dot
// segments, written as it is kept (the document must name it the same).
func validMCPOAuthClientMetadataAddress(raw string) error {
	if len(raw) > 2048 {
		return errors.New("client_id address is longer than 2048 characters")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.Path == "" || parsed.Path == "/" {
		return errors.New("client_id address must be https with a path, without a fragment or user")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return errors.New("client_id address must not have . or .. segments")
		}
	}
	normalized, err := normalizeOutboundPublicURL(raw, 2048, false)
	if err != nil {
		return errors.New("client_id address must be a public https address")
	}
	if normalized != raw {
		return errors.New("client_id address must be written in its plain form: " + normalized)
	}
	return nil
}

// resolveMCPOAuthMetadataClient is the client a metadata document's address
// names, read from there when it was not read in the last few minutes.
func resolveMCPOAuthMetadataClient(ctx context.Context, app core.App, clientID string) (storedMCPOAuthClient, error) {
	if err := validMCPOAuthClientMetadataAddress(clientID); err != nil {
		return storedMCPOAuthClient{}, err
	}
	mcpOAuthClientMetadataSeen.Lock()
	seen, known := mcpOAuthClientMetadataSeen.at[clientID]
	mcpOAuthClientMetadataSeen.Unlock()
	if known && seen.err != nil && time.Since(seen.at) < mcpOAuthClientMetadataFailedFor {
		return storedMCPOAuthClient{}, seen.err
	}
	if known && seen.err == nil && time.Since(seen.at) < mcpOAuthClientMetadataFreshFor {
		if client, err := findMCPOAuthClient(app, clientID); err == nil {
			return client, nil
		}
	}
	client, err := readMCPOAuthMetadataClient(ctx, app, clientID)
	if errors.Is(err, errMCPOAuthClientMetadataBusy) || errors.Is(err, errMCPOAuthClientCapacity) {
		return client, err
	}
	mcpOAuthClientMetadataSeen.Lock()
	// Old readings go once the map grows: each is only a few minutes' hold.
	if len(mcpOAuthClientMetadataSeen.at) > 1024 {
		for address, reading := range mcpOAuthClientMetadataSeen.at {
			if time.Since(reading.at) > mcpOAuthClientMetadataFreshFor {
				delete(mcpOAuthClientMetadataSeen.at, address)
			}
		}
	}
	mcpOAuthClientMetadataSeen.at[clientID] = mcpOAuthClientMetadataReading{at: time.Now(), err: err}
	mcpOAuthClientMetadataSeen.Unlock()
	return client, err
}

func readMCPOAuthMetadataClient(ctx context.Context, app core.App, clientID string) (storedMCPOAuthClient, error) {
	select {
	case mcpOAuthClientMetadataReads <- struct{}{}:
		defer func() { <-mcpOAuthClientMetadataReads }()
	default:
		return storedMCPOAuthClient{}, errMCPOAuthClientMetadataBusy
	}
	document, err := fetchMCPOAuthClientMetadata(ctx, clientID)
	if err != nil {
		return storedMCPOAuthClient{}, err
	}
	if document.ClientID != clientID {
		return storedMCPOAuthClient{}, errors.New("the client metadata document's client_id must be its own address")
	}
	// A document's client has no secret: it proves itself with PKCE.
	if document.TokenEndpointAuthMethod == "" {
		document.TokenEndpointAuthMethod = "none"
	}
	// A document lists what its client does with every server (Claude's
	// names a JWT bearer grant, VS Code's the device code, seen 2026-11-13);
	// only the grants offered here are kept.
	offered := []string{}
	for _, grant := range document.GrantTypes {
		if grant == "authorization_code" || grant == "refresh_token" {
			offered = append(offered, grant)
		}
	}
	if len(document.GrantTypes) > 0 && !slices.Contains(offered, "authorization_code") {
		return storedMCPOAuthClient{}, errors.New("the client metadata document's grant_types must include authorization_code")
	}
	document.GrantTypes = offered
	normalized, err := normalizeMCPOAuthClient(document.mcpOAuthClientRegistration)
	if err != nil {
		return storedMCPOAuthClient{}, err
	}
	return keepMCPOAuthMetadataClient(ctx, app, clientID, normalized)
}

func fetchMCPOAuthClientMetadata(ctx context.Context, clientID string) (mcpOAuthClientMetadataDocument, error) {
	var document mcpOAuthClientMetadataDocument
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return document, errors.New("client_id address can't be read")
	}
	request.Header.Set("Accept", "application/json")
	response, err := mcpOAuthClientMetadataHTTP.Do(request)
	if err != nil {
		return document, errors.New("the client metadata document couldn't be read from its address")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return document, fmt.Errorf("the client metadata document's address answered %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, mcpOAuthClientMetadataMaxBytes+1))
	if err != nil {
		return document, errors.New("the client metadata document couldn't be read from its address")
	}
	if len(body) > mcpOAuthClientMetadataMaxBytes {
		return document, errors.New("the client metadata document is larger than 5 KiB")
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return document, errors.New("the client metadata document isn't a JSON object")
	}
	return document, nil
}

// keepMCPOAuthMetadataClient keeps a document's client under its address,
// its name and return addresses as the document has them now.
func keepMCPOAuthMetadataClient(ctx context.Context, app core.App, clientID string, document mcpOAuthClientRegistration) (storedMCPOAuthClient, error) {
	redirects, _ := json.Marshal(document.RedirectURIs)
	var kept storedMCPOAuthClient
	err := app.RunInTransaction(func(txApp core.App) error {
		now := time.Now()
		existing, err := findMCPOAuthClient(txApp, clientID)
		if err == nil {
			if _, err := txApp.DB().Update(mcpOAuthClientsTable, dbx.Params{
				"client_name": document.ClientName, "redirect_uris_json": string(redirects),
			}, dbx.HashExp{"id": clientID}).Execute(); err != nil {
				return err
			}
			if err := touchMCPOAuthClient(txApp, clientID, now.UnixMilli()); err != nil {
				return err
			}
			existing.ClientName, existing.RedirectURIsJSON = document.ClientName, string(redirects)
			kept = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := admitMCPOAuthClientInTransaction(ctx, txApp, now.UnixMilli()); err != nil {
			return err
		}
		// Kept apart from registrations, whose hash is of their metadata.
		digest := sha256.Sum256([]byte("client-id-metadata-document\x00" + clientID))
		kept = storedMCPOAuthClient{
			ID: clientID, ClientName: document.ClientName, RedirectURIsJSON: string(redirects),
			MetadataHash: hex.EncodeToString(digest[:]), Created: now.Unix(),
		}
		if _, err := txApp.DB().Insert(mcpOAuthClientsTable, dbx.Params{
			"id": kept.ID, "client_name": kept.ClientName, "redirect_uris_json": kept.RedirectURIsJSON,
			"metadata_hash": kept.MetadataHash, "created": kept.Created, "last_used_at": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		notifyMCPCredentialRegistryAfterCommit(txApp)
		return nil
	})
	return kept, err
}
