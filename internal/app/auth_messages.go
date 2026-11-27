package app

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errAuthMessageInvalid  = errors.New(errorCodeAuthTokenInvalid)
	errAuthMessageConsumed = errors.New(errorCodeAuthTokenConsumed)
)

const authMessageSendingUncertainAfter = 2*authSMTPTimeout + time.Second

type storedAuthMessage struct {
	ID                string `db:"id"`
	UserID            string `db:"user_id"`
	Kind              string `db:"kind"`
	Recipient         string `db:"recipient"`
	TokenHash         string `db:"token_hash"`
	TokenCipher       string `db:"token_cipher"`
	PreviousTokenHash string `db:"previous_token_hash"`
	Status            string `db:"status"`
	ExpiresAt         int64  `db:"expires_at"`
	PreviousExpiresAt int64  `db:"previous_expires_at"`
	MatchedExpiresAt  int64  `db:"matched_expires_at"`
	UpdatedAt         int64  `db:"updated_at"`
	FormatVersion     int    `db:"format_version"`
}

type authMessageDeliveryClaim struct {
	Token               string
	ID                  string
	Dispatch            bool
	Recovered           bool
	ReconciledUncertain bool
}

// claimAuthMessageDelivery is the sole same-user/kind dispatch owner. It
// commits sending before provider I/O; exact retries of a sending, delivered
// or uncertain effect therefore never re-enter non-idempotent SMTP.
func claimAuthMessageDelivery(app core.App, userID, kind string, ttl time.Duration) (authMessageDeliveryClaim, error) {
	user, err := app.FindRecordById("users", userID)
	if err != nil {
		return authMessageDeliveryClaim{}, err
	}
	return claimAuthMessageDeliveryForRecipient(app, userID, kind, ttl, normalizePageShareEmail(user.Email()))
}

func claimAuthMessageDeliveryForRecipient(app core.App, userID, kind string, ttl time.Duration, recipient string) (authMessageDeliveryClaim, error) {
	now := time.Now().UTC()
	claim := authMessageDeliveryClaim{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := findAccountByEmail(txApp, recipient)
		if err != nil || current.Id != userID || (kind == authMessageVerification && normalizePageShareEmail(current.Email()) != recipient) {
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			return refusedUnlessStorage(errAuthMessageInvalid, err)
		}
		stored := storedAuthMessage{}
		err = txApp.DB().NewQuery(`
			SELECT [[id]], [[user_id]], [[kind]], [[recipient]], [[token_hash]], [[token_cipher]],
				[[previous_token_hash]], [[status]], [[expires_at]],
				[[previous_expires_at]], [[updated_at]], [[format_version]]
			FROM {{leafen_auth_messages}}
			WHERE [[user_id]] = {:userId} AND [[kind]] = {:kind} AND [[format_version]] = 3
			LIMIT 1
		`).Bind(dbx.Params{"userId": userID, "kind": kind}).One(&stored)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && stored.Recipient == recipient && stored.Status != authMessageConsumed && stored.ExpiresAt > now.UnixMilli() {
			claim = authMessageDeliveryClaim{ID: stored.ID}
			switch stored.Status {
			case authMessageSending:
				claim.ReconciledUncertain, err = reconcileStaleAuthMessageSending(
					txApp, stored, now,
				)
				return err
			case authMessageUncertain:
				return nil
			}
			existing, err := decryptAdministratorSetting(txApp, stored.TokenCipher)
			if err != nil || existing == "" || hashAuthMessageToken(existing) != stored.TokenHash {
				newToken, newHash, newCipher, err := newAuthMessageToken(txApp)
				if err != nil {
					return err
				}
				claim, err = recoverCorruptAuthMessageClaim(
					txApp, stored, newToken, newHash, newCipher,
					now.Add(ttl).UnixMilli(), now.UnixMilli(),
				)
				return err
			}
			claim.Token = existing
			switch stored.Status {
			case authMessageDelivered:
				return nil
			case authMessagePending, authMessageFailed:
				result, err := txApp.DB().NewQuery(`
					UPDATE {{leafen_auth_messages}}
					SET [[status]] = {:sending}, [[updated_at]] = {:updatedAt}
					WHERE [[id]] = {:id} AND [[format_version]] = 3
						AND [[status]] = {:priorStatus}
				`).Bind(dbx.Params{
					"sending": authMessageSending, "updatedAt": now.UnixMilli(),
					"id": stored.ID, "priorStatus": stored.Status,
				}).Execute()
				if err != nil {
					return err
				}
				changed, err := result.RowsAffected()
				if err != nil || changed != 1 {
					return refusedUnlessStorage(errAuthMessageInvalid, err)
				}
				claim.Dispatch = true
				return nil
			default:
				return errAuthMessageInvalid
			}
		}

		newToken, newHash, newCipher, err := newAuthMessageToken(txApp)
		if err != nil {
			return err
		}
		claim = authMessageDeliveryClaim{Token: newToken, ID: stored.ID, Dispatch: true}
		if claim.ID == "" {
			claim.ID = "ham3_" + newHash[:20]
			_, err = txApp.DB().Insert(authMessagesTable, dbx.Params{
				"id": claim.ID, "user_id": userID, "kind": kind, "recipient": recipient, "token_hash": newHash,
				"token_cipher": newCipher, "format_version": 3,
				"status": authMessageSending, "expires_at": now.Add(ttl).UnixMilli(),
				"consumed_at": 0, "created_at": now.UnixMilli(), "updated_at": now.UnixMilli(),
			}).Execute()
		} else {
			_, err = txApp.DB().NewQuery(`
				UPDATE {{leafen_auth_messages}}
				SET [[token_hash]] = {:tokenHash}, [[token_cipher]] = {:tokenCipher}, [[recipient]] = {:recipient},
					[[status]] = {:sending}, [[expires_at]] = {:expiresAt},
					[[previous_token_hash]] = '', [[previous_expires_at]] = 0,
					[[consumed_at]] = 0, [[updated_at]] = {:updatedAt}
				WHERE [[id]] = {:id} AND [[format_version]] = 3
			`).Bind(dbx.Params{
				"tokenHash": newHash, "tokenCipher": newCipher, "recipient": recipient,
				"sending": authMessageSending, "expiresAt": now.Add(ttl).UnixMilli(),
				"updatedAt": now.UnixMilli(), "id": claim.ID,
			}).Execute()
		}
		if err != nil {
			return err
		}
		return nil
	})
	return claim, err
}

func reconcileStaleAuthMessageSending(
	app core.App,
	stored storedAuthMessage,
	now time.Time,
) (bool, error) {
	if stored.UpdatedAt > now.Add(-authMessageSendingUncertainAfter).UnixMilli() {
		return false, nil
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_auth_messages}}
		SET [[status]] = {:uncertain}, [[updated_at]] = {:updatedAt}
		WHERE [[id]] = {:id} AND [[user_id]] = {:userId} AND [[kind]] = {:kind}
			AND [[format_version]] = 3 AND [[status]] = {:sending}
			AND [[updated_at]] = {:priorUpdatedAt}
	`).Bind(dbx.Params{
		"uncertain": authMessageUncertain, "updatedAt": now.UnixMilli(),
		"id": stored.ID, "userId": stored.UserID, "kind": stored.Kind,
		"sending": authMessageSending, "priorUpdatedAt": stored.UpdatedAt,
	}).Execute()
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

func newAuthMessageToken(app core.App) (string, string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", "", err
	}
	token := "lat_" + base64.RawURLEncoding.EncodeToString(random)
	hash := hashAuthMessageToken(token)
	cipher, err := encryptAdministratorSetting(app, cipherAuthMessage, token)
	return token, hash, cipher, err
}

func recoverCorruptAuthMessageClaim(
	app core.App,
	stored storedAuthMessage,
	newToken string,
	newHash string,
	newCipher string,
	newExpiresAt int64,
	now int64,
) (authMessageDeliveryClaim, error) {
	previousHash := stored.PreviousTokenHash
	previousExpiresAt := stored.PreviousExpiresAt
	if previousExpiresAt <= now || !validAuthMessageHash(previousHash) {
		previousHash = ""
		previousExpiresAt = 0
	}
	switch stored.Status {
	case authMessageSending, authMessageDelivered, authMessageUncertain:
		if stored.ExpiresAt > now && validAuthMessageHash(stored.TokenHash) {
			previousHash = stored.TokenHash
			previousExpiresAt = stored.ExpiresAt
		}
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_auth_messages}}
		SET [[token_hash]] = {:tokenHash}, [[token_cipher]] = {:tokenCipher},
			[[previous_token_hash]] = {:previousTokenHash},
			[[previous_expires_at]] = {:previousExpiresAt},
			[[status]] = {:sending}, [[expires_at]] = {:expiresAt},
			[[consumed_at]] = 0, [[updated_at]] = {:updatedAt}
		WHERE [[id]] = {:id} AND [[format_version]] = 3
			AND [[token_hash]] = {:storedTokenHash}
			AND [[token_cipher]] = {:storedTokenCipher}
	`).Bind(dbx.Params{
		"tokenHash": newHash, "tokenCipher": newCipher,
		"previousTokenHash": previousHash, "previousExpiresAt": previousExpiresAt,
		"sending": authMessageSending, "expiresAt": newExpiresAt,
		"updatedAt": now, "id": stored.ID,
		"storedTokenHash": stored.TokenHash, "storedTokenCipher": stored.TokenCipher,
	}).Execute()
	if err != nil {
		return authMessageDeliveryClaim{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return authMessageDeliveryClaim{}, refusedUnlessStorage(errAuthMessageInvalid, err)
	}
	return authMessageDeliveryClaim{
		Token: newToken, ID: stored.ID, Dispatch: true, Recovered: true,
	}, nil
}

func recordAuthMessageDelivery(app core.App, id string, status string, token string) error {
	switch status {
	case authMessageDelivered, authMessageFailed, authMessageUncertain:
	default:
		return errAuthMessageInvalid
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_auth_messages}}
		SET [[status]] = CASE
			WHEN [[token_hash]] != {:tokenHash} THEN [[status]]
			WHEN [[status]] = {:consumed} THEN [[status]]
			WHEN [[status]] != {:sending} THEN [[status]]
			ELSE {:status}
		END, [[updated_at]] = CASE WHEN [[token_hash]] = {:tokenHash} THEN {:updated} ELSE [[updated_at]] END
		WHERE [[id]] = {:id} AND [[format_version]] = 3
	`).Bind(dbx.Params{
		"consumed": authMessageConsumed, "sending": authMessageSending, "status": status,
		"updated": time.Now().UTC().UnixMilli(), "id": id, "tokenHash": hashAuthMessageToken(token),
	}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return refusedUnlessStorage(errAuthMessageInvalid, err)
	}
	return nil
}

// checkAuthMessage finds the unused, unexpired message a token names, as
// consumeAuthMessage does, without using it.
func checkAuthMessage(app core.App, token string, kind string) error {
	if len(token) < 40 || len(token) > 128 {
		return errAuthMessageInvalid
	}
	message, err := findAuthMessageForConsume(app, hashAuthMessageToken(token), kind)
	if errors.Is(err, sql.ErrNoRows) {
		return errAuthMessageInvalid
	}
	if err != nil {
		return err
	}
	if message.Status == authMessageConsumed {
		return errAuthMessageConsumed
	}
	if message.MatchedExpiresAt <= time.Now().UTC().UnixMilli() {
		return errAuthMessageInvalid
	}
	user, err := app.FindRecordById("users", message.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return errAuthMessageInvalid
	}
	if err != nil {
		return err
	}
	identity, err := findAccountByEmail(app, message.Recipient)
	if err != nil || identity.Id != user.Id {
		return refusedUnlessStorage(errAuthMessageInvalid, err)
	}
	return nil
}

func consumeAuthMessage(
	app core.App,
	token string,
	kind string,
	apply func(core.App, *core.Record) error,
) (*core.Record, error) {
	if len(token) < 40 || len(token) > 128 {
		return nil, errAuthMessageInvalid
	}
	var consumed *core.Record
	tokenHash := hashAuthMessageToken(token)
	err := app.RunInTransaction(func(txApp core.App) error {
		message, err := findAuthMessageForConsume(txApp, tokenHash, kind)
		if errors.Is(err, sql.ErrNoRows) {
			return errAuthMessageInvalid
		}
		if err != nil {
			return err
		}
		if message.Status == authMessageConsumed {
			return errAuthMessageConsumed
		}
		now := time.Now().UTC().UnixMilli()
		if message.MatchedExpiresAt <= now {
			return errAuthMessageInvalid
		}
		user, err := txApp.FindRecordById("users", message.UserID)
		if err != nil {
			return err
		}
		identity, err := findAccountByEmail(txApp, message.Recipient)
		if err != nil || identity.Id != user.Id || (kind == authMessageVerification && normalizePageShareEmail(user.Email()) != message.Recipient) {
			return refusedUnlessStorage(errAuthMessageInvalid, err)
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_auth_messages}}
			SET [[status]] = {:consumed}, [[consumed_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[status]] != {:consumed}
				AND (([[token_hash]] = {:hash} AND [[expires_at]] > {:now})
					OR ([[previous_token_hash]] = {:hash} AND [[previous_expires_at]] > {:now}))
		`).Bind(dbx.Params{
			"consumed": authMessageConsumed, "now": now, "id": message.ID,
			"hash": tokenHash,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAuthMessageConsumed, err)
		}
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_auth_messages}}
			SET [[status]] = {:consumed}, [[consumed_at]] = {:now}, [[updated_at]] = {:now}
			WHERE [[user_id]] = {:userId} AND [[kind]] = {:kind}
				AND [[id]] != {:id} AND [[status]] != {:consumed}
		`).Bind(dbx.Params{
			"consumed": authMessageConsumed, "now": now,
			"userId": message.UserID, "kind": kind, "id": message.ID,
		}).Execute(); err != nil {
			return err
		}
		if err := apply(txApp, user); err != nil {
			return err
		}
		consumed = user
		return nil
	})
	return consumed, err
}

func findAuthMessageForConsume(app core.App, tokenHash string, kind string) (storedAuthMessage, error) {
	message := storedAuthMessage{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[user_id]], [[kind]], [[recipient]], [[token_hash]], [[token_cipher]],
			[[previous_token_hash]], [[status]], [[expires_at]],
			[[previous_expires_at]], [[format_version]],
			[[expires_at]] AS [[matched_expires_at]]
		FROM {{leafen_auth_messages}}
		WHERE [[token_hash]] = {:hash} AND [[kind]] = {:kind}
		LIMIT 1
	`).Bind(dbx.Params{"hash": tokenHash, "kind": kind}).One(&message)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return message, err
	}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[user_id]], [[kind]], [[recipient]], [[token_hash]], [[token_cipher]],
			[[previous_token_hash]], [[status]], [[expires_at]],
			[[previous_expires_at]], [[format_version]],
			[[previous_expires_at]] AS [[matched_expires_at]]
		FROM {{leafen_auth_messages}}
		WHERE [[previous_token_hash]] = {:hash} AND [[previous_token_hash]] <> ''
			AND [[kind]] = {:kind}
		LIMIT 1
	`).Bind(dbx.Params{"hash": tokenHash, "kind": kind}).One(&message)
	return message, err
}

func hashAuthMessageToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func validAuthMessageHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
