package organization

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// An organization's owner may ask its members to sign in with a password and
// 2-step verification rather than by a registered device (the multi-server
// decision of 2026-10-20: the organization's policy comes first). Its members
// then can't join a device with a code, and the devices they joined so are
// signed out when the owner turns it on. Owners and guests are not asked, as
// single sign-on's enforcement leaves them, so no owner is locked out. Single
// sign-on only is the SAML provider's own enforcement.

const (
	organizationSignInPolicyPath         = "/organizations/{organizationId}/sign-in-policy"
	organizationSignInPolicyMaxBodyBytes = 1 << 10
	organizationSignInPasswordTwoStep    = "password_two_step"
)

// organizationSignInPolicy is whether the organization asks for a password
// and 2-step verification.
type organizationSignInPolicy struct {
	PasswordTwoStep bool `json:"passwordTwoStep"`
}

func registerOrganizationSignInPolicyRoutes(event *core.ServeEvent) {
	event.Router.GET(leafen.APIRoot+organizationSignInPolicyPath, readOrganizationSignInPolicy)
	event.Router.PUT(leafen.APIRoot+organizationSignInPolicyPath, updateOrganizationSignInPolicy).
		Bind(apis.BodyLimit(organizationSignInPolicyMaxBodyBytes))
}

func authorizeOrganizationSignInPolicy(event *core.RequestEvent) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	authorization, err := leafen.AuthorizeOrganization(event.App, user.Id, organizationID, leafen.PermissionOrganizationOwner)
	if err != nil {
		_ = organizationSignInPolicyError(event, err, "authorization")
		return leafen.OrganizationAuthorization{}, false
	}
	return authorization, true
}

func loadOrganizationSignInPolicy(app core.App, organizationID string) (organizationSignInPolicy, error) {
	count := 0
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_sign_in_policies}}
		WHERE [[organization_id]] = {:organizationId} AND [[policy]] = {:policy}`).
		Bind(dbx.Params{"organizationId": organizationID, "policy": organizationSignInPasswordTwoStep}).Row(&count)
	return organizationSignInPolicy{PasswordTwoStep: count > 0}, err
}

func readOrganizationSignInPolicy(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationSignInPolicy(event)
	if !ok {
		return nil
	}
	policy, err := loadOrganizationSignInPolicy(event.App, authorization.OrganizationID)
	if err != nil {
		return organizationSignInPolicyError(event, err, "read")
	}
	return event.JSON(http.StatusOK, policy)
}

// updateOrganizationSignInPolicy turns the requirement on or off; turning it
// on signs out the devices its members joined by a code.
func updateOrganizationSignInPolicy(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationSignInPolicy(event)
	if !ok {
		return nil
	}
	input := organizationSignInPolicy{}
	if err := event.BindBody(&input); err != nil {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeOrganizationSettingsInvalid, "Check the login policy.")
	}
	now := time.Now().UTC().UnixMilli()
	change := func(txApp core.App) error {
		statement := `DELETE FROM {{leafen_organization_sign_in_policies}} WHERE [[organization_id]] = {:organizationId}`
		if input.PasswordTwoStep {
			statement = `INSERT INTO {{leafen_organization_sign_in_policies}}
				([[organization_id]], [[policy]], [[updated_by]], [[updated_at]])
				VALUES ({:organizationId}, {:policy}, {:updatedBy}, {:now})
				ON CONFLICT ([[organization_id]]) DO UPDATE SET [[policy]] = excluded.[[policy]],
					[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]`
		}
		if _, err := txApp.DB().NewQuery(statement).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "policy": organizationSignInPasswordTwoStep,
			"updatedBy": authorization.PrincipalID, "now": now,
		}).Execute(); err != nil {
			return err
		}
		if input.PasswordTwoStep {
			if _, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_auth_sessions}} SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
				WHERE [[revoked_at]] = 0 AND [[sign_in_method]] = 'device' AND [[owner_id]] IN (
					SELECT [[user]] FROM {{leafen_organization_memberships}}
					WHERE [[organization]] = {:organizationId} AND [[status]] = 'active'
						AND [[role]] NOT IN ('owner', 'guest'))
			`).Bind(dbx.Params{"now": now, "organizationId": authorization.OrganizationID}).Execute(); err != nil {
				return err
			}
		}
		state := "off"
		if input.PasswordTwoStep {
			state = "on"
		}
		return leafen.RecordOrganizationAudit(
			txApp, authorization.OrganizationID, authorization.PrincipalID,
			"organization.sign_in_policy.update", "organization", organizationSignInPasswordTwoStep+":"+state,
		)
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return organizationSignInPolicyError(event, err, "write")
	}
	if input.PasswordTwoStep {
		leafen.RecheckCollaborationAccessAfterCommit(event.App)
	}
	return event.JSON(http.StatusOK, input)
}

// deviceSignInAllowed refuses a registered device's sign-in to a member of
// an organization that asks for a password and 2-step verification.
func deviceSignInAllowed(app core.App, userID string) error {
	blocked := 0
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_memberships}} [[membership]]
		JOIN {{leafen_organization_sign_in_policies}} [[policy]] ON [[policy]].[[organization_id]] = [[membership]].[[organization]]
		WHERE [[membership]].[[user]] = {:userId} AND [[membership]].[[status]] = 'active'
			AND [[membership]].[[role]] NOT IN ('owner', 'guest') AND [[policy]].[[policy]] = {:policy}`).
		Bind(dbx.Params{"userId": userID, "policy": organizationSignInPasswordTwoStep}).Row(&blocked)
	if err != nil {
		return err
	}
	if blocked > 0 {
		return leafen.ErrDeviceSignInRestricted
	}
	return nil
}

func organizationSignInPolicyError(event *core.RequestEvent, err error, stage string) error {
	if leafen.IsPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		return leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
	}
	return leafen.APIErrorWithCause(
		event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationSettingsInvalid,
		"Couldn't process the login policy.", "organization_sign_in_policy", stage,
		leafen.ErrorCodeOrganizationSettingsInvalid, err,
	)
}
