package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionImportPageDiscovery struct {
	SourceID   string `db:"source_id"`
	Kind       string `db:"kind"`
	ParentKind string `db:"parent_kind"`
	Blueprint  bool   `db:"blueprint"`
	Cursor     string `db:"discovery_cursor"`
	Position   int    `db:"discovery_position"`
}

type notionImportPageDiscoveryAdvance struct {
	SourceID         string
	Kind             string
	ExpectedCursor   string
	ExpectedPosition int
	NextCursor       string
	NextPosition     int
	Complete         bool
}

func loadNotionImportPageDiscovery(app core.App, jobID string) (notionImportPageDiscovery, bool, error) {
	stored := notionImportPageDiscovery{}
	err := app.DB().NewQuery(`
		SELECT [[source_id]], [[kind]], [[parent_kind]], [[blueprint]],
			[[discovery_cursor]], [[discovery_position]]
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[kind]] IN (` + notionImportPendingSourceKindsSQL + `)
			AND [[discovery_status]] = 'pending'
		ORDER BY [[sequence]]
		LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return notionImportPageDiscovery{}, false, nil
	}
	return stored, err == nil, err
}

func (r *durableJobRunner) discoverNotionPageChunk(
	ctx context.Context,
	job storedJob,
	payload notionImportJobPayload,
	client notionProviderClient,
	discovery notionImportPageDiscovery,
) error {
	noteNotionMigrationLiveSource(r.app, payload.MigrationID, job.ID, discovery.SourceID, discovery.Kind)
	presentationSources, presentationFiles := []stagedNotionSource(nil), []string(nil)
	if discovery.Kind == "page" && discovery.Cursor == "" && discovery.Position == 0 {
		var err error
		if presentationSources, presentationFiles, err = recordNotionImportPagePresentation(
			ctx, r.app, client, job.ID, discovery.SourceID,
		); err != nil {
			return err
		}
	}
	children, err := client.children(ctx, discovery.SourceID, discovery.Cursor)
	if notionProviderRefused(err) {
		// A plain heading is read in case it holds blocks: none given is none
		// (notion_import_heading_children.go).
		plain, plainErr := notionImportPlainHeadingSource(r.app, job.ID, discovery)
		if plainErr != nil {
			return plainErr
		}
		if plain {
			return r.commitNotionDiscoveryWithPage(job, payload, nil, &notionImportPageDiscoveryAdvance{
				SourceID: discovery.SourceID, Kind: discovery.Kind, ExpectedCursor: discovery.Cursor,
				ExpectedPosition: discovery.Position, NextCursor: "", NextPosition: discovery.Position, Complete: true,
			})
		}
		// A sub-page the connection lost, or a block Notion won't list: what is
		// under it is reported where it was, and the rest of the import goes on.
		issue, issueErr := notionImportChildrenRefusedIssue(r.app, job.ID, discovery.SourceID)
		if issueErr != nil {
			return issueErr
		}
		fieldPages, fieldErr := notionImportFieldPageSources(ctx, r.app, client, job.ID, discovery, nil)
		if fieldErr != nil {
			return fieldErr
		}
		issue = append(issue, fieldPages...)
		return r.commitNotionDiscoveryWithPage(job, payload, append(presentationSources, issue...), &notionImportPageDiscoveryAdvance{
			SourceID: discovery.SourceID, Kind: discovery.Kind, ExpectedCursor: discovery.Cursor,
			ExpectedPosition: discovery.Position, NextCursor: "", NextPosition: discovery.Position, Complete: true,
		})
	}
	if err != nil {
		return err
	}
	results, err := notionProviderPageResults(children)
	if err != nil {
		return err
	}
	nextCursor, err := notionProviderContinuationCursor(discovery.Cursor, children)
	if err != nil {
		return err
	}
	// A sub-page's own blocks sit on that page; any other block's children
	// sit inside it.
	parentKind := "block"
	if discovery.Kind == "page" || discovery.Kind == "row" {
		parentKind = discovery.Kind
	}
	runPlan, err := loadNotionMigrationRunPlan(r.app, job.Owner, payload)
	if err != nil {
		return err
	}
	// A callout with no words whose first line is empty holds that line
	// first; its children follow it.
	startPosition, staged := discovery.Position, make([]stagedNotionSource, 0, len(results)+1)
	emptyLine, err := notionImportCalloutEmptyFirstLine(r.app, job.ID, discovery, results)
	if err != nil {
		return err
	}
	if emptyLine != nil {
		startPosition, staged = 1, append(staged, *emptyLine)
	}
	sources, databaseIDs, fileIDs, err := stageNotionPageChildren(
		r.app, discovery.SourceID, parentKind, discovery.Blueprint || discovery.Kind == "template",
		startPosition, runPlan.keep(results), staged,
	)
	if err == nil {
		// Childless plain headings are read as their page's Markdown says
		// (notion_import_heading_children.go).
		err = notionImportHeadingReads(ctx, r.app, client, job.ID, discovery.SourceID, parentKind,
			discovery.Blueprint || discovery.Kind == "template", runPlan.keep(results), sources)
	}
	if err == nil {
		// The data source mentions Notion's API left out of its lines come
		// back from their page's Markdown (notion_import_data_source_mentions.go).
		err = notionImportDataSourceMentionReads(ctx, r.app, client, job.ID, discovery.SourceID, parentKind,
			discovery.Blueprint || discovery.Kind == "template", runPlan.keep(results), sources)
	}
	if err != nil {
		return err
	}
	// When its blocks were made bounds the search for pages hanging on them
	// (notion_import_unmentioned_pages.go).
	payload.noteUnmentionedParents(runPlan.keep(results), discovery.Blueprint || discovery.Kind == "template")
	if err := nameNotionImportOutsideLinks(ctx, r.app, client, job.ID, sources); err != nil {
		return err
	}
	// The pages hanging on its lines come after them (notion_import_line_pages.go).
	linePages, err := notionImportLinePageSources(
		ctx, r.app, client, job.ID, runPlan.keep(results), discovery.Blueprint || discovery.Kind == "template",
	)
	if err != nil {
		return err
	}
	sources = append(append(sources, linePages...), presentationSources...)
	// Those hanging on its title or cells once its whole body is staged, so
	// that a sub-page it lists stays one (notion_import_field_pages.go).
	if !children.HasMore {
		fieldPages, err := notionImportFieldPageSources(ctx, r.app, client, job.ID, discovery, sources)
		if err != nil {
			return err
		}
		sources = append(sources, fieldPages...)
	}
	payload.PendingDatabaseIDs = append(payload.PendingDatabaseIDs, databaseIDs...)
	payload.PendingFileIDs = append(append(payload.PendingFileIDs, fileIDs...), presentationFiles...)
	advance := &notionImportPageDiscoveryAdvance{
		SourceID: discovery.SourceID, Kind: discovery.Kind, ExpectedCursor: discovery.Cursor,
		ExpectedPosition: discovery.Position, NextCursor: nextCursor,
		NextPosition: startPosition + len(results), Complete: !children.HasMore,
	}
	return r.commitNotionDiscoveryWithPage(job, payload, sources, advance)
}

// notionImportChildrenRefusedIssue is the report of a staged block or page whose
// children Notion refused. It stands where the source does; a block's type is
// the one it was staged with.
func notionImportChildrenRefusedIssue(app core.App, jobID, sourceID string) ([]stagedNotionSource, error) {
	stored := storedNotionSource{}
	err := app.DB().NewQuery(`
		SELECT [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[text]]
		FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
	`).Bind(dbx.Params{"jobId": jobID, "sourceId": sourceID}).One(&stored)
	if err != nil {
		return nil, err
	}
	blockType := stored.Kind
	switch stored.Kind {
	case "page":
		blockType = "child_page"
	case "row":
		blockType = "page"
	case "block":
		if plan, err := decodeNotionParagraphSourceText(stored.Text); err == nil {
			blockType = notionImportStagedIssueType(stored, plan)
		}
	}
	return []stagedNotionSource{{
		SourceID: stored.SourceID + "#" + notionImportIssueInvalidBlock, Kind: "issue",
		ParentSourceID: stored.ParentSourceID, ParentKind: stored.ParentKind, Position: stored.Position,
		Title: blockType, Text: notionImportIssueInvalidBlock,
	}}, nil
}

func advanceNotionImportPageDiscovery(
	app core.App,
	jobID string,
	advance *notionImportPageDiscoveryAdvance,
) error {
	if advance == nil {
		return nil
	}
	status := notionImportPageDiscoveryPending
	if advance.Complete {
		status = notionImportPageDiscoveryComplete
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_sources}}
		SET [[discovery_status]] = {:status}, [[discovery_cursor]] = {:nextCursor},
			[[discovery_position]] = {:nextPosition}
		WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId} AND [[kind]] = {:kind}
			AND [[discovery_status]] = 'pending'
			AND [[discovery_cursor]] = {:expectedCursor}
			AND [[discovery_position]] = {:expectedPosition}
	`).Bind(dbx.Params{
		"status": status, "nextCursor": advance.NextCursor, "nextPosition": advance.NextPosition,
		"jobId": jobID, "sourceId": advance.SourceID, "kind": advance.Kind,
		"expectedCursor": advance.ExpectedCursor, "expectedPosition": advance.ExpectedPosition,
	}).Execute()
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return errors.New(errorCodeNotionImportCreateFailed)
	}
	return nil
}
