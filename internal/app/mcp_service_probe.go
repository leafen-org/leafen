package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Notion looks at a custom MCP server's address as it is typed (seen
// 2026-10-13): a check mark in the field, then the form for how the server
// signs in — its sign-in page (a note that connecting sends the person to the
// service's own site to sign in, the scopes it asks for, and under "고급 설정" the redirect
// address and an OAuth client of the person's own) or, for any other server,
// the headers to send it — or a line under the field saying what is wrong
// with the address. The server looks the same way, connecting nothing, and
// says too when the person must make a client on the service for Leafen
// before signing in (mcp_service_oauth_reach.go), or that the sign-in server
// the service named isn't answering now (mcp_service_sign_in_server.go).

const (
	mcpServiceSignInPage    = "oauth"
	mcpServiceSignInHeaders = "headers"
)

type mcpServiceProbe struct {
	URL    string `json:"url"`
	SignIn string `json:"signIn"`
	// Open: the server answered with no sign-in at all.
	Open        bool     `json:"open"`
	Name        string   `json:"name,omitempty"`
	Scopes      []string `json:"scopes"`
	CallbackURL string   `json:"callbackUrl,omitempty"`
	// ClientNeeded: the server can't be the service's client by itself, so
	// the person makes one there — as the refusal's code for connecting
	// without one: the service registers no clients, or reads this server at
	// an address it can't reach (with PublicURL, the server's public address
	// where it has one). Note says why, as an agent is told.
	ClientNeeded string `json:"clientNeeded,omitempty"`
	PublicURL    string `json:"publicUrl,omitempty"`
	Note         string `json:"note,omitempty"`
	// SignInUnavailable: the sign-in server the service named isn't
	// answering now (mcp_service_sign_in_server.go) — its sign-in page all
	// the same, rather than the headers' form; Note says so.
	SignInUnavailable bool `json:"signInUnavailable,omitzero"`
}

// ProbeMCPService says how the service at the address signs in.
func ProbeMCPService(ctx context.Context, app core.App, userID, rawURL, origin string) (mcpServiceProbe, error) {
	probe := mcpServiceProbe{SignIn: mcpServiceSignInHeaders, Scopes: []string{}}
	serviceURL, err := normalizeMCPServiceURL(rawURL)
	if err != nil {
		return probe, err
	}
	probe.URL = serviceURL
	if err := refuseMCPServiceByPolicy(app, userID, serviceURL); err != nil {
		return probe, err
	}
	ctx, cancel := context.WithTimeout(ctx, mcpServiceOAuthSeconds*time.Second)
	defer cancel()
	session, challenges, err := openMCPServiceSession(ctx, serviceURL, nil)
	switch {
	case err == nil:
		probe.Open = true
		if initialized := session.InitializeResult(); initialized != nil && initialized.ServerInfo != nil {
			probe.Name = appOneLine(firstNonempty(initialized.ServerInfo.Title, initialized.ServerInfo.Name), maxMCPServiceNameRunes)
		}
		session.Close()
	case errors.Is(err, errMCPServiceSignIn):
		// A sign-in Leafen can't use (no PKCE) is left to the headers' form.
		server, err := findMCPServiceAuthServer(ctx, newMCPServiceHTTPClient(), serviceURL, challenges)
		if errors.Is(err, errMCPServiceSignInUnavailable) {
			probe.SignIn, probe.CallbackURL = mcpServiceSignInPage, origin+apiRoot+mcpServiceCallbackPath
			probe.SignInUnavailable, probe.Note = true, mcpServiceSignInUnavailableText
		}
		if err == nil && server.found {
			probe.SignIn, probe.CallbackURL = mcpServiceSignInPage, origin+apiRoot+mcpServiceCallbackPath
			probe.Scopes = append(probe.Scopes, server.scopes...)
			if need := mcpServiceClientNeed(ctx, server.meta, origin); need != nil {
				unreachable := &mcpServiceUnreachable{}
				probe.ClientNeeded, probe.Note = errorCodeMCPServiceClientNeeded, mcpServiceClientNeedText(need, mcpScreenLanguageIn(ctx, app, userID))
				if errors.As(need, &unreachable) {
					probe.ClientNeeded, probe.PublicURL = errorCodeMCPServiceAddressUnreachable, unreachable.public
				}
			}
		}
	case errors.Is(err, errOutboundUnsafeDestination):
		return probe, err
	}
	return probe, nil
}

func probeMCPServiceRoute(event *core.RequestEvent) error {
	userID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	input := struct {
		URL string `json:"url"`
	}{}
	if event.BindBody(&input) != nil {
		return mcpServiceFailure(event, errMCPServiceInvalid)
	}
	if strings.TrimSpace(input.URL) == "" {
		return refuseMissingInput(event, errorCodeMCPServiceInvalid, "url", "the https address of the service's MCP server.")
	}
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	probe, err := ProbeMCPService(event.Request.Context(), event.App, userID, input.URL, origin)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, probe)
}
