package organization

import (
	"errors"
	"strconv"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

// validAuditRetentionDays takes 0, keeping the audit log (the default:
// Leafen chooses no period for anyone), or a period of a day or more.
func validAuditRetentionDays(days int) bool {
	return days == 0 || days >= leafen.AuditRetentionMinimumDays
}

func updateOrganizationAuditRetentionPolicy(
	app core.App,
	authorization leafen.OrganizationAuthorization,
	retentionDays int,
) (leafen.AuditRetentionPolicyResponse, error) {
	if !validAuditRetentionDays(retentionDays) {
		return leafen.AuditRetentionPolicyResponse{}, leafen.ErrAuditRetentionInvalid
	}
	response := leafen.AuditRetentionPolicyResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		currentAuthorization, err := leafen.AuthorizeOrganization(
			txApp, authorization.PrincipalID, authorization.OrganizationID,
			leafen.PermissionOrganizationAuditLog,
		)
		if err != nil {
			return err
		}
		current, err := leafen.LoadOrganizationAuditRetentionPolicy(
			txApp, currentAuthorization.OrganizationID,
		)
		if err != nil {
			return err
		}
		if current.RetentionDays == retentionDays {
			current.Duplicate = true
			response = current
			return nil
		}
		now := time.Now().UTC().UnixMilli()
		revision := current.Revision + 1
		if err := leafen.StoreOrganizationAuditRetentionPolicy(
			txApp, currentAuthorization.OrganizationID, retentionDays, revision,
			currentAuthorization.PrincipalID, now,
		); err != nil {
			return errors.Join(leafen.ErrAuditRetentionUnavailable, err)
		}
		if err := leafen.BumpOrganizationPolicy(txApp, currentAuthorization.OrganizationID, now); err != nil {
			return errors.Join(leafen.ErrAuditRetentionUnavailable, err)
		}
		// The days kept before and after (0 keeps all), which the line says.
		if err := leafen.RecordOrganizationAuditEvent(txApp, leafen.OrganizationAuditEvent{
			OrganizationID: currentAuthorization.OrganizationID, ActorID: currentAuthorization.PrincipalID,
			Action: "organization.audit_retention.updated", TargetType: "audit_retention_policy",
			TargetID: currentAuthorization.OrganizationID + ":" + strconv.Itoa(revision),
			Metadata: map[string]any{"from": current.RetentionDays, "to": retentionDays}, OccurredAt: now,
		}); err != nil {
			return errors.Join(leafen.ErrAuditRetentionUnavailable, err)
		}
		response = leafen.AuditRetentionPolicyResponse{
			OrganizationID: currentAuthorization.OrganizationID,
			RetentionDays:  retentionDays, Revision: revision, UpdatedAt: now,
		}
		if retentionDays == 0 {
			// Keeping the log is having no policy, as it reads from now on.
			response.Revision, response.UpdatedAt = 0, 0
		}
		return nil
	})
	return response, err
}
