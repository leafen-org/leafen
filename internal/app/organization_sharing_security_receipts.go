package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The receipts of the organization's sharing security changes: each change's
// request ID with the hash of what it asked and the answer it got, so a
// repeated request replays that answer; the newest are kept, the rest pruned.

type storedOrganizationSharingSecurityReceipt struct {
	RequestHash  string `db:"request_hash"`
	ResponseJSON string `db:"response_json"`
}

func readOrganizationSharingSecurityReceipt(app core.App, organizationID, actorID, requestID string) (storedOrganizationSharingSecurityReceipt, error) {
	receipt := storedOrganizationSharingSecurityReceipt{}
	err := app.DB().NewQuery(`SELECT [[request_hash]],[[response_json]] FROM {{leafen_organization_sharing_security_receipts}}
		WHERE [[organization_id]]={:organizationId} AND [[actor_id]]={:actorId} AND [[request_id]]={:requestId}`).
		Bind(dbx.Params{"organizationId": organizationID, "actorId": actorID, "requestId": requestID}).One(&receipt)
	return receipt, err
}

func nextOrganizationSharingSecurityReceiptCreated(app core.App, organizationID string, floor int64) (int64, error) {
	var newest int64
	err := app.DB().NewQuery(`SELECT COALESCE(MAX([[created]]),-1) FROM {{leafen_organization_sharing_security_receipts}}
		WHERE [[organization_id]]={:organizationId}`).Bind(dbx.Params{"organizationId": organizationID}).Row(&newest)
	if err != nil {
		return 0, err
	}
	if newest >= floor {
		return newest + 1, nil
	}
	return floor, nil
}

func pruneOrganizationSharingSecurityReceipts(app core.App, organizationID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_organization_sharing_security_receipts}} WHERE [[rowid]] IN (
		SELECT [[rowid]] FROM {{leafen_organization_sharing_security_receipts}} WHERE [[organization_id]]={:organizationId}
		ORDER BY [[created]] DESC,[[request_id]] DESC,[[actor_id]] DESC LIMIT -1 OFFSET {:receiptLimit})`).
		Bind(dbx.Params{"organizationId": organizationID, "receiptLimit": organizationSharingSecurityReceiptLimit}).Execute()
	return err
}
