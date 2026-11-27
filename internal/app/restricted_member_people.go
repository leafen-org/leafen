package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A restricted member works with the people they share a place with, as
// Notion's help ("Who's who in a workspace", read 2026-10-12) has them
// share and @mention only the members of the same teamspaces and pages:
// the people of their teamspaces (themselves or through a group), the
// people their shared pages are shared with, and the workspace's owner.

// restrictedVisiblePeopleSQL selects, as user_id, the people a restricted
// member ({:readerId}) sees in the workspace ({:workspaceId}).
func restrictedVisiblePeopleSQL() string {
	return `
		SELECT [[workspace_owner]] AS [[user_id]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
		UNION SELECT {:readerId}
		UNION SELECT [[peer]].[[principal_id]] FROM (` + teamspaceEffectiveMembersSQL() + `) [[peer]]
			WHERE [[peer]].[[teamspace_id]] IN (SELECT [[mine]].[[teamspace_id]] FROM (` + teamspaceEffectiveMembersSQL() + `) [[mine]]
				WHERE [[mine]].[[principal_id]] = {:readerId})
		UNION SELECT [[peer_share]].[[principal_id]] FROM {{leafen_page_permissions}} [[peer_share]]
			WHERE [[peer_share]].[[workspace_id]] = {:workspaceId} AND [[peer_share]].[[principal_type]] = 'user'
				AND [[peer_share]].[[page_id]] IN (SELECT [[my_share]].[[page_id]] FROM {{leafen_page_permissions}} [[my_share]]
					WHERE [[my_share]].[[workspace_id]] = {:workspaceId} AND [[my_share]].[[principal_type]] = 'user'
						AND [[my_share]].[[principal_id]] = {:readerId})`
}

// restrictedPeopleFilterSQL narrows workspacePeopleSQL to the people a
// restricted reader sees; nothing for anyone else.
func restrictedPeopleFilterSQL(restricted bool) string {
	if !restricted {
		return ""
	}
	return ` AND [[user]].[[id]] IN (` + restrictedVisiblePeopleSQL() + `)`
}

// restrictedReaderSees is whether a restricted member shares a place with the person.
func restrictedReaderSees(app core.App, readerID, workspaceID, userID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM (` + restrictedVisiblePeopleSQL() + `) [[visible]] WHERE [[visible]].[[user_id]] = {:userId}`).
		Bind(dbx.Params{"readerId": readerID, "workspaceId": workspaceID, "userId": userID}).Row(&count)
	return count > 0, err
}

// peopleEmailShown is the address a reader is shown for a person in the
// workspace's people outside the share window — the "@" menu and person
// pickers, the people directory, a person's card and the members list. A
// guest or a restricted member (narrow) is shown only their own: they see
// the names of the people they share a place with, as Notion offers a guest
// the person who shared a page with them by name in "@" (seen 2026-11-15,
// no address on the row), but no one else's address — the share window's
// narrowing carried to every list of people.
func peopleEmailShown(narrow bool, readerID, personID, email string) string {
	if narrow && personID != readerID {
		return ""
	}
	return email
}
