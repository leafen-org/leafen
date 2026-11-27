package app

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Whom page analytics shows as having viewed a page (the audit decision of
// 2026-10-21 20:05, 4): each person chooses, as Notion's "내 조회 기록 표시"
// (person, the default); everyone is shown, and no one hides (shown); or no
// one is named, the views only counted (hidden). The organization's audit
// managers choose it; the read log keeps every read whichever it is
// (read_log.go).

const (
	viewDisplayPerson = "person"
	viewDisplayShown  = "shown"
	viewDisplayHidden = "hidden"
)

// viewDisplayPolicyOf is an organization's policy, each person's own when
// never set.
func viewDisplayPolicyOf(app core.App, organizationID string) viewDisplayPolicyResponse {
	stored := struct {
		Display  string `db:"display"`
		Revision int    `db:"revision"`
	}{Display: viewDisplayPerson}
	_ = app.DB().NewQuery(`SELECT [[display]], [[revision]] FROM {{leafen_organization_view_display_policies}}
		WHERE [[organization_id]] = {:organization}`).Bind(dbx.Params{"organization": organizationID}).One(&stored)
	return viewDisplayPolicyResponse{OrganizationID: organizationID, Display: stored.Display, Revision: stored.Revision}
}

// workspaceViewDisplay is the policy of a workspace's organization.
func workspaceViewDisplay(app core.App, workspaceID string) (string, error) {
	display := viewDisplayPerson
	err := app.DB().NewQuery(`
		SELECT COALESCE((SELECT [[policy]].[[display]] FROM {{leafen_organization_view_display_policies}} [[policy]]
			WHERE [[policy]].[[organization_id]] = [[workspace]].[[organization]]), 'person')
		FROM {{leafen_workspaces}} [[workspace]] WHERE [[workspace]].[[id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&display)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return display, nil
}

func readViewDisplayPolicyRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	return event.JSON(http.StatusOK, viewDisplayPolicyOf(event.App, authorization.OrganizationID))
}

// updateViewDisplayPolicyRoute sets the policy, by those who keep the audit
// log, the change in it.
func updateViewDisplayPolicyRoute(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	input := viewDisplayPolicyInput{}
	if err := event.BindBody(&input); err != nil || input.Display != viewDisplayPerson && input.Display != viewDisplayShown && input.Display != viewDisplayHidden {
		return apiError(event, http.StatusBadRequest, errorCodeOrganizationAuditInvalid, "Check the view display policy.")
	}
	var response viewDisplayPolicyResponse
	err := event.App.RunInTransaction(func(txApp core.App) error {
		current := viewDisplayPolicyOf(txApp, authorization.OrganizationID)
		if current.Revision != input.Revision {
			return errOrganizationPolicyRevisionChanged
		}
		now := time.Now().UnixMilli()
		response = viewDisplayPolicyResponse{OrganizationID: authorization.OrganizationID, Display: input.Display, Revision: current.Revision + 1}
		if _, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_organization_view_display_policies}}
			([[organization_id]], [[display]], [[revision]], [[updated_by]], [[updated_at]])
			VALUES ({:organization}, {:display}, {:revision}, {:by}, {:now})
			ON CONFLICT ([[organization_id]]) DO UPDATE SET [[display]] = excluded.[[display]], [[revision]] = excluded.[[revision]],
				[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`).
			Bind(dbx.Params{"organization": authorization.OrganizationID, "display": input.Display, "revision": response.Revision,
				"by": authorization.PrincipalID, "now": now}).Execute(); err != nil {
			return err
		}
		return insertOrganizationAuditEvent(txApp, organizationAuditEvent{
			OrganizationID: authorization.OrganizationID, ActorID: authorization.PrincipalID, Action: "analytics.view_display.policy",
			TargetType: "organization", TargetID: authorization.OrganizationID,
			Metadata: map[string]any{"from": current.Display, "to": input.Display}, OccurredAt: now,
		})
	})
	if err != nil {
		return organizationAuditError(event, err, "view-display-policy")
	}
	return event.JSON(http.StatusOK, response)
}
