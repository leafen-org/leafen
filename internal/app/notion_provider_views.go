package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Notion serves a database's views, in its tab order, only in API version
// 2026-03-11: the list names each view and each view is read on its own.

const notionProviderViewAPIVersion = "2026-03-11"

// notionProviderStatusError is a Notion answer other than success; it reads
// as the import's provider failure, or, where Notion refuses the token itself
// (401), as the token rejected, so the person is told to connect it again.
type notionProviderStatusError struct {
	status int
}

func (err *notionProviderStatusError) Error() string {
	if err.status == http.StatusUnauthorized {
		return errorCodeNotionImportTokenRejected
	}
	return errorCodeNotionImportProviderFailed
}

// notionProviderTokenRejected reports whether Notion refused the token.
func notionProviderTokenRejected(err error) bool {
	var status *notionProviderStatusError
	return errors.As(err, &status) && status.status == http.StatusUnauthorized
}

// errNotionProviderViewUnreadable is a view Notion returned in a shape the
// import cannot read.
var errNotionProviderViewUnreadable = errors.New(errorCodeNotionImportProviderFailed)

// notionProviderRefused reports whether Notion declined to give something it
// may keep from this integration or API version, or gave a view the import
// cannot read, rather than failing.
func notionProviderRefused(err error) bool {
	if errors.Is(err, errNotionProviderViewUnreadable) {
		return true
	}
	var status *notionProviderStatusError
	if !errors.As(err, &status) {
		return false
	}
	switch status.status {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound:
		return true
	}
	return false
}

type notionProviderViewPage struct {
	IDs        []string
	HasMore    bool
	NextCursor string
}

// notionProviderView is a Notion view as the view API returns it.
type notionProviderView struct {
	Object          string          `json:"object"`
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	DataSourceID    *string         `json:"data_source_id"`
	DashboardViewID *string         `json:"dashboard_view_id"`
	Filter          json.RawMessage `json:"filter"`
	Sorts           json.RawMessage `json:"sorts"`
	QuickFilters    json.RawMessage `json:"quick_filters"`
	Configuration   json.RawMessage `json:"configuration"`
}

func (client notionProviderClient) databaseViews(ctx context.Context, databaseID, cursor string) (notionProviderViewPage, error) {
	query := url.Values{
		"database_id": []string{databaseID},
		"page_size":   []string{fmt.Sprint(notionImportProviderPageItems)},
	}
	if cursor != "" {
		query.Set("start_cursor", cursor)
	}
	var page struct {
		Object  string `json:"object"`
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
		HasMore    bool    `json:"has_more"`
		NextCursor *string `json:"next_cursor"`
	}
	if err := client.requestVersion(
		ctx, notionProviderViewAPIVersion, http.MethodGet, "/v1/views?"+query.Encode(), nil, &page,
	); err != nil {
		return notionProviderViewPage{}, err
	}
	if page.Object != "" && page.Object != "list" || len(page.Results) > notionImportProviderPageItems {
		return notionProviderViewPage{}, errors.New(errorCodeNotionImportProviderFailed)
	}
	result := notionProviderViewPage{HasMore: page.HasMore, IDs: make([]string, 0, len(page.Results))}
	for _, view := range page.Results {
		if view.ID == "" {
			return notionProviderViewPage{}, errors.New(errorCodeNotionImportProviderFailed)
		}
		result.IDs = append(result.IDs, view.ID)
	}
	next, err := notionProviderContinuationCursor(cursor, notionProviderPage{HasMore: page.HasMore, NextCursor: page.NextCursor})
	if err != nil {
		return notionProviderViewPage{}, err
	}
	result.NextCursor = next
	return result, nil
}

func (client notionProviderClient) view(ctx context.Context, id string) (notionProviderView, error) {
	var view notionProviderView
	if err := client.requestVersion(
		ctx, notionProviderViewAPIVersion, http.MethodGet, "/v1/views/"+url.PathEscape(id), nil, &view,
	); err != nil {
		return notionProviderView{}, err
	}
	if view.Object != "view" || !notionImportSameID(view.ID, id) || view.Type == "" {
		return notionProviderView{}, errNotionProviderViewUnreadable
	}
	return view, nil
}
