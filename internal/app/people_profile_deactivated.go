package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person People lists as deactivated — a former guest the workspace keeps
// (peopleFormerPeopleSQL), or someone an organization suspended — has a
// profile like anyone's, as Notion's (seen 2026-11-15 twice, a free test
// workspace, its guest removed): their name with "비활성화됨" under it,
// About, Email, Membership Type "Deactivated" and their teamspaces, no best
// collaborators. It opened where People's row did, not "not found". Their
// name in a mention shows no card and opens nothing (seen 2026-11-15, the
// mention of a guest removed, before and after a reload).

const personMembershipDeactivated = "deactivated"

// loadFormerPerson reads a former guest of the workspace that People keeps
// as deactivated: not its owner, a member nor its guest again, and not an
// app's account.
func loadFormerPerson(app core.App, workspaceID, personID string) (workspacePerson, error) {
	person := workspacePerson{}
	err := app.DB().NewQuery(workspaceGuestSharesSQL + `
		SELECT [[former]].[[id]] AS [[user_id]], [[former]].[[name]] AS [[display_name]],
			[[former]].[[profile_avatar]] AS [[avatar]]
		FROM (` + peopleFormerPeopleSQL(` AND [[user]].[[id]] = {:personId} AND NOT `+integrationAccountSQL("[[user]].[[id]]")) + `) [[former]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "personId": personID}).One(&person)
	return person, err
}

// personCardMembership is the person's place in the workspace as their card
// and profile name it: deactivated when People lists them so, a guest, the
// owner, a restricted member or a member.
func personCardMembership(app core.App, authorization workspaceAuthorization, personID string, guest, deactivated bool) (string, error) {
	switch {
	case deactivated:
		return personMembershipDeactivated, nil
	case guest:
		return personMembershipGuest, nil
	case personID == authorization.WorkspaceOwnerID:
		return personMembershipOwner, nil
	}
	restricted, err := workspaceMemberRestricted(app, authorization.WorkspaceID, personID)
	if err != nil || !restricted {
		return personMembershipMember, err
	}
	return personMembershipRestricted, nil
}
