package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const organizationSettingsMutationLockNamespace = "organization-settings-mutation"

func mutateOrganizationSettings(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	input := organizationSettingsMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return organizationSettingsError(event, bodyRefusal(errOrganizationSettingsInvalid, "settings change", err), "mutation-bind")
	}
	normalizeOrganizationSettingsInput(&input)
	requirement, err := validateOrganizationSettingsInput(organizationID, input)
	if err != nil {
		return organizationSettingsError(event, err, "mutation-validate")
	}
	if _, err := authorizeOrganization(event.App, user.Id, organizationID, requirement); err != nil {
		return organizationSettingsError(event, err, "mutation-authorization")
	}
	check := OrganizationDomainCheck{}
	if input.Action == organizationSettingsActionDomainVerify {
		check, err = prepareOrganizationDomainVerification(
			event.Request.Context(), event.App, organizationID, input.TargetID,
		)
		if err != nil {
			return organizationSettingsError(event, err, "domain-verification")
		}
	}
	requestHash, err := organizationSettingsRequestHash(input)
	if err != nil {
		return organizationSettingsError(event, err, "mutation-hash")
	}
	release := acquireProductOperationLock(organizationSettingsMutationLockNamespace, organizationID)
	defer release()
	response := organizationSettingsMutationResponse{}
	var revocations []string
	change := func(txApp core.App) error {
		response, revocations = organizationSettingsMutationResponse{}, nil
		authorization, err := authorizeOrganization(txApp, user.Id, organizationID, requirement)
		if err != nil {
			return err
		}
		receipt, err := readOrganizationSettingsReceipt(txApp, organizationID, user.Id, input.RequestID)
		if err == nil {
			if receipt.RequestHash != requestHash {
				return errors.New(errorCodeOrganizationSettingsRequestIDReused)
			}
			if json.Unmarshal([]byte(receipt.ResponseJSON), &response) != nil {
				return errors.New(errorCodeOrganizationSettingsStateInvalid)
			}
			markOrganizationSettingsReceiptDuplicate(&response)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		currentRevision, err := readOrganizationSettingsRevision(txApp, organizationID)
		if err != nil {
			return err
		}
		if currentRevision != input.ExpectedRevision {
			return errors.New(errorCodeOrganizationSettingsConflict)
		}
		now := time.Now().UTC().UnixMilli()
		subject, err := readOrganizationSettingsAuditSubject(txApp, organizationID, input)
		if err != nil {
			return err
		}
		switch input.Action {
		case organizationSettingsActionWorkspaceCreationPolicy:
			_, err = mutateOrganizationSettingsPolicy(txApp, organizationID, input)
		case organizationSettingsActionFeedRetention, organizationSettingsActionAnalyticsRetention:
			err = mutateOrganizationActivityRetention(txApp, organizationID, user.Id, input, now)
		case organizationSettingsActionGroupCreate,
			organizationSettingsActionGroupUpdate,
			organizationSettingsActionGroupDelete,
			organizationSettingsActionGroupMemberAdd,
			organizationSettingsActionGroupMemberRemove:
			response.Group, response.DeletedID, err = mutateManualOrganizationGroup(
				txApp, organizationID, user.Id, input, now,
			)
		case organizationSettingsActionDomainAdd,
			organizationSettingsActionDomainVerify,
			organizationSettingsActionDomainRemove:
			response.Domain, response.DeletedID, err = mutateOrganizationDomain(
				txApp, organizationID, input, check, now,
			)
		case organizationSettingsActionMemberDeactivate,
			organizationSettingsActionMemberReactivate,
			organizationSettingsActionMemberRemove,
			organizationSettingsActionMemberRole,
			organizationSettingsActionOwnerTransfer,
			organizationSettingsActionAdminRoleAdd,
			organizationSettingsActionAdminRoleRemove:
			response.Members, response.DeletedID, revocations, err = mutateOrganizationMember(txApp, authorization, input)
		default:
			err = errOrganizationSettingsInvalid
		}
		if err != nil {
			return err
		}
		nextRevision, err := readOrganizationSettingsRevision(txApp, organizationID)
		if err != nil {
			return err
		}
		if nextRevision == currentRevision {
			nextRevision, err = bumpOrganizationSettingsRevision(txApp, organizationID, currentRevision)
		}
		if err != nil || nextRevision <= currentRevision {
			return errors.Join(errors.New(errorCodeOrganizationSettingsStateInvalid), err)
		}
		refreshed, err := authorizeOrganization(txApp, user.Id, organizationID, permissionOrganizationAdmin)
		if err != nil {
			return err
		}
		summary, err := readOrganizationSettingsSummary(txApp, refreshed)
		if err != nil {
			return err
		}
		response.Organization = &summary
		response.Receipt = organizationSettingsMutationReceipt{RequestID: input.RequestID, Revision: nextRevision}
		target, metadata := subject.line(input, response)
		metadata["requestId"], metadata["revision"] = input.RequestID, nextRevision
		if err := recordOrganizationAuditEvent(txApp, organizationAuditEvent{
			ID:             organizationAuditID(organizationID, "organization_settings.mutate", user.Id+"\x00"+input.RequestID),
			OrganizationID: organizationID, ActorID: user.Id,
			Action: "organization_settings." + input.Action, TargetType: "organization_settings",
			TargetID: target, Metadata: metadata, OccurredAt: now,
		}); err != nil {
			return err
		}
		return storeOrganizationSettingsReceipt(
			txApp, organizationID, user.Id, input.RequestID, requestHash, nextRevision, response, now,
		)
	}
	// A key's call to delete a group, remove a domain or a member, pass the
	// organization on or shorten what it keeps asks its person only for a
	// change that would be made (user_key_acting_ask.go).
	err = rehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := askBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return organizationSettingsError(event, err, "mutation")
	}
	// The workspaces a guest left drop their live rooms now, as a member's
	// removal does; a job that can't finish now stays queued for the runner.
	runner := workspaceMembershipRevocationRunner(event.App, nil)
	for _, jobID := range revocations {
		completed, err := runner.advanceWorkspaceMembershipRevocation(event.Request.Context(), jobID)
		if err != nil || !completed {
			runner.notify()
		}
	}
	return event.JSON(http.StatusOK, response)
}

func normalizeOrganizationSettingsInput(input *organizationSettingsMutationInput) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Action = strings.TrimSpace(input.Action)
	input.TargetID = strings.TrimSpace(input.TargetID)
	input.SecondaryTargetID = strings.TrimSpace(input.SecondaryTargetID)
	input.Value = strings.TrimSpace(input.Value)
}

func validateOrganizationSettingsInput(
	organizationID string, input organizationSettingsMutationInput,
) (organizationPermissionRequirement, error) {
	requirement, err := organizationSettingsRequirement(input.Action)
	switch {
	case err != nil:
		return permissionOrganizationActive, organizationSettingsRefused("action %q isn't an organization settings change", input.Action)
	case organizationID == "" || utf8.RuneCountInString(organizationID) > organizationIdentifierMaxRunes:
		return permissionOrganizationActive, errOrganizationSettingsInvalid
	case input.RequestID == "" || utf8.RuneCountInString(input.RequestID) > organizationSettingsRequestIDRunes:
		return permissionOrganizationActive, organizationSettingsRefused(
			"requestId is a new id of yours, 1–%d characters, so the same change sent again isn't made twice", organizationSettingsRequestIDRunes)
	case input.ExpectedRevision < 0:
		return permissionOrganizationActive, organizationSettingsRefused("expectedRevision is the settings' revision as the settings snapshot reads it")
	}
	for _, field := range []struct{ name, value string }{
		{"targetId", input.TargetID}, {"secondaryTargetId", input.SecondaryTargetID}, {"value", input.Value},
	} {
		if count := utf8.RuneCountInString(field.value); count > organizationSettingsMutationValueRunes {
			return permissionOrganizationActive, organizationSettingsRefused("%s is %d characters, over %d",
				field.name, count, organizationSettingsMutationValueRunes)
		}
	}
	return requirement, nil
}

func mutateOrganizationSettingsPolicy(
	app core.App, organizationID string, input organizationSettingsMutationInput,
) (bool, error) {
	column, allowed := "", map[string]bool{}
	switch input.Action {
	case organizationSettingsActionWorkspaceCreationPolicy:
		// The organization module keeps workspace creation to owners and admins.
		if organizationPolicy.WorkspaceCreation == nil {
			return false, errOrganizationSettingsInvalid
		}
		column, allowed = "workspace_creation_policy", map[string]bool{"owners_admins": true, "members": true}
	default:
		return false, errOrganizationSettingsInvalid
	}
	if !allowed[input.Value] {
		return false, errOrganizationSettingsInvalid
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_organization_policy_state}} SET [[` + column + `]]={:value}
		WHERE [[organization_id]]={:organizationId}`).Bind(dbx.Params{
		"value": input.Value, "organizationId": organizationID,
	}).Execute()
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

func organizationSettingsMutationTarget(input organizationSettingsMutationInput) string {
	if input.TargetID != "" {
		return input.TargetID
	}
	if input.Value != "" {
		return input.Value
	}
	return input.Action
}
