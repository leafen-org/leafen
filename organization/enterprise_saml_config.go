package organization

import (
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const defaultSAMLEmailAttribute = "email"

var (
	errSAMLProviderNotFound   = errors.New(leafen.ErrorCodeSAMLProviderNotFound)
	errSAMLCompletionInvalid  = errors.New(leafen.ErrorCodeSAMLCompletionInvalid)
	errSAMLCompletionConsumed = errors.New(leafen.ErrorCodeSAMLCompletionConsumed)
	errSAMLIdentityDenied     = errors.New(leafen.ErrorCodeSAMLIdentityDenied)
	errSAMLAccountCreationOff = errors.New(leafen.ErrorCodeSAMLAccountCreationOff)
)

type storedSAMLProvider struct {
	ID                string `db:"id"`
	OrganizationID    string `db:"organization_id"`
	Enabled           int    `db:"enabled"`
	Issuer            string `db:"issuer"`
	LoginURL          string `db:"login_url"`
	CertificateCipher string `db:"certificate_cipher"`
	AllowIDPInitiated int    `db:"allow_idp_initiated"`
	Enforcement       string `db:"enforcement"`
	EmailAttribute    string `db:"email_attribute"`
	LinkedWorkspaceID string `db:"linked_workspace_id"`
	AccountCreation   string `db:"account_creation"`
	Revision          int    `db:"revision"`
	CreatedAt         int64  `db:"created_at"`
	UpdatedAt         int64  `db:"updated_at"`
}

type samlProviderConfig struct {
	storedSAMLProvider
	Certificate string
}

type storedSAMLDomain struct {
	ProviderID string `db:"provider_id"`
	Domain     string `db:"domain"`
	Status     string `db:"status"`
	Challenge  string `db:"challenge"`
	VerifiedAt int64  `db:"verified_at"`
	CreatedAt  int64  `db:"created_at"`
	UpdatedAt  int64  `db:"updated_at"`
}

func normalizeSAMLConfiguration(input leafen.SAMLConfigurationInput) (leafen.SAMLConfigurationInput, error) {
	input.Issuer = strings.TrimSpace(input.Issuer)
	input.LoginURL = strings.TrimSpace(input.LoginURL)
	input.Certificate = strings.TrimSpace(input.Certificate)
	input.EmailAttribute = strings.TrimSpace(input.EmailAttribute)
	if input.EmailAttribute == "" {
		input.EmailAttribute = defaultSAMLEmailAttribute
	}
	if input.Enforcement == "" {
		input.Enforcement = leafen.SAMLEnforcementOptional
	}
	if input.Issuer == "" || len(input.Issuer) > 2048 || input.LoginURL == "" ||
		len(input.LoginURL) > 2048 || input.Certificate == "" || len(input.Certificate) > 65536 ||
		len(input.EmailAttribute) > 255 || !validSAMLEnforcement(input.Enforcement) {
		return leafen.SAMLConfigurationInput{}, samlRefused(leafen.ErrorCodeSAMLFieldsRequired,
			"Fill in the issuer, the identity provider's login URL and its certificate.")
	}
	if !input.Enabled && input.Enforcement != leafen.SAMLEnforcementOptional {
		return leafen.SAMLConfigurationInput{}, samlRefused(leafen.ErrorCodeSAMLEnforcementNeedsSSO,
			"Turn SAML SSO on before requiring it.")
	}
	issuer, err := url.Parse(input.Issuer)
	if err != nil || issuer.Scheme == "" || issuer.Fragment != "" {
		return leafen.SAMLConfigurationInput{}, samlRefused(leafen.ErrorCodeSAMLIssuerInvalid,
			"The issuer must be a URL or URN, such as https://idp.example.com/metadata.")
	}
	loginURL, err := url.Parse(input.LoginURL)
	if err != nil || loginURL.Scheme != "https" || loginURL.Host == "" ||
		loginURL.User != nil || loginURL.Fragment != "" {
		return leafen.SAMLConfigurationInput{}, samlRefused(leafen.ErrorCodeSAMLLoginURLNotHTTPS,
			"The identity provider's login URL must start with https://.")
	}
	block, rest := pem.Decode([]byte(input.Certificate))
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return leafen.SAMLConfigurationInput{}, samlRefused(leafen.ErrorCodeSAMLCertificateInvalid,
			"The certificate must be one X.509 certificate in PEM form, starting with -----BEGIN CERTIFICATE-----.")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return leafen.SAMLConfigurationInput{}, samlRefused(leafen.ErrorCodeSAMLCertificateInvalid,
			"The certificate must be one X.509 certificate in PEM form, starting with -----BEGIN CERTIFICATE-----.")
	}
	return input, nil
}

// samlRefusal is why SAML settings are refused, with its own code, so the
// settings say which field to fix (2026-10-16: an http sign-in URL read
// only "check the SAML settings"). It is still ErrSAMLConfigurationInvalid.
type samlRefusal struct {
	code    string
	message string
}

func samlRefused(code, message string) error { return &samlRefusal{code: code, message: message} }

func (refusal *samlRefusal) Error() string { return refusal.message }

func (refusal *samlRefusal) Is(target error) bool {
	return target == leafen.ErrSAMLConfigurationInvalid
}

func validSAMLEnforcement(value string) bool {
	return value == leafen.SAMLEnforcementOptional || value == leafen.SAMLEnforcementVerifiedDomains ||
		value == leafen.SAMLEnforcementAllMembers
}

func saveSAMLConfiguration(
	app core.App,
	organization leafen.OrganizationAuthorization,
	input leafen.SAMLConfigurationInput,
) (leafen.SAMLProviderResponse, error) {
	// The certificate is never shown again, so a save that leaves it empty
	// keeps the one stored — turning SSO off or changing its rule doesn't
	// ask for it once more (2026-10-18).
	if strings.TrimSpace(input.Certificate) == "" {
		if current, err := loadStoredSAMLProviderByOrganization(app, organization.OrganizationID); err == nil && current.CertificateCipher != "" {
			kept, err := leafen.DecryptAdministratorSetting(app, current.CertificateCipher)
			if err != nil {
				return leafen.SAMLProviderResponse{}, err
			}
			input.Certificate = kept
		} else if err != nil && !errors.Is(err, errSAMLProviderNotFound) {
			return leafen.SAMLProviderResponse{}, err
		}
	}
	normalized, err := normalizeSAMLConfiguration(input)
	if err != nil {
		return leafen.SAMLProviderResponse{}, err
	}
	normalized.LinkedWorkspaceID = strings.TrimSpace(normalized.LinkedWorkspaceID)
	// 계정 자동 생성 keeps its value when a request leaves it out (an older
	// screen or an API call that knows nothing of it); a new configuration
	// makes accounts once it names the workspace they join.
	normalized.AccountCreation = strings.TrimSpace(normalized.AccountCreation)
	if normalized.AccountCreation == "" {
		normalized.AccountCreation = leafen.SAMLAccountCreationOff
		if normalized.LinkedWorkspaceID != "" {
			normalized.AccountCreation = leafen.SAMLAccountCreationAutomatic
		}
		if current, err := loadStoredSAMLProviderByOrganization(app, organization.OrganizationID); err == nil {
			normalized.AccountCreation = current.AccountCreation
		} else if !errors.Is(err, errSAMLProviderNotFound) {
			return leafen.SAMLProviderResponse{}, err
		}
	}
	if normalized.AccountCreation != leafen.SAMLAccountCreationAutomatic && normalized.AccountCreation != leafen.SAMLAccountCreationOff {
		return leafen.SAMLProviderResponse{}, samlRefused(leafen.ErrorCodeSAMLFieldsRequired,
			"Choose whether SAML login makes accounts.")
	}
	// A newly linked workspace must be the organization's. One linked before
	// and deleted since stays as it is (sign-in then leaves new people to be
	// invited), so the rest of the settings still save.
	if normalized.LinkedWorkspaceID != "" {
		open, err := leafen.OrganizationWorkspaceOpen(app, normalized.LinkedWorkspaceID, organization.OrganizationID)
		if err != nil {
			return leafen.SAMLProviderResponse{}, err
		}
		if !open {
			current, err := loadStoredSAMLProviderByOrganization(app, organization.OrganizationID)
			if err != nil && !errors.Is(err, errSAMLProviderNotFound) {
				return leafen.SAMLProviderResponse{}, err
			}
			open = err == nil && current.LinkedWorkspaceID == normalized.LinkedWorkspaceID
		}
		if !open {
			return leafen.SAMLProviderResponse{}, samlRefused(leafen.ErrorCodeSAMLLinkedWorkspaceInvalid,
				"The linked workspace must be one of the organization's workspaces.")
		}
	}
	response := leafen.SAMLProviderResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredSAMLProviderByOrganization(txApp, organization.OrganizationID)
		if err != nil && !errors.Is(err, errSAMLProviderNotFound) {
			return err
		}
		providerID := current.ID
		revision := current.Revision + 1
		createdAt := current.CreatedAt
		now := time.Now().UTC().UnixMilli()
		if errors.Is(err, errSAMLProviderNotFound) {
			// A first provider takes its organization's id, so the metadata
			// URL the settings show before the first save — the server's
			// entity ID (newSAMLServiceProvider), which the identity provider
			// is given while its side is set up — stays the one it checks.
			// Providers made earlier keep the ids their identity providers have.
			providerID = organization.OrganizationID
			revision = 1
			createdAt = now
		}
		certificateCipher, err := leafen.EncryptAdministratorSetting(txApp, leafen.CipherSAMLProvider, normalized.Certificate)
		if err != nil {
			return err
		}
		if requiresSAMLEnforcementTransition(current, normalized) {
			if err := revokeSessionsForSAMLEnforcement(txApp, organization.OrganizationID, providerID, normalized.Enforcement, now); err != nil {
				return err
			}
		}
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_saml_providers}} (
				[[id]], [[organization_id]], [[enabled]], [[issuer]], [[login_url]],
				[[certificate_cipher]], [[allow_idp_initiated]], [[enforcement]],
				[[email_attribute]], [[linked_workspace_id]], [[account_creation]], [[revision]], [[created_at]], [[updated_at]]
			) VALUES (
				{:id}, {:organizationId}, {:enabled}, {:issuer}, {:loginUrl},
				{:certificateCipher}, {:allowIdpInitiated}, {:enforcement},
				{:emailAttribute}, {:linkedWorkspaceId}, {:accountCreation}, {:revision}, {:createdAt}, {:updatedAt}
			)
			ON CONFLICT ([[organization_id]]) DO UPDATE SET
				[[enabled]] = excluded.[[enabled]], [[issuer]] = excluded.[[issuer]],
				[[login_url]] = excluded.[[login_url]],
				[[certificate_cipher]] = excluded.[[certificate_cipher]],
				[[allow_idp_initiated]] = excluded.[[allow_idp_initiated]],
				[[enforcement]] = excluded.[[enforcement]],
				[[email_attribute]] = excluded.[[email_attribute]],
				[[linked_workspace_id]] = excluded.[[linked_workspace_id]],
				[[account_creation]] = excluded.[[account_creation]],
				[[revision]] = excluded.[[revision]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"id": providerID, "organizationId": organization.OrganizationID,
			"enabled": leafen.BoolInt(normalized.Enabled), "issuer": normalized.Issuer,
			"loginUrl": normalized.LoginURL, "certificateCipher": certificateCipher,
			"allowIdpInitiated": leafen.BoolInt(normalized.AllowIDPInitiated),
			"enforcement":       normalized.Enforcement, "emailAttribute": normalized.EmailAttribute,
			"linkedWorkspaceId": normalized.LinkedWorkspaceID,
			"accountCreation":   normalized.AccountCreation,
			"revision":          revision, "createdAt": createdAt, "updatedAt": now,
		}).Execute()
		if err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_organization_policy_state}}
			SET [[version]] = [[version]] + 1, [[updated]] = {:updated}
			WHERE [[organization_id]] = {:organizationId}
		`).Bind(dbx.Params{
			"organizationId": organization.OrganizationID, "updated": now,
		}).Execute(); err != nil {
			return err
		}
		changes, err := samlAuditChanges(txApp, current, normalized)
		if err != nil {
			return err
		}
		if err := leafen.RecordOrganizationAuditEvent(txApp, leafen.OrganizationAuditEvent{
			OrganizationID: organization.OrganizationID, ActorID: organization.PrincipalID,
			Action: "organization.saml.updated", TargetType: "saml_provider", TargetID: providerID + ":" + strconv.Itoa(revision),
			// Named by the identity provider's entity ID as it then was.
			Metadata: map[string]any{"changes": changes, "name": normalized.Issuer}, OccurredAt: now,
		}); err != nil {
			return err
		}
		response = publicSAMLProviderResponse(storedSAMLProvider{
			ID: providerID, OrganizationID: organization.OrganizationID,
			Enabled: leafen.BoolInt(normalized.Enabled), Issuer: normalized.Issuer,
			LoginURL: normalized.LoginURL, AllowIDPInitiated: leafen.BoolInt(normalized.AllowIDPInitiated),
			Enforcement: normalized.Enforcement, EmailAttribute: normalized.EmailAttribute,
			LinkedWorkspaceID: normalized.LinkedWorkspaceID, AccountCreation: normalized.AccountCreation,
			Revision: revision, CreatedAt: createdAt, UpdatedAt: now,
		})
		return nil
	})
	return response, err
}

// samlAuditChanges names what a save changed, each setting with the value it
// had and the one it took, so the audit log tells a new certificate from SSO
// alone switched on or off (2026-11-12: three saves all read only "SAML 설정
// 변경"). A certificate is said to change, never shown.
func samlAuditChanges(app core.App, current storedSAMLProvider, next leafen.SAMLConfigurationInput) ([]map[string]any, error) {
	before := map[string]any{
		"enabled": current.Enabled == 1, "issuer": current.Issuer, "loginUrl": current.LoginURL,
		"allowIdpInitiated": current.AllowIDPInitiated == 1, "enforcement": current.Enforcement,
		"emailAttribute": current.EmailAttribute, "linkedWorkspaceId": current.LinkedWorkspaceID,
		"accountCreation": current.AccountCreation,
	}
	after := map[string]any{
		"enabled": next.Enabled, "issuer": next.Issuer, "loginUrl": next.LoginURL,
		"allowIdpInitiated": next.AllowIDPInitiated, "enforcement": next.Enforcement,
		"emailAttribute": next.EmailAttribute, "linkedWorkspaceId": next.LinkedWorkspaceID,
		"accountCreation": next.AccountCreation,
	}
	changes := leafen.OrganizationSettingsChanges(before, after)
	certificate := ""
	if current.CertificateCipher != "" {
		kept, err := leafen.DecryptAdministratorSetting(app, current.CertificateCipher)
		if err != nil {
			return nil, err
		}
		certificate = kept
	}
	if strings.TrimSpace(certificate) != strings.TrimSpace(next.Certificate) {
		changes = append(changes, map[string]any{"setting": "certificate"})
	}
	return changes, nil
}

func publicSAMLProviderResponse(provider storedSAMLProvider) leafen.SAMLProviderResponse {
	return leafen.SAMLProviderResponse{
		ID: provider.ID, OrganizationID: provider.OrganizationID,
		Enabled: provider.Enabled == 1, Issuer: provider.Issuer, LoginURL: provider.LoginURL,
		AllowIDPInitiated: provider.AllowIDPInitiated == 1, Enforcement: provider.Enforcement,
		EmailAttribute: provider.EmailAttribute, LinkedWorkspaceID: provider.LinkedWorkspaceID,
		AccountCreation: provider.AccountCreation, Revision: provider.Revision,
	}
}

func requiresSAMLEnforcementTransition(current storedSAMLProvider, next leafen.SAMLConfigurationInput) bool {
	return next.Enabled && next.Enforcement != leafen.SAMLEnforcementOptional &&
		(current.Enabled == 0 || current.Enforcement != next.Enforcement)
}

func loadStoredSAMLProviderByOrganization(app core.App, organizationID string) (storedSAMLProvider, error) {
	return loadStoredSAMLProvider(app, "[[organization_id]] = {:value}", organizationID)
}

func loadStoredSAMLProviderByID(app core.App, providerID string) (storedSAMLProvider, error) {
	return loadStoredSAMLProvider(app, "[[id]] = {:value}", providerID)
}

func loadStoredSAMLProviderByIssuer(app core.App, issuer string) (storedSAMLProvider, error) {
	return loadStoredSAMLProvider(app, "[[issuer]] = {:value}", issuer)
}

func loadStoredSAMLProvider(app core.App, predicate string, value string) (storedSAMLProvider, error) {
	provider := storedSAMLProvider{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[organization_id]], [[enabled]], [[issuer]], [[login_url]],
			[[certificate_cipher]], [[allow_idp_initiated]], [[enforcement]],
			[[email_attribute]], [[linked_workspace_id]], [[account_creation]], [[revision]], [[created_at]], [[updated_at]]
		FROM {{leafen_saml_providers}} WHERE ` + predicate + `
	`).Bind(dbx.Params{"value": value}).One(&provider)
	if errors.Is(err, sql.ErrNoRows) {
		return provider, errSAMLProviderNotFound
	}
	return provider, err
}

func loadSAMLProviderConfig(app core.App, provider storedSAMLProvider) (samlProviderConfig, error) {
	certificate, err := leafen.DecryptAdministratorSetting(app, provider.CertificateCipher)
	if err != nil {
		return samlProviderConfig{}, err
	}
	return samlProviderConfig{storedSAMLProvider: provider, Certificate: certificate}, nil
}
