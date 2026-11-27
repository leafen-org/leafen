package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// After an import is published, a migration's run restructures it as its
// plan says, one step a turn: it gives its databases' properties the plan's
// names, types and option names and keeps the preserved ones as text — the
// changes a person makes in a database's property menu, so Leafen's
// property conversion converts the cells (content_graph_database_authority.go)
// — then moves pages and databases (notion_migration_restructure_moves.go).
// A merged database came over into its target as the import made it
// (notion_migration_merge.go).

// notionMigrationStep is one restructure step: a database's property
// changes, a reported property (SourceID) kept in the report, the pages'
// comments, a page or database moved under a page (TargetID), or a group of
// people mapped.
type notionMigrationStep struct {
	Kind     string
	Database notionMigrationDatabaseChange
	SourceID string
	TargetID string
	// A people step's Notion people and the members they become.
	People map[string]string
}

const (
	notionMigrationStepProperties   = "properties"
	notionMigrationStepMovePage     = "move-page"
	notionMigrationStepMoveDatabase = "move-database"
	notionMigrationStepReport       = "report"
	notionMigrationStepComments     = "comments"
	notionMigrationStepPeople       = "people"
	notionMigrationStepReportPage   = "report-page"
)

type notionMigrationPropertyChange struct {
	SourceID string
	Name     string
	Type     string
	Values   map[string]string
}

type notionMigrationDatabaseChange struct {
	SourceID   string
	Properties []notionMigrationPropertyChange
}

// notionMigrationRestructures lists the restructure steps of the plan a
// migration's import job runs, in the plan's order: property changes, then
// moves.
func notionMigrationRestructures(app core.App, ownerID string, payload notionImportJobPayload) ([]notionMigrationStep, error) {
	if payload.MigrationID == "" {
		return nil, nil
	}
	stored := struct {
		WorkspaceID  string `db:"workspace_id"`
		PlanJSON     string `db:"plan_json"`
		PlanRevision int    `db:"plan_revision"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]],[[plan_json]],[[plan_revision]] FROM {{leafen_notion_migrations}}
		WHERE [[id]]={:id} AND [[owner]]={:owner}
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
	// The plan's entries name the scanned pages and databases they change,
	// each found in the scan by its id.
	scan := notionMigrationScanStore{app: app, migrationID: payload.MigrationID, workspaceID: stored.WorkspaceID}
	databases, pages := map[string]notionMigrationDatabase{}, map[string]string{}
	keepPage := func(id string) error {
		if page, found, err := scan.page(id); err != nil || !found {
			return err
		} else {
			pages[notionMigrationSourceKey(id)] = page.ID
		}
		return nil
	}
	keepDatabase := func(id string) error {
		if database, found, err := scan.database(id); err != nil || !found {
			return err
		} else {
			databases[notionMigrationSourceKey(id)] = database
		}
		return nil
	}
	for _, planned := range plan.Pages {
		if err := errors.Join(keepPage(planned.SourceID), keepPage(planned.ParentSourceID)); err != nil {
			return nil, err
		}
	}
	for _, planned := range plan.Databases {
		if err := errors.Join(keepDatabase(planned.SourceID), keepDatabase(planned.Into), keepPage(planned.ParentSourceID)); err != nil {
			return nil, err
		}
	}
	steps, reports, moves := []notionMigrationStep{}, []notionMigrationStep{}, []notionMigrationStep{}
	for _, planned := range plan.Pages {
		if id, parent := pages[notionMigrationSourceKey(planned.SourceID)], pages[notionMigrationSourceKey(planned.ParentSourceID)]; planned.Action == "import" && id != "" && parent != "" {
			moves = append(moves, notionMigrationStep{Kind: notionMigrationStepMovePage, SourceID: id, TargetID: parent})
		}
	}
	for _, planned := range plan.Databases {
		source, found := databases[notionMigrationSourceKey(planned.SourceID)]
		if !found || (planned.Action != "import" && planned.Action != "merge") {
			continue
		}
		if parent := pages[notionMigrationSourceKey(planned.ParentSourceID)]; planned.Action == "import" && parent != "" {
			moves = append(moves, notionMigrationStep{Kind: notionMigrationStepMoveDatabase, SourceID: source.ID, TargetID: parent})
		}
		// The database's own properties, then each other data source's
		// (notion_migration_plan_sources.go).
		changes, reported := notionMigrationDatabaseChanges(plan, databases, planned, source)
		steps, reports = append(steps, changes...), append(reports, reported...)
	}
	steps = append(steps, reports...)
	if plan.Comments == "import" && !payload.CommentsLast {
		steps = append(steps, notionMigrationStep{Kind: notionMigrationStepComments})
	}
	steps = append(steps, moves...)
	// The people the plan maps, invites or leaves out (notion_migration_people.go).
	emails, err := notionMigrationInviteEmails(scan, plan.People)
	if err != nil {
		return nil, err
	}
	group := map[string]string{}
	for index, person := range plan.People {
		if target := notionMigrationPeopleTarget(person, emails); target != "" {
			group[canonicalNotionID(person.NotionUserID)] = target
		}
		if len(group) > 0 && (len(group) == notionImportedPeopleItems || index == len(plan.People)-1) {
			steps = append(steps, notionMigrationStep{Kind: notionMigrationStepPeople, TargetID: strconv.Itoa(index), People: group})
			group = map[string]string{}
		}
	}
	// A run again moves what the source moved and takes away what it
	// deleted; then what the person's Notion shows that its API doesn't
	// give is matched (notion_migration_formats_apply.go), the ledger keeps
	// how and where the run left what it moved, the check compares it with
	// Notion, and last comes the report.
	if payload.MigrationRerunSince > 0 {
		steps = append(steps, notionMigrationStep{Kind: notionMigrationStepPlace}, notionMigrationStep{Kind: notionMigrationStepRemove})
	}
	steps = append(steps, notionMigrationStep{Kind: notionMigrationStepFormats})
	// With the content done, the comments (notionMigrationRunOrder).
	if plan.Comments == "import" && payload.CommentsLast {
		steps = append(steps, notionMigrationStep{Kind: notionMigrationStepComments})
	}
	return append(steps, notionMigrationStep{Kind: notionMigrationStepLedger}, notionMigrationStep{Kind: notionMigrationStepVerify},
		notionMigrationStep{Kind: notionMigrationStepReportPage}), nil
}

func notionImportMappedID(app core.App, jobID, sourceID, kind string) (string, error) {
	var nativeID string
	err := app.DB().NewQuery(`
		SELECT [[native_id]] FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]={:kind}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID, "kind": kind}).Row(&nativeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return nativeID, err
}

// restructureNotionDatabase makes one database's property changes, its
// formulas following the names it gives (notionMigrationRenamesWithFormulas).
func restructureNotionDatabase(app core.App, job storedJob, payload notionImportJobPayload, change notionMigrationDatabaseChange) error {
	dataSourceID, err := notionImportMappedID(app, job.ID, change.SourceID, "data_source")
	if err != nil || dataSourceID == "" {
		// A database Notion didn't give was reported, not made.
		return err
	}
	pages, operations := []string{}, map[string][]graphOperation{}
	for _, property := range change.Properties {
		source := notionPropertySourceID(change.SourceID, property.SourceID)
		nativeIDs := []string{}
		if nativeID, err := notionImportMappedID(app, job.ID, source, "property"); err != nil {
			return err
		} else if nativeID != "" {
			nativeIDs = append(nativeIDs, nativeID)
		}
		// The columns a split made for it elsewhere change with it; one that
		// is another's column keeps that one's (notion_migration_split.go).
		made := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[mapping]].[[native_id]] FROM {{leafen_notion_import_mappings}} [[mapping]]
			WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='property' AND substr([[mapping]].[[source_id]], 1, length({:prefix}))={:prefix}
				AND NOT EXISTS (SELECT 1 FROM {{leafen_notion_import_mappings}} [[other]]
					WHERE [[other]].[[job_id]]={:jobId} AND [[other]].[[kind]]='property' AND [[other]].[[native_id]]=[[mapping]].[[native_id]]
						AND [[other]].[[source_id]] NOT LIKE '%:split:%')
			ORDER BY [[mapping]].[[source_id]]
		`).Bind(dbx.Params{"jobId": job.ID, "prefix": source + ":split:"}).Column(&made); err != nil {
			return err
		}
		for _, nativeID := range append(nativeIDs, made...) {
			var pageID string
			if err := app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_database_properties}} WHERE [[id]]={:id}`).
				Bind(dbx.Params{"id": nativeID}).Row(&pageID); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			stored, exists, err := loadDatabasePropertyRecord(app, pageID, nativeID)
			if err != nil {
				return err
			}
			if pageID == "" || !exists {
				continue
			}
			next := graphProperty{ID: nativeID, DataSourceID: stored.DataSourceID, Type: stored.Type, Name: stored.Name, Position: stored.Position}
			if property.Name != "" {
				next.Name = property.Name
			}
			if property.Type != "" {
				next.Type = property.Type
			}
			config := map[string]any{}
			for key, value := range stored.Config {
				config[key] = value
			}
			// The import made the options under the plan's names; a run again
			// gives each Notion option the name the plan gives it now.
			if options, ok := config["options"].([]any); ok && len(property.Values) > 0 {
				notionNames, err := notionStagedOptionNames(app, job.ID, source)
				if err != nil {
					return err
				}
				for _, option := range options {
					if fields, ok := option.(map[string]any); ok {
						if id, _ := fields["id"].(string); property.Values[notionNames[id]] != "" {
							fields["name"] = property.Values[notionNames[id]]
						}
					}
				}
			}
			if property.Type != "" {
				notionMigrationRetypeConfig(config, next.Type)
			}
			if next.Config, err = json.Marshal(config); err != nil {
				return err
			}
			if operations[pageID] == nil {
				pages = append(pages, pageID)
			}
			operations[pageID] = append(operations[pageID], graphOperation{Kind: graphOperationKindUpsertProperty, Property: &next})
		}
	}
	batches := map[string]*structuralMutationBatch{}
	for _, pageID := range pages {
		batch, err := loadStructuralMutationBatch(app, pageID, batches)
		if err != nil {
			return err
		}
		// The formulas reading what it renames read the new names.
		if operations[pageID], err = notionMigrationRenamesWithFormulas(app, pageID, operations[pageID]); err != nil {
			return err
		}
		// A relation may relate to a database on another page.
		targets := map[string]struct{}{}
		for _, operation := range operations[pageID] {
			if target := graphRelationTarget(operation.Property.Type, operation.Property.Config); target != "" {
				targets[target] = struct{}{}
			}
		}
		references, err := loadGraphReferences(app, "leafen_data_sources", targets)
		if err != nil {
			return err
		}
		sources := map[string]graphEntityReference{}
		for id, reference := range references {
			if reference.Owner == payload.AuthorityOwnerID {
				sources[id] = reference
			}
		}
		if err := applyNotionGraphOperationsWith(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner, operations[pageID],
			notionGraphApplyReferences{sources: sources}); err != nil {
			return err
		}
	}
	return saveStructuralMutationBatches(app, job.Owner, batches)
}

// restructureNotionImportChunk makes one database's changes a turn, then
// hands the job to its last step, the rows' Notion history. Asking Notion
// for the comments goes by the runner's ctx, so a stopping server ends the
// turn there and keeps what came (notion_migration_comment_asks.go); the
// other steps are Leafen's own writes of a window and end on their own.
func (r *durableJobRunner) restructureNotionImportChunk(runner context.Context, job storedJob, payload notionImportJobPayload) error {
	steps, err := notionMigrationRestructures(r.app, job.Owner, payload)
	if err != nil {
		return err
	}
	// Comments, people, a run again's places and removals, the ledger and
	// the report each go in changes of their own, outside the turn's.
	rowsRemain := false
	if payload.RestructureIndex < len(steps) {
		switch step := steps[payload.RestructureIndex]; step.Kind {
		case notionMigrationStepComments, notionMigrationStepPeople,
			notionMigrationStepPlace, notionMigrationStepRemove, notionMigrationStepLedger, notionMigrationStepVerify, notionMigrationStepReportPage:
			if _, err := authorizeNotionImportTurn(r.app, job, payload); err != nil {
				return err
			}
			ctx := context.Background()
			switch step.Kind {
			case notionMigrationStepComments:
				rowsRemain, err = r.importNotionMigrationCommentsUntilStopped(runner, job, &payload, notionMigrationPeopleNames(r.app, payload))
			case notionMigrationStepPeople:
				rowsRemain, err = r.mapNotionMigrationPeople(job, &payload, step)
			case notionMigrationStepPlace:
				rowsRemain, err = r.placeNotionMigrationMovedBlocks(ctx, job, &payload)
			case notionMigrationStepRemove:
				rowsRemain, err = r.removeNotionMigrationDeletedBlocks(ctx, job, &payload)
			case notionMigrationStepLedger:
				rowsRemain, err = recordNotionMigrationBlockStates(r.app, job, &payload)
			case notionMigrationStepVerify:
				_, err = verifyNotionMigrationRun(r.app, job.ID, payload.MigrationID)
			case notionMigrationStepReportPage:
				err = settleNotionMigrationIssues(r.app, job, payload.MigrationID)
				if err == nil {
					err = r.writeNotionMigrationReport(ctx, r.app, job, payload)
				}
			}
			if err != nil {
				return err
			}
		}
	}
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		if payload.RestructureIndex < len(steps) {
			if err := restructureNotionStep(txApp, current, payload, steps[payload.RestructureIndex]); err != nil {
				return err
			}
			if !rowsRemain {
				payload.RestructureIndex++
			}
		}
		// The rows' Notion history is set after the plan's changes, last
		// (notion_import_row_stamps.go).
		if payload.RestructureIndex >= len(steps) {
			payload.Restructuring, payload.StampingRows = false, true
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		// A turn Notion asked to wait goes on when the wait is over.
		notBefore := now.Add(durableJobReadyDelay)
		if payload.commentsAskAfter.After(notBefore) {
			notBefore = payload.commentsAskAfter
		}
		chunkIndex := current.ChunkCount + 1
		turn := sha256.Sum256([]byte("notion-migration-restructure:" + strconv.Itoa(payload.RestructureIndex)))
		if err := recordNotionImportChunk(txApp, current.ID, chunkIndex, 1, current.Cursor, hex.EncodeToString(turn[:]), now); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = 'queued', [[payload_json]] = {:payload}, [[chunk_count]] = {:chunkCount},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(encoded), "chunkCount": chunkIndex,
			"notBefore": notBefore.UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}

// notionMigrationRetypeConfig keeps of a property's settings what its new
// type reads: option types keep their options, which only a status groups.
func notionMigrationRetypeConfig(config map[string]any, kind string) {
	if !notionMigrationOptionTypes[kind] {
		delete(config, "options")
		delete(config, "groups")
		return
	}
	if kind == graphValueTypeStatus {
		return
	}
	delete(config, "groups")
	if options, ok := config["options"].([]any); ok {
		for _, option := range options {
			if fields, ok := option.(map[string]any); ok {
				delete(fields, "group")
			}
		}
	}
}

// restructureNotionStep makes one step's changes inside the turn.
func restructureNotionStep(app core.App, job storedJob, payload notionImportJobPayload, step notionMigrationStep) error {
	switch step.Kind {
	case notionMigrationStepProperties:
		return restructureNotionDatabase(app, job, payload, step.Database)
	case notionMigrationStepMovePage, notionMigrationStepMoveDatabase:
		move := restructureNotionPageMove
		if step.Kind == notionMigrationStepMoveDatabase {
			move = restructureNotionDatabaseMove
		}
		// One a person moved to the trash meanwhile stays where they left it.
		if err := move(app, job, step); !errors.Is(err, errNotionImportPassedBy) {
			return err
		}
		return countNotionMigrationChangedMeanwhile(app, payload.MigrationID, 1)
	case notionMigrationStepReport:
		if err := reportNotionMigrationProperty(app, job, payload, step); err != nil {
			return err
		}
		return clearNotionMigrationReportedProperty(app, job, payload, step)
	case notionMigrationStepFormats:
		return applyNotionMigrationFormats(app, job, payload)
	}
	return nil
}

// notionMigrationPeopleNames names the Notion people the scan met, for the
// comments they wrote.
func notionMigrationPeopleNames(app core.App, payload notionImportJobPayload) func(string) string {
	scan := notionMigrationScanStore{app: app, migrationID: payload.MigrationID}
	names := map[string]string{}
	return func(id string) string {
		if name, known := names[id]; known {
			return name
		}
		person := notionMigrationPerson{}
		if value, found, err := scan.find(notionMigrationItemPerson, id); err == nil && found {
			_ = json.Unmarshal([]byte(value), &person)
		}
		names[id] = person.Name
		return person.Name
	}
}
