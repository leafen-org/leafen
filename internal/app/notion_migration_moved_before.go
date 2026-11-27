package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What the person already moved into a workspace is moved again by running
// that move again (notion_migration_run_again.go), never by a new move of the
// same Notion roots: a new one reads all of Notion again and makes every page
// a second time (2026-11-26: told to move from Notion again after a finished
// move, an agent started a new one and the workspace held two of everything).
// So a start whose roots an earlier move of the person's into the workspace
// brought — as one of its roots, or as what its runs made inside one — is
// refused, naming that move and the way on: run it again for what changed in
// Notion since; after its run stopped, run it again to go on from what it
// made; while it runs, follow it. Only a start the person chose as a second,
// separate copy (secondCopy) goes on. A move's first run refuses the same — for
// a move started before the earlier one ran, or one whose scan holds an
// earlier move's root (a sub-page moved first, its parent after) — unless the
// move was started or is run as a second copy. A move that never ran made
// nothing, so starting another beside it stays free: the import screen starts
// a new one as the person changes what they chose, and the first of two to
// run is the one that moves. Moves from a tool's bundle are the same: their
// roots keep their IDs from one bundle to the next. Migration 865 keeps a
// start's choice of a second copy.

// notionMigrationMovedKinds are the ledger's kinds a root moved as: a page,
// a database (its first data source, by the database's ID) and a row.
const notionMigrationMovedKinds = `('page', 'data_source', 'row')`

// notionMigrationMovedBefore is a start or a first run refused for an
// earlier move of the same roots; it answers as its refusal (refusedFor).
type notionMigrationMovedBefore struct {
	earlier storedNotionMigration
	// atRun: refused at a first run. byRoot: one of its roots is one of the
	// earlier move's; else byLedger: the earlier move brought one inside its
	// own roots; else the run's scan holds the earlier move's roots.
	atRun, byRoot, byLedger bool
	refusal                 error
}

func (moved *notionMigrationMovedBefore) Error() string { return errorCodeNotionMigrationMovedBefore }

func (moved *notionMigrationMovedBefore) Unwrap() error { return moved.refusal }

// notionMigrationStartRoots are a start's roots checked, by their Notion IDs.
func notionMigrationStartRoots(given []notionImportRoot) ([]notionImportRoot, error) {
	if len(given) == 0 {
		return nil, refuse(errNotionMigrationInvalid, "roots is empty: name the Notion pages and databases to move")
	}
	roots := make([]notionImportRoot, 0, len(given))
	seen := map[string]bool{}
	for position, root := range given {
		written := root.ID
		root.ID = canonicalNotionID(root.ID)
		switch {
		case root.ID == "":
			return nil, refuse(errNotionMigrationInvalid, "roots[%d].id is a Notion page's or database's ID or address, not %q", position, written)
		case len(root.ID) > 64:
			return nil, refuse(errNotionMigrationInvalid, "roots[%d].id is at most 64 characters", position)
		case root.Kind != "page" && root.Kind != "database":
			return nil, refuse(errNotionMigrationInvalid, "roots[%d].kind is page or database, not %q", position, root.Kind)
		case seen[root.ID]:
			return nil, refuse(errNotionMigrationInvalid, "roots[%d].id names %s again", position, root.ID)
		}
		seen[root.ID] = true
		roots = append(roots, notionImportRoot{ID: root.ID, Title: strings.TrimSpace(root.Title), Kind: root.Kind})
	}
	return roots, nil
}

// notionMigrationStartMovedBefore refuses a start of roots an earlier move
// of the person's into the workspace brought, unless secondCopy.
func notionMigrationStartMovedBefore(app core.App, ownerID, workspaceID string, roots []notionImportRoot, secondCopy bool) error {
	if secondCopy {
		return nil
	}
	return notionMigrationMovedBeforeOf(app, ownerID, workspaceID, "", roots, false)
}

// notionMigrationFirstRunMovedBefore refuses a move's first run that would
// bring again what an earlier move of the person's into the workspace
// brought, unless the move was started or is run as a second copy.
func notionMigrationFirstRunMovedBefore(app core.App, stored storedNotionMigration, roots []notionImportRoot, secondCopy bool) error {
	if secondCopy || stored.RunState != "" || stored.RunStarted > 0 || stored.LastDoneStarted > 0 {
		return nil
	}
	var chosen bool
	if err := app.DB().NewQuery(`SELECT [[second_copy]] FROM {{leafen_notion_migrations}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": stored.ID}).Row(&chosen); err != nil {
		return err
	}
	if chosen {
		return nil
	}
	return notionMigrationMovedBeforeOf(app, stored.Owner, stored.WorkspaceID, stored.ID, roots, true)
}

// notionMigrationMovedBeforeOf is the refusal for the person's latest move
// into the workspace, other than self, that ran and brought one of roots —
// one of its own roots, or what its runs made from one — or, at a run, whose
// own root self's scan holds; nil for none.
func notionMigrationMovedBeforeOf(app core.App, ownerID, workspaceID, self string, roots []notionImportRoot, atRun bool) error {
	ids, sources := make([]string, 0, len(roots)), make([]string, 0, 2*len(roots))
	for _, root := range roots {
		ids, sources = append(ids, root.ID), append(sources, root.ID)
		if root.Kind == "database" {
			sources = append(sources, notionDatabaseRootPageSourceID(root.ID))
		}
	}
	idsJSON, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	sourcesJSON, err := json.Marshal(sources)
	if err != nil {
		return err
	}
	found := struct {
		ID       string `db:"id"`
		ByRoot   bool   `db:"by_root"`
		ByLedger bool   `db:"by_ledger"`
	}{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[by_root]], [[by_ledger]] FROM (
			SELECT [[earlier]].[[id]], [[earlier]].[[run_started_at]], [[earlier]].[[updated_at]],
				EXISTS (SELECT 1 FROM json_each([[earlier]].[[roots_json]]) AS [[root]]
					WHERE json_extract([[root]].[[value]], '$.id') IN (SELECT [[value]] FROM json_each({:ids}))) AS [[by_root]],
				EXISTS (SELECT 1 FROM {{leafen_notion_migration_ledger}} AS [[moved]]
					WHERE [[moved]].[[migration_id]] = [[earlier]].[[id]] AND [[moved]].[[kind]] IN ` + notionMigrationMovedKinds + `
						AND [[moved]].[[source_id]] IN (SELECT [[value]] FROM json_each({:sources}))) AS [[by_ledger]],
				EXISTS (SELECT 1 FROM json_each([[earlier]].[[roots_json]]) AS [[root]]
					JOIN {{leafen_notion_migration_scan_items}} AS [[scanned]] ON [[scanned]].[[migration_id]] = {:self}
						AND [[scanned]].[[kind]] IN ('page', 'database')
						AND [[scanned]].[[item_key]] = lower(replace(json_extract([[root]].[[value]], '$.id'), '-', ''))) AS [[by_scan]]
			FROM {{leafen_notion_migrations}} AS [[earlier]] INDEXED BY [[idx_leafen_notion_migrations_owner]]
			WHERE [[earlier]].[[workspace_id]] = {:workspace} AND [[earlier]].[[owner]] = {:owner} AND [[earlier]].[[id]] != {:self}
				AND ([[earlier]].[[run_state]] != '' OR [[earlier]].[[run_started_at]] > 0 OR [[earlier]].[[last_done_run_started_at]] > 0))
		WHERE [[by_root]] OR [[by_ledger]] OR [[by_scan]]
		ORDER BY [[run_started_at]] DESC, [[updated_at]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"workspace": workspaceID, "owner": ownerID, "self": self, "ids": string(idsJSON), "sources": string(sourcesJSON),
	}).One(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	earlier, err := loadNotionMigration(app, ownerID, workspaceID, found.ID)
	if err != nil {
		return err
	}
	moved := &notionMigrationMovedBefore{earlier: earlier, atRun: atRun, byRoot: found.ByRoot, byLedger: found.ByLedger}
	moved.refusal = refusedFor(http.StatusConflict, errorCodeNotionMigrationMovedBefore, moved.words(false),
		apiErrorField{Name: "roots", Problem: "already moved here"})
	return moved
}

// names are the earlier move's roots, by their titles where the scan read them.
func (moved *notionMigrationMovedBefore) names() string {
	roots := []notionImportRoot{}
	_ = json.Unmarshal([]byte(moved.earlier.RootsJSON), &roots)
	names := make([]string, 0, len(roots))
	for _, root := range roots {
		names = append(names, fmt.Sprintf("%q", firstNonempty(root.Title, root.ID)))
	}
	return strings.Join(names, ", ")
}

// words say why and the way on, in the REST API's field names or, for the
// agent's tools, in theirs.
func (moved *notionMigrationMovedBefore) words(tools bool) string {
	earlier := moved.earlier
	field, named, made := "Check roots: ", "what they name", "a new migration would make"
	if moved.atRun {
		field, named, made = "Check the migration: ", "what its roots name", "running this one would make"
	}
	why := field + moved.names() + " came into this workspace with migration " + earlier.ID + ", so " + made +
		" every page of it a second time."
	if !moved.byRoot {
		why = field + named + " came into this workspace inside " + moved.names() + " with migration " + earlier.ID +
			", so " + made + " those pages a second time."
	}
	if !moved.byRoot && !moved.byLedger {
		why = field + "its scan holds " + moved.names() + ", which came into this workspace with migration " + earlier.ID +
			", so " + made + " those pages a second time."
	}
	run := notionMigrationRunCall(earlier.ID, earlier.PlanRevision, tools)
	follow := "GET …/notion-migrations/" + earlier.ID
	again, copied := `start again with "secondCopy": true.`, `run it with "secondCopy": true.`
	if tools {
		follow = fmt.Sprintf(`leafen_notion_migration_status {"migration_id": %q, "wait_seconds": 45}`, earlier.ID)
		again, copied = "start again with second_copy true.", "run it with second_copy true."
	}
	way := " Run that one again now, " + notionMigrationRunAgainWay(earlier.ID, earlier.PlanRevision, tools)
	switch earlier.RunState {
	case notionMigrationRunRunning:
		way = " It is moving now: follow it with " + follow + ", and once it is done run it again for what changed in Notion since."
	case notionMigrationRunFailed:
		way = " Its last run stopped: run it again to go on from what it made: " + run + "."
	}
	only := " Only if the person chose a second, separate copy, " + again
	if moved.atRun {
		only = " Only if the person chose a second, separate copy, " + copied
	}
	return why + way + only
}

// notionMigrationRunCall is the call that runs a migration again, in the
// REST API's words or, for the agent's tools, in theirs.
func notionMigrationRunCall(migrationID string, planRevision int, tools bool) string {
	if tools {
		return fmt.Sprintf(`leafen_notion_migration_run {"migration_id": %q, "plan_revision": %d} (through leafen_call where your list leaves it out)`,
			migrationID, planRevision)
	}
	return fmt.Sprintf(`POST …/notion-migrations/%s/run with {"requestId", "planRevision": %d}`, migrationID, planRevision)
}

// notionMigrationRunAgainWay is how a move made already moves again when
// the person asks to move from Notion: run again on the plan they agreed,
// with nothing new to ask them. The start's refusal and a done move's
// status and plan (notionMigrationAskedAgain) say it in these same words
// (2026-11-26, Codex runs: told by the refusal to run it again, agents read
// the done move's status, which said only that a report was left, and
// reported it moved without running it, so what Notion had gained never
// came).
func notionMigrationRunAgainWay(migrationID string, planRevision int, tools bool) string {
	return "on the plan the person agreed — no new scan and nothing new to ask them: " +
		notionMigrationRunCall(migrationID, planRevision, tools) + " brings what changed in Notion since."
}

// notionMigrationAskedAgain leads what an agent reads of a move whose run is
// done — its status (notionMigrationDoneAgain) and its plan
// (withNotionMigrationAskedAgain) — with the run again, for a person who
// asked to move from Notion after it ended: the move made already, whose
// plan they agreed, so no question waits on them. Else the answer goes on
// as for a run the conversation made. "" for a move whose run isn't done.
func notionMigrationAskedAgain(stored storedNotionMigration) string {
	if stored.RunState != notionMigrationRunDone {
		return ""
	}
	return "If the person asked you to move from Notion after this move's last run ended (you didn't run it in this conversation), " +
		"it is the move made already, and what they ask for is what changed in Notion since: run it again now, " +
		notionMigrationRunAgainWay(stored.ID, stored.PlanRevision, true) + " Else: "
}

// withNotionMigrationAskedAgain leads a plan's answer with the run again
// when its move's run is done (notionMigrationAskedAgain): read after the
// start's refusal, the one question it said to ask, as for a move that
// never ran, had Codex runs ask the person again before running (2026-11-26).
func withNotionMigrationAskedAgain(app core.App, ownerID, workspaceID string, output leafenMCPNotionMigrationPlanOutput) leafenMCPNotionMigrationPlanOutput {
	stored, err := loadNotionMigration(app, ownerID, workspaceID, output.MigrationID)
	if err != nil {
		return output
	}
	if again := notionMigrationAskedAgain(stored); again != "" {
		output.Next = again + output.Next
	}
	return output
}

// mcpNotionMigrationMovedBefore is a start or run refused for an earlier move
// of the same roots as the agent's tools say it, or nil.
func mcpNotionMigrationMovedBefore(err error) error {
	var moved *notionMigrationMovedBefore
	if !errors.As(err, &moved) {
		return nil
	}
	return mcpInvalid("%s", moved.words(true))
}

// mcpNotionMigrationStartNotMade is why the start made no migration, for a
// failure that isn't a refusal of its input.
func mcpNotionMigrationStartNotMade(err error) error {
	if moved := mcpNotionMigrationMovedBefore(err); moved != nil {
		return moved
	}
	return mcpInvalid("connection_id names a Notion connection of this workspace (leafen_notion_migration_sources), and roots the Notion pages to move")
}

// mcpNotionMigrationRunNotStarted is why the run didn't start, for a failure
// its tool doesn't name otherwise.
func mcpNotionMigrationRunNotStarted(err error) error {
	if moved := mcpNotionMigrationMovedBefore(err); moved != nil {
		return moved
	}
	return mcpInvalid("the run didn't start: check migration_id and plan_revision against leafen_notion_migration_plan")
}
