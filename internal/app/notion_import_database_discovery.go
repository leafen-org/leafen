package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionDatabaseRowValuePlan struct {
	Type  string          `json:"type"`
	Value any             `json:"value"`
	Spans []graphTextSpan `json:"spans,omitempty"`
}

func (r *durableJobRunner) discoverNotionDatabaseChunk(
	ctx context.Context,
	job storedJob,
	payload notionImportJobPayload,
	client notionProviderClient,
) error {
	databaseID := payload.PendingDatabaseIDs[0]
	if payload.ActiveDatabaseID != "" && payload.ActiveDatabaseID != databaseID {
		return errors.New(errorCodeNotionImportProviderFailed)
	}
	if payload.DatabaseSourceKey != "" && !payload.DatabasePropertiesComplete {
		return r.discoverNotionDataSourceProperties(ctx, job, payload, client)
	}
	propertiesComplete := payload.DatabasePropertiesComplete
	if payload.ActiveDatabaseID != "" && payload.DatabasePropertyFingerprint == "" && payload.DatabasePropertyCursor == "" {
		// Jobs admitted before property continuations already committed the full
		// property set before persisting ActiveDatabaseID. Preserve their direct
		// database-row resume point instead of replaying provider metadata.
		propertiesComplete = true
	}
	if payload.ActiveDatabaseID == "" || !propertiesComplete {
		database, first, others, err := client.databaseForImport(ctx, databaseID)
		if payload.ActiveDatabaseID == "" && notionProviderRefused(err) {
			// A linked database comes with its views, and one not shared with the
			// connection is one Notion will not give: it is reported where it
			// was, and the rest of the import goes on.
			views, unavailable, err := notionImportLinkedViewSources(ctx, client, &payload, databaseID)
			if err != nil {
				return err
			}
			switch {
			case len(views) > 0:
				err = stageNotionImportLinkedBlock(r.app.DB(), job.ID, databaseID)
			case unavailable:
				// Of views whose source Notion won't give (notion_import_linked_views.go).
				err = reportNotionImportDatabaseUnavailable(r.app, job.ID, databaseID, notionImportIssueLinkedSourceUnavailable)
			default:
				err = reportNotionImportDatabaseUnavailable(r.app, job.ID, databaseID, notionImportIssueDatabaseUnavailable)
			}
			if err != nil {
				return err
			}
			finishNotionImportDatabase(&payload)
			return r.commitNotionDiscovery(job, payload, views)
		}
		if err != nil {
			return err
		}
		if database.Object != "database" || database.ID != databaseID {
			return errors.New(errorCodeNotionImportProviderFailed)
		}
		// One Notion never named comes over unnamed (notion_import_database_unnamed.go).
		if err := unnameNotionImportDatabase(r.app, job.ID, databaseID, database, others); err != nil {
			return err
		}
		// An inline database keeps its cover, shown when it is opened alone; a
		// full-page database's cover goes on the page that holds it.
		inline := database.IsInline == nil || *database.IsInline
		if err := recordNotionImportPresentation(
			r.app, job.ID, databaseID, "database", notionImportPresentationText(database, inline),
		); err != nil {
			return err
		}
		// A full-page database in a page comes in as a sub-page of kind
		// database holding it, where its block stood, as in Notion: the page
		// takes the database's place in the order, and the database goes in it.
		fullPage := payload.ActiveDatabaseID == "" && database.IsInline != nil && !*database.IsInline
		staging, takeover := []stagedNotionSource(nil), (*notionImportSourceTakeover)(nil)
		if fullPage {
			staged := storedNotionSource{}
			if err := r.app.DB().NewQuery(`
				SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]], [[text]]
				FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[source_id]] = {:sourceId}
			`).Bind(dbx.Params{"jobId": job.ID, "sourceId": databaseID}).One(&staged); err != nil {
				return err
			}
			if staged.Kind == "database" && staged.ParentSourceID != notionDatabaseRootPageSourceID(databaseID) {
				pageSourceID := notionDatabaseRootPageSourceID(databaseID)
				takeover = &notionImportSourceTakeover{sourceID: databaseID, source: stagedNotionSource{
					SourceID: pageSourceID, Kind: "page", ParentSourceID: staged.ParentSourceID,
					ParentKind: staged.ParentKind, Position: staged.Position,
					Title: notionObjectTitle(database), Text: notionImportPageSourceText(database),
				}}
				staging = []stagedNotionSource{{
					SourceID: databaseID, Kind: "database", ParentSourceID: pageSourceID,
					ParentKind: "page", Position: 0, Title: staged.Title, Text: staged.Text,
				}}
			}
		}
		presentationSources, presentationFiles := []stagedNotionSource(nil), []string(nil)
		if payload.DatabasePropertyCursor == "" && payload.ActiveDatabaseID == "" {
			if presentationSources, presentationFiles, err = notionImportPresentationFileSources(
				r.app, database, databaseID, true, true,
			); err != nil {
				return err
			}
			// Its other data sources follow the first's archived rows.
			payload.DatabaseOtherSources = others
			if len(others) > 0 {
				presentationSources = append(presentationSources, notionImportFirstDataSource(database, first, others))
			}
			if err := nameNotionImportDatabaseOfSeveral(r.app, job, database, first, others); err != nil {
				return err
			}
		}
		sources, propertyCursor, propertiesComplete, fingerprint, err := notionDatabasePropertyChunk(
			database,
			payload.DatabasePropertyCursor,
			notionImportProviderPageItems,
		)
		if err != nil {
			return err
		}
		if payload.DatabasePropertyFingerprint != "" && payload.DatabasePropertyFingerprint != fingerprint {
			return errors.New(errorCodeNotionImportProviderFailed)
		}
		payload.ActiveDatabaseID = databaseID
		payload.DatabaseDataSourceID = first.ID
		payload.DatabasePropertyCursor = propertyCursor
		payload.DatabasePropertyFingerprint = fingerprint
		payload.DatabasePropertiesComplete = propertiesComplete
		payload.PendingFileIDs = append(payload.PendingFileIDs, presentationFiles...)
		staging = append(append(staging, sources...), presentationSources...)
		return r.commitNotionDiscoveryTakingOver(job, payload, staging, nil, takeover)
	}

	if payload.DatabaseViewPhase {
		return r.discoverNotionDatabaseViews(ctx, job, payload, client)
	}
	if payload.DatabaseArchivedPhase {
		return r.discoverNotionArchivedDatabaseRows(ctx, job, payload, client)
	}
	var rows notionProviderPage
	var err error
	if payload.DatabaseDataSourceID != "" {
		rows, err = client.dataSourceRows(ctx, payload.DatabaseDataSourceID, payload.DatabaseCursor)
	} else {
		rows, err = client.databaseRows(ctx, databaseID, payload.DatabaseCursor)
	}
	if err != nil {
		return err
	}
	sources, err := stageNotionDatabaseRowPage(ctx, r.app, client, &payload, notionImportActiveSourceKey(payload), rows, false)
	if err != nil {
		return err
	}
	if !rows.HasMore {
		payload.DatabaseArchivedPhase = true
	}
	return r.commitNotionDiscovery(job, payload, sources)
}

// stageNotionDatabaseRowPage stages one provider page of a database's rows,
// archived when read from the archived partition, with their hosted icons and covers,
// and moves the row cursor past it.
func stageNotionDatabaseRowPage(
	ctx context.Context, app core.App, client notionProviderClient,
	payload *notionImportJobPayload, databaseID string, rows notionProviderPage, archived bool,
) ([]stagedNotionSource, error) {
	results, err := notionProviderPageResults(rows)
	if err != nil {
		return nil, err
	}
	if err := completeNotionRowProperties(ctx, client, results); err != nil {
		return nil, err
	}
	nextCursor, err := notionProviderContinuationCursor(payload.DatabaseCursor, rows)
	if err != nil {
		return nil, err
	}
	for index := range results {
		results[index].IsArchived = results[index].IsArchived || archived
	}
	sources, fileIDs, err := notionDatabaseRowSources(app, databaseID, results, payload.DatabaseRowPosition, &payload.DatabaseRowMinutes)
	if err != nil {
		return nil, err
	}
	// A page can hang on a row's cells (notion_import_unmentioned_pages.go).
	payload.noteUnmentionedParents(results, false)
	payload.PendingFileIDs = append(payload.PendingFileIDs, fileIDs...)
	for _, row := range results {
		if row.Object != "page" || row.ID == "" {
			continue
		}
		iconSources, iconFiles, err := notionImportPresentationFileSources(app, row, row.ID, false, true)
		if err != nil {
			return nil, err
		}
		sources = append(sources, iconSources...)
		payload.PendingFileIDs = append(payload.PendingFileIDs, iconFiles...)
	}
	payload.DatabaseRowPosition += len(results)
	payload.DatabaseCursor = nextCursor
	return sources, nil
}

func notionDatabasePropertyChunk(
	database notionProviderObject,
	cursor string,
	limit int,
) ([]stagedNotionSource, string, bool, string, error) {
	fingerprint, err := notionDatabasePropertyFingerprint(database.Properties)
	if err != nil {
		return nil, "", false, "", err
	}
	if limit <= 0 {
		return nil, "", false, "", errors.New(errorCodeNotionImportProviderFailed)
	}
	names := make([]string, 0, len(database.Properties))
	for name := range database.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	start := 0
	if cursor != "" {
		index := sort.SearchStrings(names, cursor)
		if index >= len(names) || names[index] != cursor {
			return nil, "", false, "", errors.New(errorCodeNotionImportProviderFailed)
		}
		start = index + 1
	}
	end := min(start+limit, len(names))
	sources := make([]stagedNotionSource, 0, end-start)
	issues := []stagedNotionSource{}
	for offset, name := range names[start:end] {
		property := database.Properties[name]
		sourceID := notionPropertySourceID(database.ID, property.ID)
		title := notionImportPropertyName(name, property)
		plan, reasons, supported := notionImportPropertyPlanFor(database, name, property)
		if !supported {
			issues = append(issues, notionImportPropertyIssueSource(
				database.ID, sourceID, title, notionImportIssuePropertyUnsupported, start+offset,
			))
			continue
		}
		encoded, err := json.Marshal(plan)
		if err != nil {
			return nil, "", false, "", err
		}
		sources = append(sources, stagedNotionSource{
			SourceID: sourceID, Kind: "property",
			ParentSourceID: database.ID, Position: start + offset, Title: title, Text: string(encoded),
		})
		for _, reason := range reasons {
			issues = append(issues, notionImportPropertyIssueSource(database.ID, sourceID, title, reason, start+offset))
		}
	}
	sources = append(sources, issues...)
	complete := end == len(names)
	nextCursor := ""
	if !complete {
		nextCursor = names[end-1]
	}
	return sources, nextCursor, complete, fingerprint, nil
}

func notionDatabasePropertyFingerprint(properties map[string]notionProviderProperty) (string, error) {
	encoded, err := json.Marshal(properties)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func notionPropertySourceID(databaseID, propertyID string) string {
	return databaseID + ":property:" + propertyID
}

// reportNotionImportDatabaseUnavailable turns a staged database Notion will
// not give into the report of it in its place, so no empty database is made.
func reportNotionImportDatabaseUnavailable(app core.App, jobID, databaseID, reason string) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_notion_import_sources}} SET [[kind]]='issue', [[text]]={:reason}
		WHERE [[job_id]]={:jobId} AND [[source_id]]={:sourceId} AND [[kind]]='database'
	`).Bind(dbx.Params{
		"jobId": jobID, "sourceId": databaseID, "reason": reason,
	}).Execute()
	return err
}
