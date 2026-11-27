package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errOrganizationStorageLimitInvalid        = errors.New(errorCodeOrganizationStorageLimitInvalid)
	errOrganizationStoragePolicyIdentityReuse = errors.New(errorCodeOrganizationStoragePolicyRequestIDReused)
	errOrganizationStoragePolicyConflict      = errors.New(errorCodeOrganizationStoragePolicyConflict)
)

type storedOrganizationStoragePolicyReceipt struct {
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

func registerOrganizationStoragePolicyRoutes(event *core.ServeEvent) {
	event.Router.Route(
		organizationStorageLimitUpdateMethod,
		apiRoot+organizationStorageLimitUpdatePath,
		updateOrganizationStorageLimit,
	).Bind(apis.BodyLimit(organizationStorageMutationMaxBodyBytes))
}

func updateOrganizationStorageLimit(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	input := organizationStorageLimitUpdateInput{}
	if err := event.BindBody(&input); err != nil {
		return organizationStoragePolicyError(event, errOrganizationStorageLimitInvalid, "bind")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	if organizationID == "" || input.RequestID == "" ||
		utf8.RuneCountInString(organizationID) > organizationIdentifierMaxRunes ||
		utf8.RuneCountInString(input.RequestID) > organizationStoragePolicyRequestIDRunes ||
		input.ExpectedPolicyVersion < 0 ||
		(input.StorageLimitBytes != nil && *input.StorageLimitBytes < 0) {
		return organizationStoragePolicyError(event, errOrganizationStorageLimitInvalid, "validate")
	}
	requestHash, err := organizationStoragePolicyRequestHash(input)
	if err != nil {
		return organizationStoragePolicyError(event, err, "hash")
	}
	release := acquireProductFileOperationLock(
		productOrganizationStorageLockNamespace, organizationID,
	)
	defer release()
	response := organizationStorageLimitUpdateResponse{}
	change := func(txApp core.App) error {
		authorization, err := authorizeOrganization(
			txApp, user.Id, organizationID, permissionOrganizationBillingAdmin,
		)
		if err != nil {
			return err
		}
		receipt, err := readOrganizationStoragePolicyReceipt(
			txApp, organizationID, user.Id, input.RequestID,
		)
		if err == nil {
			if receipt.RequestHash != requestHash {
				return errOrganizationStoragePolicyIdentityReuse
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
		organization, err := txApp.FindRecordById(organizationsCollection, organizationID)
		if err != nil {
			return err
		}
		policyVersion, err := readOrganizationStoragePolicyVersion(txApp, organizationID)
		if err != nil {
			return err
		}
		if policyVersion != input.ExpectedPolicyVersion {
			return errOrganizationStoragePolicyConflict
		}
		previous, err := decodeOrganizationStorageLimit(
			organization.GetString("storage_limit_bytes"),
		)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		nextPolicyVersion := policyVersion
		if !equalOrganizationStorageLimits(previous, input.StorageLimitBytes) {
			stored := ""
			if input.StorageLimitBytes != nil {
				stored = strconv.FormatInt(*input.StorageLimitBytes, 10)
			}
			organization.Set("storage_limit_bytes", stored)
			if err := txApp.Save(organization); err != nil {
				return err
			}
			nextPolicyVersion, err = bumpOrganizationStoragePolicyVersion(
				txApp, organizationID, policyVersion, now,
			)
			if err != nil {
				return err
			}
			if err := recordOrganizationAuditEvent(txApp, organizationAuditEvent{
				ID: organizationAuditID(
					organizationID,
					"organization_settings.update",
					organizationID+"\x00"+authorization.PrincipalID+"\x00"+input.RequestID+
						"\x00"+strconv.Itoa(nextPolicyVersion),
				),
				OrganizationID: organizationID, ActorID: authorization.PrincipalID,
				Action: "organization_settings.update", TargetType: "organization", TargetID: organizationID,
				Metadata: map[string]any{
					"requestId": input.RequestID, "policyVersion": nextPolicyVersion,
					"storageLimitBytes": input.StorageLimitBytes,
				},
				OccurredAt: now,
			}); err != nil {
				return err
			}
		}
		settingsRevision, err := readOrganizationSettingsRevision(txApp, organizationID)
		if err != nil {
			return err
		}
		if err := organizationStoragePolicyResponse(
			authorization, organization, input.RequestID, nextPolicyVersion, settingsRevision, &response,
		); err != nil {
			return err
		}
		encodedResponse, err := json.Marshal(response)
		if err != nil || len(encodedResponse) > organizationStoragePolicyResponseBytes {
			return errOrganizationStorageStateInvalid
		}
		receiptCreated, err := nextOrganizationStoragePolicyReceiptCreated(
			txApp, organizationID, now,
		)
		if err != nil {
			return err
		}
		if _, err := txApp.DB().Insert(
			organizationStoragePolicyReceiptsTable,
			dbx.Params{
				"organization_id": organizationID, "actor_id": user.Id,
				"request_id": input.RequestID, "request_hash": requestHash,
				"policy_version": nextPolicyVersion,
				"response_json":  string(encodedResponse), "created": receiptCreated,
			},
		).Execute(); err != nil {
			return err
		}
		return pruneOrganizationStoragePolicyReceipts(txApp, organizationID)
	}
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	err = rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		response = organizationStorageLimitUpdateResponse{}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return organizationStoragePolicyError(event, err, "update")
	}
	return event.JSON(http.StatusOK, response)
}

func organizationStoragePolicyResponse(
	authorization organizationAuthorization,
	organization *core.Record,
	requestID string,
	policyVersion, settingsRevision int,
	response *organizationStorageLimitUpdateResponse,
) error {
	storageLimit, err := decodeOrganizationStorageLimit(
		organization.GetString("storage_limit_bytes"),
	)
	if err != nil {
		return err
	}
	response.Organization = organizationResponse{
		ID: organization.Id, Name: organization.GetString("name"),
		Role: authorization.Role, Status: authorization.Status,
		PolicyVersion:     policyVersion,
		SettingsRevision:  settingsRevision,
		StorageLimitBytes: storageLimit,
	}
	response.Receipt = organizationStoragePolicyReceipt{
		RequestID: requestID, PolicyVersion: policyVersion,
	}
	return nil
}

func organizationStoragePolicyRequestHash(input organizationStorageLimitUpdateInput) (string, error) {
	payload, err := json.Marshal(struct {
		ExpectedPolicyVersion int    `json:"expectedPolicyVersion"`
		StorageLimitBytes     *int64 `json:"storageLimitBytes"`
	}{
		ExpectedPolicyVersion: input.ExpectedPolicyVersion,
		StorageLimitBytes:     input.StorageLimitBytes,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func readOrganizationStoragePolicyReceipt(
	app core.App,
	organizationID string,
	actorID string,
	requestID string,
) (storedOrganizationStoragePolicyReceipt, error) {
	receipt := storedOrganizationStoragePolicyReceipt{}
	err := app.DB().NewQuery(`
		SELECT [[request_hash]], [[response_json]]
		FROM {{leafen_organization_storage_policy_receipts}}
		WHERE [[organization_id]]={:organizationId}
			AND [[actor_id]]={:actorId} AND [[request_id]]={:requestId}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "actorId": actorID, "requestId": requestID,
	}).One(&receipt)
	return receipt, err
}

func readOrganizationStoragePolicyVersion(app core.App, organizationID string) (int, error) {
	var version int
	err := app.DB().NewQuery(`
		SELECT [[version]] FROM {{leafen_organization_policy_state}}
		WHERE [[organization_id]]={:organizationId}
	`).Bind(dbx.Params{"organizationId": organizationID}).Row(&version)
	return version, err
}

func bumpOrganizationStoragePolicyVersion(
	app core.App,
	organizationID string,
	expectedVersion int,
	now int64,
) (int, error) {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_policy_state}}
		SET [[version]]=[[version]]+1, [[updated]]={:now}
		WHERE [[organization_id]]={:organizationId} AND [[version]]={:expectedVersion}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "expectedVersion": expectedVersion, "now": now,
	}).Execute()
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if changed != 1 {
		return 0, errOrganizationStoragePolicyConflict
	}
	return expectedVersion + 1, nil
}

func nextOrganizationStoragePolicyReceiptCreated(
	app core.App,
	organizationID string,
	floor int64,
) (int64, error) {
	var newest int64
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[created]]), -1)
		FROM {{leafen_organization_storage_policy_receipts}}
		WHERE [[organization_id]]={:organizationId}
	`).Bind(dbx.Params{"organizationId": organizationID}).Row(&newest)
	if err != nil {
		return 0, err
	}
	if newest >= floor {
		return newest + 1, nil
	}
	return floor, nil
}

func pruneOrganizationStoragePolicyReceipts(app core.App, organizationID string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_organization_storage_policy_receipts}}
		WHERE [[rowid]] IN (
			SELECT [[rowid]] FROM {{leafen_organization_storage_policy_receipts}}
			WHERE [[organization_id]]={:organizationId}
			ORDER BY [[created]] DESC, [[request_id]] DESC, [[actor_id]] DESC
			LIMIT -1 OFFSET {:receiptLimit}
		)
	`).Bind(dbx.Params{
		"organizationId": organizationID,
		"receiptLimit":   organizationStoragePolicyReceiptLimit,
	}).Execute()
	return err
}

func decodeOrganizationStorageLimit(value string) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return nil, errOrganizationStorageStateInvalid
	}
	return &parsed, nil
}

func equalOrganizationStorageLimits(left *int64, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func organizationStoragePolicyError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case errors.Is(err, errOrganizationStorageLimitInvalid):
		return apiErrorWithCause(
			event, http.StatusBadRequest, errorCodeOrganizationStorageLimitInvalid,
			"Check the organization storage limit.", "organization_storage.policy", stage,
			errorCodeOrganizationStorageLimitInvalid, err,
		)
	case errors.Is(err, errOrganizationStoragePolicyIdentityReuse):
		return apiErrorWithCause(
			event, http.StatusConflict, errorCodeOrganizationStoragePolicyRequestIDReused,
			"The same storage policy request can't be reused with different content.",
			"organization_storage.policy", stage,
			errorCodeOrganizationStoragePolicyRequestIDReused, err,
		)
	case errors.Is(err, errOrganizationStoragePolicyConflict):
		return apiErrorWithCause(
			event, http.StatusConflict, errorCodeOrganizationStoragePolicyConflict,
			"The organization storage policy already changed.", "organization_storage.policy", stage,
			errorCodeOrganizationStoragePolicyConflict, err,
		)
	case isPermissionDenied(err):
		return apiErrorWithCause(
			event, http.StatusNotFound, errorCodeOrganizationNotFound,
			"Organization not found.", "organization_storage.policy", stage,
			errorCodeOrganizationNotFound, err,
		)
	default:
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeOrganizationStorageStateInvalid,
			"Couldn't change the organization storage limit.", "organization_storage.policy", stage,
			errorCodeOrganizationStorageStateInvalid, err,
		)
	}
}
