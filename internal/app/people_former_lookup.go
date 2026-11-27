package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Someone People keeps as deactivated — a former guest — is still named where
// the workspace's pages name them, as Notion's (seen 2026-11-15 twice, a free
// test workspace: its removed guest in a person property's cell read by name
// and picture, not as someone unknown). A lookup names them, marked
// deactivated, for whoever is shown the workspace's guests: no picker offers
// them, and their mention opens nothing (Notion's, seen 2026-11-15).

// lookupFormerPeople names those among ids the workspace keeps as former
// guests: not its owner, a member nor its guest again, and not an app's
// account.
func lookupFormerPeople(app core.App, workspaceID string, ids []string) ([]workspacePerson, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	people := []workspacePerson{}
	if err := app.DB().NewQuery(workspaceGuestSharesSQL + `
		SELECT [[former]].[[id]] AS [[user_id]], [[former]].[[name]] AS [[display_name]],
			[[former]].[[profile_avatar]] AS [[avatar]]
		FROM (` + peopleFormerPeopleSQL(` AND [[user]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
			AND NOT `+integrationAccountSQL("[[user]].[[id]]")) + `) [[former]]
		ORDER BY [[former]].[[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&people); err != nil {
		return nil, err
	}
	for index := range people {
		people[index].Deactivated = true
	}
	return people, nil
}
