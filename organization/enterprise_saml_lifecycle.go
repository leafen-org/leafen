package organization

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/crewjam/saml"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	samlAssertionRetentionSeconds = 86400
	samlLifecycleCleanupBatchSize = 256
)

type storedSAMLRequest struct {
	ID           string `db:"id"`
	ProviderID   string `db:"provider_id"`
	RelayState   string `db:"relay_state"`
	AppReturn    string `db:"app_return"`
	AppChallenge string `db:"app_challenge"`
	ExpiresAt    int64  `db:"expires_at"`
	ConsumedAt   int64  `db:"consumed_at"`
	CreatedAt    int64  `db:"created_at"`
}

type storedSAMLCompletion struct {
	ID           string `db:"id"`
	ProviderID   string `db:"provider_id"`
	UserID       string `db:"user_id"`
	AppChallenge string `db:"app_challenge"`
	ExpiresAt    int64  `db:"expires_at"`
	ConsumedAt   int64  `db:"consumed_at"`
	// ProviderSecondFactor: the assertion named multi-factor authentication.
	ProviderSecondFactor int `db:"provider_second_factor"`
}

// samlResponseContext is what a SAML response answers: its provider, the
// request it answers and where to go after; AppReturn and AppChallenge are
// the app's when an app started it (auth_app_return.go).
type samlResponseContext struct {
	Provider     storedSAMLProvider
	RequestID    string
	RequestIDs   []string
	RelayState   string
	AppReturn    string
	AppChallenge string
	IDPInitiated bool
}

func storeSAMLAuthenticationRequest(
	app core.App,
	providerID string,
	requestID string,
	relayState string,
	appReturn leafen.AuthAppReturn,
) error {
	now := time.Now().UTC()
	_, err := app.DB().Insert(leafen.SAMLRequestsTable, dbx.Params{
		"id": requestID, "provider_id": providerID, "relay_state": relayState,
		"app_return": appReturn.Address, "app_challenge": appReturn.Challenge,
		"expires_at":  now.Add(leafen.SAMLRequestTTLSeconds * time.Second).UnixMilli(),
		"consumed_at": 0, "created_at": now.UnixMilli(),
	}).Execute()
	return err
}

func resolveSAMLResponseContext(
	app core.App,
	envelope samlEnvelope,
) (samlResponseContext, error) {
	now := time.Now().UTC().UnixMilli()
	if envelope.InResponseTo != "" {
		request := storedSAMLRequest{}
		err := app.DB().NewQuery(`
			SELECT [[id]], [[provider_id]], [[relay_state]], [[app_return]], [[app_challenge]],
				[[expires_at]], [[consumed_at]], [[created_at]]
			FROM {{leafen_saml_requests}}
			WHERE [[id]] = {:id} AND [[consumed_at]] = 0 AND [[expires_at]] > {:now}
		`).Bind(dbx.Params{"id": envelope.InResponseTo, "now": now}).One(&request)
		if errors.Is(err, sql.ErrNoRows) {
			return samlResponseContext{}, newSAMLProtocolError(leafen.ErrorCodeSAMLRequestMismatch, "SAML request is missing, expired or consumed")
		}
		if err != nil {
			return samlResponseContext{}, err
		}
		provider, err := loadStoredSAMLProviderByID(app, request.ProviderID)
		if err != nil {
			if errors.Is(err, errSAMLProviderNotFound) {
				return samlResponseContext{}, newSAMLProtocolError(leafen.ErrorCodeSAMLRequestMismatch, "SAML response provider does not match its request")
			}
			return samlResponseContext{}, err
		}
		if provider.Enabled != 1 || provider.Issuer != strings.TrimSpace(envelope.Issuer.Value) {
			return samlResponseContext{}, newSAMLProtocolError(leafen.ErrorCodeSAMLRequestMismatch, "SAML response provider does not match its request")
		}
		context := samlResponseContext{
			Provider: provider, RequestID: request.ID, RequestIDs: []string{request.ID},
			RelayState: request.RelayState, AppReturn: request.AppReturn, AppChallenge: request.AppChallenge,
		}
		if err := validateSAMLEnvelopeCorrelation(envelope, context.RequestIDs, false); err != nil {
			return samlResponseContext{}, err
		}
		return context, nil
	}
	provider, err := loadStoredSAMLProviderByIssuer(app, strings.TrimSpace(envelope.Issuer.Value))
	if err != nil {
		if errors.Is(err, errSAMLProviderNotFound) {
			return samlResponseContext{}, newSAMLProtocolError(leafen.ErrorCodeSAMLRequestMismatch, "unsolicited SAML response is not enabled")
		}
		return samlResponseContext{}, err
	}
	if provider.Enabled != 1 || provider.AllowIDPInitiated != 1 {
		return samlResponseContext{}, newSAMLProtocolError(leafen.ErrorCodeSAMLRequestMismatch, "unsolicited SAML response is not enabled")
	}
	context := samlResponseContext{Provider: provider, RelayState: "/", IDPInitiated: true}
	if err := validateSAMLEnvelopeCorrelation(envelope, nil, true); err != nil {
		return samlResponseContext{}, err
	}
	return context, nil
}

func acceptSAMLAssertion(
	app core.App,
	responseContext samlResponseContext,
	assertion *saml.Assertion,
	assertionID string,
) (string, string, error) {
	completionCode, err := leafen.NewAuthProviderToken("lsc_", 32)
	if err != nil {
		return "", "", err
	}
	completionHash := leafen.HashAuthProviderToken(completionCode)
	relayState, err := validateSAMLRelayState(responseContext.RelayState)
	if err != nil {
		return "", "", err
	}
	// A first sign-in makes the account: its password no one knows is
	// hashed before the transaction (password_outside_transaction.go).
	var unknown *leafen.PreparedPassword
	if config, err := loadSAMLProviderConfig(app, responseContext.Provider); err == nil {
		if email, err := samlAssertionEmail(assertion, config.EmailAttribute); err == nil {
			unknown = scimUnknownPassword(app, email)
		}
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		now := time.Now().UTC()
		if err := cleanupExpiredSAMLLifecycle(txApp, now.UnixMilli()); err != nil {
			return err
		}
		config, err := loadSAMLProviderConfig(txApp, responseContext.Provider)
		if err != nil {
			return err
		}
		email, err := samlAssertionEmail(assertion, config.EmailAttribute)
		if err != nil {
			return err
		}
		user, err := authorizeSAMLIdentity(txApp, responseContext.Provider, email, unknown)
		if err != nil {
			return err
		}
		assertionKey := leafen.AuthProviderExternalIdentityID(responseContext.Provider.ID, assertionID)
		_, err = txApp.DB().Insert(leafen.SAMLAssertionsTable, dbx.Params{
			"id": assertionKey, "provider_id": responseContext.Provider.ID,
			"assertion_id": assertionID, "user_id": user.Id,
			"accepted_at": now.UnixMilli(),
			"expires_at":  now.Add(samlAssertionRetentionSeconds * time.Second).UnixMilli(),
		}).Execute()
		if err != nil {
			return newSAMLProtocolError(leafen.ErrorCodeSAMLAssertionReplayed, "SAML assertion was already accepted")
		}
		if responseContext.RequestID != "" {
			result, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_saml_requests}} SET [[consumed_at]] = {:now}
				WHERE [[id]] = {:id} AND [[provider_id]] = {:providerId}
					AND [[consumed_at]] = 0 AND [[expires_at]] > {:now}
			`).Bind(dbx.Params{
				"now": now.UnixMilli(), "id": responseContext.RequestID,
				"providerId": responseContext.Provider.ID,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil || changed != 1 {
				return newSAMLProtocolError(leafen.ErrorCodeSAMLRequestMismatch, "SAML request was already consumed")
			}
		}
		_, err = txApp.DB().Insert(leafen.SAMLCompletionsTable, dbx.Params{
			"id":        "lsc_" + completionHash[:20],
			"code_hash": completionHash, "provider_id": responseContext.Provider.ID,
			"user_id":                user.Id,
			"app_challenge":          responseContext.AppChallenge,
			"provider_second_factor": samlProviderSecondFactorFlag(assertion),
			"expires_at":             now.Add(leafen.SAMLCompletionTTLSeconds * time.Second).UnixMilli(),
			"consumed_at":            0, "created_at": now.UnixMilli(),
		}).Execute()
		return err
	})
	return completionCode, relayState, err
}

func authorizeSAMLIdentity(
	app core.App,
	provider storedSAMLProvider,
	email string,
	unknown *leafen.PreparedPassword,
) (*core.Record, error) {
	user, err := leafen.FindAccountByEmail(app, email)
	if errors.Is(err, sql.ErrNoRows) {
		user, err = provisionSAMLAccount(app, provider, email, unknown)
	} else if err == nil {
		// The organization's identity provider proved the address.
		err = leafen.ClaimUnprovenAddress(app, user, email)
	}
	if err != nil {
		return nil, err
	}
	authorization, err := leafen.AuthorizeOrganization(app, user.Id, provider.OrganizationID, leafen.PermissionOrganizationActive)
	if leafen.IsPermissionDenied(err) || (err == nil && authorization.Role == leafen.OrganizationRoleGuest) {
		return nil, errSAMLIdentityDenied
	}
	if err != nil {
		return nil, err
	}
	if err := requireVerifiedOrganizationDomain(app, provider.OrganizationID, email); err != nil {
		return nil, errSAMLIdentityDenied
	}
	return user, nil
}

func consumeSAMLCompletion(
	app core.App,
	code string,
	verifier string,
	metadata leafen.BrowserSessionMetadata,
) (leafen.SessionResponse, error) {
	if len(code) < 40 || len(code) > 128 {
		return leafen.SessionResponse{}, errSAMLCompletionInvalid
	}
	response := leafen.SessionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		completion := storedSAMLCompletion{}
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[provider_id]], [[user_id]], [[app_challenge]], [[expires_at]], [[consumed_at]], [[provider_second_factor]]
			FROM {{leafen_saml_completions}} WHERE [[code_hash]] = {:codeHash}
		`).Bind(dbx.Params{"codeHash": leafen.HashAuthProviderToken(code)}).One(&completion)
		if errors.Is(err, sql.ErrNoRows) {
			return errSAMLCompletionInvalid
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if completion.ConsumedAt != 0 {
			return errSAMLCompletionConsumed
		}
		if completion.ExpiresAt <= now {
			return errSAMLCompletionInvalid
		}
		// A completion an app's sign-in brought back is taken only from that app.
		if completion.AppChallenge != "" && !leafen.AuthAppVerifierMatches(completion.AppChallenge, verifier) {
			return errSAMLCompletionInvalid
		}
		user, err := txApp.FindRecordById("users", completion.UserID)
		if err != nil {
			return errSAMLCompletionInvalid
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_saml_completions}} SET [[consumed_at]] = {:now}
			WHERE [[id]] = {:id} AND [[consumed_at]] = 0 AND [[expires_at]] > {:now}
		`).Bind(dbx.Params{"now": now, "id": completion.ID}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return errSAMLCompletionConsumed
		}
		// The identity provider's own 2-step assurance, when its assertion named it.
		response, err = leafen.IssueAuthenticatedBrowserSessionInTransaction(
			txApp, user, metadata, false,
			leafen.SAMLAuthenticationMethodSAML, completion.ProviderID, leafen.AuthProviderSecondFactor(completion.ProviderSecondFactor),
		)
		if err != nil {
			return err
		}
		return leafen.RecordAuthAttemptInTransactionSuccessWithMetadata(
			txApp, user.Id, user.Email(), leafen.AuthAuditMethodSAMLSignin, leafen.AuthAuditPhaseVerify, metadata,
		)
	})
	return response, err
}

func cleanupExpiredSAMLLifecycle(app core.App, now int64) error {
	for _, cleanup := range []struct {
		table  string
		column string
	}{
		{leafen.SAMLRequestsTable, "expires_at"},
		{leafen.SAMLAssertionsTable, "expires_at"},
		{leafen.SAMLCompletionsTable, "expires_at"},
	} {
		if _, err := app.DB().NewQuery(`
			DELETE FROM {{` + cleanup.table + `}} WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{` + cleanup.table + `}}
				WHERE [[` + cleanup.column + `]] <= {:now}
				ORDER BY [[` + cleanup.column + `]], [[rowid]] LIMIT {:limit}
			)
		`).Bind(dbx.Params{"now": now, "limit": samlLifecycleCleanupBatchSize}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// provisionSAMLAccount makes the account of someone the organization's
// identity provider signs in for the first time, with an address in one of
// the organization's verified domains: it is made whatever the server's
// sign-up policy, as the organization's own SSO (2026-10-18), and joins the
// organization and its linked workspace as a member.
func provisionSAMLAccount(app core.App, provider storedSAMLProvider, email string, unknown *leafen.PreparedPassword) (*core.Record, error) {
	// With 계정 자동 생성 off, as Notion's, only people who already have an
	// account sign in: someone new is asked to have the organization invite them.
	if provider.AccountCreation == leafen.SAMLAccountCreationOff {
		return nil, errSAMLAccountCreationOff
	}
	if err := requireVerifiedOrganizationDomain(app, provider.OrganizationID, email); err != nil {
		return nil, errSAMLIdentityDenied
	}
	name, _, _ := strings.Cut(email, "@")
	user, err := findOrCreateSCIMUser(app, email, name, unknown)
	if err != nil {
		return nil, err
	}
	collection, err := app.FindCollectionByNameOrId(leafen.OrganizationMembershipsCollection)
	if err != nil {
		return nil, err
	}
	id, err := newSCIMRecordID("lom_")
	if err != nil {
		return nil, err
	}
	membership := core.NewRecord(collection)
	membership.Id = id
	membership.Set("organization", provider.OrganizationID)
	membership.Set("user", user.Id)
	membership.Set("role", leafen.OrganizationRoleMember)
	membership.Set("status", leafen.OrganizationMemberStatusActive)
	membership.Set("scim_managed", false)
	if err := app.Save(membership); err != nil {
		return nil, err
	}
	// Into the workspace the organization linked, as Notion's linked workspace.
	// One deleted since leaves the person to be invited.
	if provider.LinkedWorkspaceID != "" {
		open, err := leafen.OrganizationWorkspaceOpen(app, provider.LinkedWorkspaceID, provider.OrganizationID)
		if err != nil {
			return nil, err
		}
		if open {
			if err := leafen.JoinOrganizationWorkspace(app, provider.LinkedWorkspaceID, provider.OrganizationID, user.Id,
				map[string]any{"via": "saml"}); err != nil {
				return nil, err
			}
		}
	}
	return user, nil
}
