package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const maxNotionDatabaseRowPagePeople = 100

// notionPersonInWorkspaceSQL holds for a person in a workspace as Notion's API
// names them: its owner, a member, or a guest of its organization (seen
// 2026-11-05: a guest reads as a whole user and can be written).
const notionPersonInWorkspaceSQL = `([[user]].[[id]]=[[workspace]].[[workspace_owner]]
	OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
		WHERE [[member]].[[workspace_id]]=[[workspace]].[[id]] AND [[member]].[[user_id]]=[[user]].[[id]] AND [[member]].[[role]]='member')
	OR EXISTS (SELECT 1 FROM {{` + organizationMembershipsCollection + `}} [[guest]]
		WHERE [[guest]].[[organization]]=[[workspace]].[[organization]] AND [[guest]].[[user]]=[[user]].[[id]]
			AND [[guest]].[[status]]='` + organizationMemberStatusActive + `' AND [[guest]].[[role]]='` + organizationRoleGuest + `'))`

// notionPeopleInWorkspace reads the people of these IDs who are in the workspace.
func notionPeopleInWorkspace(app core.App, workspaceID string, internalIDs []string) (map[string]storedNotionDirectoryUser, error) {
	encoded, err := json.Marshal(internalIDs)
	if err != nil {
		return nil, err
	}
	rows := []storedNotionDirectoryUser{}
	if err := app.DB().NewQuery(`
		SELECT [[user]].[[id]], [[user]].[[name]],
			[[user]].[[profile_avatar]] AS [[avatar]], [[user]].[[email]],
			[[user]].[[verified]] AS [[email_verified]]
		FROM {{users}} [[user]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]={:workspaceId}
		WHERE [[user]].[[id]] IN (SELECT [[value]] FROM json_each({:userIds})) AND ` + notionPersonInWorkspaceSQL,
	).Bind(dbx.Params{"userIds": string(encoded), "workspaceId": workspaceID}).All(&rows); err != nil {
		return nil, err
	}
	found := make(map[string]storedNotionDirectoryUser, len(rows))
	for _, row := range rows {
		found[row.ID] = row
	}
	return found, nil
}

// notionDatabaseRowPagePeople reads a person value as Notion's API does, each
// as notionPersonObject reads them. One of them never refuses the row.
func notionDatabaseRowPagePeople(
	app core.App,
	identity notionOAuthBearerIdentity,
	internalIDs []string,
) ([]map[string]any, error) {
	people := make([]map[string]any, 0, len(internalIDs))
	known, err := notionPeopleInWorkspace(app, identity.WorkspaceID, internalIDs)
	if err != nil {
		return nil, err
	}
	bots, err := loadPeopleValueBots(app, identity.WorkspaceID, internalIDs)
	if err != nil {
		return nil, err
	}
	invitees, err := loadPeopleValueInvitees(app, identity.WorkspaceID, internalIDs)
	if err != nil {
		return nil, err
	}
	// An invitee whose account proved the address reads as that person
	// (people_value_invitees.go).
	accounts := []string{}
	for _, invitee := range invitees {
		if invitee.AccountID != "" {
			accounts = append(accounts, invitee.AccountID)
		}
	}
	if len(accounts) > 0 {
		joined, err := notionPeopleInWorkspace(app, identity.WorkspaceID, accounts)
		if err != nil {
			return nil, err
		}
		for id, user := range joined {
			known[id] = user
		}
	}
	seen := make(map[string]struct{}, len(internalIDs))
	_, emails := notionIdentityReadsPeople(app, identity)
	for _, internalID := range internalIDs {
		if invitee, found := invitees[internalID]; found {
			if invitee.AccountID == "" {
				if _, duplicate := seen[internalID]; !duplicate {
					seen[internalID] = struct{}{}
					people = append(people, notionPeopleValueInviteeObject(identity, emails, invitee))
				}
				continue
			}
			internalID = invitee.AccountID
		}
		if _, duplicate := seen[internalID]; duplicate {
			continue
		}
		seen[internalID] = struct{}{}
		// A bot reads as one; one whose connection is gone, by its ID alone.
		if notionBotUserIDShape(internalID) {
			if bot, found := bots[internalID]; found {
				people = append(people, notionPeopleValueBotObject(identity, bot))
			} else {
				people = append(people, map[string]any{"object": "user", "id": internalID})
			}
			continue
		}
		if !validNotionCompatibleInternalID(internalID) {
			continue
		}
		user, inWorkspace := known[internalID]
		publicID, err := notionCompatibleUserID(internalID)
		if err != nil {
			return nil, err
		}
		person, err := notionPersonObject(identity, emails, user, inWorkspace, publicID)
		if err != nil {
			return nil, err
		}
		people = append(people, person)
	}
	return people, nil
}

// notionPersonObject is a person as a token reads them, in a person value
// and a person mention alike (seen 2026-11-11): a whole user for a token
// that may read users — with their email for one that may read it
// (notionIdentityReadsPeople) — and the ID alone, publicID, for one that may
// not or for someone not in the workspace.
func notionPersonObject(
	identity notionOAuthBearerIdentity, emails bool, user storedNotionDirectoryUser, inWorkspace bool, publicID string,
) (map[string]any, error) {
	if !inWorkspace || !notionOAuthHasScope(identity, "workspace:read") {
		return map[string]any{"object": "user", "id": publicID}, nil
	}
	email := user.Email
	if !emails {
		email = ""
	}
	return notionUserObjectFromFields(user.ID, user.Name, user.Avatar, email, user.EmailVerified, identity.Origin)
}

// notionDatabaseRowPageAuthor is a row page's creator, or its last editor,
// as their property reads them: the bot of the connection that made it, or
// last edited it, as Notion's (seen 2026-11-11), and otherwise the person,
// as its created_by and last_edited_by name them (connectionAuthorIDs).
func notionDatabaseRowPageAuthor(
	app core.App, authorization notionDatabaseRowPageAuthorization, edited bool,
) (map[string]any, error) {
	authority := authorization.Authority
	createdGrant, editedGrant, err := connectionAuthorGrants(
		app, connectionAuthoredRow, authority.RowID, notionBlockEditMark(authority.UpdatedAt),
	)
	if err != nil {
		return nil, err
	}
	grant, person := createdGrant, authority.CreatedBy
	if edited {
		grant, person = editedGrant, authority.LastEditedBy
	}
	if grant == "" {
		return notionDatabaseRowPageWholeUser(app, authorization.Identity, person)
	}
	return notionConnectionBotValue(app, authorization.Identity, grant)
}

// notionConnectionBotValue is a connection's bot as a person value reads
// it: its name and an empty bot object, or the ID alone for a token that
// may not read users, as a person's (notionPersonObject).
func notionConnectionBotValue(app core.App, identity notionOAuthBearerIdentity, grantID string) (map[string]any, error) {
	id := notionOAuthBotID(grantID)
	if !notionOAuthHasScope(identity, "workspace:read") {
		return map[string]any{"object": "user", "id": id}, nil
	}
	grant, found, err := findNotionOAuthGrantByID(app, grantID)
	if err != nil {
		return nil, err
	}
	var name any
	if found && grant.ClientName != "" {
		name = grant.ClientName
	}
	return map[string]any{
		"object": "user", "id": id, "name": name, "avatar_url": nil, "type": "bot", "bot": map[string]any{},
	}, nil
}
