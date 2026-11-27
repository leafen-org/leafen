package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A copy into another workspace — a page moved there, which is copied, or a
// database's block copied there — keeps its person values as they are, so
// they may name people who aren't that workspace's: the first workspace's
// members, whom the copy's maker read there. Their names come along, kept
// by the copy's top page, and read as a person mention's written name does
// (person_mention_names.go): a person the workspace doesn't know reads by
// the name they were written with — here, the name the copy found them by —
// renamed or gone since. Only a reader who can open the copy's top page
// reads it, as only they read the values that name them (user 2026-11-23:
// the name shows only where the copied content that names it can be read,
// with no lookup of the server's accounts by ID outside it); nobody is
// invited, added or offered to pick. Such a person reads as one who isn't
// the workspace's: named, not offered in a picker, their mention opening no
// profile (workspacePerson.Deactivated). The screen and the app's API
// (lookupWorkspacePeople), the MCP's users (notion-get-users) and an export
// (workspaceExportDocument.loadPeople) name them so. Their kept name stays
// when their account is deleted, as a mention's written name does
// (account_deletion_workspace_inventory.go); it goes with the copy's top
// page, deleted for good.

const workspaceCarriedPeopleTable = "leafen_workspace_carried_people"

func ensureNativeWorkspaceCarriedPeopleSchema(app core.App) error {
	for _, statement := range []string{
		`CREATE TABLE {{leafen_workspace_carried_people}} (
			[[workspace_id]] TEXT NOT NULL,
			[[page_id]] TEXT NOT NULL REFERENCES {{leafen_pages}} ([[id]]) ON DELETE CASCADE,
			[[person_id]] TEXT NOT NULL,
			[[name]] TEXT NOT NULL CHECK (length([[name]]) > 0),
			[[carried_at]] INTEGER NOT NULL CHECK ([[carried_at]] > 0),
			PRIMARY KEY ([[page_id]], [[person_id]])
		)`,
		`CREATE INDEX [[idx_leafen_workspace_carried_people_person]]
			ON {{leafen_workspace_carried_people}} ([[workspace_id]], [[person_id]])`,
	} {
		if _, err := app.DB().NewQuery(statement).Execute(); err != nil {
			return err
		}
	}
	if err := trackInstanceProductExportSourceRevisions(app, workspaceCarriedPeopleTable); err != nil {
		return err
	}
	return validateNativeWorkspaceCarriedPeopleSchema(app)
}

func validateNativeWorkspaceCarriedPeopleSchema(app core.App) error {
	var columns, indexes int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_workspace_carried_people')
		WHERE [[name]] IN ('workspace_id', 'page_id', 'person_id', 'name', 'carried_at') AND [[notnull]] = 1`).Row(&columns); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM [[sqlite_master]]
		WHERE [[type]] = 'index' AND [[name]] = 'idx_leafen_workspace_carried_people_person'`).Row(&indexes); err != nil {
		return err
	}
	if columns != 5 || indexes != 1 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}

// keepPageDuplicationCarriedPeople keeps, as a copy into another workspace
// ends, the names of the people its databases' person values name who
// aren't that workspace's, by the copy's top page.
func keepPageDuplicationCarriedPeople(app core.App, job storedPageDuplicationJob) error {
	if !job.crossWorkspace() {
		return nil
	}
	return keepCarriedPeople(app, job.sourceWorkspace(), job.WorkspaceID, job.TargetRootPageID, `
		(SELECT [[target_id]] FROM {{leafen_page_duplication_identity_map}}
			WHERE [[job_id]] = {:jobId} AND [[kind]] = 'page')`, dbx.Params{"jobId": job.ID})
}

// keepBlockCopyCarriedPeople is keepPageDuplicationCarriedPeople for a
// database's block copied from another workspace, by the page it went to.
func keepBlockCopyCarriedPeople(app core.App, job storedBlockMoveJob) error {
	if !job.crossWorkspace() {
		return nil
	}
	return keepCarriedPeople(app, job.sourceWorkspace(), job.WorkspaceID, job.DestinationPageID,
		`({:pageId})`, dbx.Params{"pageId": job.DestinationPageID})
}

// keepCarriedPeople keeps the names of the people the copy's pages (pages,
// SQL naming their IDs in parentheses) name — in their person values, in
// their text's person mentions, and in their comments' mentions (a page
// moved there brings its comments, page_workspace_move.go) — those the
// source workspace names — its owner and members, or people a copy brought
// it — and the target doesn't.
func keepCarriedPeople(app core.App, sourceWorkspaceID, targetWorkspaceID, topPageID, pages string, params dbx.Params) error {
	ids := []string{}
	params["workspaceId"] = targetWorkspaceID
	if err := app.DB().NewQuery(`
		SELECT [[person]].[[value]] FROM {{leafen_database_row_values}} [[cell]], json_each([[cell]].[[value]]) [[person]]
		WHERE [[cell]].[[workspace_id]] = {:workspaceId} AND [[cell]].[[type]] = 'person'
			AND json_valid([[cell]].[[value]]) AND json_type([[cell]].[[value]]) = 'array'
			AND [[person]].[[type]] = 'text' AND [[cell]].[[page_id]] IN ` + pages + `
		UNION
		SELECT json_extract([[span]].[[value]], '$.userId') FROM {{leafen_comments}} [[comment]],
			json_each([[comment]].[[body_json]], '$.rich') [[span]]
		WHERE [[comment]].[[workspace_id]] = {:workspaceId} AND [[comment]].[[page_id]] IN ` + pages + `
			AND json_valid([[comment]].[[body_json]]) AND json_extract([[span]].[[value]], '$.mention') = 'person'
		ORDER BY 1
	`).Bind(params).Column(&ids); err != nil {
		return err
	}
	mentioned, err := carriedTextPeople(app, pages, params)
	if err != nil {
		return err
	}
	ids = slices.Compact(slices.Sorted(slices.Values(append(ids, mentioned...))))
	if len(ids) == 0 {
		return nil
	}
	known, err := personMentionNames(app, targetWorkspaceID, ids)
	if err != nil {
		return err
	}
	source, err := personMentionNames(app, sourceWorkspaceID, ids)
	if err != nil {
		return err
	}
	brought, err := workspaceCarriedPeopleNames(app, sourceWorkspaceID, ids)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	for _, id := range ids {
		name := source[id]
		if name == "" {
			name = brought[id]
		}
		if name == "" || known[id] != "" || notionBotUserIDShape(id) {
			continue
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_workspace_carried_people}} ([[workspace_id]], [[page_id]], [[person_id]], [[name]], [[carried_at]])
			VALUES ({:workspaceId}, {:pageId}, {:personId}, {:name}, {:now})
			ON CONFLICT ([[page_id]], [[person_id]]) DO UPDATE SET [[name]] = excluded.[[name]], [[carried_at]] = excluded.[[carried_at]]
		`).Bind(dbx.Params{
			"workspaceId": targetWorkspaceID, "pageId": topPageID, "personId": id, "name": strings.TrimSpace(name), "now": now,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// carriedTextPeople are the people the copy's pages' text @mentions, read
// from the text itself at the copy's end: the mentions' projection
// (page_person_mention_notifications.go) comes later, from the page's
// reading for reminders, and keeps only people who may open the page — not
// one who isn't the workspace's (2026-11-23: a page duplicated into another
// workspace read its mention of the first workspace's member unnamed).
func carriedTextPeople(app core.App, pages string, params dbx.Params) ([]string, error) {
	pageIDs := []string{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_pages}} WHERE [[id]] IN ` + pages + ` ORDER BY [[id]]`).
		Bind(params).Column(&pageIDs); err != nil {
		return nil, err
	}
	people := []string{}
	for _, pageID := range pageIDs {
		doc, found, err := loadYgoDoc(app, pageID)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		blocks := []storedReminderBlock{}
		if err := app.DB().NewQuery(`
			SELECT [[id]], [[type]], [[last_edited_by]],
				CASE WHEN instr([[content]], '"mention"') > 0 THEN [[content]] ELSE '' END AS [[mention_content]]
			FROM {{leafen_blocks}} WHERE [[page_id]] = {:pageId}
		`).Bind(dbx.Params{"pageId": pageID}).All(&blocks); err != nil {
			return nil, err
		}
		for _, block := range blocks {
			people = append(people, blockTextPeople(blockMentionText(doc, block))...)
		}
	}
	return people, nil
}

type workspaceCarriedPerson struct {
	PersonID string `db:"person_id"`
	PageID   string `db:"page_id"`
	Name     string `db:"name"`
}

func loadWorkspaceCarriedPeople(app core.App, workspaceID string, ids []string) ([]workspaceCarriedPerson, error) {
	rows := []workspaceCarriedPerson{}
	if len(ids) == 0 {
		return rows, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	err = app.DB().NewQuery(`
		SELECT [[person_id]], [[page_id]], [[name]] FROM {{leafen_workspace_carried_people}}
		WHERE [[workspace_id]] = {:workspaceId} AND [[person_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[person_id]], [[carried_at]] DESC, [[page_id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "ids": string(encoded)}).All(&rows)
	return rows, err
}

// workspaceCarriedPeopleNames are the kept names of these people in the
// workspace, for the workspace's own reading: a copy onward, an export of
// the content that names them.
func workspaceCarriedPeopleNames(app core.App, workspaceID string, ids []string) (map[string]string, error) {
	rows, err := loadWorkspaceCarriedPeople(app, workspaceID, ids)
	names := map[string]string{}
	for _, row := range rows {
		if names[row.PersonID] == "" {
			names[row.PersonID] = row.Name
		}
	}
	return names, err
}

// lookupWorkspaceCarriedPeople names those of ids a copy brought into the
// workspace for a reader who can open a top page that keeps them, as people
// who aren't the workspace's.
func lookupWorkspaceCarriedPeople(app core.App, readerID, workspaceID string, ids []string) ([]workspacePerson, error) {
	rows, err := loadWorkspaceCarriedPeople(app, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	readable := map[string]bool{}
	named := map[string]bool{}
	people := []workspacePerson{}
	for _, row := range rows {
		if named[row.PersonID] {
			continue
		}
		reads, checked := readable[row.PageID]
		if !checked {
			_, err := authorizePage(app, readerID, row.PageID, permissionWorkspaceRead)
			if err != nil && !isPermissionDenied(err) && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			reads = err == nil
			readable[row.PageID] = reads
		}
		if reads {
			named[row.PersonID] = true
			people = append(people, workspacePerson{UserID: row.PersonID, DisplayName: row.Name, Deactivated: true})
		}
	}
	return people, nil
}

// withWorkspaceCarriedPeople adds to people those of ids a copy brought
// into the workspace whom people doesn't name, as the reader reads them.
func withWorkspaceCarriedPeople(app core.App, readerID, workspaceID string, ids []string, people []workspacePerson) ([]workspacePerson, error) {
	named := map[string]bool{}
	for _, person := range people {
		named[person.UserID] = true
	}
	rest := []string{}
	for _, id := range ids {
		if !named[id] {
			rest = append(rest, id)
		}
	}
	carried, err := lookupWorkspaceCarriedPeople(app, readerID, workspaceID, rest)
	return append(people, carried...), err
}

// carriedNotionDirectoryUser is a person a copy brought into the workspace
// as the MCP's users name them for a reader of the copy: by the kept name,
// with no email and no picture.
func carriedNotionDirectoryUser(app core.App, readerID, workspaceID, personID string) (storedNotionDirectoryUser, bool, error) {
	people, err := lookupWorkspaceCarriedPeople(app, readerID, workspaceID, []string{personID})
	if err != nil || len(people) == 0 {
		return storedNotionDirectoryUser{}, false, err
	}
	return storedNotionDirectoryUser{ID: people[0].UserID, Name: people[0].DisplayName}, true, nil
}

// loadCarriedPeople names, in an export, the people a copy brought into the
// workspace who aren't its own by the names the copy kept, as the app does.
func (document *workspaceExportDocument) loadCarriedPeople(app core.App, ids []string) error {
	carried, err := workspaceCarriedPeopleNames(app, document.WorkspaceID, ids)
	if err != nil || len(carried) == 0 {
		return err
	}
	wanted := make([]string, 0, len(carried))
	for id := range carried {
		wanted = append(wanted, id)
	}
	own, err := personMentionNames(app, document.WorkspaceID, wanted)
	if err != nil {
		return err
	}
	for id, name := range carried {
		if own[id] == "" {
			document.People[id] = name
		}
	}
	return nil
}
