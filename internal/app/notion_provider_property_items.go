package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// A Notion page lists at most 25 related pages, people or pieces of text in
// one property; the rest come from the property item endpoint. The import
// reads a cut-short value to its end before it stages the row, page by page,
// as far as a row value can hold: past twice a value's bytes of Notion's
// items (each larger than what it becomes) nothing more could be kept, and
// the value keeps what was read, marked as cut short.

const (
	notionPagePropertyItemLimit = 25
	maxNotionPropertyItemBytes  = 2 * maxGraphChangeBytes
)

// notionPropertyMayBeCutShort reports whether Notion may have left part of a
// page's property value out.
func notionPropertyMayBeCutShort(property notionProviderProperty) bool {
	switch property.Type {
	case "relation":
		return property.HasMore
	case "people", "title", "rich_text":
		items := []json.RawMessage{}
		return json.Unmarshal(notionImportPropertySettings(property), &items) == nil &&
			len(items) >= notionPagePropertyItemLimit
	}
	return false
}

// completeNotionRowProperties reads each cut-short value of the rows to its
// end.
func completeNotionRowProperties(ctx context.Context, client notionProviderClient, rows []notionProviderObject) error {
	for index := range rows {
		for name, property := range rows[index].Properties {
			if !notionPropertyMayBeCutShort(property) {
				continue
			}
			items, complete, err := client.propertyItems(ctx, rows[index].ID, property)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Notion refusing the value leaves it cut short; anything that may
			// pass, a rate limit, an outage or the network, retries the turn.
			status := &notionProviderStatusError{}
			if err != nil && (!errors.As(err, &status) || status.status >= 500 || status.status == http.StatusTooManyRequests) {
				return err
			}
			if err != nil || len(items) == 0 {
				property.HasMore = true
				rows[index].Properties[name] = property
				continue
			}
			encoded, err := json.Marshal(items)
			if err != nil {
				return err
			}
			property.Settings, property.HasMore = encoded, !complete
			switch property.Type {
			case "title":
				property.Title = encoded
			case "rich_text":
				property.RichText = encoded
			case "people":
				property.People = encoded
			}
			rows[index].Properties[name] = property
		}
	}
	return nil
}

// propertyItems reads a page property's items page by page; complete is
// false when there were more than a row value can hold, with those read.
func (client notionProviderClient) propertyItems(
	ctx context.Context, pageID string, property notionProviderProperty,
) ([]json.RawMessage, bool, error) {
	propertyID, err := url.PathUnescape(property.ID)
	if err != nil {
		propertyID = property.ID
	}
	items := []json.RawMessage{}
	read := 0
	cursor := ""
	for {
		query := url.Values{"page_size": {"100"}}
		if cursor != "" {
			query.Set("start_cursor", cursor)
		}
		page := struct {
			Results    []map[string]json.RawMessage `json:"results"`
			HasMore    bool                         `json:"has_more"`
			NextCursor *string                      `json:"next_cursor"`
		}{}
		path := "/v1/pages/" + url.PathEscape(pageID) + "/properties/" + url.PathEscape(propertyID) + "?" + query.Encode()
		if err := client.request(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, false, err
		}
		for _, result := range page.Results {
			var kind string
			if json.Unmarshal(result["type"], &kind) != nil || kind != property.Type || len(result[kind]) == 0 {
				return nil, false, errors.New(errorCodeNotionImportProviderFailed)
			}
			items = append(items, result[kind])
			read += len(result[kind])
		}
		if read > maxNotionPropertyItemBytes {
			return items, false, nil
		}
		if !page.HasMore || page.NextCursor == nil || *page.NextCursor == "" || *page.NextCursor == cursor {
			return items, true, nil
		}
		cursor = *page.NextCursor
	}
}
