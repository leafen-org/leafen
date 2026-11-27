package app

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// One bounded pending address per authenticated user; pending requests do not
// reserve somebody else's email. Mail delivery is claimed before SMTP and an
// uncertain effect is never blindly dispatched again.
type accountEmailChallenge struct {
	UserID       string `db:"user_id"`
	Email        string `db:"email"`
	Nonce        string `db:"nonce"`
	BaseRevision string `db:"base_revision"`
	CodeHash     string `db:"code_hash"`
	CodeCipher   string `db:"code_cipher"`
	Status       string `db:"status"`
	Attempts     int    `db:"attempts_remaining"`
	ExpiresAt    int64  `db:"expires_at"`
	UpdatedAt    int64  `db:"updated_at"`
	Code         string
	Dispatch     bool
	// The account's own address when the address was asked for, which gets a
	// code of its own: both codes are needed, as Notion's (seen 2026-11-09).
	CurrentEmail string
	CurrentCode  string
}

// The codes a challenge sent, kept encrypted in its code column; a challenge
// made before the current address took a code holds the one code as it is.
type accountEmailCodes struct {
	Code         string `json:"code"`
	Current      string `json:"current,omitempty"`
	CurrentEmail string `json:"currentEmail,omitempty"`
}

func (claim *accountEmailChallenge) openCodes(app core.App) error {
	plain, err := decryptAdministratorSetting(app, claim.CodeCipher)
	if err != nil {
		return err
	}
	codes := accountEmailCodes{Code: plain}
	if strings.HasPrefix(plain, "{") {
		if err := json.Unmarshal([]byte(plain), &codes); err != nil {
			return err
		}
	}
	claim.Code, claim.CurrentCode, claim.CurrentEmail = codes.Code, codes.Current, codes.CurrentEmail
	return nil
}

// A code as Notion's address codes are ("dqYVcm"): six letters, read
// without regard to case.
const accountEmailCodeLetters = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"

func newAccountEmailCode() (string, error) {
	code := make([]byte, 6)
	for index := range code {
		pick, err := rand.Int(rand.Reader, big.NewInt(int64(len(accountEmailCodeLetters))))
		if err != nil {
			return "", err
		}
		code[index] = accountEmailCodeLetters[pick.Int64()]
	}
	return string(code), nil
}

// The codes one challenge checks: the new address's, and the current one's when it took one.
func accountEmailCodePair(code, current string) string {
	if strings.TrimSpace(current) == "" {
		return code
	}
	return strings.TrimSpace(code) + "\x00" + strings.TrimSpace(current)
}

func loadAccountEmailChallenge(app core.App, userID string) (accountEmailChallenge, error) {
	stored := accountEmailChallenge{}
	err := app.DB().NewQuery(`SELECT [[user_id]], [[email]], [[nonce]], [[base_revision]], [[code_hash]], [[code_cipher]],
		[[status]], [[attempts_remaining]], [[expires_at]], [[updated_at]] FROM {{leafen_account_email_challenges}}
		WHERE [[user_id]] = {:userId}`).Bind(dbx.Params{"userId": userID}).One(&stored)
	return stored, err
}

func accountEmailCodeHash(userID, email, nonce, code string) string {
	return hashAuthMFAValue(userID + "\x00" + email + "\x00" + nonce + "\x00" + strings.ToLower(strings.TrimSpace(code)))
}

func claimAccountEmailChallenge(app core.App, userID, email, revision string) (accountEmailChallenge, error) {
	claim := accountEmailChallenge{}
	err := app.RunInTransaction(func(txApp core.App) error {
		user, err := currentAccountEmailMutation(txApp, userID, revision)
		if err != nil {
			return err
		}
		if err := requireAccountEmailAddition(txApp, user, email); err != nil {
			return err
		}
		state, err := accountEmailSnapshot(txApp, user)
		if err != nil {
			return err
		}
		if !state.CanAdd {
			return errAccountEmailLimit
		}
		if _, err := findAccountByEmail(txApp, email); err == nil {
			return errRegistrationAccountExists
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now().UTC()
		stored, err := loadAccountEmailChallenge(txApp, userID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && stored.Status != authMessageConsumed && stored.ExpiresAt > now.UnixMilli() {
			if stored.Email != email || stored.BaseRevision != revision || stored.Attempts == 0 {
				return errAccountEmailChallenge
			}
			claim = stored
			// Which address took the second code, for the field that asks for it.
			if err := claim.openCodes(txApp); err != nil {
				return refusedUnlessStorage(errAccountEmailChallenge, err)
			}
			if stored.Status == authMessageSending && stored.UpdatedAt <= now.Add(-authMessageSendingUncertainAfter).UnixMilli() {
				_, err := txApp.DB().NewQuery(`UPDATE {{leafen_account_email_challenges}} SET [[status]] = 'uncertain', [[updated_at]] = {:now}
					WHERE [[user_id]] = {:userId} AND [[nonce]] = {:nonce} AND [[status]] = 'sending'`).
					Bind(dbx.Params{"now": now.UnixMilli(), "userId": userID, "nonce": stored.Nonce}).Execute()
				return err
			}
			if stored.Status != authMessageFailed {
				return nil
			}
			if accountEmailCodeHash(userID, email, stored.Nonce, accountEmailCodePair(claim.Code, claim.CurrentCode)) != stored.CodeHash {
				return errAccountEmailChallenge
			}
			_, err = txApp.DB().NewQuery(`UPDATE {{leafen_account_email_challenges}} SET [[status]] = 'sending', [[updated_at]] = {:now}
				WHERE [[user_id]] = {:userId} AND [[nonce]] = {:nonce} AND [[status]] = 'failed'`).
				Bind(dbx.Params{"now": now.UnixMilli(), "userId": userID, "nonce": stored.Nonce}).Execute()
			claim.Dispatch = err == nil
			return err
		}
		nonce, err := generateMFATicket()
		if err != nil {
			return err
		}
		code, err := newAccountEmailCode()
		if err != nil {
			return err
		}
		codes := accountEmailCodes{Code: code}
		// An account that has an address proves it there too; its first address has only the new one's.
		if current := normalizePageShareEmail(user.Email()); current != "" {
			if codes.Current, err = newAccountEmailCode(); err != nil {
				return err
			}
			codes.CurrentEmail = current
		}
		kept, err := json.Marshal(codes)
		if err != nil {
			return err
		}
		cipher, err := encryptAdministratorSetting(txApp, cipherEmailChallenge, string(kept))
		if err != nil {
			return err
		}
		claim = accountEmailChallenge{UserID: userID, Email: email, Nonce: nonce, BaseRevision: revision,
			Code: code, CurrentCode: codes.Current, CurrentEmail: codes.CurrentEmail, CodeCipher: cipher,
			CodeHash: accountEmailCodeHash(userID, email, nonce, accountEmailCodePair(code, codes.Current)),
			Status:   authMessageSending, Attempts: 5, ExpiresAt: now.Add(10 * time.Minute).UnixMilli(), UpdatedAt: now.UnixMilli(), Dispatch: true}
		_, err = txApp.DB().NewQuery(`INSERT INTO {{leafen_account_email_challenges}} VALUES
			({:userId}, {:email}, {:nonce}, {:revision}, {:hash}, {:cipher}, 'sending', 5, {:expires}, {:now})
			ON CONFLICT ([[user_id]]) DO UPDATE SET [[email]] = excluded.[[email]], [[nonce]] = excluded.[[nonce]],
			[[base_revision]] = excluded.[[base_revision]], [[code_hash]] = excluded.[[code_hash]], [[code_cipher]] = excluded.[[code_cipher]],
			[[status]] = 'sending', [[attempts_remaining]] = 5, [[expires_at]] = excluded.[[expires_at]], [[updated_at]] = excluded.[[updated_at]]`).
			Bind(dbx.Params{"userId": userID, "email": email, "nonce": nonce, "revision": revision, "hash": claim.CodeHash,
				"cipher": cipher, "expires": claim.ExpiresAt, "now": claim.UpdatedAt}).Execute()
		return err
	})
	return claim, err
}

func deliverAccountEmailChallenge(event *core.RequestEvent, settings storedAdministratorSettings, claim accountEmailChallenge, language string) error {
	password, err := decryptAdministratorSetting(event.App, settings.SMTPPasswordCipher)
	outcome := smtpDeliverySettledFailure
	if err == nil {
		subject, body := "Verify your Leafen email", "Enter this code in your account’s email settings within 10 minutes:\r\n\r\n"+claim.Code
		if language == accountLanguageKo {
			subject, body = "리펜 이메일 확인", "10분 안에 계정의 이메일 관리에서 다음 코드를 입력해 주세요:\r\n\r\n"+claim.Code
		}
		outcome, err = sendConfiguredSMTP(settings, password, claim.Email, buildAuthEmail(settings, claim.Email, subject, body))
		// The account's own address gets its code too; the worse of the two deliveries stands.
		if outcome != smtpDeliverySettledFailure && claim.CurrentEmail != "" {
			subject, body = "Confirm adding an email to Leafen", claim.Email+" is being added to your Leafen account. Enter this code where it is being added, within 10 minutes:\r\n\r\n"+claim.CurrentCode
			if language == accountLanguageKo {
				subject, body = "리펜 이메일 추가 확인", "리펜 계정에 "+claim.Email+" 주소를 더하고 있습니다. 10분 안에 주소를 더하는 창에 다음 코드를 입력해 주세요:\r\n\r\n"+claim.CurrentCode
			}
			second, secondErr := sendConfiguredSMTP(settings, password, claim.CurrentEmail, buildAuthEmail(settings, claim.CurrentEmail, subject, body))
			if second == smtpDeliverySettledFailure || (second == smtpDeliveryUncertain && outcome == smtpDeliveryAccepted) {
				outcome = second
			}
			err = errors.Join(err, secondErr)
		}
	}
	status := authMessageFailed
	if outcome == smtpDeliveryAccepted {
		status = authMessageDelivered
	}
	if outcome == smtpDeliveryUncertain {
		status = authMessageUncertain
	}
	_, receiptErr := event.App.DB().NewQuery(`UPDATE {{leafen_account_email_challenges}} SET [[status]] = {:status}, [[updated_at]] = {:now}
		WHERE [[user_id]] = {:userId} AND [[nonce]] = {:nonce} AND [[status]] = 'sending'`).
		Bind(dbx.Params{"status": status, "now": time.Now().UTC().UnixMilli(), "userId": claim.UserID, "nonce": claim.Nonce}).Execute()
	if receiptErr != nil || outcome == smtpDeliveryUncertain {
		logAuthMessageDeliveryUncertain(event, errors.Join(err, receiptErr))
	}
	if outcome == smtpDeliverySettledFailure {
		return errors.Join(err, receiptErr)
	}
	return nil
}

func consumeAccountEmailChallenge(app core.App, userID string, input accountEmailConfirmInput) (accountEmailState, error) {
	state := accountEmailState{}
	var outcome error
	err := app.RunInTransaction(func(txApp core.App) error {
		stored, err := loadAccountEmailChallenge(txApp, userID)
		if err != nil {
			return err
		}
		email, valid := normalizeEmailIdentity(input.Email)
		if !valid || stored.Nonce != input.Nonce || stored.Email != email {
			return errAccountEmailChallenge
		}
		if stored.Status == authMessageConsumed {
			user, err := findAccountByEmail(txApp, email)
			if err != nil || user.Id != userID {
				return refusedUnlessStorage(errAccountEmailChallenge, err)
			}
			state, err = accountEmailSnapshot(txApp, user)
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if stored.ExpiresAt <= now || stored.Attempts <= 0 || stored.Status == authMessageFailed {
			return errAccountEmailChallenge
		}
		user, err := currentAccountEmailMutation(txApp, userID, stored.BaseRevision)
		if err != nil {
			return err
		}
		if err := requireAccountEmailAddition(txApp, user, email); err != nil {
			return err
		}
		entered := accountEmailCodePair(input.VerificationCode, input.CurrentVerificationCode)
		if subtle.ConstantTimeCompare([]byte(stored.CodeHash), []byte(accountEmailCodeHash(userID, email, input.Nonce, entered))) != 1 {
			_, err := txApp.DB().NewQuery(`UPDATE {{leafen_account_email_challenges}} SET [[attempts_remaining]] = [[attempts_remaining]] - 1
				WHERE [[user_id]] = {:userId} AND [[nonce]] = {:nonce} AND [[attempts_remaining]] > 0`).
				Bind(dbx.Params{"userId": userID, "nonce": input.Nonce}).Execute()
			outcome = errAccountEmailChallenge
			return err // commit the failed attempt; rolling back would permit unlimited guesses
		}
		if err := addVerifiedAccountEmail(txApp, user, email); err != nil {
			return err
		}
		if err := auditAccountEmailChange(txApp, user, email, "add"); err != nil {
			return err
		}
		// A first address became the account's own.
		if user, err = txApp.FindRecordById("users", userID); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_account_email_challenges}} SET [[status]] = 'consumed', [[code_cipher]] = '', [[updated_at]] = {:now}
			WHERE [[user_id]] = {:userId} AND [[nonce]] = {:nonce}`).Bind(dbx.Params{"now": now, "userId": userID, "nonce": input.Nonce}).Execute(); err != nil {
			return err
		}
		if err := accountEmailsChanged(txApp, user); err != nil {
			return err
		}
		state, err = accountEmailSnapshot(txApp, user)
		return err
	})
	if err != nil {
		return state, err
	}
	return state, outcome
}
