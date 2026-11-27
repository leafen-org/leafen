package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errAuthProviderStateInvalid    = errors.New(errorCodeAuthProviderStateInvalid)
	errAuthProviderExchangeInvalid = errors.New(errorCodeAuthProviderExchangeInvalid)
	errAuthProviderExchangeUsed    = errors.New(errorCodeAuthProviderExchangeConsumed)
)

type storedAuthProviderState struct {
	ID               string `db:"id"`
	Provider         string `db:"provider"`
	SettingsRevision int    `db:"settings_revision"`
	Issuer           string `db:"issuer"`
	NonceHash        string `db:"nonce_hash"`
	VerifierCipher   string `db:"verifier_cipher"`
	RedirectURI      string `db:"redirect_uri"`
	AppReturn        string `db:"app_return"`
	AppChallenge     string `db:"app_challenge"`
	Status           string `db:"status"`
	ExpiresAt        int64  `db:"expires_at"`
}

type storedAuthProviderExchange struct {
	ID           string `db:"id"`
	UserID       string `db:"user_id"`
	AppChallenge string `db:"app_challenge"`
	Status       string `db:"status"`
	ExpiresAt    int64  `db:"expires_at"`
	// ProviderSecondFactor: the provider vouched for a second factor.
	ProviderSecondFactor int `db:"provider_second_factor"`
}

func createAuthProviderState(
	app core.App,
	config authProviderConfig,
	redirectURI string,
	appReturn authAppReturn,
) (state string, nonce string, verifier string, err error) {
	state, err = newAuthProviderToken("las_", 32)
	if err != nil {
		return "", "", "", err
	}
	nonce, err = newAuthProviderToken("lan_", 24)
	if err != nil {
		return "", "", "", err
	}
	verifier, err = newAuthProviderToken("", 32)
	if err != nil {
		return "", "", "", err
	}
	verifierCipher, err := encryptAuthProviderVerifier(app, verifier)
	if err != nil {
		return "", "", "", err
	}
	now := time.Now().UTC()
	hash := hashAuthProviderToken(state)
	err = app.RunInTransaction(func(txApp core.App) error {
		if err := pruneAuthProviderArtifacts(txApp, now.UnixMilli()); err != nil {
			return err
		}
		_, err := txApp.DB().Insert(authProviderStatesTable, dbx.Params{
			"id": "las_" + hash[:20], "state_hash": hash, "provider": config.ID,
			"settings_revision": config.SettingsRevision, "issuer": config.Issuer,
			"nonce_hash": hashAuthProviderToken(nonce), "verifier_cipher": verifierCipher,
			"redirect_uri": redirectURI, "app_return": appReturn.Address, "app_challenge": appReturn.Challenge,
			"status":     authProviderStatePending,
			"expires_at": now.Add(authProviderStateTTLSeconds * time.Second).UnixMilli(),
			"created_at": now.UnixMilli(), "updated_at": now.UnixMilli(),
		}).Execute()
		return err
	})
	return state, nonce, verifier, err
}

func claimAuthProviderState(app core.App, provider, state string) (storedAuthProviderState, error) {
	if len(state) < 40 || len(state) > 128 {
		return storedAuthProviderState{}, errAuthProviderStateInvalid
	}
	claimed := storedAuthProviderState{}
	err := app.RunInTransaction(func(txApp core.App) error {
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[provider]], [[settings_revision]], [[issuer]], [[nonce_hash]],
				[[verifier_cipher]], [[redirect_uri]], [[app_return]], [[app_challenge]], [[status]], [[expires_at]]
			FROM {{leafen_auth_provider_states}} WHERE [[state_hash]] = {:stateHash}
		`).Bind(dbx.Params{"stateHash": hashAuthProviderToken(state)}).One(&claimed)
		if errors.Is(err, sql.ErrNoRows) {
			return errAuthProviderStateInvalid
		}
		if err != nil {
			return err
		}
		if claimed.Provider != provider ||
			claimed.Status != authProviderStatePending || claimed.ExpiresAt <= time.Now().UTC().UnixMilli() {
			return errAuthProviderStateInvalid
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_auth_provider_states}}
			SET [[status]] = {:processing}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = {:pending} AND [[expires_at]] > {:now}
		`).Bind(dbx.Params{
			"processing": authProviderStateProcessing, "now": time.Now().UTC().UnixMilli(),
			"id": claimed.ID, "pending": authProviderStatePending,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAuthProviderStateInvalid, err)
		}
		claimed.Status = authProviderStateProcessing
		return nil
	})
	return claimed, err
}

func markAuthProviderStateFailed(app core.App, stateID string) {
	_, _ = app.DB().NewQuery(`
		UPDATE {{leafen_auth_provider_states}} SET [[status]] = {:failed}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[status]] = {:processing}
	`).Bind(dbx.Params{
		"failed": authProviderStateFailed, "now": time.Now().UTC().UnixMilli(),
		"id": stateID, "processing": authProviderStateProcessing,
	}).Execute()
}

func pruneAuthProviderArtifacts(app core.App, now int64) error {
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_auth_provider_states}} WHERE [[id]] IN (
			SELECT [[id]] FROM {{leafen_auth_provider_states}}
			WHERE [[expires_at]] <= {:now} ORDER BY [[expires_at]] LIMIT 64
		)
	`).Bind(dbx.Params{"now": now}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_auth_provider_exchanges}} WHERE [[id]] IN (
			SELECT [[id]] FROM {{leafen_auth_provider_exchanges}}
			WHERE [[expires_at]] <= {:now} ORDER BY [[expires_at]] LIMIT 64
		)
	`).Bind(dbx.Params{"now": now}).Execute()
	return err
}
