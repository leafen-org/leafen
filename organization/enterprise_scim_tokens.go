package organization

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	scimTokenIDPrefix = "lst_"
	scimSecretPrefix  = "lsc_"
	scimTokenActive   = "active"
	scimTokenRevoked  = "revoked"
)

type storedSCIMConfiguration struct {
	OrganizationID   string `db:"organization_id"`
	Enabled          int    `db:"enabled"`
	ProvisioningMode string `db:"provisioning_mode"`
	Deprovision      string `db:"deprovision_action"`
	Revision         int    `db:"revision"`
	UpdatedBy        string `db:"updated_by"`
	CreatedAt        int64  `db:"created_at"`
	UpdatedAt        int64  `db:"updated_at"`
}

type storedSCIMToken struct {
	ID             string `db:"id"`
	OrganizationID string `db:"organization_id"`
	Label          string `db:"label"`
	TokenPrefix    string `db:"token_prefix"`
	TokenHash      string `db:"token_hash"`
	Status         string `db:"status"`
	ExpiresAt      int64  `db:"expires_at"`
	LastUsedAt     int64  `db:"last_used_at"`
	CreatedBy      string `db:"created_by"`
	CreatedAt      int64  `db:"created_at"`
	RevokedAt      int64  `db:"revoked_at"`
}

func saveSCIMConfiguration(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	input leafen.SCIMConfigurationInput,
) (leafen.SCIMConfigurationResponse, error) {
	if input.ProvisioningMode != leafen.SCIMProvisioningModeV2 ||
		input.DeprovisionAction != leafen.SCIMDeprovisionDeactivate {
		return leafen.SCIMConfigurationResponse{}, errOrganizationDomainNotVerified
	}
	now := time.Now().UTC().UnixMilli()
	err := app.RunInTransaction(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_scim_settings}} (
				[[organization_id]], [[enabled]], [[provisioning_mode]], [[deprovision_action]],
				[[revision]], [[updated_by]], [[created_at]], [[updated_at]]
			) VALUES (
				{:organizationId}, {:enabled}, {:mode}, {:deprovision}, 1, {:actorId}, {:now}, {:now}
			)
			ON CONFLICT ([[organization_id]]) DO UPDATE SET
				[[enabled]] = excluded.[[enabled]],
				[[provisioning_mode]] = excluded.[[provisioning_mode]],
				[[deprovision_action]] = excluded.[[deprovision_action]],
				[[revision]] = {{leafen_scim_settings}}.[[revision]] + 1,
				[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "enabled": leafen.BoolInt(input.Enabled),
			"mode": input.ProvisioningMode, "deprovision": input.DeprovisionAction,
			"actorId": authorization.PrincipalID, "now": now,
		}).Execute()
		if err != nil {
			return err
		}
		if err := leafen.BumpOrganizationPolicy(txApp, authorization.OrganizationID, now); err != nil {
			return err
		}
		return leafen.RecordOrganizationAudit(
			txApp, authorization.OrganizationID, authorization.PrincipalID,
			"organization.scim.configuration.updated", "organization", authorization.OrganizationID,
		)
	})
	if err != nil {
		return leafen.SCIMConfigurationResponse{}, err
	}
	return loadSCIMConfigurationResponse(app, authorization.OrganizationID)
}

func loadSCIMConfiguration(app core.App, organizationID string) (storedSCIMConfiguration, error) {
	row := storedSCIMConfiguration{}
	err := app.DB().NewQuery(`
		SELECT [[organization_id]], [[enabled]], [[provisioning_mode]], [[deprovision_action]],
			[[revision]], [[updated_by]], [[created_at]], [[updated_at]]
		FROM {{leafen_scim_settings}} WHERE [[organization_id]] = {:organizationId}
	`).Bind(dbx.Params{"organizationId": organizationID}).One(&row)
	return row, err
}

func loadSCIMConfigurationResponse(app core.App, organizationID string) (leafen.SCIMConfigurationResponse, error) {
	configuration, err := loadSCIMConfiguration(app, organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return leafen.SCIMConfigurationResponse{
			ProvisioningMode:  leafen.SCIMProvisioningModeV2,
			DeprovisionAction: leafen.SCIMDeprovisionDeactivate,
			Tokens:            []leafen.SCIMTokenMetadata{},
		}, nil
	}
	if err != nil {
		return leafen.SCIMConfigurationResponse{}, err
	}
	rows := []storedSCIMToken{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[organization_id]], [[label]], [[token_prefix]], [[token_hash]],
			[[status]], [[expires_at]], [[last_used_at]], [[created_by]], [[created_at]], [[revoked_at]]
		FROM {{leafen_scim_tokens}} WHERE [[organization_id]] = {:organizationId}
		ORDER BY [[created_at]], [[id]]
	`).Bind(dbx.Params{"organizationId": organizationID}).All(&rows); err != nil {
		return leafen.SCIMConfigurationResponse{}, err
	}
	tokens := make([]leafen.SCIMTokenMetadata, 0, len(rows))
	for _, row := range rows {
		tokens = append(tokens, leafen.SCIMTokenMetadata{
			ID: row.ID, Label: row.Label, Status: row.Status, CreatedAt: row.CreatedAt,
			ExpiresAt: row.ExpiresAt, LastUsedAt: row.LastUsedAt,
		})
	}
	return leafen.SCIMConfigurationResponse{
		Enabled: configuration.Enabled == 1, ProvisioningMode: configuration.ProvisioningMode,
		DeprovisionAction: configuration.Deprovision, Revision: configuration.Revision, Tokens: tokens,
	}, nil
}

func createSCIMToken(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	input leafen.SCIMTokenCreateInput,
) (leafen.SCIMTokenCreationResponse, error) {
	label := strings.TrimSpace(input.Label)
	if label == "" || utf8.RuneCountInString(label) > leafen.SCIMMaxTokenLabelRunes {
		return leafen.SCIMTokenCreationResponse{}, errOrganizationDomainNotVerified
	}
	configuration, err := loadSCIMConfiguration(app, authorization.OrganizationID)
	if err != nil || configuration.Enabled != 1 {
		return leafen.SCIMTokenCreationResponse{}, errOrganizationDomainNotVerified
	}
	secret, err := leafen.NewAuthProviderToken(scimSecretPrefix, 32)
	if err != nil {
		return leafen.SCIMTokenCreationResponse{}, err
	}
	id, err := leafen.NewAuthProviderToken(scimTokenIDPrefix, 12)
	if err != nil {
		return leafen.SCIMTokenCreationResponse{}, err
	}
	now := time.Now().UTC().UnixMilli()
	digest := sha256.Sum256([]byte(secret))
	prefix := secret[:min(20, len(secret))]
	err = app.RunInTransaction(func(txApp core.App) error {
		_, err := txApp.DB().Insert(leafen.SCIMTokensTable, dbx.Params{
			"id": id, "organization_id": authorization.OrganizationID, "label": label,
			"token_prefix": prefix, "token_hash": hex.EncodeToString(digest[:]),
			"status": scimTokenActive, "expires_at": 0, "last_used_at": 0,
			"created_by": authorization.PrincipalID, "created_at": now, "revoked_at": 0,
		}).Execute()
		if err != nil {
			return err
		}
		return leafen.RecordOrganizationAudit(
			txApp, authorization.OrganizationID, authorization.PrincipalID,
			"organization.scim.token.created", "scim_token", id,
		)
	})
	if err != nil {
		return leafen.SCIMTokenCreationResponse{}, err
	}
	return leafen.SCIMTokenCreationResponse{ID: id, Label: label, Token: secret, CreatedAt: now}, nil
}

func revokeSCIMToken(app core.App, authorization leafen.OrganizationAuthorization, tokenID string) error {
	now := time.Now().UTC().UnixMilli()
	return app.RunInTransaction(func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_scim_tokens}}
			SET [[status]] = {:revoked}, [[revoked_at]] = {:now}
			WHERE [[id]] = {:id} AND [[organization_id]] = {:organizationId}
				AND [[status]] = {:active}
		`).Bind(dbx.Params{
			"revoked": scimTokenRevoked, "now": now, "id": tokenID,
			"organizationId": authorization.OrganizationID, "active": scimTokenActive,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return sql.ErrNoRows
		}
		return leafen.RecordOrganizationAudit(
			txApp, authorization.OrganizationID, authorization.PrincipalID,
			"organization.scim.token.revoked", "scim_token", tokenID,
		)
	})
}

func authenticateSCIMToken(app core.App, secret string) (storedSCIMToken, error) {
	if !strings.HasPrefix(secret, scimSecretPrefix) || len(secret) < 24 {
		return storedSCIMToken{}, sql.ErrNoRows
	}
	row := storedSCIMToken{}
	prefix := secret[:min(20, len(secret))]
	err := app.DB().NewQuery(`
		SELECT [[token]].[[id]], [[token]].[[organization_id]], [[token]].[[label]],
			[[token]].[[token_prefix]], [[token]].[[token_hash]], [[token]].[[status]],
			[[token]].[[expires_at]], [[token]].[[last_used_at]], [[token]].[[created_by]],
			[[token]].[[created_at]], [[token]].[[revoked_at]]
		FROM {{leafen_scim_tokens}} [[token]]
		JOIN {{leafen_scim_settings}} [[setting]]
			ON [[setting]].[[organization_id]] = [[token]].[[organization_id]]
		WHERE [[token]].[[token_prefix]] = {:prefix} AND [[token]].[[status]] = {:active}
			AND [[setting]].[[enabled]] = 1
	`).Bind(dbx.Params{"prefix": prefix, "active": scimTokenActive}).One(&row)
	if err != nil {
		return storedSCIMToken{}, err
	}
	expected, err := hex.DecodeString(row.TokenHash)
	if err != nil {
		return storedSCIMToken{}, sql.ErrNoRows
	}
	digest := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(digest[:], expected) != 1 ||
		(row.ExpiresAt > 0 && row.ExpiresAt <= time.Now().UTC().UnixMilli()) {
		return storedSCIMToken{}, sql.ErrNoRows
	}
	now := time.Now().UTC().UnixMilli()
	_, _ = app.DB().NewQuery(`
		UPDATE {{leafen_scim_tokens}} SET [[last_used_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = {:active}
			AND ([[last_used_at]] = 0 OR [[last_used_at]] < {:refreshBefore})
	`).Bind(dbx.Params{
		"now": now, "refreshBefore": now - 60_000, "id": row.ID, "active": scimTokenActive,
	}).Execute()
	return row, nil
}
