package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
)

// A Notion database query leaves archived pages out, and only the data
// source query of API version 2025-09-03 returns them, with is_archived.
// After a data source's rows the import reads its archived rows, which it
// stages as archived rows of the same data source, then the database's next
// data source, and after the last the database's views.

const notionProviderDataSourceAPIVersion = "2025-09-03"

func (client notionProviderClient) databaseDataSourceIDs(ctx context.Context, databaseID string) ([]string, error) {
	var database struct {
		Object      string `json:"object"`
		ID          string `json:"id"`
		DataSources []struct {
			ID string `json:"id"`
		} `json:"data_sources"`
	}
	if err := client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/databases/"+url.PathEscape(databaseID), nil, &database,
	); err != nil {
		return nil, err
	}
	if database.Object != "database" || database.ID != databaseID ||
		len(database.DataSources) > notionImportProviderPageItems {
		return nil, errors.New(errorCodeNotionImportProviderFailed)
	}
	ids := make([]string, 0, len(database.DataSources))
	for _, dataSource := range database.DataSources {
		if dataSource.ID == "" {
			return nil, errors.New(errorCodeNotionImportProviderFailed)
		}
		ids = append(ids, dataSource.ID)
	}
	return ids, nil
}

func (client notionProviderClient) archivedDataSourceRows(
	ctx context.Context, dataSourceID, cursor string,
) (notionProviderPage, error) {
	payload := map[string]any{"page_size": notionImportProviderPageItems, "is_archived": true}
	if cursor != "" {
		payload["start_cursor"] = cursor
	}
	var page notionProviderPage
	err := client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodPost,
		"/v1/data_sources/"+url.PathEscape(dataSourceID)+"/query", payload, &page,
	)
	return page, err
}

func (r *durableJobRunner) discoverNotionArchivedDatabaseRows(
	ctx context.Context,
	job storedJob,
	payload notionImportJobPayload,
	client notionProviderClient,
) error {
	databaseID := payload.ActiveDatabaseID
	if !payload.DatabaseArchivedSourcesLoaded {
		// A data source read in its own version is the one whose archived rows
		// follow; a database read in the older version has one, which the
		// data source version names.
		ids := []string{payload.DatabaseDataSourceID}
		if payload.DatabaseDataSourceID == "" {
			var err error
			if ids, err = client.databaseDataSourceIDs(ctx, databaseID); err != nil {
				return err
			}
			if len(ids) > 1 {
				ids = ids[:1]
			}
		}
		// The first's own Notion ID goes on the staged database, whose entry
		// in the move's map stands for it (notion_address_moved_data_sources.go).
		if payload.DatabaseSourceKey == "" && len(ids) > 0 {
			if err := recordNotionImportDatabaseDataSource(r.app, job.ID, databaseID, ids[0]); err != nil {
				return err
			}
		}
		payload.DatabaseArchivedSourcesLoaded = true
		payload.DatabaseArchivedSourceIDs = ids
		for _, id := range ids {
			if !slices.Contains(payload.DatabaseSourceIDs, id) {
				payload.DatabaseSourceIDs = append(payload.DatabaseSourceIDs, id)
			}
		}
		payload.DatabaseCursor = ""
		if len(ids) == 0 {
			nextNotionImportDatabaseSource(&payload)
		}
		return r.commitNotionDiscovery(job, payload, nil)
	}
	if len(payload.DatabaseArchivedSourceIDs) == 0 {
		nextNotionImportDatabaseSource(&payload)
		return r.commitNotionDiscovery(job, payload, nil)
	}
	rows, err := client.archivedDataSourceRows(ctx, payload.DatabaseArchivedSourceIDs[0], payload.DatabaseCursor)
	if err != nil {
		return err
	}
	sources, err := stageNotionDatabaseRowPage(ctx, r.app, client, &payload, notionImportActiveSourceKey(payload), rows, true)
	if err != nil {
		return err
	}
	if !rows.HasMore {
		payload.DatabaseArchivedSourceIDs = payload.DatabaseArchivedSourceIDs[1:]
		if len(payload.DatabaseArchivedSourceIDs) == 0 {
			nextNotionImportDatabaseSource(&payload)
		}
	}
	return r.commitNotionDiscovery(job, payload, sources)
}

// finishNotionImportDatabase moves discovery on to the next pending database.
func finishNotionImportDatabase(payload *notionImportJobPayload) {
	payload.PendingDatabaseIDs = payload.PendingDatabaseIDs[1:]
	payload.ActiveDatabaseID = ""
	payload.DatabasePropertyCursor = ""
	payload.DatabasePropertyFingerprint = ""
	payload.DatabasePropertiesComplete = false
	payload.DatabaseCursor = ""
	payload.DatabaseRowPosition = 0
	payload.DatabaseRowMinutes = notionImportRowMinutes{}
	payload.DatabaseDataSourceID = ""
	payload.DatabaseOtherSources = nil
	payload.DatabaseSourceKey = ""
	payload.DatabaseSourceName = ""
	payload.DatabaseSourceIndex = 0
	payload.DatabaseViewSourcesSeen = nil
	payload.DatabaseArchivedPhase = false
	payload.DatabaseArchivedSourcesLoaded = false
	payload.DatabaseArchivedSourceIDs = nil
	payload.DatabaseSourceIDs = nil
	payload.DatabaseViewPhase = false
	payload.DatabaseViewsListed = false
	payload.DatabaseViewCursor = ""
	payload.DatabaseViewIDs = nil
	payload.DatabaseViewPosition = 0
	payload.DatabaseViewPrimary = false
	payload.DatabaseViewManualOrder = false
}

// startNotionImportDatabaseViews moves discovery on from a database's rows to
// its views.
func startNotionImportDatabaseViews(payload *notionImportJobPayload) {
	payload.DatabaseArchivedPhase = false
	payload.DatabaseArchivedSourceIDs = nil
	payload.DatabaseCursor = ""
	payload.DatabaseViewPhase = true
}
