package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func completeAuthProviderSignIn(
	app core.App,
	state storedAuthProviderState,
	claims authProviderClaims,
) (string, error) {
	code, err := newAuthProviderToken("lao_", 32)
	if err != nil {
		return "", err
	}
	exchangeHash := hashAuthProviderToken(code)
	now := time.Now().UTC()
	// A first sign-in makes an account, its password one no one knows,
	// hashed before the transaction (password_outside_transaction.go).
	newPassword := preparedPassword{}
	if _, identityExists, err := findAuthProviderUser(app, state.Provider, claims.Issuer, claims.Subject); err == nil && !identityExists {
		if _, err := findAccountByEmail(app, claims.Email); errors.Is(err, sql.ErrNoRows) {
			if prepared, err := unknownPassword(app, "lap_"); err == nil {
				newPassword = prepared
			}
		}
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		user, identityExists, err := findAuthProviderUser(
			txApp, state.Provider, claims.Issuer, claims.Subject,
		)
		if err != nil {
			return err
		}
		if !identityExists {
			user, err = findOrCreateAuthProviderUser(txApp, claims, newPassword)
			if err != nil {
				return err
			}
			if err := createAuthProviderExternalIdentity(
				txApp, state.Provider, claims.Issuer, claims.Subject, user,
			); err != nil {
				return err
			}
		}
		if _, err := txApp.DB().Insert(authProviderExchangesTable, dbx.Params{
			"id": "lao_" + exchangeHash[:20], "code_hash": exchangeHash, "user_id": user.Id,
			"app_challenge":          state.AppChallenge,
			"provider_second_factor": authProviderSecondFactorFlag(claims.SecondFactor),
			"status":                 authProviderExchangePending,
			"expires_at":             now.Add(authProviderExchangeTTLSeconds * time.Second).UnixMilli(),
			"consumed_at":            0, "created_at": now.UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_auth_provider_states}}
			SET [[status]] = {:consumed}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = {:processing}
		`).Bind(dbx.Params{
			"consumed": authProviderStateConsumed, "now": now.UnixMilli(),
			"id": state.ID, "processing": authProviderStateProcessing,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAuthProviderStateInvalid, err)
		}
		return nil
	})
	return code, err
}

func authProviderExternalIdentityID(issuer, subject string) string {
	digest := sha256.Sum256([]byte(
		strconv.Itoa(len(issuer)) + ":" + issuer + strconv.Itoa(len(subject)) + ":" + subject,
	))
	return "leafen_" + hex.EncodeToString(digest[:])
}

func findAuthProviderUser(app core.App, provider, issuer, subject string) (*core.Record, bool, error) {
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return nil, false, err
	}
	external, err := app.FindFirstExternalAuthByExpr(dbx.HashExp{
		"collectionRef": users.Id,
		"provider":      provider,
		"providerId":    authProviderExternalIdentityID(issuer, subject),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	user, err := app.FindRecordById("users", external.RecordRef())
	return user, true, err
}

func createAuthProviderExternalIdentity(
	app core.App,
	provider, issuer, subject string,
	user *core.Record,
) error {
	external := core.NewExternalAuth(app)
	external.SetCollectionRef(user.Collection().Id)
	external.SetRecordRef(user.Id)
	external.SetProvider(provider)
	external.SetProviderId(authProviderExternalIdentityID(issuer, subject))
	return app.Save(external)
}

func findOrCreateAuthProviderUser(app core.App, claims authProviderClaims, newPassword preparedPassword) (*core.Record, error) {
	user, err := findAccountByEmail(app, claims.Email)
	if err == nil {
		if err := claimUnprovenAddress(app, user, claims.Email); err != nil {
			return nil, err
		}
		if !user.Verified() && normalizePageShareEmail(user.Email()) == claims.Email {
			user.SetVerified(true)
			if err := app.Save(user); err != nil {
				return nil, err
			}
			// The sign-in provider confirmed the address (account_email_proof.go).
			if err := markAccountEmailProven(app, claims.Email); err != nil {
				return nil, err
			}
			if err := admitVerifiedEmailInvitations(app, user); err != nil {
				return nil, err
			}
		}
		return user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// Where sign-up is closed the provider makes an account only for an
	// address it proved that an invitation or a workspace's allowed domain
	// admits (signup_invitations.go).
	admitted, err := signupAddressAdmitted(app, claims.Email)
	if err != nil {
		return nil, err
	}
	if _, err := authorizeSelfServiceAccountCreation(app, signupAdmission{Invited: admitted && claims.EmailVerified}); err != nil {
		return nil, err
	}
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return nil, err
	}
	if newPassword.plain == "" {
		if newPassword, err = unknownPassword(app, "lap_"); err != nil {
			return nil, err
		}
	}
	user = core.NewRecord(users)
	user.SetEmail(claims.Email)
	newPassword.set(user)
	user.SetVerified(true)
	user.Set("name", claims.Name)
	if err := app.Save(user); err != nil {
		return nil, err
	}
	// Its person signs in through the provider and sets a password, if
	// ever, in settings (account_password_known.go).
	if err := markAccountPasswordUnknown(app, user.Id); err != nil {
		return nil, err
	}
	// The sign-in provider confirmed the address (account_email_proof.go).
	if err := markAccountEmailProven(app, claims.Email); err != nil {
		return nil, err
	}
	if err := admitVerifiedEmailInvitations(app, user); err != nil {
		return nil, err
	}
	return user, nil
}

func consumeAuthProviderExchange(
	app core.App,
	code string,
	verifier string,
	metadata browserSessionMetadata,
) (sessionResponse, authMFAChallengeResponse, string, error) {
	if len(code) < 40 || len(code) > 128 {
		return sessionResponse{}, authMFAChallengeResponse{}, "", errAuthProviderExchangeInvalid
	}
	response := sessionResponse{}
	challenge := authMFAChallengeResponse{}
	ownerID := ""
	err := app.RunInTransaction(func(txApp core.App) error {
		exchange := storedAuthProviderExchange{}
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[user_id]], [[app_challenge]], [[status]], [[expires_at]], [[provider_second_factor]]
			FROM {{leafen_auth_provider_exchanges}} WHERE [[code_hash]] = {:codeHash}
		`).Bind(dbx.Params{"codeHash": hashAuthProviderToken(code)}).One(&exchange)
		if errors.Is(err, sql.ErrNoRows) {
			return errAuthProviderExchangeInvalid
		}
		if err != nil {
			return err
		}
		ownerID = exchange.UserID
		if exchange.Status == authProviderExchangeConsumed {
			return errAuthProviderExchangeUsed
		}
		now := time.Now().UTC().UnixMilli()
		if exchange.Status != authProviderExchangePending || exchange.ExpiresAt <= now {
			return errAuthProviderExchangeInvalid
		}
		// A code an app's sign-in brought back is taken only from that app.
		if exchange.AppChallenge != "" && !authAppVerifierMatches(exchange.AppChallenge, verifier) {
			return errAuthProviderExchangeInvalid
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_auth_provider_exchanges}}
			SET [[status]] = {:consumed}, [[consumed_at]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] = {:pending} AND [[expires_at]] > {:now}
		`).Bind(dbx.Params{
			"consumed": authProviderExchangeConsumed, "now": now, "id": exchange.ID,
			"pending": authProviderExchangePending,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAuthProviderExchangeUsed, err)
		}
		user, err := txApp.FindRecordById("users", exchange.UserID)
		if err != nil {
			return err
		}
		// The provider's sign-in is one factor, or two when it vouched so;
		// 2-step verification asks for its code before the session opens,
		// as after a password.
		mfaRequired := false
		response, challenge, mfaRequired, err = beginMFAChallengeForSignInInTransaction(
			txApp, user, metadata, false, authSessionReplayIdentity{}, samlAuthenticationMethodOAuth,
			authProviderSecondFactor(exchange.ProviderSecondFactor),
		)
		if err != nil {
			return err
		}
		return recordAuthAttemptInTransactionSuccessWithMetadata(
			txApp, user.Id, user.Email(), authAuditMethodOAuthSignin,
			signInStepPhase(mfaRequired), metadata,
		)
	})
	return response, challenge, ownerID, err
}
