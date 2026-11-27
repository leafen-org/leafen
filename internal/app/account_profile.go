package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errAccountProfileIdentityReused   = errors.New(errorCodeAccountProfileIdentityReused)
	errAccountProfileRevisionConflict = errors.New(errorCodeAccountProfileRevisionConflict)
)

type storedAccountProfileReceipt struct {
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

func registerAccountProfileRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/account/profile", readAccountProfile)
	event.Router.PATCH(apiRoot+"/account/profile", updateAccountProfile).
		Bind(apis.BodyLimit(accountProfileMaxBodyBytes))
}

func accountProfileFor(user *core.Record, revision int) accountProfileResponse {
	return accountProfileResponse{
		ID: user.Id, Email: user.Email(), DisplayName: user.GetString("name"),
		Avatar: user.GetString("profile_avatar"), Revision: revision,
	}
}

func readAccountProfileRevision(app core.App, userID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`
		SELECT [[revision]] FROM {{leafen_account_profile_state}}
		WHERE [[user_id]] = {:userId}
	`).Bind(dbx.Params{"userId": userID}).Row(&revision)
	return revision, err
}

func readAccountProfile(event *core.RequestEvent) error {
	actor, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	response := accountProfileResponse{}
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		user, err := txApp.FindRecordById("users", actor.Id)
		if err != nil {
			return err
		}
		revision, err := readAccountProfileRevision(txApp, actor.Id)
		if err != nil {
			return err
		}
		response = accountProfileFor(user, revision)
		return nil
	})
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountProfileReadFailed,
			"Couldn't load the profile.", "account-profile.read", "transaction",
			errorCodeAccountProfileReadFailed, err,
		)
	}
	return event.JSON(http.StatusOK, response)
}

// accountProfileSentFields is which of a profile change's name and photo its
// request sent: one left out, or sent null, stays as the profile has it, as
// a page metadata change's title does (page_metadata_sent_fields.go;
// 2026-11-13: a new name sent alone took the photo away). The screen sends
// both.
type accountProfileSentFields struct {
	DisplayName *string `json:"displayName"`
	Avatar      *string `json:"avatar"`
}

func normalizeAccountProfileInput(input *accountProfileMutationInput, sent accountProfileSentFields, request *http.Request) bool {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Avatar = strings.TrimSpace(input.Avatar)
	return input.RequestID != "" && utf8.RuneCountInString(input.RequestID) <= maxAccountProfileRequestIDRunes &&
		input.BaseRevision >= 0 && (input.DisplayName != "" || sent.DisplayName == nil) &&
		utf8.RuneCountInString(input.DisplayName) <= maxAuthNameLength &&
		utf8.RuneCountInString(input.Avatar) <= maxAccountProfileAvatarRunes &&
		validAccountAvatar(input.Avatar) &&
		!isManagedFileMetadataLocator(input.Avatar, request)
}

// accountProfileRequestHash is a profile change's request hash; one that
// sent both name and photo hashes as it always did.
func accountProfileRequestHash(input accountProfileMutationInput, sent accountProfileSentFields) (string, error) {
	payload, err := json.Marshal(struct {
		BaseRevision int    `json:"baseRevision"`
		DisplayName  string `json:"displayName"`
		Avatar       string `json:"avatar"`
		NameLeftOut  bool   `json:"nameLeftOut,omitzero"`
		PhotoLeftOut bool   `json:"photoLeftOut,omitzero"`
	}{input.BaseRevision, input.DisplayName, input.Avatar, sent.DisplayName == nil, sent.Avatar == nil})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func readAccountProfileReceipt(app core.App, userID, requestID string) (storedAccountProfileReceipt, error) {
	receipt := storedAccountProfileReceipt{}
	err := app.DB().NewQuery(`
		SELECT [[request_hash]], [[response_json]]
		FROM {{leafen_account_profile_receipts}}
		WHERE [[user_id]] = {:userId} AND [[request_id]] = {:requestId}
	`).Bind(dbx.Params{"userId": userID, "requestId": requestID}).One(&receipt)
	return receipt, err
}

// mutateAccountProfileAuthority is the only post-creation writer for the
// canonical display name/avatar pair. Callers supply their own authorization
// and transaction boundary; this owner atomically advances the shared revision.
func mutateAccountProfileAuthority(
	app core.App,
	user *core.Record,
	displayName string,
	avatar string,
) (accountProfileResponse, error) {
	currentRevision, err := readAccountProfileRevision(app, user.Id)
	if err != nil {
		return accountProfileResponse{}, err
	}
	if user.GetString("name") == displayName && user.GetString("profile_avatar") == avatar {
		return accountProfileFor(user, currentRevision), nil
	}
	nextRevision := currentRevision + 1
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_account_profile_state}}
		SET [[revision]] = {:nextRevision}, [[updated]] = {:updated}
		WHERE [[user_id]] = {:userId} AND [[revision]] = {:currentRevision}
	`).Bind(dbx.Params{
		"nextRevision": nextRevision, "updated": time.Now().UTC().UnixMilli(),
		"userId": user.Id, "currentRevision": currentRevision,
	}).Execute()
	if err != nil {
		return accountProfileResponse{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return accountProfileResponse{}, refusedUnlessStorage(errAccountProfileRevisionConflict, err)
	}
	user.Set("name", displayName)
	user.Set("profile_avatar", avatar)
	if err := app.Save(user); err != nil {
		return accountProfileResponse{}, err
	}
	return accountProfileFor(user, nextRevision), nil
}

func pruneAccountProfileReceipts(app core.App, userID string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_account_profile_receipts}}
		WHERE [[user_id]] = {:userId} AND [[request_id]] IN (
			SELECT [[request_id]] FROM {{leafen_account_profile_receipts}}
			WHERE [[user_id]] = {:userId}
			ORDER BY [[created]] DESC, [[request_id]] DESC
			LIMIT -1 OFFSET {:receiptLimit}
		)
	`).Bind(dbx.Params{
		"userId": userID, "receiptLimit": accountProfileReceiptLimit,
	}).Execute()
	return err
}

func updateAccountProfile(event *core.RequestEvent) error {
	actor, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accountProfileMutationInput{}
	sent := accountProfileSentFields{}
	if err := rereadableRequestBody(event); err != nil || event.BindBody(&input) != nil || event.BindBody(&sent) != nil ||
		!normalizeAccountProfileInput(&input, sent, event.Request) {
		return apiError(event, http.StatusBadRequest, errorCodeAccountProfileInvalid, "Check the profile.")
	}
	requestHash, err := accountProfileRequestHash(input, sent)
	if err != nil {
		return accountProfileInternalError(event, err)
	}

	response := accountProfileMutationResponse{}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		receipt, err := readAccountProfileReceipt(txApp, actor.Id, input.RequestID)
		if err == nil {
			if receipt.RequestHash != requestHash {
				return errAccountProfileIdentityReused
			}
			if err := json.Unmarshal([]byte(receipt.ResponseJSON), &response); err != nil {
				return err
			}
			response.Receipt.Duplicate = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		user, err := txApp.FindRecordById("users", actor.Id)
		if err != nil {
			return err
		}
		currentRevision, err := readAccountProfileRevision(txApp, actor.Id)
		if err != nil {
			return err
		}
		if currentRevision != input.BaseRevision {
			return errAccountProfileRevisionConflict
		}
		// An organization may keep its managed users' names and photos
		// (organization_managed_user_policies.go).
		if rules, err := loadManagedUserRules(txApp, user); err != nil {
			return err
		} else if rules.Managed && !rules.ProfileChanges {
			return errProfileChangesDisabled
		}
		if sent.DisplayName == nil {
			input.DisplayName = user.GetString("name")
		}
		if sent.Avatar == nil {
			input.Avatar = user.GetString("profile_avatar")
		}
		profile, err := mutateAccountProfileAuthority(
			txApp, user, input.DisplayName, input.Avatar,
		)
		if err != nil {
			return err
		}
		response = accountProfileMutationResponse{
			Profile: profile,
			Receipt: accountProfileMutationReceipt{
				RequestID: input.RequestID, Revision: profile.Revision,
			},
		}
		stored, err := json.Marshal(response)
		if err != nil || len(stored) > accountProfileResponseMaxBytes {
			return errors.New(errorCodeAccountProfileSaveFailed)
		}
		_, err = txApp.DB().Insert(accountProfileReceiptsTable, dbx.Params{
			"user_id": actor.Id, "request_id": input.RequestID,
			"request_hash": requestHash, "revision": profile.Revision,
			"response_json": string(stored), "created": time.Now().UTC().UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
		return pruneAccountProfileReceipts(txApp, actor.Id)
	})
	if errors.Is(err, errAccountProfileIdentityReused) {
		return apiError(event, http.StatusConflict, errorCodeAccountProfileIdentityReused, "The same profile request can't be reused with different content.")
	}
	if errors.Is(err, errAccountProfileRevisionConflict) {
		return apiError(event, http.StatusConflict, errorCodeAccountProfileRevisionConflict, "The profile already changed.")
	}
	if errors.Is(err, errProfileChangesDisabled) {
		return apiError(event, http.StatusForbidden, errorCodeProfileChangesDisabled, "Your organization manages your name and photo.")
	}
	if err != nil {
		return accountProfileInternalError(event, err)
	}
	// A new name is followed where it shows once saved (personRenamed).
	return event.JSON(http.StatusOK, response)
}

func accountProfileInternalError(event *core.RequestEvent, cause error) error {
	if storageError, ok := persistentWriteStorageAPIError(
		event, cause, persistentWriteAccountProfile, "transaction",
	); ok {
		return storageError
	}
	return apiErrorWithCause(
		event, http.StatusInternalServerError, errorCodeAccountProfileSaveFailed,
		"Couldn't save the profile.", "account-profile.update", "transaction",
		errorCodeAccountProfileSaveFailed, cause,
	)
}
