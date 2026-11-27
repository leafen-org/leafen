package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	administratorAccountAuditTable              = "leafen_administrator_account_audit_events"
	administratorAccountCleanupAssignmentsTable = "leafen_administrator_account_cleanup_assignments"
	administratorAccountDirectoryAuthorityTable = "leafen_administrator_account_directory_authority"
	administratorAccountLockNamespace           = "administrator-account-state"
)

var (
	errAdministratorAccountDisabled        = errors.New(errorCodeAdministratorAccountDisabled)
	errAdministratorAccountInvalid         = errors.New(errorCodeInvalidInput)
	errAdministratorAccountRevision        = errors.New(errorCodeAdministratorAccountRevisionConflict)
	errAdministratorAccountIdentityReused  = errors.New(errorCodeAdministratorAccountIdentityReused)
	errAdministratorAccountSelfMutation    = errors.New(errorCodeAdministratorAccountSelfMutation)
	errAdministratorAccountSessionNotFound = errors.New(errorCodeAuthSessionNotFound)
)

type storedAdministratorAccountState struct {
	UserID       string `db:"user_id"`
	Status       string `db:"status"`
	Revision     int    `db:"revision"`
	RequestID    string `db:"request_id"`
	RequestHash  string `db:"request_hash"`
	CleanupJobID string `db:"cleanup_job_id"`
}

func registerAdministratorAccountStateRoutes(event *core.ServeEvent) {
	event.Router.PATCH(apiRoot+"/admin/accounts/{accountId}/state", updateAdministratorAccountState)
	event.Router.POST(apiRoot+"/admin/accounts/{accountId}/sessions/revoke", revokeAdministratorAccountSessions)
	event.Router.PUT(apiRoot+"/admin/accounts/{accountId}/administrator", updateAdministratorAccountRole)
}

func updateAdministratorAccountState(event *core.RequestEvent) error {
	input := administratorAccountStateInput{}
	if err := event.BindBody(&input); err != nil {
		return administratorAccountMutationError(event, err, "state-bind")
	}
	// A status it doesn't take is the request's to fix, not a change made
	// meanwhile (2026-11-26: "suspended" was told the account had changed).
	if input.Status != administratorAccountStatusActive && input.Status != administratorAccountStatusDisabled {
		return administratorAccountMutationError(event, refuse(errAdministratorAccountInvalid,
			"status is %s or %s, not %q", administratorAccountStatusActive, administratorAccountStatusDisabled, input.Status), "state-validate")
	}
	return applyAdministratorAccountStateMutation(event, input.RequestID, input.ExpectedRevision, input.Status, "state")
}

func revokeAdministratorAccountSessions(event *core.RequestEvent) error {
	input := administratorAccountMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return administratorAccountMutationError(event, err, "sessions-bind")
	}
	return applyAdministratorAccountStateMutation(event, input.RequestID, input.ExpectedRevision, "", "sessions-revoke")
}

func applyAdministratorAccountStateMutation(event *core.RequestEvent, requestID string, expected int, nextStatus, action string) error {
	return applyAdministratorAccountMutation(event, requestID, expected, action, nextStatus,
		func(txApp core.App, targetID string, state storedAdministratorAccountState) (string, error) {
			status := state.Status
			if nextStatus != "" {
				status = nextStatus
			}
			if action == "sessions-revoke" || status == administratorAccountStatusDisabled {
				if _, err := revokeAllBrowserSessionsCount(txApp, targetID); err != nil {
					return "", err
				}
			}
			return status, nil
		})
}

// administratorAccountChange applies one administrator action to another
// account inside its revisioned mutation and returns the status it leaves.
type administratorAccountChange func(txApp core.App, targetID string, state storedAdministratorAccountState) (string, error)

// applyAdministratorAccountMutation runs an administrator action on another
// account once per request: the expected revision must match, the account
// must not be leaving, and the action is kept in the account audit log.
func applyAdministratorAccountMutation(
	event *core.RequestEvent, requestID string, expected int, action, hashValue string, change administratorAccountChange,
) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	actor, _ := authenticatedUser(event)
	targetID := strings.TrimSpace(event.Request.PathValue("accountId"))
	requestID = strings.TrimSpace(requestID)
	if refused := administratorAccountRequestRefusal(targetID, requestID, expected); refused != nil {
		return administratorAccountMutationError(event, refused, "validate")
	}
	if targetID == actor.Id {
		return administratorAccountMutationError(event, errAdministratorAccountSelfMutation, "self")
	}
	hash := administratorAccountMutationHash(action, hashValue)
	release := acquireProductOperationLock(administratorAccountLockNamespace, targetID)
	defer release()
	replayed := false
	mutate := func(txApp core.App) error {
		state, err := loadAdministratorAccountState(txApp, targetID)
		if err != nil {
			return err
		}
		if state.RequestID == requestID {
			if state.RequestHash != hash {
				return errAdministratorAccountIdentityReused
			}
			replayed = true
			return nil
		}
		if state.Revision != expected || state.Status == administratorAccountStatusDeleting || state.Status == administratorAccountStatusDeleted {
			return errAdministratorAccountRevision
		}
		status, err := change(txApp, targetID, state)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`UPDATE {{leafen_administrator_account_states}}
			SET [[status]]={:status},[[revision]]=[[revision]]+1,[[request_id]]={:requestId},[[request_hash]]={:requestHash},
				[[updated_by]]={:actorId},[[updated_at]]={:updated}
			WHERE [[user_id]]={:targetId} AND [[revision]]={:revision}`).Bind(dbx.Params{
			"status": status, "requestId": requestID, "requestHash": hash, "actorId": actor.Id,
			"updated": now, "targetId": targetID, "revision": expected,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return refusedUnlessStorage(errAdministratorAccountRevision, err)
		}
		_, err = txApp.DB().Insert(administratorAccountAuditTable, dbx.Params{
			"id": core.GenerateDefaultRandomId(), "actor_id": actor.Id, "target_user_id": targetID,
			"action": action, "from_status": state.Status, "to_status": status,
			"revision": expected + 1, "request_id": requestID, "created_at": now,
		}).Execute()
		return err
	}
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, mutate)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(mutate)
	}
	if err != nil {
		return administratorAccountMutationError(event, err, action)
	}
	account, err := loadManagedAdministratorAccount(event.App, targetID)
	if err != nil {
		return administratorAccountMutationError(event, err, action+"-read")
	}
	return event.JSON(http.StatusOK, administratorAccountMutation{Account: account, Replayed: replayed})
}

// administratorAccountRequestRefusal is why an administrator's change of
// another account lacks what it needs, or nil: what the request lacks or
// sends wrong is said as such, and only a revision other than the account's
// is a change made meanwhile (2026-11-26: an account suspended without
// requestId and expectedRevision was told it had changed, a 409).
func administratorAccountRequestRefusal(targetID, requestID string, expected int) error {
	switch {
	case targetID == "":
		return sql.ErrNoRows
	case requestID == "" || utf8.RuneCountInString(requestID) > administratorAccountRequestIDRunes:
		return refuse(errAdministratorAccountInvalid,
			"requestId is a new id of yours, 1–%d characters, so the same change sent again isn't made twice",
			administratorAccountRequestIDRunes)
	case expected < 1:
		return errAdministratorAccountRevisionMissing
	}
	return nil
}

// The revisions an administrator's change is made at, each as its list reads it.
var (
	errAdministratorAccountRevisionMissing = refuse(errAdministratorAccountInvalid,
		"expectedRevision is the account's revision as GET /admin/accounts lists it, 1 or more")
	errAdministratorCleanupRevisionMissing = refuse(errAdministratorAccountInvalid,
		"expectedRevision is the cleanup's revision as GET /admin/accounts/{accountId}/cleanup/{jobId} reads it, 1 or more")
)

func loadAdministratorAccountState(app core.App, userID string) (storedAdministratorAccountState, error) {
	state := storedAdministratorAccountState{}
	err := app.DB().NewQuery(`SELECT [[user_id]],[[status]],[[revision]],[[request_id]],[[request_hash]],[[cleanup_job_id]]
		FROM {{leafen_administrator_account_states}} WHERE [[user_id]]={:userId}`).Bind(dbx.Params{"userId": userID}).One(&state)
	if errors.Is(err, sql.ErrNoRows) {
		if _, findErr := app.FindRecordById("users", userID); findErr != nil {
			return state, findErr
		}
	}
	return state, err
}

func administratorAccountCanAuthenticate(app core.App, userID string) (bool, error) {
	state, err := loadAdministratorAccountState(app, userID)
	return err == nil && state.Status == administratorAccountStatusActive, err
}

// authorizeAdministratorAccountSession refuses a sign-in to an account the
// server's administrator deactivated or an organization suspended.
func authorizeAdministratorAccountSession(app core.App, userID string) error {
	allowed, err := administratorAccountCanAuthenticate(app, userID)
	if err != nil {
		return err
	}
	if !allowed {
		return errAdministratorAccountDisabled
	}
	suspended, err := organizationUserSuspended(app, userID)
	if err != nil {
		return err
	}
	if suspended {
		return errAdministratorAccountDisabled
	}
	return nil
}

func administratorAccountMutationHash(action, status string) string {
	digest := sha256.Sum256([]byte(action + "\x00" + status))
	return hex.EncodeToString(digest[:])
}

func administratorAccountPasswordMutationHash(app core.App, password string) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	digest := hmac.New(sha256.New, []byte(secret))
	_, _ = digest.Write([]byte("password-reset\x00" + password))
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func administratorAccountMutationError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case errors.Is(err, errAdministratorAccountSelfMutation):
		return apiError(event, http.StatusConflict, errorCodeAdministratorAccountSelfMutation, "This action can't be performed on your own account.")
	case errors.Is(err, errAdministratorAccountIdentityReused):
		return apiError(event, http.StatusConflict, errorCodeAdministratorAccountIdentityReused, "This request was already used for a different operation.")
	case errors.Is(err, errAdministratorAccountDisabled):
		return apiError(event, http.StatusConflict, errorCodeAdministratorAccountDisabled, "Only an active account can be made a server administrator.")
	case errors.Is(err, errAdministratorAccountInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, refusalMessage("Check the account change.", refusalReason(err)))
	case errors.Is(err, errAdministratorAccountRevision):
		return apiError(event, http.StatusConflict, errorCodeAdministratorAccountRevisionConflict, "The account state changed. Check it again.")
	case errors.Is(err, errAdministratorAccountSessionNotFound):
		return authSessionNotFoundAPIError(event)
	case errors.Is(err, errLegalHoldPreventsPermanentDelete):
		return apiError(event, http.StatusPreconditionFailed, errorCodeLegalHoldPreventsPermanentDelete, "Release the active legal hold, then delete.")
	case errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeAdministratorAccountNotFound, "Account not found.")
	default:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorAccountCleanupFailed,
			"Couldn't complete the account operation.", "administrator-account", stage,
			errorCodeAdministratorAccountCleanupFailed, err)
	}
}
