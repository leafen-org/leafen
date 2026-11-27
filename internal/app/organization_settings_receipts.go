package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedOrganizationSettingsReceipt struct {
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

func markOrganizationSettingsReceiptDuplicate(response *organizationSettingsMutationResponse) {
	response.Receipt.Duplicate = true
}

func organizationSettingsRequestHash(input organizationSettingsMutationInput) (string, error) {
	canonical, err := json.Marshal(struct {
		ExpectedRevision  int    `json:"expectedRevision"`
		Action            string `json:"action"`
		TargetID          string `json:"targetId"`
		SecondaryTargetID string `json:"secondaryTargetId"`
		Value             string `json:"value"`
	}{input.ExpectedRevision, input.Action, input.TargetID, input.SecondaryTargetID, input.Value})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func readOrganizationSettingsReceipt(
	app core.App, organizationID, actorID, requestID string,
) (storedOrganizationSettingsReceipt, error) {
	receipt := storedOrganizationSettingsReceipt{}
	err := app.DB().NewQuery(`SELECT [[request_hash]], [[response_json]]
		FROM {{leafen_organization_settings_receipts}}
		WHERE [[organization_id]]={:organizationId} AND [[actor_id]]={:actorId} AND [[request_id]]={:requestId}`).Bind(
		dbx.Params{"organizationId": organizationID, "actorId": actorID, "requestId": requestID},
	).One(&receipt)
	return receipt, err
}

func storeOrganizationSettingsReceipt(
	app core.App, organizationID, actorID, requestID, requestHash string,
	revision int, response organizationSettingsMutationResponse, now int64,
) error {
	encoded, err := json.Marshal(response)
	if err != nil || len(encoded) > organizationSettingsResponseBytes {
		return errOrganizationSettingsInvalid
	}
	created, err := nextOrganizationSettingsReceiptCreated(app, organizationID, now)
	if err != nil {
		return err
	}
	if _, err := app.DB().Insert(organizationSettingsReceiptsTable, dbx.Params{
		"organization_id": organizationID, "actor_id": actorID, "request_id": requestID,
		"request_hash": requestHash, "revision": revision, "response_json": string(encoded), "created": created,
	}).Execute(); err != nil {
		return err
	}
	return pruneOrganizationSettingsReceipts(app, organizationID)
}

func nextOrganizationSettingsReceiptCreated(app core.App, organizationID string, floor int64) (int64, error) {
	var newest int64
	err := app.DB().NewQuery(`SELECT COALESCE(MAX([[created]]), -1)
		FROM {{leafen_organization_settings_receipts}} WHERE [[organization_id]]={:organizationId}`).Bind(
		dbx.Params{"organizationId": organizationID},
	).Row(&newest)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	if newest >= floor {
		return newest + 1, nil
	}
	return floor, nil
}

func pruneOrganizationSettingsReceipts(app core.App, organizationID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_organization_settings_receipts}}
		WHERE [[rowid]] IN (SELECT [[rowid]] FROM {{leafen_organization_settings_receipts}}
			WHERE [[organization_id]]={:organizationId}
			ORDER BY [[created]] DESC, [[request_id]] DESC, [[actor_id]] DESC
			LIMIT -1 OFFSET {:limit})`).Bind(dbx.Params{
		"organizationId": organizationID, "limit": organizationSettingsReceiptLimit,
	}).Execute()
	return err
}
