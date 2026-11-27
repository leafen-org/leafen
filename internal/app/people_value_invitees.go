package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person value may name someone invited who has no account yet, as
// Notion's people take an address invited from a person property's "초대와
// 멘션" (seen 2026-11-12: the address went into the cell at once, a first
// letter on a circle and the address as its name; with the row page's
// share taken back, the value stayed). The value keeps an invitee's own ID,
// which the workspace's register names by its address. The address is its
// name until an account proves it; from then the invitee reads as that
// account, and the runner (people_value_invitee_runner.go) turns the
// workspace's values naming it into the account's.

const (
	peopleValueInviteesPath = "/workspaces/{workspaceId}/people/invitees"
	maxPeopleValueInvitees  = 20
)

// peopleValueInviteeID is an invitee's ID in its workspace: a UUID its
// address fixes, scheme nibble 4 keeping it apart from the reversible user
// codec (1), object identities (2) and bots (3).
func peopleValueInviteeID(workspaceID, email string) string {
	digest := sha256.Sum256([]byte("leafen:people-invitee:v1\x00" + workspaceID + "\x00" + normalizePageShareEmail(email)))
	digest[0] = digest[0]&0x0f | 0x40
	digest[6] = digest[6]&0x0f | 0x80
	digest[8] = digest[8]&0x3f | 0x80
	var encoded [16]byte
	copy(encoded[:], digest[:16])
	return formatNotionUserUUID(encoded)
}

// peopleValueInviteeIDShape tells an ID made as an invitee's.
func peopleValueInviteeIDShape(id string) bool {
	decoded, err := parseNotionUserUUID(notionPublicUserID(id))
	return err == nil && decoded[0]>>4 == 4 && decoded[6]>>4 == 8 && decoded[8]>>6 == 2
}

type peopleValueInvitee struct {
	ID    string `db:"id"`
	Email string `db:"email"`
	// AccountID is the account that proved the address, "" before one has.
	AccountID string `db:"account_id"`
	// Invited says the address still holds the workspace's invitation: a page
	// shared with it or a membership waiting for it.
	Invited bool `db:"invited"`
}

// loadPeopleValueInvitees reads the workspace's invitees of these IDs.
func loadPeopleValueInvitees(app core.App, workspaceID string, ids []string) (map[string]peopleValueInvitee, error) {
	wanted := make([]string, 0, len(ids))
	for _, id := range ids {
		if peopleValueInviteeIDShape(id) {
			wanted = append(wanted, id)
		}
	}
	found := make(map[string]peopleValueInvitee, len(wanted))
	if len(wanted) == 0 {
		return found, nil
	}
	encoded, err := json.Marshal(wanted)
	if err != nil {
		return nil, err
	}
	rows := []peopleValueInvitee{}
	if err := app.DB().NewQuery(`
		SELECT [[invitee]].[[id]], [[invitee]].[[email]],
			COALESCE((SELECT [[proof]].[[user_id]] FROM {{leafen_account_emails}} [[proof]]
				WHERE [[proof]].[[email]] = [[invitee]].[[email]] AND ` + provenAccountEmailSQL("[[proof]].") + `
				ORDER BY [[proof]].[[user_id]] LIMIT 1), '') AS [[account_id]],
			(EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[share]]
				WHERE [[share]].[[workspace_id]] = [[invitee]].[[workspace_id]] AND [[share]].[[principal_type]] = 'email'
					AND lower(trim([[share]].[[principal_id]])) = [[invitee]].[[email]])
			OR EXISTS (SELECT 1 FROM {{leafen_workspace_invitations}} [[invitation]]
				WHERE [[invitation]].[[workspace_id]] = [[invitee]].[[workspace_id]]
					AND [[invitation]].[[email]] = [[invitee]].[[email]] AND [[invitation]].[[status]] = 'pending')) AS [[invited]]
		FROM {{leafen_people_value_invitees}} [[invitee]]
		WHERE [[invitee]].[[workspace_id]] = {:workspaceId} AND [[invitee]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		found[row.ID] = row
	}
	return found, nil
}

// notionPeopleValueInviteeObject is an invitee as a person value reads it to
// a token: the account that proved the address, once one has, as that
// person; before, a person named by the address while it holds the
// workspace's invitation, and the ID alone once it doesn't — as Notion's
// reads someone invited whose access was taken back (seen 2026-11-12).
func notionPeopleValueInviteeObject(identity notionOAuthBearerIdentity, emails bool, invitee peopleValueInvitee) map[string]any {
	if !invitee.Invited || !notionOAuthHasScope(identity, "workspace:read") {
		return map[string]any{"object": "user", "id": invitee.ID}
	}
	person := map[string]any{}
	var name any
	if emails {
		person["email"], name = invitee.Email, invitee.Email
	}
	return map[string]any{"object": "user", "id": invitee.ID, "type": "person", "name": name, "avatar_url": nil, "person": person}
}

// peopleValueInviteePeople are invitees as the app's people lookup names
// them: by the account that proved the address, else by the address.
func peopleValueInviteePeople(app core.App, invitees map[string]peopleValueInvitee) ([]workspacePerson, error) {
	people := make([]workspacePerson, 0, len(invitees))
	for _, invitee := range invitees {
		person := workspacePerson{UserID: invitee.ID, Email: invitee.Email, Invitee: true}
		if invitee.AccountID != "" {
			if account, err := app.FindRecordById("users", invitee.AccountID); err == nil {
				person = workspacePerson{UserID: invitee.ID, DisplayName: account.GetString("name"),
					Avatar: account.GetString("profile_avatar"), Email: invitee.Email, JoinedAt: account.GetString("created")}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			notifyPeopleValueInviteeRunner(app)
		}
		people = append(people, person)
	}
	return people, nil
}

// peopleValueInviteeStatus is how a people search marks an invitee it
// offers, apart from the organization's active people.
const peopleValueInviteeStatus = "invited"

// peopleValueInviteesToPick are the workspace's invitees whose address holds
// the query, as a person picker and an "@" menu offer them after its people,
// as Notion's offer a guest invited by an address that has no account yet
// (seen 2026-11-12: in other rows' person pickers by the address, its first
// letter on a circle, and in the "@" menu marked "게스트"): the addresses
// its live pages (out of the trash with every page above them,
// pageOutOfTrashPathSQL) are shared with or a membership waits for, that no account
// proves — one a membership waits for is a member to be, not a guest
// (people_value_invitee_membership.go). Each is registered as an invitee the first time it is offered
// (registerOfferedPeopleValueInvitees), so a value naming it reads and turns
// into the account that proves the address later.
func peopleValueInviteesToPick(app core.App, workspaceID, query string, limit int) ([]storedOrganizationPerson, []string, error) {
	rows := []struct {
		Email      string `db:"email"`
		Registered bool   `db:"registered"`
		Member     bool   `db:"member"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[address]].[[email]], EXISTS (SELECT 1 FROM {{leafen_people_value_invitees}} [[invitee]]
			WHERE [[invitee]].[[workspace_id]] = {:workspaceId} AND [[invitee]].[[email]] = [[address]].[[email]]) AS [[registered]],
			` + pendingMembershipInvitationSQL("{:workspaceId}", "[[address]].[[email]]", false) + ` AS [[member]]
		FROM (
			SELECT lower(trim([[share]].[[principal_id]])) AS [[email]] FROM {{leafen_page_permissions}} [[share]]
			JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[share]].[[page_id]]
			WHERE [[share]].[[workspace_id]] = {:workspaceId} AND [[share]].[[principal_type]] = 'email'
				AND [[page]].[[notion_import_staging]] = 0 AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
			UNION SELECT [[invitation]].[[email]] FROM {{leafen_workspace_invitations}} [[invitation]]
			WHERE [[invitation]].[[workspace_id]] = {:workspaceId} AND [[invitation]].[[status]] = 'pending'
		) [[address]]
		WHERE length([[address]].[[email]]) BETWEEN 3 AND 320 AND ({:query} = '' OR instr([[address]].[[email]], {:query}) > 0)
			AND NOT EXISTS (SELECT 1 FROM {{leafen_account_emails}} [[proof]]
				WHERE [[proof]].[[email]] = [[address]].[[email]] AND ` + provenAccountEmailSQL("[[proof]].") + `)
		ORDER BY [[address]].[[email]] LIMIT {:limit}
	`).Bind(dbx.Params{"workspaceId": workspaceID, "query": query, "limit": limit}).All(&rows); err != nil {
		return nil, nil, err
	}
	people := make([]storedOrganizationPerson, 0, len(rows))
	unregistered := []string{}
	for _, row := range rows {
		// An address invited to become a member is one to be, not a guest
		// (people_value_invitee_membership.go).
		role := organizationRoleGuest
		if row.Member {
			role = organizationRoleMember
		}
		people = append(people, storedOrganizationPerson{UserID: peopleValueInviteeID(workspaceID, row.Email), Email: row.Email,
			OrganizationRole: role, Status: peopleValueInviteeStatus})
		if !row.Registered {
			unregistered = append(unregistered, row.Email)
		}
	}
	return people, unregistered, nil
}

// registerOfferedPeopleValueInvitees registers the addresses a picker was
// just offered as the workspace's invitees, the reader as who named them.
func registerOfferedPeopleValueInvitees(app core.App, workspaceID, readerID string, emails []string) error {
	if len(emails) == 0 {
		return nil
	}
	return app.RunInTransaction(func(txApp core.App) error {
		for _, email := range emails {
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_people_value_invitees}} ([[id]], [[workspace_id]], [[email]], [[invited_by]], [[created_at]])
				VALUES ({:id}, {:workspaceId}, {:email}, {:reader}, {:now}) ON CONFLICT DO NOTHING
			`).Bind(dbx.Params{
				"id": peopleValueInviteeID(workspaceID, email), "workspaceId": workspaceID, "email": email,
				"reader": readerID, "now": time.Now().UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
		}
		// The runner looks for an account proving them while any waits.
		notifyPeopleValueInviteeRunner(txApp)
		return nil
	})
}

// registerPeopleValueInvitees answers, for each address the workspace has
// just invited, whom a person value names: the account that proved it, or
// the invitee it keeps until one does. An address the workspace hasn't
// invited — no page shared with it, no membership waiting — is left out.
func registerPeopleValueInvitees(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := struct {
		Emails []string `json:"emails"`
	}{}
	if err := event.BindBody(&input); err != nil || len(input.Emails) == 0 || len(input.Emails) > maxPeopleValueInvitees {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Name between 1 and 20 addresses.")
	}
	emails := make([]string, 0, len(input.Emails))
	for _, raw := range input.Emails {
		email, valid := normalizeWorkspaceMemberEmail(raw)
		if !valid {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the addresses.")
		}
		emails = append(emails, email)
	}
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead); err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "invitees-authorize", errorCodeWorkspaceNotFound, err)
	}
	ids := make([]string, 0, len(emails))
	err := event.App.RunInTransaction(func(txApp core.App) error {
		for _, email := range emails {
			id := peopleValueInviteeID(workspaceID, email)
			if _, err := txApp.DB().NewQuery(`
				INSERT INTO {{leafen_people_value_invitees}} ([[id]], [[workspace_id]], [[email]], [[invited_by]], [[created_at]])
				SELECT {:id}, {:workspaceId}, {:email}, {:inviter}, {:now}
				WHERE EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[share]]
						WHERE [[share]].[[workspace_id]] = {:workspaceId} AND [[share]].[[principal_type]] = 'email'
							AND lower(trim([[share]].[[principal_id]])) = {:email})
					OR EXISTS (SELECT 1 FROM {{leafen_workspace_invitations}} [[invitation]]
						WHERE [[invitation]].[[workspace_id]] = {:workspaceId} AND [[invitation]].[[email]] = {:email}
							AND [[invitation]].[[status]] = 'pending')
				ON CONFLICT ([[id]]) DO NOTHING
			`).Bind(dbx.Params{
				"id": id, "workspaceId": workspaceID, "email": email, "inviter": user.Id, "now": time.Now().UnixMilli(),
			}).Execute(); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		// The runner looks for an account proving them while any waits.
		notifyPeopleValueInviteeRunner(txApp)
		return nil
	})
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "invitees", errorCodeWorkspaceNotFound, err)
	}
	invitees, err := loadPeopleValueInvitees(event.App, workspaceID, ids)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "invitees-read", errorCodeWorkspaceNotFound, err)
	}
	people := []workspacePerson{}
	for _, id := range ids {
		invitee, found := invitees[id]
		if !found || !invitee.Invited {
			continue
		}
		// Someone whose account proved the address is named as themselves.
		if invitee.AccountID != "" {
			account, err := event.App.FindRecordById("users", invitee.AccountID)
			if err != nil {
				return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
					"Couldn't read the workspace's people.", "workspace_people", "invitees-account", errorCodeWorkspaceNotFound, err)
			}
			people = append(people, workspacePerson{UserID: account.Id, DisplayName: account.GetString("name"),
				Avatar: account.GetString("profile_avatar"), JoinedAt: account.GetString("created")})
			continue
		}
		people = append(people, workspacePerson{UserID: invitee.ID, Email: invitee.Email, Invitee: true})
	}
	return event.JSON(http.StatusOK, map[string]any{"people": people})
}
