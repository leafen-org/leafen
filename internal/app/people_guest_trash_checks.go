package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Someone stops being a guest when the last page shared with them goes to
// the trash — the page itself, or a page above it, its database row or its
// database (workspaceGuestSharesSQL goes by the one trash rule,
// pageOutOfTrashPathSQL) — and is one again when it comes back. The
// baseline's trigger marked a workspace for People's look
// (people_guest_rows.go) only when a shared page itself went to the trash;
// a page above it, whose shares are not its own, went unmarked. A trigger
// can't walk down to the pages under one (SQLite takes no WITH in a
// trigger), so migration 860 marks the workspace whenever any page, row or
// database there goes to the trash or comes back while a person holds a
// share in it, and the look reads its guests again.

const (
	nativePeopleGuestTrashChecksVersion = 860
	nativePeopleGuestTrashChecksName    = "native_people_guest_trash_checks"
	peopleGuestPageTrashTrigger         = "leafen_people_guest_check_page_trash"
)

// peopleGuestTrashTriggers are the triggers that mark a workspace for its
// guests when something there goes to the trash or comes back.
var peopleGuestTrashTriggers = []string{
	peopleGuestPageTrashTrigger,
	"leafen_people_guest_check_row_trash_insert",
	"leafen_people_guest_check_row_trash_delete",
	"leafen_people_guest_check_database_trash_insert",
	"leafen_people_guest_check_database_trash_delete",
}

// peopleGuestCheckSQL marks the workspace the expression names for People's
// look while a person holds a share there.
func peopleGuestCheckSQL(workspace string) string {
	return `INSERT INTO {{leafen_people_guest_checks}} ([[workspace_id]])
			SELECT [[checked]].[[id]] FROM {{leafen_workspaces}} [[checked]]
			WHERE [[checked]].[[deletion_job_id]] = '' AND [[checked]].[[id]] = ` + workspace + `
				AND EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[share]]
					WHERE [[share]].[[workspace_id]] = [[checked]].[[id]] AND [[share]].[[principal_type]] IN ('user', 'email'))
			ON CONFLICT ([[workspace_id]]) DO UPDATE SET [[dirty]] = 1;`
}

func peopleGuestTrashTriggerStatements() []string {
	container := `(SELECT [[container]].[[workspace_id]] FROM {{leafen_database_containers}} [[container]] WHERE [[container]].[[id]] = %s.[[container_id]])`
	return []string{
		`CREATE TRIGGER [[` + peopleGuestPageTrashTrigger + `]] AFTER UPDATE ON {{leafen_pages}}
		WHEN NEW.[[in_trash]] IS NOT OLD.[[in_trash]] OR NEW.[[notion_import_staging]] IS NOT OLD.[[notion_import_staging]]
		BEGIN ` + peopleGuestCheckSQL("NEW.[[workspace]]") + ` END`,
		`CREATE TRIGGER [[leafen_people_guest_check_row_trash_insert]] AFTER INSERT ON {{leafen_database_row_trash}}
		BEGIN ` + peopleGuestCheckSQL("NEW.[[workspace_id]]") + ` END`,
		`CREATE TRIGGER [[leafen_people_guest_check_row_trash_delete]] AFTER DELETE ON {{leafen_database_row_trash}}
		BEGIN ` + peopleGuestCheckSQL("OLD.[[workspace_id]]") + ` END`,
		`CREATE TRIGGER [[leafen_people_guest_check_database_trash_insert]] AFTER INSERT ON {{leafen_database_container_trash}}
		BEGIN ` + peopleGuestCheckSQL(fmt.Sprintf(container, "NEW")) + ` END`,
		`CREATE TRIGGER [[leafen_people_guest_check_database_trash_delete]] AFTER DELETE ON {{leafen_database_container_trash}}
		BEGIN ` + peopleGuestCheckSQL(fmt.Sprintf(container, "OLD")) + ` END`,
	}
}

// ensureNativePeopleGuestTrashChecks puts the triggers in place and marks
// every workspace a person holds a share in, so its guests are read again
// once by the one trash rule.
func ensureNativePeopleGuestTrashChecks(app core.App) error {
	for _, trigger := range peopleGuestTrashTriggers {
		if _, err := app.DB().NewQuery(`DROP TRIGGER IF EXISTS [[` + trigger + `]]`).Execute(); err != nil {
			return err
		}
	}
	for _, statement := range peopleGuestTrashTriggerStatements() {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_people_guest_checks}} ([[workspace_id]])
		SELECT DISTINCT [[share]].[[workspace_id]] FROM {{leafen_page_permissions}} [[share]]
		JOIN {{leafen_workspaces}} [[checked]] ON [[checked]].[[id]] = [[share]].[[workspace_id]] AND [[checked]].[[deletion_job_id]] = ''
		WHERE [[share]].[[principal_type]] IN ('user', 'email')
		ON CONFLICT ([[workspace_id]]) DO UPDATE SET [[dirty]] = 1
	`).Execute()
	return err
}

func validateNativePeopleGuestTrashChecks(app core.App) error {
	for _, trigger := range peopleGuestTrashTriggers {
		var body string
		if err := app.DB().NewQuery(`SELECT [[sql]] FROM [[sqlite_master]] WHERE [[type]] = 'trigger' AND [[name]] = {:name}`).
			Bind(dbx.Params{"name": trigger}).Row(&body); err != nil {
			return fmt.Errorf("%s: trigger %s: %w", errorCodeSchemaMigrationHistoryInvalid, trigger, err)
		}
		if !strings.Contains(body, "`leafen_people_guest_checks`") || !strings.Contains(body, "`leafen_page_permissions`") {
			return errors.New(errorCodeSchemaMigrationHistoryInvalid + ": trigger " + trigger + " doesn't mark the workspace's guests")
		}
	}
	return nil
}
