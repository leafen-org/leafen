package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/oauth2"
)

// A service hub connection after the server's address changes (the
// multi-server decision's 2026-10-20 change "서버 주소는 바뀔 수 있다"): its
// sign-in goes back to the address stored with it, which forwards here, and
// its tokens renew without one, so it goes on. At its next sign-in a client
// the server registered itself, or one known by the server's client
// metadata document, moves to the new address by itself — registered or
// named anew, kept apart until that sign-in succeeds so the working one
// stays (pending_plan); the old document is answered at the old address
// meanwhile; one known by the document is made anew at its next sign-in
// at the same address too, registered where the service takes that. A
// client the person made on the service keeps the address the
// service has, as the server's own sign-in does (auth_provider_redirects.go):
// the person is shown the new one to add, says when it is added, and the
// first sign-in back at it ends the note.

const (
	mcpServiceClientGiven      = "given"
	mcpServiceClientDocument   = "document"
	mcpServiceClientRegistered = "registered"
)

func mcpServiceReturnKey(serviceID string) string {
	return "service:" + serviceID
}

// clientKind is how the server is a client of the service: as the person's
// own client unless it knows it registered itself or named its document.
func (stored storedMCPService) clientKind() string {
	switch {
	case stored.ClientKind != "":
		return stored.ClientKind
	case strings.HasSuffix(stored.ClientID, apiRoot+mcpServiceClientMetadataPath):
		return mcpServiceClientDocument
	default:
		return mcpServiceClientGiven
	}
}

// mcpServicePendingPlan is a service's client made anew for the address,
// used for the sign-in under way and kept once it succeeds.
type mcpServicePendingPlan struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorizationEndpoint"`
	TokenEndpoint         string `json:"tokenEndpoint"`
	RevocationEndpoint    string `json:"revocationEndpoint"`
	Resource              string `json:"resource"`
	Scope                 string `json:"scope"`
	ClientID              string `json:"clientId"`
	ClientSecret          string `json:"clientSecret"`
	ClientAuth            int    `json:"clientAuth"`
	RedirectURI           string `json:"redirectUri"`
	Kind                  string `json:"kind"`
}

// prepareMCPServiceSignIn readies a service's sign-in at origin: a client
// the server made for another address is made anew, pending; the person's
// own client goes back to the address the service has.
func prepareMCPServiceSignIn(ctx context.Context, app core.App, stored storedMCPService, origin string) error {
	current := origin + apiRoot + mcpServiceCallbackPath
	if stored.Auth != mcpServiceAuthOAuth {
		return nil
	}
	// The person's own client: the address the service has, taken from the
	// kept one each time, as a sign-in said added and then undone may have
	// left the other in the row.
	if stored.clientKind() == mcpServiceClientGiven {
		key := mcpServiceReturnKey(stored.ID)
		KeepEarlierReturn(app, key, stored.ClientID, stored.RedirectURI, current)
		chosen := KeptReturnFor(app, key, stored.ClientID, current)
		if chosen == stored.RedirectURI {
			return nil
		}
		_, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[redirect_uri]] = {:redirect} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"redirect": chosen, "id": stored.ID}).Execute()
		return err
	}
	// One named by the document is made anew even at the same address, as a
	// service's own registration goes first (mcp_service_oauth_register.go):
	// the document may be out of the service's reach.
	same := stored.RedirectURI == current
	if same && stored.clientKind() != mcpServiceClientDocument {
		return clearMCPServicePendingPlan(app, stored.ID)
	}
	document := ""
	if strings.HasPrefix(origin, "https://") {
		document = origin + apiRoot + mcpServiceClientMetadataPath
	}
	plan, err := discoverMCPServiceOAuth(ctx, stored.URL, nil, current, mcpServiceClient{}, document, strings.Fields(stored.Scope))
	// The service can't read the document here any more: said, as when
	// connecting, rather than sending the person to its refusal.
	if same && !errors.Is(err, errMCPServiceAddressUnreachable) && (err != nil || plan.ClientKind == mcpServiceClientDocument) {
		return clearMCPServicePendingPlan(app, stored.ID)
	}
	if err != nil {
		return err
	}
	pending := mcpServicePendingPlan{Issuer: plan.Issuer, AuthorizationEndpoint: plan.AuthorizationEndpoint, TokenEndpoint: plan.TokenEndpoint,
		RevocationEndpoint: plan.RevocationEndpoint, Resource: plan.Resource, Scope: plan.Scope, ClientID: plan.ClientID,
		ClientSecret: plan.ClientSecret, ClientAuth: int(plan.ClientAuth), RedirectURI: current, Kind: plan.ClientKind}
	encoded, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	sealed, err := encryptAdministratorSetting(app, cipherMCPService, string(encoded))
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[pending_plan]] = {:plan} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"plan": sealed, "id": stored.ID}).Execute()
	return err
}

func clearMCPServicePendingPlan(app core.App, serviceID string) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[pending_plan]] = '' WHERE [[id]] = {:id} AND [[pending_plan]] <> ''`).
		Bind(dbx.Params{"id": serviceID}).Execute()
	return err
}

// pendingPlan is the client made anew for the sign-in under way, if any.
func (stored storedMCPService) pendingPlan(app core.App) (mcpServicePendingPlan, bool) {
	pending := mcpServicePendingPlan{}
	if stored.PendingPlan == "" {
		return pending, false
	}
	opened, err := decryptAdministratorSetting(app, stored.PendingPlan)
	if err != nil || json.Unmarshal([]byte(opened), &pending) != nil {
		return pending, false
	}
	return pending, true
}

// signInConfig is the client a sign-in goes by: the one made anew while
// pending, else the service's.
func (stored storedMCPService) signInConfig(app core.App) (*oauth2.Config, string, string, error) {
	if pending, ok := stored.pendingPlan(app); ok {
		return &oauth2.Config{
			ClientID: pending.ClientID, ClientSecret: pending.ClientSecret, RedirectURL: pending.RedirectURI, Scopes: strings.Fields(pending.Scope),
			Endpoint: oauth2.Endpoint{AuthURL: pending.AuthorizationEndpoint, TokenURL: pending.TokenEndpoint, AuthStyle: oauth2.AuthStyle(pending.ClientAuth)},
		}, pending.Issuer, pending.Resource, nil
	}
	config, err := stored.oauthConfig(app)
	return config, stored.Issuer, stored.Resource, err
}

// keepMCPServiceSignIn keeps, once a sign-in succeeded, the client made anew
// for it, or the return address the person's own client came back to.
func keepMCPServiceSignIn(app core.App, stored storedMCPService) error {
	pending, ok := stored.pendingPlan(app)
	if !ok {
		if stored.clientKind() == mcpServiceClientGiven {
			KeepReturn(app, mcpServiceReturnKey(stored.ID), stored.ClientID, stored.RedirectURI)
		}
		return nil
	}
	secret := ""
	if pending.ClientSecret != "" {
		sealed, err := encryptAdministratorSetting(app, cipherMCPService, pending.ClientSecret)
		if err != nil {
			return err
		}
		secret = sealed
	}
	_, err := app.DB().NewQuery(`UPDATE {{leafen_mcp_services}} SET [[issuer]] = {:issuer}, [[authorization_endpoint]] = {:authorization},
		[[token_endpoint]] = {:token}, [[revocation_endpoint]] = {:revocation}, [[resource]] = {:resource}, [[scope]] = {:scope},
		[[client_id]] = {:client}, [[client_secret]] = {:secret}, [[client_auth]] = {:auth}, [[redirect_uri]] = {:redirect},
		[[client_kind]] = {:kind}, [[pending_plan]] = '' WHERE [[id]] = {:id}`).Bind(dbx.Params{
		"issuer": pending.Issuer, "authorization": pending.AuthorizationEndpoint, "token": pending.TokenEndpoint,
		"revocation": pending.RevocationEndpoint, "resource": pending.Resource, "scope": pending.Scope, "client": pending.ClientID,
		"secret": secret, "auth": pending.ClientAuth, "redirect": pending.RedirectURI, "kind": pending.Kind, "id": stored.ID,
	}).Execute()
	return err
}

// keepMCPServiceReturnsBefore keeps, for the services the people signed in
// to with clients of their own at the address just left, that address, and
// tells each person which need the new one.
func keepMCPServiceReturnsBefore(app core.App, previous, address string) {
	rows := []storedMCPService{}
	if err := app.DB().NewQuery(`SELECT ` + storedMCPServiceColumns + ` FROM {{leafen_mcp_services}} [[service]]
		WHERE [[auth]] = {:oauth} AND [[redirect_uri]] = {:redirect}`).Bind(dbx.Params{
		"oauth": mcpServiceAuthOAuth, "redirect": previous + apiRoot + mcpServiceCallbackPath,
	}).All(&rows); err != nil {
		app.Logger().Warn("leafen service return addresses not kept", "error", err)
		return
	}
	needed := map[string][]string{}
	for _, stored := range rows {
		if stored.clientKind() != mcpServiceClientGiven {
			continue
		}
		if KeepEarlierReturn(app, mcpServiceReturnKey(stored.ID), stored.ClientID, stored.RedirectURI, address+apiRoot+mcpServiceCallbackPath) {
			needed[stored.UserID] = append(needed[stored.UserID], stored.Name)
		}
	}
	for person, names := range needed {
		NotifyReturnChange(app, []string{person}, "", address, strings.Join(names, ", "), "/settings?section=integrations")
	}
}

// withReturn fills in, for a service the person signed in to with a client
// of their own, the address the service still has and whether they said
// the new one, at origin, is added.
func (stored storedMCPService) withReturn(app core.App, service MCPService, origin string) MCPService {
	if stored.Auth != mcpServiceAuthOAuth || stored.clientKind() != mcpServiceClientGiven || origin == "" {
		return service
	}
	current := origin + apiRoot + mcpServiceCallbackPath
	earlier, added := ProjectKeptReturn(app, mcpServiceReturnKey(stored.ID), stored.ClientID, current)
	if earlier == "" && stored.RedirectURI != current && movedResource(stored.RedirectURI) == current {
		earlier = stored.RedirectURI
	}
	service.EarlierCallback, service.CallbackAdded = earlier, added
	if earlier != "" {
		service.Callback = current
	}
	return service
}

// setMCPServiceReturnRoute is the person saying their client on the service
// has the new return address, or taking it back.
func setMCPServiceReturnRoute(event *core.RequestEvent) error {
	userID, ok := mcpServiceRequestUser(event)
	if !ok {
		return nil
	}
	stored, err := loadMCPService(event.App, userID, event.Request.PathValue("serviceId"))
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	input := struct {
		Added bool `json:"added"`
	}{}
	if err := event.BindBody(&input); err != nil || stored.clientKind() != mcpServiceClientGiven {
		return mcpServiceFailure(event, errMCPServiceInvalid)
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return mcpServiceFailure(event, err)
	}
	current := origin + apiRoot + mcpServiceCallbackPath
	KeepEarlierReturn(event.App, mcpServiceReturnKey(stored.ID), stored.ClientID, stored.RedirectURI, current)
	if _, err := SetKeptReturnAdded(event.App, mcpServiceReturnKey(stored.ID), current, input.Added); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return mcpServiceFailure(event, err)
	}
	return event.JSON(http.StatusOK, map[string]any{"service": stored.withReturn(event.App, stored.view(), origin)})
}
