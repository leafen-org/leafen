package organization

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's MFA enforcement (its help, read 2026-10-10): an organization's
// owner chooses which of its workspaces require their guests to use 2-step
// verification; a guest who hasn't sees a verification screen there and
// goes on once they have. Members are not asked. Leafen asks a guest to
// have 2-step verification turned on and every session they're signed in
// with to have gone through it — a 2-step or recovery code, a passkey, or
// the code that turned it on — as the session-based enforcement the owner
// chose (2026-10-10).

const (
	organizationGuestMFAPath         = "/organizations/{organizationId}/guest-mfa"
	organizationGuestMFAMaxBodyBytes = 4 << 10
	organizationGuestMFAWindow       = 500
)

type organizationGuestMFAWorkspace struct {
	ID       string `json:"id" db:"id"`
	Name     string `json:"name" db:"name"`
	Required bool   `json:"required" db:"required"`
}

// organizationGuestMFAResponse is a window of the organization's workspaces
// by name, how many of all of them require it, and where the next window
// starts while more follow.
type organizationGuestMFAResponse struct {
	Workspaces []organizationGuestMFAWorkspace `json:"workspaces"`
	Required   int                             `json:"required"`
	NextCursor string                          `json:"nextCursor,omitempty"`
}

// organizationGuestMFAInput turns the requirement on or off for one workspace.
type organizationGuestMFAInput struct {
	WorkspaceID string `json:"workspaceId"`
	Required    bool   `json:"required"`
}

var errOrganizationGuestMFAInvalid = errors.New(leafen.ErrorCodeOrganizationSettingsInvalid)

func registerOrganizationGuestMFARoutes(event *core.ServeEvent) {
	event.Router.GET(leafen.APIRoot+organizationGuestMFAPath, readOrganizationGuestMFA)
	event.Router.PUT(leafen.APIRoot+organizationGuestMFAPath, updateOrganizationGuestMFA).
		Bind(apis.BodyLimit(organizationGuestMFAMaxBodyBytes))
}

func authorizeOrganizationGuestMFA(event *core.RequestEvent) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	authorization, err := leafen.AuthorizeOrganization(event.App, user.Id, organizationID, leafen.PermissionOrganizationOwner)
	if err != nil {
		_ = organizationGuestMFAError(event, err, "authorization")
		return leafen.OrganizationAuthorization{}, false
	}
	return authorization, true
}

func readOrganizationGuestMFA(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationGuestMFA(event)
	if !ok {
		return nil
	}
	response, err := loadOrganizationGuestMFA(event.App, authorization.OrganizationID, event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return organizationGuestMFAError(event, err, "read")
	}
	return event.JSON(http.StatusOK, response)
}

// updateOrganizationGuestMFA turns the requirement on or off for one of the
// organization's workspaces.
func updateOrganizationGuestMFA(event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationGuestMFA(event)
	if !ok {
		return nil
	}
	input := organizationGuestMFAInput{}
	if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.WorkspaceID) == "" {
		return organizationGuestMFAError(event, errOrganizationGuestMFAInvalid, "bind")
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	now := time.Now().UTC().UnixMilli()
	response := organizationGuestMFAResponse{Workspaces: []organizationGuestMFAWorkspace{}}
	change := func(txApp core.App) error {
		workspace := organizationGuestMFAWorkspace{}
		if err := txApp.DB().NewQuery(`SELECT [[id]], [[name]] FROM {{leafen_workspaces}}
			WHERE [[id]] = {:workspaceId} AND [[organization]] = {:organizationId} AND [[deletion_job_id]] = ''`).
			Bind(dbx.Params{"workspaceId": workspaceID, "organizationId": authorization.OrganizationID}).One(&workspace); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errOrganizationGuestMFAInvalid
			}
			return err
		}
		statement := `DELETE FROM {{leafen_organization_guest_mfa_workspaces}}
			WHERE [[organization_id]] = {:organizationId} AND [[workspace_id]] = {:workspaceId}`
		if input.Required {
			statement = `INSERT INTO {{leafen_organization_guest_mfa_workspaces}}
				([[organization_id]], [[workspace_id]], [[enabled_by]], [[created_at]])
				VALUES ({:organizationId}, {:workspaceId}, {:enabledBy}, {:now})
				ON CONFLICT DO NOTHING`
		}
		if _, err := txApp.DB().NewQuery(statement).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "workspaceId": workspaceID,
			"enabledBy": authorization.PrincipalID, "now": now,
		}).Execute(); err != nil {
			return err
		}
		state := "off"
		if input.Required {
			state = "on"
		}
		if err := leafen.RecordOrganizationAudit(
			txApp, authorization.OrganizationID, authorization.PrincipalID,
			"organization.guest_mfa.update", "workspace", fmt.Sprintf("%s:%s", workspaceID, state),
		); err != nil {
			return err
		}
		workspace.Required = input.Required
		response.Workspaces = append(response.Workspaces, workspace)
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_guest_mfa_workspaces}} [[mfa]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[mfa]].[[workspace_id]]
				AND [[workspace]].[[organization]] = [[mfa]].[[organization_id]] AND [[workspace]].[[deletion_job_id]] = ''
			WHERE [[mfa]].[[organization_id]] = {:organizationId}`).
			Bind(dbx.Params{"organizationId": authorization.OrganizationID}).Row(&response.Required); err != nil {
			return err
		}
		return leafen.BumpOrganizationPolicy(txApp, authorization.OrganizationID, now)
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
		return organizationGuestMFAError(event, err, "write")
	}
	return event.JSON(http.StatusOK, response)
}

// loadOrganizationGuestMFA lists a window of the organization's workspaces by
// name from the cursor on, and which require their guests to use 2-step
// verification, with how many of all of them do.
func loadOrganizationGuestMFA(app core.App, organizationID, cursor string) (organizationGuestMFAResponse, error) {
	afterName, afterID := "", ""
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		name, id, found := strings.Cut(string(decoded), "\x00")
		if err != nil || !found || id == "" {
			return organizationGuestMFAResponse{}, errOrganizationGuestMFAInvalid
		}
		afterName, afterID = name, id
	}
	rows := []struct {
		organizationGuestMFAWorkspace
		SortName string `db:"sort_name"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[workspace]].[[id]], [[workspace]].[[name]], lower([[workspace]].[[name]]) AS [[sort_name]],
			EXISTS (SELECT 1 FROM {{leafen_organization_guest_mfa_workspaces}} [[mfa]]
				WHERE [[mfa]].[[workspace_id]] = [[workspace]].[[id]]
					AND [[mfa]].[[organization_id]] = [[workspace]].[[organization]]) AS [[required]]
		FROM {{leafen_workspaces}} [[workspace]]
		WHERE [[workspace]].[[organization]] = {:organizationId} AND [[workspace]].[[deletion_job_id]] = ''
			AND (lower([[workspace]].[[name]]), [[workspace]].[[id]]) > ({:afterName}, {:afterId})
		ORDER BY lower([[workspace]].[[name]]), [[workspace]].[[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{"organizationId": organizationID, "afterName": afterName, "afterId": afterID,
		"limit": organizationGuestMFAWindow + 1}).All(&rows); err != nil {
		return organizationGuestMFAResponse{}, err
	}
	response := organizationGuestMFAResponse{Workspaces: []organizationGuestMFAWorkspace{}}
	if len(rows) > organizationGuestMFAWindow {
		rows = rows[:organizationGuestMFAWindow]
		last := rows[len(rows)-1]
		response.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.SortName + "\x00" + last.ID))
	}
	for _, row := range rows {
		response.Workspaces = append(response.Workspaces, row.organizationGuestMFAWorkspace)
	}
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_guest_mfa_workspaces}} [[mfa]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[mfa]].[[workspace_id]]
			AND [[workspace]].[[organization]] = [[mfa]].[[organization_id]] AND [[workspace]].[[deletion_job_id]] = ''
		WHERE [[mfa]].[[organization_id]] = {:organizationId}`).
		Bind(dbx.Params{"organizationId": organizationID}).Row(&response.Required)
	return response, err
}

// guestWorkspaceMFA refuses a guest a workspace that requires its guests to
// use 2-step verification when they haven't turned it on.
func guestWorkspaceMFA(app core.App, principalID string, workspace *core.Record) error {
	var required int
	err := app.DB().NewQuery(`SELECT EXISTS (SELECT 1 FROM {{leafen_organization_guest_mfa_workspaces}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[organization_id]] = {:organizationId})`).Bind(dbx.Params{
		"workspaceId": workspace.Id, "organizationId": workspace.GetString("organization"),
	}).Row(&required)
	if err != nil || required == 0 {
		return err
	}
	// Every live sign-in of the account went through a second factor — a
	// 2-step code, a passkey or its sign-in provider's assurance — with
	// 2-step verification on or a provider-vouched session: the session asking
	// did, whichever it is (the session isn't passed to access checks).
	proven, err := leafen.AccountSessionsCarrySecondFactor(app, principalID)
	if err != nil {
		return err
	}
	if !proven {
		return leafen.GuestMFARequired()
	}
	return nil
}

func organizationGuestMFAError(event *core.RequestEvent, err error, stage string) error {
	if leafen.IsPermissionDenied(err) || errors.Is(err, sql.ErrNoRows) {
		return leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found.")
	}
	if errors.Is(err, errOrganizationGuestMFAInvalid) {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeOrganizationSettingsInvalid, "Check the workspaces.")
	}
	return leafen.APIErrorWithCause(
		event, http.StatusInternalServerError, leafen.ErrorCodeOrganizationSettingsInvalid,
		"Couldn't process the guest 2-step verification setting.", "organization_guest_mfa", stage,
		leafen.ErrorCodeOrganizationSettingsInvalid, err,
	)
}
