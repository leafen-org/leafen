package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The Notion people and integrations a migration brought that match no
// member keep their Notion names here, by the ID their values hold
// (notion-user:<Notion ID>), so a person value Notion gave by ID alone, and a
// page or row they made, show their name and not a code; an integration
// shows as one, an account-less nameplate. Who made and last changed an
// imported page or row while that was a Notion person or integration kept
// as such goes beside it in the second table: the page keeps the person
// running the migration in its account columns, and the nameplate shows
// where Leafen names its maker and last editor — the last editor only while
// the page is as the migration left it.

// keepNotionImportedPeople keeps the names of a migration's Notion people and
// integrations its plan matches to no member, as its scan read them.
func keepNotionImportedPeople(app core.App, workspaceID string, people []notionMigrationPerson, mapped map[string]bool) error {
	for _, person := range people {
		id, name := strings.TrimSpace(person.ID), strings.TrimSpace(person.Name)
		if id == "" || name == "" || mapped[canonicalNotionID(id)] {
			continue
		}
		kind := notionMigrationPersonPerson
		if person.Type == notionMigrationPersonIntegration {
			kind = notionMigrationPersonIntegration
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_notion_imported_people}} ([[workspace_id]], [[source_id]], [[display_name]], [[kind]], [[email]])
			VALUES ({:workspaceId}, {:sourceId}, {:name}, {:kind}, {:email})
			ON CONFLICT ([[workspace_id]], [[source_id]]) DO UPDATE SET
				[[display_name]] = excluded.[[display_name]], [[kind]] = excluded.[[kind]], [[email]] = excluded.[[email]]
		`).Bind(dbx.Params{
			"workspaceId": workspaceID, "sourceId": importedPeopleSourcePrefix + canonicalNotionID(id), "name": name, "kind": kind,
			"email": strings.ToLower(strings.TrimSpace(person.Email)),
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// lookupNotionImportedPeople names the kept Notion people and integrations
// among these IDs, an integration as a connection is named.
func lookupNotionImportedPeople(app core.App, workspaceID string, ids []string) ([]workspacePerson, error) {
	wanted := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.HasPrefix(id, importedPeopleSourcePrefix) {
			wanted = append(wanted, id)
		}
	}
	people := []workspacePerson{}
	if len(wanted) == 0 {
		return people, nil
	}
	encoded, err := json.Marshal(wanted)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		SourceID string `db:"source_id"`
		Name     string `db:"display_name"`
		Kind     string `db:"kind"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]], [[display_name]], [[kind]] FROM {{leafen_notion_imported_people}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[source_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		people = append(people, workspacePerson{
			UserID: row.SourceID, DisplayName: row.Name, Bot: row.Kind == notionMigrationPersonIntegration,
		})
	}
	return people, nil
}
