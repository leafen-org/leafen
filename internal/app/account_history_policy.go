package app

import (
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Whether those with full access to a teamspace or a page see the
// part of another's history in what they hold (the audit decision of
// 2026-10-21 20:05, 1): on until the organization's audit managers turn it
// off (account_history.go).

// accountHistoryPolicyOf is an organization's policy, on when never set.
func accountHistoryPolicyOf(app core.App, organizationID string) accountHistoryPolicyResponse {
	stored := struct {
		ScopedView bool `db:"scoped_view"`
		Revision   int  `db:"revision"`
	}{ScopedView: true}
	_ = app.DB().NewQuery(`SELECT [[scoped_view]], [[revision]] FROM {{leafen_organization_account_history_policies}}
		WHERE [[organization_id]] = {:organization}`).Bind(dbx.Params{"organization": organizationID}).One(&stored)
	return accountHistoryPolicyResponse{OrganizationID: organizationID, ScopedView: stored.ScopedView, Revision: stored.Revision}
}

func readAccountHistoryPolicyRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	return event.JSON(http.StatusOK, accountHistoryPolicyOf(event.App, authorization.OrganizationID))
}

// updateAccountHistoryPolicyRoute sets the policy, by those who keep the
// audit log, the change in it.
func updateAccountHistoryPolicyRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	input := accountHistoryPolicyInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeOrganizationAuditInvalid, "Check the account history policy.")
	}
	var response accountHistoryPolicyResponse
	change := func(txApp core.App) error {
		current := accountHistoryPolicyOf(txApp, authorization.OrganizationID)
		if current.Revision != input.Revision {
			return errOrganizationPolicyRevisionChanged
		}
		now := time.Now().UnixMilli()
		response = accountHistoryPolicyResponse{OrganizationID: authorization.OrganizationID, ScopedView: input.ScopedView, Revision: current.Revision + 1}
		if _, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_organization_account_history_policies}}
			([[organization_id]], [[scoped_view]], [[revision]], [[updated_by]], [[updated_at]])
			VALUES ({:organization}, {:scoped}, {:revision}, {:by}, {:now})
			ON CONFLICT ([[organization_id]]) DO UPDATE SET [[scoped_view]] = excluded.[[scoped_view]], [[revision]] = excluded.[[revision]],
				[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`).
			Bind(dbx.Params{"organization": authorization.OrganizationID, "scoped": input.ScopedView, "revision": response.Revision,
				"by": authorization.PrincipalID, "now": now}).Execute(); err != nil {
			return err
		}
		return insertOrganizationAuditEvent(txApp, organizationAuditEvent{
			OrganizationID: authorization.OrganizationID, ActorID: authorization.PrincipalID, Action: "account_history.policy",
			TargetType: "organization", TargetID: authorization.OrganizationID, Metadata: map[string]any{"scopedView": input.ScopedView}, OccurredAt: now,
		})
	}
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return organizationAuditError(event, err, "account-history-policy")
	}
	return event.JSON(http.StatusOK, response)
}
