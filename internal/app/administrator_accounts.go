package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errAdministratorAccountExists   = errors.New(errorCodeAdministratorAccountExists)
	errAdministratorAccountNotFound = errors.New(errorCodeAdministratorAccountNotFound)
)

func registerAdministratorAccountRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/admin/accounts", createAdministratorManagedAccount)
	event.Router.PUT(apiRoot+"/admin/accounts/{accountId}/password", resetAdministratorManagedAccountPassword)
	registerAdministratorAccountDirectoryRoutes(event)
	registerServerAuditRoutes(event)
	registerAdministratorWorkspaceRoutes(event)
	registerAdministratorAccountStateRoutes(event)
	registerAdministratorAccountDeviceRoutes(event)
	registerAdministratorAccountCleanupRoutes(event)
	registerAccountPasswordStateRoutes(event)
	registerAdministratorDeviceCodeRoutes(event)
}

func createAdministratorManagedAccount(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	administrator, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := administratorAccountInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the account details.")
	}
	email, emailOK := normalizeEmailIdentity(input.Email)
	input.Email = email
	input.Name = strings.TrimSpace(input.Name)
	password, disclosed, err := administratorTemporaryPassword(input.Password)
	if err != nil || !emailOK || input.Name == "" || len(input.Name) > maxAuthNameLength {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the account details.")
	}

	var account *core.Record
	// Hashed before the transaction (password_outside_transaction.go).
	hashed := preparePassword(event.App, "users", password)
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, findErr := findAccountByEmail(txApp, input.Email); findErr == nil {
			return errAdministratorAccountExists
		} else if !errors.Is(findErr, sql.ErrNoRows) {
			return findErr
		}
		users, findErr := txApp.FindCollectionByNameOrId("users")
		if findErr != nil {
			return findErr
		}
		account = core.NewRecord(users)
		account.SetEmail(input.Email)
		hashed.set(account)
		account.Set("name", input.Name)
		account.SetVerified(true)
		if saveErr := txApp.Save(account); saveErr != nil {
			return saveErr
		}
		// Where people can't sign up, an invited address joins once an
		// administrator makes its account.
		if err := admitVerifiedEmailInvitations(txApp, account); err != nil {
			return err
		}
		if err := setMustChangePasswordInTransaction(
			txApp, account.Id, administrator.Id, accountPasswordReasonCreated,
		); err != nil {
			return err
		}
		return recordAdministratorAccountAudit(txApp, administrator.Id, account.Id, "create", "",
			administratorAccountStatusActive, 1, "create", time.Now().UTC().UnixMilli())
	})
	if errors.Is(err, errAdministratorAccountExists) {
		return apiError(event, http.StatusConflict, errorCodeAdministratorAccountExists, "This email is already in use.")
	}
	if err != nil || account == nil {
		if storageError, ok := authStorageAPIError(
			event, err, "administrator_account.create", "storage-write",
		); ok {
			return storageError
		}
		return apiErrorWithCause(
			event, http.StatusConflict, errorCodeAdministratorAccountCreateFailed,
			"Couldn't create the account.", "administrator_account.create", "storage-write",
			"ADMINISTRATOR_ACCOUNT_CREATE_FAILED", err,
		)
	}
	managed, err := loadManagedAdministratorAccount(event.App, account.Id)
	if err != nil {
		return administratorAccountMutationError(event, err, "create-read")
	}
	return event.JSON(http.StatusCreated, administratorAccountMutation{
		Account:           managed,
		TemporaryPassword: disclosed,
	})
}

func resetAdministratorManagedAccountPassword(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	administrator, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	targetID := strings.TrimSpace(event.Request.PathValue("accountId"))
	if targetID == administrator.Id {
		return apiError(event, http.StatusConflict, errorCodeAdministratorAccountSelfReset, "Your own password can't be reset through this route.")
	}
	input := administratorAccountPasswordInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the password.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	// Left out, Leafen makes the temporary password and answers it this once,
	// as a new account's (user 2026-11-23: an agent resets one and hands it
	// over without seeing it, agent_secret_notes.go).
	password, disclosed, err := administratorTemporaryPassword(input.Password)
	if err != nil || targetID == "" || input.RequestID == "" || input.ExpectedRevision < 1 ||
		len([]rune(input.RequestID)) > administratorAccountRequestIDRunes {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the password.")
	}
	// The request is the same again with the same password given, or with
	// none given: a made one is never kept, so a replay answers none.
	hash, err := administratorAccountPasswordMutationHash(event.App, administratorResetPasswordAsked(disclosed, password))
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAdministratorAccountPasswordResetFailed,
			"Couldn't reset the password.", "administrator_account.password_reset",
			"secret-read", "ADMINISTRATOR_ACCOUNT_PASSWORD_RESET_FAILED", err,
		)
	}

	release := acquireProductOperationLock(administratorAccountLockNamespace, targetID)
	defer release()
	replayed := false
	// Hashed before the transaction (password_outside_transaction.go).
	hashed := preparePassword(event.App, "users", password)
	err = event.App.RunInTransaction(func(txApp core.App) error {
		current, findErr := txApp.FindRecordById("users", targetID)
		if errors.Is(findErr, sql.ErrNoRows) || current == nil {
			return errAdministratorAccountNotFound
		}
		if findErr != nil {
			return findErr
		}
		state, stateErr := loadAdministratorAccountState(txApp, targetID)
		if stateErr != nil {
			return stateErr
		}
		if state.RequestID == input.RequestID {
			if state.RequestHash != hash {
				return errAdministratorAccountIdentityReused
			}
			replayed = true
			return nil
		}
		if state.Revision != input.ExpectedRevision || state.Status == administratorAccountStatusDeleting || state.Status == administratorAccountStatusDeleted {
			return errAdministratorAccountRevision
		}
		hashed.set(current)
		if saveErr := txApp.Save(current); saveErr != nil {
			return saveErr
		}
		if knownErr := markAccountPasswordKnown(txApp, current.Id); knownErr != nil {
			return knownErr
		}
		if revokeErr := revokeAllBrowserSessions(txApp, current.Id); revokeErr != nil {
			return revokeErr
		}
		if stateErr := setMustChangePasswordInTransaction(
			txApp, current.Id, administrator.Id, accountPasswordReasonReset,
		); stateErr != nil {
			return stateErr
		}
		now := time.Now().UTC().UnixMilli()
		result, stateErr := txApp.DB().NewQuery(`UPDATE {{leafen_administrator_account_states}}
			SET [[revision]]=[[revision]]+1,[[request_id]]={:requestId},[[request_hash]]={:requestHash},
				[[updated_by]]={:actorId},[[updated_at]]={:updated}
			WHERE [[user_id]]={:targetId} AND [[revision]]={:revision}`).Bind(dbx.Params{
			"requestId": input.RequestID, "requestHash": hash, "actorId": administrator.Id,
			"updated": now, "targetId": targetID, "revision": input.ExpectedRevision,
		}).Execute()
		if stateErr != nil {
			return stateErr
		}
		changed, stateErr := result.RowsAffected()
		if stateErr != nil || changed != 1 {
			return refusedUnlessStorage(errAdministratorAccountRevision, stateErr)
		}
		_, stateErr = txApp.DB().Insert(administratorAccountAuditTable, dbx.Params{
			"id": core.GenerateDefaultRandomId(), "actor_id": administrator.Id, "target_user_id": targetID,
			"action": "password-reset", "from_status": state.Status, "to_status": state.Status,
			"revision": input.ExpectedRevision + 1, "request_id": input.RequestID, "created_at": now,
		}).Execute()
		return stateErr
	})
	if errors.Is(err, errAdministratorAccountNotFound) {
		return apiError(event, http.StatusNotFound, errorCodeAdministratorAccountNotFound, "Account not found.")
	}
	if errors.Is(err, errAdministratorAccountIdentityReused) || errors.Is(err, errAdministratorAccountRevision) {
		return administratorAccountMutationError(event, err, "password-reset")
	}
	if err != nil {
		if storageError, ok := authStorageAPIError(
			event, err, "administrator_account.password_reset", "storage-write",
		); ok {
			return storageError
		}
		return apiErrorWithCause(
			event, http.StatusConflict, errorCodeAdministratorAccountPasswordResetFailed,
			"Couldn't reset the password.", "administrator_account.password_reset",
			"storage-write", "ADMINISTRATOR_ACCOUNT_PASSWORD_RESET_FAILED", err,
		)
	}
	// Its person is told by mail, and by whom (2026-10-26: nothing went).
	if !replayed {
		facts := requestNoticeFacts(event)
		facts.ByAdministrator = accountLabel(administrator)
		if notice, ok := accountSecurityNoticeOf(event.App, targetID, securityChangePasswordResetBy, facts, event.Request); ok {
			sendAccountSecurityNoticeSoon(event.App, notice)
		}
	}
	managed, err := loadManagedAdministratorAccount(event.App, targetID)
	if err != nil {
		return administratorAccountMutationError(event, err, "password-reset-read")
	}
	if replayed {
		disclosed = nil
	}
	return event.JSON(http.StatusOK, administratorAccountMutation{
		Account:           managed,
		TemporaryPassword: disclosed,
		Replayed:          replayed,
	})
}

// administratorResetPasswordAsked is what a reset asked for, as its request
// is told apart from another: the password given, or Leafen's making one
// (a mark shorter than any password taken).
func administratorResetPasswordAsked(made *string, password string) string {
	if made != nil {
		return "\x00made"
	}
	return password
}

// administratorTemporaryPassword is the password given, or — none given,
// or an empty one — a temporary one Leafen makes for a person who holds
// none yet, answered once (a new account, a reset, the reset command; user
// 2026-11-23, agent_secret_notes.go).
func administratorTemporaryPassword(input *string) (string, *string, error) {
	if input != nil && *input != "" {
		password := *input
		if !authPasswordAcceptable(password) {
			return "", nil, errors.New(errorCodeInvalidInput)
		}
		return password, nil, nil
	}
	password, err := newMadePassword("Temp-")
	if err != nil {
		return "", nil, err
	}
	return password, &password, nil
}

// newMadePassword is the one place a password nobody holds yet is made: a
// temporary one (its prefix says so) or a person's own new one
// (accountNewPassword). It passes every password rule.
func newMadePassword(prefix string) (string, error) {
	random := make([]byte, 18)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(random) + "!9", nil
}
