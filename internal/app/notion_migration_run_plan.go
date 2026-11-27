package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What the import engine reads from the plan a migration runs: the pages
// and databases it leaves out, the titles it gives them and where the
// import goes. Discovery leaves a skipped page or database — and what is
// under it — unread; apply gives titles and places the import's top under
// the plan's page, and makes a merged database's rows in the one it goes
// into (notion_migration_merge.go) and a split one's in the one each goes to
// (notion_migration_split.go). A run reads the plan revision it was started
// with.

type notionMigrationRunPlan struct {
	DestinationPageID string
	Files             string
	skipped           map[string]bool
	titles            map[string]string
	// The databases merged into another, by key: the one each goes into.
	merges map[string]string
	// Each planned property as the plan leaves it, by database (or data
	// source) key and property key.
	columns map[string]map[string]notionMigrationPlanProperty
	// The staged source of each merge target, and of each database bringing
	// columns into one, found once.
	targets map[string]string
	// The splits of each database, by key, in the plan's order
	// (notion_migration_split.go).
	splits map[string][]notionMigrationPlanSplit
	// The workspace member the plan gives each Notion person, by Notion ID,
	// and what it does with each other one (notion_migration_people.go).
	people  map[string]string
	choices map[string]string
	// The split targets a window made a database's columns in, and the
	// formulas being made there (notion_migration_split_columns.go).
	splitMade, splitReading map[string]bool
	// Each formula's and rollup's definition in a target, found once
	// (notion_migration_merge_computed.go).
	definitions map[string]string
}

// loadNotionMigrationRunPlan reads the plan of the migration an import job
// runs, or nil for an import on its own.
func loadNotionMigrationRunPlan(app core.App, ownerID string, payload notionImportJobPayload) (*notionMigrationRunPlan, error) {
	if payload.MigrationID == "" {
		return nil, nil
	}
	stored := struct {
		PlanJSON     string `db:"plan_json"`
		PlanRevision int    `db:"plan_revision"`
		WorkspaceID  string `db:"workspace_id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[plan_json]],[[plan_revision]],[[workspace_id]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id} AND [[owner]]={:owner}
	`).Bind(dbx.Params{"id": payload.MigrationID, "owner": ownerID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && stored.PlanRevision != payload.MigrationPlanRevision) {
		return nil, errNotionMigrationPlanConflict
	}
	if err != nil {
		return nil, err
	}
	plan := notionMigrationPlan{}
	if err := json.Unmarshal([]byte(stored.PlanJSON), &plan); err != nil {
		return nil, err
	}
	run := &notionMigrationRunPlan{
		DestinationPageID: strings.TrimSpace(plan.DestinationPageID), Files: plan.Files,
		skipped: map[string]bool{}, titles: map[string]string{},
		merges: map[string]string{}, columns: notionMigrationPlanColumns(plan), targets: map[string]string{},
		splits: map[string][]notionMigrationPlanSplit{}, people: map[string]string{}, choices: map[string]string{},
	}
	for _, person := range plan.People {
		id, member := strings.TrimSpace(person.NotionUserID), strings.TrimSpace(person.MemberID)
		if id != "" && member != "" {
			run.people[id] = member
		}
		run.choices[id] = notionMigrationPersonChoice(person)
	}
	place := func(sourceID, action, title string) {
		key := notionMigrationSourceKey(sourceID)
		run.skipped[key] = action == "skip"
		if title = strings.TrimSpace(title); title != "" {
			run.titles[key] = title
		}
	}
	for _, page := range plan.Pages {
		place(page.SourceID, page.Action, page.Title)
	}
	for _, database := range plan.Databases {
		place(database.SourceID, database.Action, database.Title)
		key := notionMigrationSourceKey(database.SourceID)
		if database.Action == "merge" {
			run.merges[key] = database.Into
		}
		if database.Action == "import" && len(database.Split) > 0 {
			run.splits[key] = database.Split
		}
	}
	return run, nil
}

// notionMigrationPlanColumns is each property a plan lists as it leaves it,
// by database key and property key: a data source after the first has its
// properties staged under its own key (notion_import_data_sources.go).
func notionMigrationPlanColumns(plan notionMigrationPlan) map[string]map[string]notionMigrationPlanProperty {
	columns := map[string]map[string]notionMigrationPlanProperty{}
	add := func(sourceID string, properties []notionMigrationPlanProperty) {
		key := notionMigrationSourceKey(sourceID)
		columns[key] = map[string]notionMigrationPlanProperty{}
		for _, property := range properties {
			columns[key][notionMigrationPropertyKey(property.SourceID)] = property
		}
	}
	for _, database := range plan.Databases {
		add(database.SourceID, database.Properties)
		for _, source := range database.DataSources {
			add(notionImportDataSourceKey(database.SourceID, source.SourceID), source.Properties)
		}
	}
	return columns
}

// keep drops the sub-pages and databases the plan leaves out from a page's
// blocks, before discovery stages them.
func (run *notionMigrationRunPlan) keep(blocks []notionProviderObject) []notionProviderObject {
	if run == nil {
		return blocks
	}
	kept := make([]notionProviderObject, 0, len(blocks))
	for _, block := range blocks {
		if (block.Type == "child_page" || block.Type == "child_database") && run.skips(block.ID) {
			continue
		}
		kept = append(kept, block)
	}
	return kept
}

// member is the workspace member the plan gives a Notion person, or nothing.
func (run *notionMigrationRunPlan) member(notionUserID string) string {
	if run == nil {
		return ""
	}
	return run.people[strings.TrimSpace(notionUserID)]
}

// plate is the kept Notion person or integration who stands as maker or
// editor beside the person running the migration: one the plan maps to no
// member and doesn't leave out, by the ID their values hold; "" for none.
func (run *notionMigrationRunPlan) plate(notionUserID string) string {
	id := strings.TrimSpace(notionUserID)
	if run == nil || id == "" || run.people[id] != "" || run.choices[id] == notionMigrationChoiceClear {
		return ""
	}
	return importedPeopleSourcePrefix + canonicalNotionID(id)
}

func (run *notionMigrationRunPlan) skips(sourceID string) bool {
	return run != nil && run.skipped[notionMigrationSourceKey(sourceID)]
}

// title gives a staged page or database the plan's title for it; a
// full-page database's page takes its database's.
func (run *notionMigrationRunPlan) title(source storedNotionSource) string {
	if run == nil || (source.Kind != "page" && source.Kind != "database") ||
		(source.Kind == "database" && source.ParentKind == notionImportPropertyIssueParentKind) {
		return source.Title
	}
	if title, ok := run.titles[notionMigrationSourceKey(strings.TrimPrefix(source.SourceID, notionDatabaseRootPageSourceID("")))]; ok {
		return title
	}
	return source.Title
}

// destination is the page the import's top goes under, which the person
// must still be able to edit in the import's workspace, out of the trash: a
// destination that went to the trash since the plan was agreed stops the
// run, its plan's check then saying why (notionMigrationDestinationProblem).
func (run *notionMigrationRunPlan) destination(app core.App, job storedJob) (string, error) {
	if run == nil || run.DestinationPageID == "" {
		return "", nil
	}
	code, err := notionMigrationDestinationProblem(app, job.Owner, job.WorkspaceID, run.DestinationPageID)
	if err != nil {
		return "", err
	}
	if code != "" {
		return "", errors.New(errorCodeNotionMigrationPlanBlocked)
	}
	return run.DestinationPageID, nil
}

// notionMigrationDestinationProblem is what keeps a migration's pages from
// going under the page its plan names, "" for nothing: a page the person
// may not edit in the migration's workspace or deleted for good, or one in
// the trash — itself or under a page that is —, told as such to one who may
// edit it, as an import's place is (workspace_import_target_gone.go), so
// they restore it or choose another place. A migration under a page in the
// trash went into the trash with it, out of sight.
func notionMigrationDestinationProblem(app core.App, ownerID, workspaceID, pageID string) (string, error) {
	page, err := authorizePage(app, ownerID, pageID, permissionWorkspaceWrite)
	if err != nil || page.Workspace.WorkspaceID != workspaceID {
		return notionMigrationProblemDestinationInvalid, nil
	}
	gone := workspaceImportTargetGone(app, pageID)
	if refusal, refused := workspaceImportTargetGoneRefusal(gone); refused {
		if refusal.code == errorCodePageInTrash {
			return notionMigrationProblemDestinationInTrash, nil
		}
		return notionMigrationProblemDestinationInvalid, nil
	}
	return "", gone
}

// skipsFiles reports whether the plan leaves the import's files in Notion;
// pages' and databases' icons and covers still come.
func (run *notionMigrationRunPlan) skipsFiles() bool {
	return run != nil && run.Files == "skip"
}
