package organization

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The Identity item's managed user policies, as Notion's (its help, read
// 2026-10-02; the rows seen behind its upgrade): whether managed users may
// change their name and profile photo ("관리 대상 사용자의 프로필 편집
// 허용"), whether they may join workspaces their organization doesn't own
// ("외부 워크스페이스 액세스"), and how long they stay signed in before
// signing in again ("세션 기간", 1 hour to 180 days, 180 by default). A
// person on domains two organizations verified follows the stricter of
// them. Notion's support access row is its own staff's.

const (
	organizationManagedUserPoliciesPath  = "/organizations/{organizationId}/managed-user-policies"
	accountOrganizationPoliciesPath      = "/account/organization-policies"
	managedUserSessionDefaultHours       = 180 * 24
	errorCodeManagedUserPoliciesInvalid  = "LEAFEN_MANAGED_USER_POLICIES_INVALID"
	managedUserDomainOfUserSQL           = `lower(substr([[user]].[[email]], instr([[user]].[[email]], '@') + 1))`
	managedUserVerifiedDomainForEmailSQL = `[[domain]].[[status]] = 'verified' AND lower([[domain]].[[domain]]) = {:domain}`
)

type organizationManagedUserPolicies struct {
	SecondaryEmails      string `json:"secondaryEmails" db:"secondary_emails"`
	ProfileChanges       bool   `json:"profileChanges" db:"profile_changes"`
	ExternalWorkspaces   bool   `json:"externalWorkspaces" db:"external_workspaces"`
	SessionDurationHours int    `json:"sessionDurationHours" db:"session_duration_hours"`
}

type organizationManagedUserPolicyChange struct {
	SecondaryEmails      *string `json:"secondaryEmails"`
	ProfileChanges       *bool   `json:"profileChanges"`
	ExternalWorkspaces   *bool   `json:"externalWorkspaces"`
	SessionDurationHours *int    `json:"sessionDurationHours"`
}

// loadManagedUserRules reads the strictest rules of the organizations that
// verified a person's email domain; someone on no verified domain has none.
func managedAccountRules(app core.App, user *core.Record) (leafen.ManagedUserRules, error) {
	rules := leafen.ManagedUserRules{ProfileChanges: true}
	err := app.DB().NewQuery(`
		SELECT COUNT(*) > 0 AS [[managed]],
			COALESCE(MIN(COALESCE([[policy]].[[profile_changes]], 1)), 1) AS [[profile_changes]],
			COALESCE(MIN(COALESCE([[policy]].[[session_duration_hours]], {:defaultHours})), 0) AS [[session_hours]]
		FROM {{leafen_organization_domains}} [[domain]]
		LEFT JOIN {{leafen_organization_managed_user_policies}} [[policy]]
			ON [[policy]].[[organization_id]] = [[domain]].[[organization_id]]
		WHERE [[domain]].[[status]] = 'verified' AND [[domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("{:userId}") + `)`,
	).Bind(dbx.Params{"userId": user.Id, "defaultHours": managedUserSessionDefaultHours}).One(&rules)
	return rules, err
}

// requireExternalWorkspaceJoin refuses a person a workspace of another
// organization when an organization managing them keeps them to its own.
func externalWorkspaceJoin(app core.App, userID, workspaceOrganizationID string) error {
	user, err := app.FindRecordById("users", userID)
	if err != nil {
		return err
	}
	var blocked int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_organization_domains}} [[domain]]
		JOIN {{leafen_organization_managed_user_policies}} [[policy]]
			ON [[policy]].[[organization_id]] = [[domain]].[[organization_id]]
		WHERE [[domain]].[[status]] = 'verified' AND [[domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("{:userId}") + `)
			AND [[policy]].[[external_workspaces]] = 0 AND [[domain]].[[organization_id]] <> {:organizationId}
	`).Bind(dbx.Params{"userId": user.Id, "organizationId": workspaceOrganizationID}).Row(&blocked); err != nil {
		return err
	}
	if blocked > 0 {
		return leafen.ErrExternalWorkspacesDisabled
	}
	return nil
}

// applyManagedUserSessionDeadlines brings the open sign-ins of the people on
// an organization's verified domains to their current session duration: a
// shorter one ends them sooner, a longer one lets renewals go further.
func applyManagedUserSessionDeadlines(app core.App, organizationID string, now int64) error {
	_, err := app.DB().NewQuery(`
		WITH [[managed]] AS (
			SELECT [[user]].[[id]] AS [[user_id]],
				(SELECT MIN(COALESCE([[policy]].[[session_duration_hours]], {:defaultHours}))
					FROM {{leafen_organization_domains}} [[any_domain]]
					LEFT JOIN {{leafen_organization_managed_user_policies}} [[policy]]
						ON [[policy]].[[organization_id]] = [[any_domain]].[[organization_id]]
					WHERE [[any_domain]].[[status]] = 'verified'
						AND [[any_domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("[[user]].[[id]]") + `)) AS [[hours]]
			FROM {{users}} [[user]]
			WHERE EXISTS (SELECT 1 FROM {{leafen_organization_domains}} [[domain]]
				WHERE [[domain]].[[organization_id]] = {:organizationId} AND [[domain]].[[status]] = 'verified'
					AND [[domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("[[user]].[[id]]") + `))
		)
		UPDATE {{leafen_auth_sessions}} SET
			[[deadline_at]] = [[created_at]] + [[managed]].[[hours]] * {:hourMillis},
			[[expires_at]] = MIN([[expires_at]], [[created_at]] + [[managed]].[[hours]] * {:hourMillis}),
			[[updated_at]] = {:now}
		FROM [[managed]]
		WHERE {{leafen_auth_sessions}}.[[owner_id]] = [[managed]].[[user_id]]
			AND {{leafen_auth_sessions}}.[[revoked_at]] = 0 AND {{leafen_auth_sessions}}.[[expires_at]] > {:now}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "defaultHours": managedUserSessionDefaultHours,
		"hourMillis": leafen.ManagedUserPolicyHoursMillis, "now": now,
	}).Execute()
	if err == nil {
		leafen.RecheckCollaborationAccessAfterCommit(app)
	}
	return err
}

func registerManagedUserPolicyRoutes(event *core.ServeEvent) {
	event.Router.GET(leafen.APIRoot+organizationManagedUserPoliciesPath, readOrganizationManagedUserPolicies)
	event.Router.PATCH(leafen.APIRoot+organizationManagedUserPoliciesPath, changeOrganizationManagedUserPolicies)
	event.Router.GET(leafen.APIRoot+accountOrganizationPoliciesPath, readAccountOrganizationPolicies)
}

func loadOrganizationManagedUserPolicies(app core.App, organizationID string) (organizationManagedUserPolicies, error) {
	policies := organizationManagedUserPolicies{
		SecondaryEmails: "disabled", ProfileChanges: true, ExternalWorkspaces: true, SessionDurationHours: managedUserSessionDefaultHours,
	}
	err := app.DB().NewQuery(`
		SELECT [[secondary_emails]], [[profile_changes]], [[external_workspaces]], [[session_duration_hours]]
		FROM {{leafen_organization_managed_user_policies}} WHERE [[organization_id]] = {:organizationId}
	`).Bind(dbx.Params{"organizationId": organizationID}).One(&policies)
	if errors.Is(err, sql.ErrNoRows) {
		return policies, nil
	}
	return policies, err
}

func readOrganizationManagedUserPolicies(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	policies, err := loadOrganizationManagedUserPolicies(event.App, authorization.OrganizationID)
	if err != nil {
		return managedUsersError(event, err, "policies-read")
	}
	return event.JSON(http.StatusOK, policies)
}

func changeOrganizationManagedUserPolicies(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	input := organizationManagedUserPolicyChange{}
	if err := event.BindBody(&input); err != nil ||
		(input.SecondaryEmails == nil && input.ProfileChanges == nil && input.ExternalWorkspaces == nil && input.SessionDurationHours == nil) ||
		(input.SessionDurationHours != nil &&
			(*input.SessionDurationHours < 1 || *input.SessionDurationHours > managedUserSessionDefaultHours)) {
		return leafen.APIError(event, http.StatusBadRequest, errorCodeManagedUserPoliciesInvalid, "Check the managed user policies.")
	}
	if input.SecondaryEmails != nil && *input.SecondaryEmails != "disabled" && *input.SecondaryEmails != "verified_domains" && *input.SecondaryEmails != "any" {
		return leafen.APIError(event, http.StatusBadRequest, errorCodeManagedUserPoliciesInvalid, "Check the secondary email policy.")
	}
	var policies organizationManagedUserPolicies
	change := func(txApp core.App) error {
		current, err := loadOrganizationManagedUserPolicies(txApp, authorization.OrganizationID)
		if err != nil {
			return err
		}
		next := current
		if input.SecondaryEmails != nil {
			next.SecondaryEmails = *input.SecondaryEmails
		}
		if input.ProfileChanges != nil {
			next.ProfileChanges = *input.ProfileChanges
		}
		if input.ExternalWorkspaces != nil {
			next.ExternalWorkspaces = *input.ExternalWorkspaces
		}
		if input.SessionDurationHours != nil {
			next.SessionDurationHours = *input.SessionDurationHours
		}
		policies = next
		if next == current {
			return nil
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_organization_managed_user_policies}}
				([[organization_id]], [[secondary_emails]], [[profile_changes]], [[external_workspaces]], [[session_duration_hours]], [[updated_by]], [[updated_at]])
			VALUES ({:organizationId}, {:secondaryEmails}, {:profileChanges}, {:externalWorkspaces}, {:hours}, {:actorId}, {:now})
			ON CONFLICT ([[organization_id]]) DO UPDATE SET
				[[secondary_emails]] = excluded.[[secondary_emails]], [[profile_changes]] = excluded.[[profile_changes]], [[external_workspaces]] = excluded.[[external_workspaces]],
				[[session_duration_hours]] = excluded.[[session_duration_hours]],
				[[updated_by]] = excluded.[[updated_by]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "secondaryEmails": next.SecondaryEmails, "profileChanges": next.ProfileChanges,
			"externalWorkspaces": next.ExternalWorkspaces, "hours": next.SessionDurationHours,
			"actorId": authorization.PrincipalID, "now": now,
		}).Execute(); err != nil {
			return err
		}
		if next.SessionDurationHours != current.SessionDurationHours {
			if err := applyManagedUserSessionDeadlines(txApp, authorization.OrganizationID, now); err != nil {
				return err
			}
		}
		return leafen.RecordOrganizationAuditEvent(txApp, leafen.OrganizationAuditEvent{
			ID: leafen.OrganizationAuditID(authorization.OrganizationID, "managed_user.policies",
				authorization.OrganizationID+"@"+strconv.FormatInt(now, 10)),
			OrganizationID: authorization.OrganizationID, ActorID: authorization.PrincipalID,
			Action: "managed_user.policies", TargetType: "organization", TargetID: authorization.OrganizationID,
			Metadata: map[string]any{
				"secondaryEmails": next.SecondaryEmails, "profileChanges": next.ProfileChanges, "externalWorkspaces": next.ExternalWorkspaces,
				"sessionDurationHours": next.SessionDurationHours,
			},
			OccurredAt: now,
		})
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
		return managedUsersError(event, err, "policies-change")
	}
	return event.JSON(http.StatusOK, policies)
}

// readAccountOrganizationPolicies tells the app what the person's
// organizations ask of them, to show their profile as theirs to change or not.
func readAccountOrganizationPolicies(event *core.RequestEvent) error {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		return leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
	}
	rules, err := managedAccountRules(event.App, user)
	if err != nil {
		return leafen.APIErrorWithCause(event, http.StatusInternalServerError, errorCodeManagedUserPoliciesInvalid,
			"Couldn't read the organization's policies.", "account_organization_policies", "read",
			errorCodeManagedUserPoliciesInvalid, err)
	}
	return event.JSON(http.StatusOK, map[string]bool{"managed": rules.Managed, "profileChanges": !rules.Managed || rules.ProfileChanges})
}
