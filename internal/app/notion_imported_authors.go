package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// What a migration's stamps keep of the Notion people and integrations it
// keeps (notion_imported_people.go): their names once, as the scan read them,
// before the first page takes its history; then each page's and row's maker
// and last editor among them, beside the person running the migration.

// keepNotionMigrationPeopleNames keeps the names of a migration's scanned
// people and integrations the plan maps to no member.
func keepNotionMigrationPeopleNames(app core.App, job storedJob, payload notionImportJobPayload, run *notionMigrationRunPlan) error {
	if run == nil || payload.MigrationID == "" {
		return nil
	}
	stored, err := loadNotionMigration(app, job.Owner, job.WorkspaceID, payload.MigrationID)
	if err != nil {
		return err
	}
	people := []notionMigrationPerson{}
	if err := notionMigrationScanOf(app, stored).walk(notionMigrationItemPerson, func(value string) error {
		person := notionMigrationPerson{}
		if err := json.Unmarshal([]byte(value), &person); err != nil {
			return err
		}
		people = append(people, person)
		return nil
	}); err != nil {
		return err
	}
	mapped := make(map[string]bool, len(run.people))
	for id := range run.people {
		mapped[canonicalNotionID(id)] = true
	}
	return keepNotionImportedPeople(app, job.WorkspaceID, people, mapped)
}

// stampNotionImportedAuthors keeps who made and last changed a page or row
// while that was a kept Notion person or integration with a kept name, and
// when it was last changed, so the last editor's stands only until someone
// changes it here.
func stampNotionImportedAuthors(app core.App, workspaceID, pageID string, stamp notionImportRowStamp, run *notionMigrationRunPlan) error {
	created, edited := run.plate(stamp.CreatedBy), run.plate(stamp.LastEditedBy)
	if created == "" && edited == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_imported_authors}}
			([[page_id]], [[workspace_id]], [[created_source]], [[edited_source]], [[edited_at]])
		SELECT {:pageId}, {:workspaceId},
			CASE WHEN EXISTS (SELECT 1 FROM {{leafen_notion_imported_people}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[source_id]] = {:created}) THEN {:created} ELSE '' END,
			CASE WHEN EXISTS (SELECT 1 FROM {{leafen_notion_imported_people}}
				WHERE [[workspace_id]] = {:workspaceId} AND [[source_id]] = {:edited}) THEN {:edited} ELSE '' END,
			{:editedAt}
		WHERE TRUE
		ON CONFLICT ([[page_id]]) DO UPDATE SET [[created_source]] = excluded.[[created_source]],
			[[edited_source]] = excluded.[[edited_source]], [[edited_at]] = excluded.[[edited_at]]
	`).Bind(dbx.Params{
		"pageId": pageID, "workspaceId": workspaceID, "created": strings.TrimSpace(created),
		"edited": strings.TrimSpace(edited), "editedAt": stamp.Edited.UnixMilli(),
	}).Execute()
	return err
}

// notionImportedAuthorsOf reads the kept Notion maker and last editor of a
// page or row: the last editor while it was last changed when the migration
// left it (updated, as its record keeps it), else ""; none on a failed read,
// which leaves the page's own.
func notionImportedAuthorsOf(app core.App, pageID, updated string) (string, string) {
	stored := struct {
		Created  string `db:"created_source"`
		Edited   string `db:"edited_source"`
		EditedAt int64  `db:"edited_at"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[created_source]], [[edited_source]], [[edited_at]] FROM {{leafen_notion_imported_authors}}
		WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).One(&stored); err != nil {
		return "", ""
	}
	if at, err := types.ParseDateTime(updated); err != nil || at.Time().UnixMilli() != stored.EditedAt {
		stored.Edited = ""
	}
	return stored.Created, stored.Edited
}
