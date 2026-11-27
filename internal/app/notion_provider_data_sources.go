package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Notion refuses a database with more than one data source in the API
// version the import reads databases in. The import reads such a database in
// the data source version instead: the database's own title, icon and cover,
// and the properties and rows of its first data source, then each other
// data source in turn.

// notionProviderOtherDataSource is a data source of a database of several,
// by its ID and name: the first, or one after it, which the import reads
// after the first.
type notionProviderOtherDataSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Unnamed is a first one whose own title is none, which the list names
	// "New data source" (notion_import_database_unnamed.go).
	Unnamed bool `json:"-"`
}

// databaseForImport reads a database with its properties, and, for a
// database read through its first data source, that source, by its ID and
// name, and the others.
func (client notionProviderClient) databaseForImport(
	ctx context.Context, databaseID string,
) (notionProviderObject, notionProviderOtherDataSource, []notionProviderOtherDataSource, error) {
	database, refused := client.database(ctx, databaseID)
	if refused == nil {
		return database, notionProviderOtherDataSource{}, nil, client.readInlineDatabaseCover(ctx, &database)
	}
	return client.databaseOfSources(ctx, databaseID, refused)
}

// databaseOfSources reads, in the data source version, a database the older
// version refused: one of several data sources, with its first and the
// others; one refused for another reason stays refused.
func (client notionProviderClient) databaseOfSources(
	ctx context.Context, databaseID string, refused error,
) (notionProviderObject, notionProviderOtherDataSource, []notionProviderOtherDataSource, error) {
	none := notionProviderOtherDataSource{}
	var status *notionProviderStatusError
	if !errors.As(refused, &status) || status.status != http.StatusBadRequest {
		return notionProviderObject{}, none, nil, refused
	}
	var raw json.RawMessage
	if err := client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/databases/"+url.PathEscape(databaseID), nil, &raw,
	); err != nil {
		return notionProviderObject{}, none, nil, refused
	}
	listed := struct {
		DataSources []notionProviderOtherDataSource `json:"data_sources"`
	}{}
	database := notionProviderObject{}
	if json.Unmarshal(raw, &database) != nil || json.Unmarshal(raw, &listed) != nil ||
		database.Object != "database" || database.ID != databaseID ||
		len(listed.DataSources) < 2 || len(listed.DataSources) > notionImportProviderPageItems {
		// A database Notion refused for another reason stays refused.
		return notionProviderObject{}, none, nil, refused
	}
	for _, source := range listed.DataSources {
		if source.ID == "" {
			return notionProviderObject{}, none, nil, errors.New(errorCodeNotionImportProviderFailed)
		}
	}
	first := listed.DataSources[0]
	source, err := client.dataSource(ctx, first.ID)
	if err != nil {
		return notionProviderObject{}, none, nil, err
	}
	database.Properties = source.Properties
	// The first is named as its data source is, as each other one is
	// (discoverNotionDataSourceProperties).
	if title := notionObjectTitle(source); strings.TrimSpace(title) != "" {
		first.Name = title
	} else {
		first.Unnamed = true
	}
	return database, first, listed.DataSources[1:], nil
}

// readInlineDatabaseCover gives an inline database the cover Notion's older
// version leaves out: it reads null there, while the data source version's
// database has it (seen 2026-10-01). A rate limit or cancellation retries the
// turn; any other failure leaves the database without a cover.
func (client notionProviderClient) readInlineDatabaseCover(ctx context.Context, database *notionProviderObject) error {
	if database.Object != "database" || database.IsInline != nil && !*database.IsInline ||
		len(database.Cover) > 0 && string(database.Cover) != "null" {
		return nil
	}
	newer := struct {
		Cover json.RawMessage `json:"cover"`
	}{}
	err := client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/databases/"+url.PathEscape(database.ID), nil, &newer,
	)
	rateLimited := &notionProviderRateLimitError{}
	if errors.As(err, &rateLimited) || ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	if err == nil {
		database.Cover = newer.Cover
	}
	return nil
}

// dataSourceRows reads a page of a data source's rows.
func (client notionProviderClient) dataSourceRows(
	ctx context.Context, dataSourceID, cursor string,
) (notionProviderPage, error) {
	payload := map[string]any{"page_size": notionImportProviderPageItems}
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

// dataSource reads a data source with its properties.
func (client notionProviderClient) dataSource(ctx context.Context, dataSourceID string) (notionProviderObject, error) {
	var source notionProviderObject
	if err := client.requestVersion(
		ctx, notionProviderDataSourceAPIVersion, http.MethodGet, "/v1/data_sources/"+url.PathEscape(dataSourceID), nil, &source,
	); err != nil {
		return notionProviderObject{}, err
	}
	if source.Object != "data_source" || source.ID != dataSourceID {
		return notionProviderObject{}, errors.New(errorCodeNotionImportProviderFailed)
	}
	return source, nil
}

// notionImportDataSourceIssue stages that a database's data source Notion
// would not give was not imported, under the database.
func notionImportDataSourceIssue(databaseID string, other notionProviderOtherDataSource, position int) stagedNotionSource {
	return notionImportViewIssueSource(
		databaseID, notionImportDataSourceKey(databaseID, other.ID), other.Name,
		notionImportIssueDataSourceUnavailable, position,
	)
}
