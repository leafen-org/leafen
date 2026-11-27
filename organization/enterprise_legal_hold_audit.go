package organization

import (
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// recordLegalHoldAudit writes a legal hold's audit line — made, changed,
// released, a custodian added or taken off — with the hold's name as it is
// in the same change, so the log says which hold each line was (2026-11-13:
// five holds made and released read only "법적 보존 생성됨 · 〈작업자〉")
// and still says it once the hold is renamed.
func recordLegalHoldAudit(app core.App, organizationID, actorID, action, holdID string, at int64) error {
	name := ""
	if err := app.DB().NewQuery(`
		SELECT [[name]] FROM {{leafen_organization_legal_holds}}
		WHERE [[id]] = {:id} AND [[organization_id]] = {:organizationId}
	`).Bind(dbx.Params{"id": holdID, "organizationId": organizationID}).Row(&name); err != nil {
		return err
	}
	if at <= 0 {
		at = time.Now().UTC().UnixMilli()
	}
	return leafen.RecordOrganizationAuditEvent(app, leafen.OrganizationAuditEvent{
		OrganizationID: organizationID, ActorID: actorID, Action: action,
		TargetType: "legal_hold", TargetID: holdID, Metadata: map[string]any{"name": name}, OccurredAt: at,
	})
}
