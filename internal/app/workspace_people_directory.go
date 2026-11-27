package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The people a workspace's members see named across the app — in person
// properties, mentions, bylines and "by" lines — as Notion names them: the
// workspace's owner and members with their names and pictures, and whether
// an organization suspended them (whose picture Notion crosses out). Any
// member reads it; the owner's member directory with emails and roles stays
// the settings' (workspace_member_directory.go). A guest, who reads only the
// pages shared with them, asks for the people those pages show by their IDs
// and hears of the workspace's owner and members among them.

const (
	workspacePeopleDirectoryPath  = "/workspaces/{workspaceId}/people"
	workspacePeopleDirectoryLimit = 1000
	workspacePeopleLookupPath     = "/workspaces/{workspaceId}/people/lookup"
	workspacePeopleLookupLimit    = 100
)

type workspacePerson struct {
	UserID      string `json:"userId" db:"user_id"`
	DisplayName string `json:"displayName" db:"display_name"`
	Avatar      string `json:"avatar" db:"avatar"`
	Suspended   bool   `json:"suspended" db:"suspended"`
	// JoinedAt is when the account came to this server: two people of the
	// same name with no email between them are told apart by it (2026-11-05).
	JoinedAt string `json:"joinedAt" db:"joined_at"`
	// Bot is a bot a person value names (notion_people_value_bots.go), or
	// an app's own account (integration_accounts.go): named and pictured as
	// a connection, never "〈name〉님" and never offered to pick.
	Bot bool `json:"bot,omitzero" db:"-"`
	// Invitee is someone invited a person value names before an account
	// proved their address (people_value_invitees.go), named by Email.
	Invitee bool   `json:"invitee,omitzero" db:"-"`
	Email   string `json:"email,omitempty" db:"-"`
	// Deactivated is a former guest People keeps (people_former_lookup.go):
	// named, never offered to pick, their mention opening nothing.
	Deactivated bool `json:"deactivated,omitzero" db:"-"`
}

// workspacePeopleCursor is where the next page of a people list starts:
// after the last person of the page, in its order (by name as SQLite's
// lower() folds it, then id).
type workspacePeopleCursor struct {
	Name string `json:"n"`
	ID   string `json:"i"`
}

func (cursor workspacePeopleCursor) encode() string {
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// readWorkspacePeopleCursor reads ?cursor=, the zero cursor when there is none.
func readWorkspacePeopleCursor(event *core.RequestEvent) (workspacePeopleCursor, bool) {
	cursor := workspacePeopleCursor{}
	raw := strings.TrimSpace(event.Request.URL.Query().Get("cursor"))
	if raw == "" {
		return cursor, true
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	return cursor, err == nil && len(raw) <= 2048 && json.Unmarshal(decoded, &cursor) == nil && cursor.ID != ""
}

// sqliteLower folds a name as SQLite's lower() does: ASCII letters only.
func sqliteLower(text string) string {
	folded := []byte(text)
	for index, char := range folded {
		if char >= 'A' && char <= 'Z' {
			folded[index] = char + 'a' - 'A'
		}
	}
	return string(folded)
}

func registerWorkspacePeopleDirectoryRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+workspacePeopleDirectoryPath, readWorkspacePeopleDirectory)
	event.Router.POST(apiRoot+workspacePeopleLookupPath, lookupWorkspacePeople)
	event.Router.POST(apiRoot+peopleValueInviteesPath, registerPeopleValueInvitees)
}

// workspacePeopleSQL reads the workspace's people; whether an organization
// suspended them is the organization module's to say.
func workspacePeopleSQL() string {
	return `
	SELECT [[user]].[[id]] AS [[user_id]], [[user]].[[name]] AS [[display_name]],
		[[user]].[[profile_avatar]] AS [[avatar]], [[user]].[[created]] AS [[joined_at]],
		` + organizationSuspendedSQL("[[user]].[[id]]") + ` AS [[suspended]]
	FROM {{users}} [[user]]
	WHERE [[user]].[[id]] IN (
		SELECT [[workspace_owner]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
		UNION SELECT [[user_id]] FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]] = {:workspaceId})`
}

// lookupWorkspacePeople names the people asked for by ID who are the
// workspace's owner or members, for its members and for its guests; and,
// for a member who isn't restricted, its guests and former guests; and the workspace's
// connections' bots and its invitees among them.
func lookupWorkspacePeople(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := struct {
		UserIDs []string `json:"userIds"`
	}{}
	if err := event.BindBody(&input); err != nil || len(input.UserIDs) == 0 || len(input.UserIDs) > workspacePeopleLookupLimit {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Ask for between 1 and 100 people.")
	}
	ids := make([]string, 0, len(input.UserIDs))
	for _, id := range input.UserIDs {
		if id = strings.TrimSpace(id); id != "" && len(id) <= maxWorkspacePermissionIdentifierLength && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	allowed, err := workspacePeopleReader(event.App, user.Id, workspaceID)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "lookup-authorize", errorCodeWorkspaceNotFound, err)
	}
	if !allowed {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	people := []workspacePerson{}
	if err := event.App.DB().NewQuery(workspacePeopleSQL() + `
		AND [[user]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[user]].[[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&people); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "lookup", errorCodeWorkspaceNotFound, err)
	}
	// Its guests asked for, named for a member as Settings › People › Guests
	// names them: its guests by a share.
	guests := []workspacePerson{}
	if member, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead); err == nil && !member.Restricted {
		if err := event.App.DB().NewQuery(workspaceGuestSharesSQL + `
			SELECT [[user]].[[id]] AS [[user_id]], [[user]].[[name]] AS [[display_name]], [[user]].[[profile_avatar]] AS [[avatar]],
				[[user]].[[created]] AS [[joined_at]],
				` + organizationSuspendedSQL("[[user]].[[id]]") + ` AS [[suspended]]
			FROM {{users}} [[user]]
			WHERE [[user]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
				AND [[user]].[[id]] IN (SELECT [[account_id]] FROM [[guest_share]] WHERE [[account_id]] IS NOT NULL)
			ORDER BY [[user]].[[id]]
		`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&guests); err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
				"Couldn't read the workspace's people.", "workspace_people", "lookup-guests", errorCodeWorkspaceNotFound, err)
		}
		// And its former guests, named as deactivated.
		former, err := lookupFormerPeople(event.App, workspaceID, ids)
		if err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
				"Couldn't read the workspace's people.", "workspace_people", "lookup-former", errorCodeWorkspaceNotFound, err)
		}
		guests = append(guests, former...)
	} else if err != nil && !isPermissionDenied(err) {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "lookup-guests", errorCodeWorkspaceNotFound, err)
	}
	// The workspace's connections' bots its person values name.
	bots, err := loadPeopleValueBots(event.App, workspaceID, ids)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "lookup-bots", errorCodeWorkspaceNotFound, err)
	}
	people = append(people, guests...)
	// An app's own account among them is named as a connection is.
	known := make([]string, 0, len(people))
	for _, person := range people {
		known = append(known, person.UserID)
	}
	apps, err := integrationAccountUsers(event.App, known)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "lookup-apps", errorCodeWorkspaceNotFound, err)
	}
	for index := range people {
		people[index].Bot = apps[people[index].UserID]
	}
	for _, id := range ids {
		if bot, found := bots[id]; found {
			people = append(people, workspacePerson{UserID: bot.ID, DisplayName: bot.Name, Avatar: bot.Icon, Bot: true})
		}
	}
	// And the people invited into its person values, by their addresses, and
	// the Notion people and integrations a migration kept, by their Notion
	// names (notion_imported_people.go).
	invitees, err := loadPeopleValueInvitees(event.App, workspaceID, ids)
	if err == nil {
		var invited []workspacePerson
		if invited, err = peopleValueInviteePeople(event.App, invitees); err == nil {
			people = append(people, invited...)
		}
	}
	if err == nil {
		var imported []workspacePerson
		if imported, err = lookupNotionImportedPeople(event.App, workspaceID, ids); err == nil {
			people = append(people, imported...)
		}
	}
	// And the people a copy from another workspace brought in its values,
	// for a reader of the copy (workspace_carried_people.go).
	if err == nil {
		people, err = withWorkspaceCarriedPeople(event.App, user.Id, workspaceID, ids, people)
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "lookup-invitees", errorCodeWorkspaceNotFound, err)
	}
	return event.JSON(http.StatusOK, map[string]any{"people": people})
}

// workspacePeopleReader says whether the person reads the workspace — as its
// owner or a member — or is a guest in it, with pages shared to them there.
func workspacePeopleReader(app core.App, userID, workspaceID string) (bool, error) {
	if _, err := authorizeWorkspace(app, userID, workspaceID, permissionWorkspaceRead); err == nil {
		return true, nil
	} else if !isPermissionDenied(err) {
		return false, err
	}
	// A guest there: a live page of it is shared with them.
	return hasBootstrapGuestShare(app, userID, workspaceID)
}

func readWorkspacePeopleDirectory(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	authorization, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead)
	if err != nil {
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "authorize", errorCodeWorkspaceNotFound, err)
	}
	// A page of the directory at a time: ?cursor= from the page before
	// reads on after its last person.
	after, ok := readWorkspacePeopleCursor(event)
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceNotFound, "The people cursor isn't one this server gave.")
	}
	people := []workspacePerson{}
	// The people to pick from leave out apps' accounts.
	// A restricted member picks among the people they share a place with
	// (restricted_member_people.go).
	if err := event.App.DB().NewQuery(workspacePeopleSQL() + restrictedPeopleFilterSQL(authorization.Restricted) + `
		AND NOT ` + integrationAccountSQL("[[user]].[[id]]") + `
		AND ({:afterId} = '' OR lower([[user]].[[name]]) > {:afterName}
			OR (lower([[user]].[[name]]) = {:afterName} AND [[user]].[[id]] > {:afterId}))
		ORDER BY lower([[user]].[[name]]), [[user]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "readerId": user.Id, "limit": workspacePeopleDirectoryLimit + 1,
		"afterName": after.Name, "afterId": after.ID,
	}).All(&people); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "read", errorCodeWorkspaceNotFound, err)
	}
	hasMore := len(people) > workspacePeopleDirectoryLimit
	nextCursor := ""
	if hasMore {
		people = people[:workspacePeopleDirectoryLimit]
		last := people[len(people)-1]
		nextCursor = workspacePeopleCursor{Name: sqliteLower(last.DisplayName), ID: last.UserID}.encode()
	}
	// With them, the workspace's switches for its people's profiles and cards.
	profiles, err := loadPeopleProfileSettings(event.App, authorization.WorkspaceID)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceNotFound,
			"Couldn't read the workspace's people.", "workspace_people", "profiles", errorCodeWorkspaceNotFound, err)
	}
	answer := map[string]any{"people": people, "hasMore": hasMore, "profiles": profiles}
	if nextCursor != "" {
		answer["nextCursor"] = nextCursor
	}
	return event.JSON(http.StatusOK, answer)
}
