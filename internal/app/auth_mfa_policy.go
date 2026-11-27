package app

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	authMFAFactorIDPrefix = "lmf_"
	authMFAFactorTypeTOTP = "totp"
)

var (
	errMFAAlreadyEnabled      = errors.New(errorCodeMFAAlreadyEnabled)
	errMFAFactorNotFound      = errors.New(errorCodeMFAFactorNotFound)
	errMFACodeInvalid         = errors.New(errorCodeMFACodeInvalid)
	errMFAPasswordInvalid     = errors.New(errorCodeInvalidCredentials)
	errMFARecoveryCodeInvalid = errors.New(errorCodeMFARecoveryCodeInvalid)
	errMFAChallengeInvalid    = errors.New(errorCodeMFAChallengeInvalid)
	errMFAReadBound           = errors.New("MFA_AUTHORITY_BOUND_EXCEEDED")
	authMFANow                = func() time.Time { return time.Now().UTC() }
)

type storedAuthMFAFactor struct {
	ID           string `db:"id"`
	OwnerID      string `db:"owner_id"`
	Type         string `db:"type"`
	SecretCipher string `db:"secret_cipher"`
	VerifiedAt   int64  `db:"verified_at"`
	LastCounter  int64  `db:"last_counter"`
	CreatedAt    int64  `db:"created_at"`
}

type storedAuthMFAChallenge struct {
	TicketHash        string `db:"ticket_hash"`
	OwnerID           string `db:"owner_id"`
	ReplaceOldest     bool   `db:"replace_oldest"`
	SignInMethod      string `db:"sign_in_method"`
	AttemptsRemaining int    `db:"attempts_remaining"`
	ExpiresAt         int64  `db:"expires_at"`
	ConsumedAt        int64  `db:"consumed_at"`
}

type storedRecoveryCode struct {
	ID       string `db:"id"`
	CodeHash string `db:"code_hash"`
}

type authMFACompletionResult struct {
	Session        sessionResponse
	OwnerID        string
	AuditReference string
	AuditRecorded  bool
}

func storedMFAFactors(app core.App, ownerID string) ([]storedAuthMFAFactor, error) {
	factors := []storedAuthMFAFactor{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[type]], [[secret_cipher]], [[verified_at]],
			[[last_counter]], [[created_at]]
		FROM {{leafen_auth_mfa_factors}} WHERE [[owner_id]] = {:ownerId}
		ORDER BY [[created_at]], [[id]] LIMIT 9
	`).Bind(dbx.Params{"ownerId": ownerID}).All(&factors)
	if err == nil && len(factors) > authMFAFactorLimit {
		err = errMFAReadBound
	}
	return factors, err
}

func verifiedTOTPMFAFactor(app core.App, ownerID string) (storedAuthMFAFactor, bool, error) {
	factors, err := storedMFAFactors(app, ownerID)
	if err != nil {
		return storedAuthMFAFactor{}, false, err
	}
	for _, factor := range factors {
		if factor.Type == authMFAFactorTypeTOTP && factor.VerifiedAt > 0 {
			return factor, true, nil
		}
	}
	return storedAuthMFAFactor{}, false, nil
}

func readAuthMFAState(app core.App, ownerID string) (authMFAStateResponse, error) {
	factors, err := storedMFAFactors(app, ownerID)
	if err != nil {
		return authMFAStateResponse{}, err
	}
	response := authMFAStateResponse{Factors: make([]authMFAFactorResponse, 0, len(factors))}
	for _, factor := range factors {
		response.Factors = append(response.Factors, authMFAFactorResponse{
			ID: factor.ID, Type: factor.Type, Verified: factor.VerifiedAt > 0,
			CreatedAt: time.UnixMilli(factor.CreatedAt).UTC().Format(time.RFC3339Nano),
		})
	}
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_auth_mfa_recovery_codes}}
		WHERE [[owner_id]] = {:ownerId} AND [[used_at]] = 0
	`).Bind(dbx.Params{"ownerId": ownerID}).Row(&response.RecoveryCodesRemaining); err != nil {
		return authMFAStateResponse{}, err
	}
	if response.RecoveryCodesRemaining > authMFARecoveryCodeLimit {
		return authMFAStateResponse{}, errMFAReadBound
	}
	return response, nil
}

func startTOTPEnrollment(app core.App, user *core.Record) (authMFAEnrollmentResponse, error) {
	secret, err := generateTOTPSecret()
	if err != nil {
		return authMFAEnrollmentResponse{}, err
	}
	cipher, err := encryptAdministratorSetting(app, cipherMFAFactor, secret)
	if err != nil {
		return authMFAEnrollmentResponse{}, err
	}
	codes, hashes, err := generateRecoveryCodes(user.Id)
	if err != nil {
		return authMFAEnrollmentResponse{}, err
	}
	factorID := authMFAFactorIDPrefix + security.RandomString(24)
	now := authMFANow().UnixMilli()
	err = app.RunInTransaction(func(txApp core.App) error {
		factors, err := storedMFAFactors(txApp, user.Id)
		if err != nil {
			return err
		}
		for _, factor := range factors {
			if factor.Type == authMFAFactorTypeTOTP && factor.VerifiedAt > 0 {
				return errMFAAlreadyEnabled
			}
		}
		if _, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_auth_mfa_factors}}
			WHERE [[owner_id]] = {:ownerId} AND [[type]] = 'totp' AND [[verified_at]] = 0
		`).Bind(dbx.Params{"ownerId": user.Id}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().Insert(authMFAFactorsTable, dbx.Params{
			"id": factorID, "owner_id": user.Id, "type": authMFAFactorTypeTOTP,
			"secret_cipher": cipher, "verified_at": 0, "last_counter": -1,
			"created_at": now, "updated_at": now,
		}).Execute(); err != nil {
			return err
		}
		for index, hash := range hashes {
			if _, err := txApp.DB().Insert(authMFARecoveryCodesTable, dbx.Params{
				"id": "lrc_" + security.RandomString(24), "factor_id": factorID,
				"owner_id": user.Id, "code_hash": hash, "used_at": 0,
				"created_at": now + int64(index),
			}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return authMFAEnrollmentResponse{}, err
	}
	label := url.PathEscape(user.Email())
	if label == "" {
		label = url.PathEscape(accountLabel(user))
	}
	query := url.Values{"secret": {secret}, "issuer": {"Leafen"}, "digits": {"6"}, "period": {"30"}}
	return authMFAEnrollmentResponse{
		FactorID: factorID, Secret: secret,
		QRCodeURI:     "otpauth://totp/Leafen:" + label + "?" + query.Encode(),
		RecoveryCodes: codes,
	}, nil
}

func confirmTOTPEnrollment(app core.App, ownerID string, factorID string, code string) (authMFAStateResponse, error) {
	err := app.RunInTransaction(func(txApp core.App) error {
		var factor storedAuthMFAFactor
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[owner_id]], [[type]], [[secret_cipher]], [[verified_at]],
				[[last_counter]], [[created_at]] FROM {{leafen_auth_mfa_factors}}
			WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[verified_at]] = 0
		`).Bind(dbx.Params{"id": factorID, "ownerId": ownerID}).One(&factor)
		if err != nil {
			return refusedUnlessStorage(errMFAFactorNotFound, err)
		}
		secret, err := decryptAdministratorSetting(txApp, factor.SecretCipher)
		if err != nil {
			return err
		}
		if _, valid := matchingTOTPCounter(secret, code, authMFANow(), -1); !valid {
			return errMFACodeInvalid
		}
		now := authMFANow().UnixMilli()
		consumeResult, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_auth_mfa_factors}} SET [[verified_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[verified_at]] = 0
		`).Bind(dbx.Params{"now": now, "id": factorID, "ownerId": ownerID}).Execute()
		if err != nil {
			return err
		}
		changed, err := consumeResult.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errMFAFactorNotFound, err)
		}
		return nil
	})
	if err != nil {
		return authMFAStateResponse{}, err
	}
	return readAuthMFAState(app, ownerID)
}

func beginMFAChallengeInTransaction(
	app core.App,
	user *core.Record,
	metadata browserSessionMetadata,
	replaceOldest bool,
	replay authSessionReplayIdentity,
) (sessionResponse, authMFAChallengeResponse, bool, error) {
	return beginMFAChallengeForSignInInTransaction(app, user, metadata, replaceOldest, replay, samlAuthenticationMethodPassword, "")
}

// beginMFAChallengeForSignInInTransaction opens the session a one-factor
// sign-in earned — a password or a sign-in provider — or, for an account
// with 2-step verification, asks for its code first, as a password sign-in
// always has. A passkey is two factors and is never asked.
func beginMFAChallengeForSignInInTransaction(
	app core.App,
	user *core.Record,
	metadata browserSessionMetadata,
	replaceOldest bool,
	replay authSessionReplayIdentity,
	method string,
	providerSecondFactor string,
) (sessionResponse, authMFAChallengeResponse, bool, error) {
	if err := authorizeEnterpriseSSOSignIn(app, user.Id, method, ""); err != nil {
		return sessionResponse{}, authMFAChallengeResponse{}, false, err
	}
	if _, enabled, err := verifiedTOTPMFAFactor(app, user.Id); err != nil || !enabled {
		if err != nil {
			return sessionResponse{}, authMFAChallengeResponse{}, false, err
		}
		response, err := issueAuthenticatedBrowserSessionWithReplayInTransaction(
			app, user, metadata, replaceOldest, method, "", providerSecondFactor, replay,
		)
		return response, authMFAChallengeResponse{}, false, err
	}
	ticket, err := generateMFATicket()
	if err != nil {
		return sessionResponse{}, authMFAChallengeResponse{}, false, err
	}
	now := authMFANow()
	expiresAt := now.Add(authMFAChallengeTTLSeconds * time.Second)
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_auth_mfa_challenges}} WHERE [[owner_id]] = {:ownerId}`).
		Bind(dbx.Params{"ownerId": user.Id}).Execute(); err != nil {
		return sessionResponse{}, authMFAChallengeResponse{}, false, err
	}
	if _, err := app.DB().Insert(authMFAChallengesTable, dbx.Params{
		"ticket_hash": hashAuthMFAValue(ticket), "owner_id": user.Id,
		"replace_oldest": replaceOldest, "sign_in_method": method, "attempts_remaining": authMFAChallengeAttemptLimit,
		"expires_at": expiresAt.UnixMilli(), "consumed_at": 0, "created_at": now.UnixMilli(),
	}).Execute(); err != nil {
		return sessionResponse{}, authMFAChallengeResponse{}, false, err
	}
	return sessionResponse{}, authMFAChallengeResponse{
		MFARequired: true, Ticket: ticket, ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		AttemptsRemaining: authMFAChallengeAttemptLimit,
	}, true, nil
}

func loadMFAChallenge(app core.App, ticket string, now int64) (storedAuthMFAChallenge, error) {
	var challenge storedAuthMFAChallenge
	err := app.DB().NewQuery(`
		SELECT [[ticket_hash]], [[owner_id]], [[replace_oldest]], [[sign_in_method]], [[attempts_remaining]],
			[[expires_at]], [[consumed_at]] FROM {{leafen_auth_mfa_challenges}}
		WHERE [[ticket_hash]] = {:ticketHash}
	`).Bind(dbx.Params{"ticketHash": hashAuthMFAValue(strings.TrimSpace(ticket))}).One(&challenge)
	if err != nil || challenge.ConsumedAt != 0 || challenge.ExpiresAt <= now || challenge.AttemptsRemaining <= 0 {
		return storedAuthMFAChallenge{}, refusedUnlessStorage(errMFAChallengeInvalid, err)
	}
	return challenge, nil
}

func recordInvalidMFAAttempt(app core.App, challenge storedAuthMFAChallenge, now int64) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_auth_mfa_challenges}}
		SET [[attempts_remaining]] = [[attempts_remaining]] - 1
		WHERE [[ticket_hash]] = {:ticketHash} AND [[consumed_at]] = 0
			AND [[expires_at]] > {:now} AND [[attempts_remaining]] > 0
	`).Bind(dbx.Params{"ticketHash": challenge.TicketHash, "now": now}).Execute()
	return err
}

func completeMFAChallenge(app core.App, ticket string, proof string, recovery bool, metadata browserSessionMetadata) (authMFACompletionResult, error) {
	result := authMFACompletionResult{AuditReference: newDiagnosticReference()}
	var outcome error
	auditStaged := false
	err := app.RunInTransaction(func(txApp core.App) error {
		now := authMFANow().UnixMilli()
		challenge, err := loadMFAChallenge(txApp, ticket, now)
		if err != nil {
			return err
		}
		result.OwnerID = challenge.OwnerID
		factor, enabled, err := verifiedTOTPMFAFactor(txApp, challenge.OwnerID)
		if err != nil {
			return err
		}
		if !enabled {
			return errMFAChallengeInvalid
		}
		valid := false
		if recovery {
			codes := []storedRecoveryCode{}
			err = txApp.DB().NewQuery(`
				SELECT [[id]], [[code_hash]] FROM {{leafen_auth_mfa_recovery_codes}}
				WHERE [[owner_id]] = {:ownerId} AND [[factor_id]] = {:factorId} AND [[used_at]] = 0
				ORDER BY [[created_at]], [[id]] LIMIT 9
			`).Bind(dbx.Params{"ownerId": challenge.OwnerID, "factorId": factor.ID}).All(&codes)
			if err != nil || len(codes) > authMFARecoveryCodeLimit {
				if err == nil {
					err = errMFAReadBound
				}
				return err
			}
			target := recoveryCodeHash(challenge.OwnerID, proof)
			for _, code := range codes {
				if subtle.ConstantTimeCompare([]byte(code.CodeHash), []byte(target)) == 1 {
					result, err := txApp.DB().NewQuery(`
						UPDATE {{leafen_auth_mfa_recovery_codes}} SET [[used_at]] = {:now}
						WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[used_at]] = 0
					`).Bind(dbx.Params{"now": now, "id": code.ID, "ownerId": challenge.OwnerID}).Execute()
					if err != nil {
						return err
					}
					changed, _ := result.RowsAffected()
					valid = changed == 1
					break
				}
			}
			outcome = errMFARecoveryCodeInvalid
		} else {
			secret, err := decryptAdministratorSetting(txApp, factor.SecretCipher)
			if err != nil {
				return err
			}
			counter, matched := matchingTOTPCounter(secret, proof, authMFANow(), factor.LastCounter)
			if matched {
				result, err := txApp.DB().NewQuery(`
					UPDATE {{leafen_auth_mfa_factors}} SET [[last_counter]] = {:counter}, [[updated_at]] = {:now}
					WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[last_counter]] < {:counter}
				`).Bind(dbx.Params{"counter": counter, "now": now, "id": factor.ID, "ownerId": challenge.OwnerID}).Execute()
				if err != nil {
					return err
				}
				changed, _ := result.RowsAffected()
				valid = changed == 1
			}
			outcome = errMFACodeInvalid
		}
		if !valid {
			if err := recordInvalidMFAAttempt(txApp, challenge, now); err != nil {
				return err
			}
			method := authAuditMethodMFATOTP
			if recovery {
				method = authAuditMethodMFARecovery
			}
			if _, err := recordAuthAttemptInTransaction(txApp, authAuditAttempt{
				OwnerID: challenge.OwnerID, Method: method, Phase: authAuditPhaseVerify,
				Outcome: authAuditOutcomeFailure, ReasonCode: outcome.Error(), ReasonStatus: http.StatusUnauthorized,
				DiagnosticReference: result.AuditReference, Metadata: metadata,
			}); err != nil {
				return err
			}
			auditStaged = true
			return nil
		}
		challengeResult, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_auth_mfa_challenges}} SET [[consumed_at]] = {:now}
			WHERE [[ticket_hash]] = {:ticketHash} AND [[consumed_at]] = 0
		`).Bind(dbx.Params{"now": now, "ticketHash": challenge.TicketHash}).Execute()
		if err != nil {
			return err
		}
		changed, err := challengeResult.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errMFAChallengeInvalid, err)
		}
		user, err := txApp.FindRecordById("users", challenge.OwnerID)
		if err != nil {
			return err
		}
		// The session went through the 2-step code, or a recovery code.
		result.Session, err = issueAuthenticatedBrowserSessionInTransaction(
			txApp, user, metadata, challenge.ReplaceOldest,
			challenge.SignInMethod, "", "totp",
		)
		if err != nil {
			return err
		}
		method := authAuditMethodMFATOTP
		if recovery {
			method = authAuditMethodMFARecovery
		}
		if _, err := recordAuthAttemptInTransaction(txApp, authAuditAttempt{
			OwnerID: challenge.OwnerID, TargetEmail: user.Email(), Method: method,
			Phase: authAuditPhaseVerify, Outcome: authAuditOutcomeSuccess,
			DiagnosticReference: result.AuditReference, Metadata: metadata,
		}); err != nil {
			return err
		}
		auditStaged = true
		outcome = nil
		return nil
	})
	if err != nil {
		result.AuditRecorded = false
		return result, err
	}
	result.AuditRecorded = auditStaged
	return result, outcome
}

// mfaPasswordGiven checks a password given to confirm a 2-step change, before
// the transaction (password_outside_transaction.go).
func mfaPasswordGiven(user *core.Record, input authMFARevalidationInput) bool {
	return input.Password != "" && user.ValidatePassword(input.Password)
}

// revalidateMFAInTransaction accepts the password checked before, or else a
// current code; a wrong password sent alone is refused as the password, not
// as a code it didn't send.
func revalidateMFAInTransaction(app core.App, user *core.Record, input authMFARevalidationInput, passwordGiven bool) error {
	if passwordGiven {
		return nil
	}
	if input.Password != "" && strings.TrimSpace(input.Code) == "" {
		return errMFAPasswordInvalid
	}
	return consumeTOTPCode(app, user.Id, input.Code)
}

// consumeTOTPCode accepts a current 2-step code of the account once.
func consumeTOTPCode(app core.App, ownerID string, code string) error {
	factor, enabled, err := verifiedTOTPMFAFactor(app, ownerID)
	if err != nil {
		return err
	}
	if !enabled || strings.TrimSpace(code) == "" {
		return errMFACodeInvalid
	}
	secret, err := decryptAdministratorSetting(app, factor.SecretCipher)
	if err != nil {
		return err
	}
	counter, valid := matchingTOTPCounter(secret, code, authMFANow(), factor.LastCounter)
	if !valid {
		return errMFACodeInvalid
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_auth_mfa_factors}} SET [[last_counter]] = {:counter}, [[updated_at]] = {:now}
		WHERE [[id]] = {:id} AND [[owner_id]] = {:ownerId} AND [[last_counter]] < {:counter}
	`).Bind(dbx.Params{
		"counter": counter, "now": authMFANow().UnixMilli(), "id": factor.ID, "ownerId": ownerID,
	}).Execute()
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return errMFACodeInvalid
	}
	return nil
}

func disableTOTPFactor(app core.App, user *core.Record, input authMFARevalidationInput) error {
	passwordGiven := mfaPasswordGiven(user, input)
	return app.RunInTransaction(func(txApp core.App) error {
		if _, enabled, err := verifiedTOTPMFAFactor(txApp, user.Id); err != nil || !enabled {
			if err != nil {
				return err
			}
			return errMFAFactorNotFound
		}
		if err := revalidateMFAInTransaction(txApp, user, input, passwordGiven); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_auth_mfa_challenges}} WHERE [[owner_id]] = {:ownerId}`).
			Bind(dbx.Params{"ownerId": user.Id}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_auth_mfa_factors}} WHERE [[owner_id]] = {:ownerId} AND [[type]] = 'totp'`).
			Bind(dbx.Params{"ownerId": user.Id}).Execute()
		return err
	})
}

func regenerateRecoveryCodes(app core.App, user *core.Record, input authMFARevalidationInput) (authMFARecoveryCodesResponse, error) {
	codes, hashes, err := generateRecoveryCodes(user.Id)
	if err != nil {
		return authMFARecoveryCodesResponse{}, err
	}
	passwordGiven := mfaPasswordGiven(user, input)
	err = app.RunInTransaction(func(txApp core.App) error {
		factor, enabled, err := verifiedTOTPMFAFactor(txApp, user.Id)
		if err != nil {
			return err
		}
		if !enabled {
			return errMFAFactorNotFound
		}
		if err := revalidateMFAInTransaction(txApp, user, input, passwordGiven); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_auth_mfa_recovery_codes}} WHERE [[owner_id]] = {:ownerId}`).
			Bind(dbx.Params{"ownerId": user.Id}).Execute(); err != nil {
			return err
		}
		now := authMFANow().UnixMilli()
		for index, hash := range hashes {
			if _, err := txApp.DB().Insert(authMFARecoveryCodesTable, dbx.Params{
				"id": "lrc_" + security.RandomString(24), "factor_id": factor.ID,
				"owner_id": user.Id, "code_hash": hash, "used_at": 0,
				"created_at": now + int64(index),
			}).Execute(); err != nil {
				return err
			}
		}
		return nil
	})
	return authMFARecoveryCodesResponse{RecoveryCodes: codes}, err
}
