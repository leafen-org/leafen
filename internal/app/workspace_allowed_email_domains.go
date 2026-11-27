package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's "허용된 이메일 도메인" (설정 → 일반, seen 2026-10-03, and its
// help): a workspace's owners name email domains, only ones its members'
// emails use, and anyone whose email is at one of them joins the workspace
// as a member without an invitation, from "워크스페이스 생성 또는 참여". An
// email counts only where an address can't be claimed unproven: the account's
// email is confirmed and the server either asks new accounts to confirm their
// email or lets no one sign up on their own.

const (
	workspaceAllowedEmailDomainsPath   = "/workspaces/{workspaceId}/allowed-email-domains"
	workspaceAllowedEmailDomainPath    = "/workspaces/{workspaceId}/allowed-email-domains/{domain}"
	workspaceJoinPath                  = "/workspaces/{workspaceId}/join"
	joinableWorkspacesPath             = "/account/joinable-workspaces"
	errorCodeAllowedEmailDomainInvalid = "LEAFEN_ALLOWED_EMAIL_DOMAIN_INVALID"
	errorCodeAllowedEmailDomainUnused  = "LEAFEN_ALLOWED_EMAIL_DOMAIN_UNUSED"
	joinableWorkspacesWindow           = 100
)

var (
	errAllowedEmailDomainUnused = errors.New(errorCodeAllowedEmailDomainUnused)
	errWorkspaceNotJoinable     = errors.New("workspace isn't open to the person's email domain")
)

type workspaceAllowedEmailDomain struct {
	Domain    string `json:"domain" db:"domain"`
	CreatedAt int64  `json:"createdAt" db:"created_at"`
}

type workspaceAllowedEmailDomainsResponse struct {
	Domains []workspaceAllowedEmailDomain `json:"domains"`
	// People may join by these domains on this server.
	JoinsAvailable bool `json:"joinsAvailable"`
}

type joinableWorkspace struct {
	ID      string `json:"id" db:"id"`
	Name    string `json:"name" db:"name"`
	Icon    string `json:"icon" db:"icon"`
	Members int    `json:"members" db:"members"`
}

func registerWorkspaceAllowedEmailDomainRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspaceAllowedEmailDomainsPath, readWorkspaceAllowedEmailDomains)
	event.Router.PUT(apiRoot+workspaceAllowedEmailDomainPath, addWorkspaceAllowedEmailDomain)
	event.Router.DELETE(apiRoot+workspaceAllowedEmailDomainPath, removeWorkspaceAllowedEmailDomain)
	event.Router.GET(apiRoot+joinableWorkspacesPath, readJoinableWorkspaces)
	event.Router.POST(apiRoot+workspaceJoinPath, joinWorkspaceByEmailDomain)
}

// emailJoinsAvailable says whether a person's email may join workspaces by its
// domain on this server, and which domain.
func emailJoinsAvailable(app core.App, user *core.Record) ([]string, bool, error) {
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return nil, false, err
	}
	if !serverProvesEmails(settings) {
		return nil, false, nil
	}
	addresses, err := verifiedAccountEmails(app, user.Id)
	if err != nil {
		return nil, false, err
	}
	domains := []string{}
	seen := map[string]bool{}
	for _, address := range addresses {
		domain := emailDomain(address)
		if domain != "" && !seen[domain] {
			domains = append(domains, domain)
			seen[domain] = true
		}
	}
	return domains, len(domains) > 0, nil
}

// serverProvesEmails says whether every new account's email is proven: the
// server asks for confirmation, or only its administrator, SCIM or the
// app's own owner makes accounts.
func serverProvesEmails(settings storedAdministratorSettings) bool {
	return settings.RequireEmailVerification || settings.SignupPolicy == administratorSignupPolicyClosed
}

func loadWorkspaceAllowedEmailDomains(app core.App, workspaceID string) (workspaceAllowedEmailDomainsResponse, error) {
	response := workspaceAllowedEmailDomainsResponse{Domains: []workspaceAllowedEmailDomain{}}
	if err := app.DB().NewQuery(`
		SELECT [[domain]], [[created_at]] FROM {{leafen_workspace_allowed_email_domains}}
		WHERE [[workspace_id]] = {:workspaceId} ORDER BY [[domain]]
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&response.Domains); err != nil {
		return response, err
	}
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return response, err
	}
	response.JoinsAvailable = serverProvesEmails(settings)
	return response, nil
}

func authorizeAllowedEmailDomains(event *core.RequestEvent) (workspaceAuthorization, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return workspaceAuthorization{}, false
	}
	authorization, err := authorizeWorkspace(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), permissionWorkspaceOwner,
	)
	if err != nil {
		if isPermissionDenied(err) {
			_ = apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		} else {
			_ = apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
				"Couldn't read the workspace's email domains.", "allowed_email_domains", "authorize", errorCodeWorkspaceNotFound, err)
		}
		return workspaceAuthorization{}, false
	}
	return authorization, true
}

func readWorkspaceAllowedEmailDomains(event *core.RequestEvent) error {
	authorization, ok := authorizeAllowedEmailDomains(event)
	if !ok {
		return nil
	}
	return respondWorkspaceAllowedEmailDomains(event, authorization.WorkspaceID)
}

func respondWorkspaceAllowedEmailDomains(event *core.RequestEvent, workspaceID string) error {
	response, err := loadWorkspaceAllowedEmailDomains(event.App, workspaceID)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's email domains.", "allowed_email_domains", "read", errorCodeWorkspaceNotFound, err)
	}
	return event.JSON(http.StatusOK, response)
}

// addWorkspaceAllowedEmailDomain names a domain that one of the workspace's
// owner and members uses; someone with another domain joins first.
func addWorkspaceAllowedEmailDomain(event *core.RequestEvent) error {
	authorization, ok := authorizeAllowedEmailDomains(event)
	if !ok {
		return nil
	}
	domain, err := normalizeOrganizationDomain(event.Request.PathValue("domain"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAllowedEmailDomainInvalid, "Enter an email domain such as example.com.")
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		used, err := workspaceMembersUseDomain(txApp, authorization, domain)
		if err != nil {
			return err
		}
		if !used {
			return errAllowedEmailDomainUnused
		}
		now := time.Now().UTC().UnixMilli()
		result, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_allowed_email_domains}} ([[workspace_id]], [[domain]], [[created_at]])
			VALUES ({:workspaceId}, {:domain}, {:now}) ON CONFLICT DO NOTHING
		`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "domain": domain, "now": now}).Execute()
		if err != nil {
			return err
		}
		if added, _ := result.RowsAffected(); added == 0 {
			return nil
		}
		return recordAllowedEmailDomainAudit(txApp, authorization, "workspace.allowed_email_domain.add", domain, now)
	})
	if errors.Is(err, errAllowedEmailDomainUnused) {
		return apiError(event, http.StatusConflict, errorCodeAllowedEmailDomainUnused,
			"Only a domain the workspace's members use can be added; someone at that domain joins first.")
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't add the email domain.", "allowed_email_domains", "add", errorCodeWorkspaceNotFound, err)
	}
	return respondWorkspaceAllowedEmailDomains(event, authorization.WorkspaceID)
}

func removeWorkspaceAllowedEmailDomain(event *core.RequestEvent) error {
	authorization, ok := authorizeAllowedEmailDomains(event)
	if !ok {
		return nil
	}
	domain, err := normalizeOrganizationDomain(event.Request.PathValue("domain"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAllowedEmailDomainInvalid, "Enter an email domain such as example.com.")
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		result, err := txApp.DB().NewQuery(`
			DELETE FROM {{leafen_workspace_allowed_email_domains}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[domain]] = {:domain}
		`).Bind(dbx.Params{"workspaceId": authorization.WorkspaceID, "domain": domain}).Execute()
		if err != nil {
			return err
		}
		// A domain the workspace doesn't allow is refused, never answered as
		// removed: a mistyped one would leave the meant one allowed (2026-11-26).
		if removed, _ := result.RowsAffected(); removed == 0 {
			return refusedFor(http.StatusNotFound, errorCodeAllowedEmailDomainNotFound, "Check domain: "+domain+" isn't one of this "+
				"workspace's allowed email domains. GET /workspaces/{workspaceId}/allowed-email-domains lists them.")
		}
		return recordAllowedEmailDomainAudit(
			txApp, authorization, "workspace.allowed_email_domain.remove", domain, time.Now().UTC().UnixMilli(),
		)
	})
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't remove the email domain.", "allowed_email_domains", "remove", errorCodeWorkspaceNotFound, err)
	}
	return respondWorkspaceAllowedEmailDomains(event, authorization.WorkspaceID)
}

func recordAllowedEmailDomainAudit(app core.App, authorization workspaceAuthorization, action, domain string, now int64) error {
	// Each change is its own event; the default identity keeps one per
	// workspace and action.
	return recordOrganizationAuditEvent(app, organizationAuditEvent{
		ID:             core.GenerateDefaultRandomId(),
		OrganizationID: authorization.Organization.OrganizationID, WorkspaceID: authorization.WorkspaceID,
		ActorID: authorization.PrincipalID, Action: action, TargetType: "workspace", TargetID: authorization.WorkspaceID,
		Metadata: map[string]any{"domain": domain}, OccurredAt: now,
	})
}

// workspaceMembersUseDomain says whether the workspace's owner or one of its
// members has an email at the domain.
func workspaceMembersUseDomain(app core.App, authorization workspaceAuthorization, domain string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_account_emails}} WHERE ` + provenAccountEmailSQL("") + `
		AND lower(substr([[email]], instr([[email]], '@') + 1)) = {:domain}
		AND ([[user_id]] = {:ownerId} OR [[user_id]] IN (SELECT [[user_id]] FROM {{leafen_workspace_memberships}}
			WHERE [[workspace_id]] = {:workspaceId} AND [[role]] = 'member'))`).Bind(dbx.Params{
		"domain": domain, "ownerId": authorization.WorkspaceOwnerID, "workspaceId": authorization.WorkspaceID,
	}).Row(&count)
	return count > 0, err
}

// readJoinableWorkspaces lists the workspaces the person may join by their
// email's domain, which aren't already theirs.
func readJoinableWorkspaces(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	after, err := decodeTextIDCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAllowedEmailDomainInvalid, "Give the nextCursor of the last list, or none to start.")
	}
	workspaces, next, err := loadJoinableWorkspaces(event.App, user, "", after)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspaces to join.", "joinable_workspaces", "read", errorCodeWorkspaceNotFound, err)
	}
	response := map[string]any{"workspaces": workspaces}
	if next != "" {
		response["nextCursor"] = next
	}
	// Someone with no workspace is offered one to make only where they may.
	if errors.Is(authorizeDomainWorkspaceCreation(event.App, user), errWorkspaceCreationRestricted) {
		response["creationRestricted"] = true
	}
	return event.JSON(http.StatusOK, response)
}

// loadJoinableWorkspaces is a window of the workspaces the person may join,
// by name from the cursor on; it looks at a bounded batch each time, and next
// goes on from the last one looked at ("" once all are).
func loadJoinableWorkspaces(app core.App, user *core.Record, workspaceID string, after textIDCursor) ([]joinableWorkspace, string, error) {
	workspaces := []joinableWorkspace{}
	_, available, err := emailJoinsAvailable(app, user)
	if err != nil || !available {
		return workspaces, "", err
	}
	candidates := []struct {
		joinableWorkspace
		OrganizationID string `db:"organization"`
		SortName       string `db:"sort_name"`
	}{}
	scan := joinableWorkspacesWindow * 4
	if err := app.DB().NewQuery(`
		SELECT DISTINCT [[workspace]].[[id]], [[workspace]].[[name]], [[workspace]].[[icon]], [[workspace]].[[organization]],
			lower([[workspace]].[[name]]) AS [[sort_name]],
			` + workspaceMemberCountSQL("[[workspace]].[[id]]") + ` AS [[members]]
		FROM {{leafen_workspace_allowed_email_domains}} [[allowed]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[allowed]].[[workspace_id]]
		WHERE [[allowed]].[[domain]] IN (SELECT lower(substr([[email]], instr([[email]], '@') + 1)) FROM {{leafen_account_emails}} WHERE [[user_id]] = {:userId} AND ` + provenAccountEmailSQL("") + `) AND [[workspace]].[[deletion_job_id]] = ''
			AND ({:workspaceId} = '' OR [[workspace]].[[id]] = {:workspaceId})
			AND [[workspace]].[[workspace_owner]] != {:userId}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[mine]]
				WHERE [[mine]].[[workspace_id]] = [[workspace]].[[id]] AND [[mine]].[[user_id]] = {:userId}
					AND [[mine]].[[role]] = 'member')
			AND ({:afterId} = '' OR lower([[workspace]].[[name]]) > {:afterName}
				OR (lower([[workspace]].[[name]]) = {:afterName} AND [[workspace]].[[id]] > {:afterId}))
		ORDER BY lower([[workspace]].[[name]]), [[workspace]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "userId": user.Id, "limit": scan + 1, "afterName": after.At, "afterId": after.ID,
	}).All(&candidates); err != nil {
		return nil, "", err
	}
	more := len(candidates) > scan
	if more {
		candidates = candidates[:scan]
	}
	for index, candidate := range candidates {
		// An organization may keep the people it manages to its own workspaces.
		if err := requireExternalWorkspaceJoin(app, user.Id, candidate.OrganizationID); errors.Is(err, errExternalWorkspacesDisabled) {
			continue
		} else if err != nil {
			return nil, "", err
		}
		workspaces = append(workspaces, candidate.joinableWorkspace)
		if len(workspaces) == joinableWorkspacesWindow && (index+1 < len(candidates) || more) {
			return workspaces, encodeTextIDCursor(candidate.SortName, candidate.ID), nil
		}
	}
	if more {
		last := candidates[len(candidates)-1]
		return workspaces, encodeTextIDCursor(last.SortName, last.ID), nil
	}
	return workspaces, "", nil
}

// joinWorkspaceByEmailDomain makes the person a member of a workspace open to
// their email's domain, as one its owner added.
func joinWorkspaceByEmailDomain(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	err := event.App.RunInTransaction(func(txApp core.App) error {
		joinable, _, err := loadJoinableWorkspaces(txApp, user, workspaceID, textIDCursor{})
		if err != nil {
			return err
		}
		if len(joinable) == 0 {
			return errWorkspaceNotJoinable
		}
		workspace, err := txApp.FindRecordById(workspacesCollection, workspaceID)
		if err != nil {
			return err
		}
		allowed, err := administratorAccountCanAuthenticate(txApp, user.Id)
		if err != nil {
			return err
		}
		if !allowed {
			return errWorkspaceNotJoinable
		}
		if policy, err := loadWorkspaceMemberAddPolicy(txApp); err != nil {
			return err
		} else if policy != administratorMemberAddPolicyEnabled {
			return errWorkspaceMemberAddDisabled
		}
		organizationID := workspace.GetString("organization")
		if err := admitOrganizationWorkspaceMember(txApp, organizationID, user.Id, user.Id); errors.Is(err, errOrganizationMemberInactive) {
			return errWorkspaceNotJoinable
		} else if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		if err := joinWorkspaceAsMember(txApp, workspaceID, user.Id, user.Id, now); err != nil {
			return err
		}
		if _, err := advanceWorkspacePermissionCursor(txApp, workspaceID, now); err != nil {
			return err
		}
		domains, _, _ := emailJoinsAvailable(txApp, user)
		return recordOrganizationAuditEvent(txApp, organizationAuditEvent{
			ID:             core.GenerateDefaultRandomId(),
			OrganizationID: organizationID, WorkspaceID: workspaceID, ActorID: user.Id,
			Action: "workspace_member.join", TargetType: "workspace_member", TargetID: user.Id,
			Metadata:   map[string]any{"role": workspaceRoleMember, "allowedEmailDomains": domains},
			OccurredAt: now,
		})
	})
	switch {
	case errors.Is(err, errWorkspaceNotJoinable), errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	case errors.Is(err, errWorkspaceMemberAddDisabled):
		return apiError(event, http.StatusForbidden, errorCodeWorkspaceMemberAddDisabled, "Server policy blocked adding workspace members.")
	case err != nil:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't join the workspace.", "joinable_workspaces", "join", errorCodeWorkspaceNotFound, err)
	}
	return event.JSON(http.StatusOK, map[string]string{"workspaceId": workspaceID, "role": workspaceRoleMember})
}
