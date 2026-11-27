package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A Notion row's values are staged in the shape the row's native properties
// store, except where the value names something the import makes later: a
// Notion-hosted file (copied before the row is applied) and a relation's
// related pages (linked once every database has been applied). Computed values
// are left for Leafen to compute; formulas' and rollups' shown text is kept aside.
// What a value loses is reported against its property.

// notionImportRowCreatedKey carries the time Notion created a row among its
// staged values; no property source ID has this form.
const notionImportRowCreatedKey = "leafen:row-created-time"

// notionImportRowEditedKey carries the time Notion last changed a row, which
// the row keeps (notion_import_row_stamps.go) and a migration run again reads.
const notionImportRowEditedKey = "leafen:row-edited-time"

// notionImportRowAuthorsKey carries the Notion people who made a row and last
// changed it, which the row takes where the migration's plan maps them
// (notion_import_row_stamps.go).
const notionImportRowAuthorsKey = "leafen:row-authors"

// notionImportPropertyFileMarker names a Notion-hosted file in a row's files
// value: <row>#property-file:<property>:<index>.
const notionImportPropertyFileMarker = "#property-file:"

// notionImportPropertyFileSourceKey names a hosted file in a staged files
// value; the row takes the file its copy made.
const notionImportPropertyFileSourceKey = "notionFileSource"

func notionDatabaseRowSources(
	app core.App, databaseID string, rows []notionProviderObject, start int, minutes *notionImportRowMinutes,
) ([]stagedNotionSource, []string, error) {
	sources := make([]stagedNotionSource, 0, len(rows))
	issues := []stagedNotionSource{}
	fileIDs := []string{}
	reported := map[string]struct{}{}
	for index, row := range rows {
		if row.Object != "page" || row.ID == "" {
			continue
		}
		values, rowIssues, files, err := notionImportRowValues(app, databaseID, row)
		if err != nil {
			return nil, nil, err
		}
		stageNotionImportRowAside(values, databaseID, row)
		if row.IsArchived {
			values[notionImportRowArchivedKey] = notionDatabaseRowValuePlan{Type: "archived", Value: true}
		}
		if created, err := time.Parse(time.RFC3339, strings.TrimSpace(row.CreatedTime)); err == nil {
			values[notionImportRowCreatedKey] = notionDatabaseRowValuePlan{
				Type: "created", Value: minutes.created(created).Format(time.RFC3339Nano),
			}
		}
		if edited, err := time.Parse(time.RFC3339, strings.TrimSpace(row.LastEditedTime)); err == nil {
			values[notionImportRowEditedKey] = notionDatabaseRowValuePlan{
				Type: "edited", Value: edited.UTC().Format(time.RFC3339Nano),
			}
		}
		if made, changed := notionProviderUserID(row.CreatedBy), notionProviderUserID(row.LastEditedBy); made != "" || changed != "" {
			values[notionImportRowAuthorsKey] = notionDatabaseRowValuePlan{
				Type: "authors", Value: map[string]any{"createdBy": made, "lastEditedBy": changed},
			}
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return nil, nil, err
		}
		sources = append(sources, stagedNotionSource{
			SourceID: row.ID, Kind: "row", ParentSourceID: databaseID,
			Position: start + index, Text: string(encoded),
			DiscoveryStatus: notionImportPageDiscoveryPending,
		})
		for _, file := range files {
			sources = append(sources, file)
			fileIDs = append(fileIDs, file.SourceID)
		}
		for _, issue := range rowIssues {
			if _, duplicate := reported[issue.SourceID]; !duplicate {
				reported[issue.SourceID] = struct{}{}
				issues = append(issues, issue)
			}
		}
	}
	return append(sources, issues...), fileIDs, nil
}

// notionImportRowValues stages one row's property values with the issues
// they raise and the hosted files they hold.
func notionImportRowValues(
	app core.App, databaseID string, row notionProviderObject,
) (map[string]notionDatabaseRowValuePlan, []stagedNotionSource, []stagedNotionSource, error) {
	values := map[string]notionDatabaseRowValuePlan{}
	issues, files := []stagedNotionSource{}, []stagedNotionSource{}
	names := make([]string, 0, len(row.Properties))
	for name := range row.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		property := row.Properties[name]
		if notionGraphValueType(property.Type) == "" {
			continue
		}
		sourceID := notionPropertySourceID(databaseID, property.ID)
		value, reasons, hosted, err := notionImportRowValue(app, row.ID, sourceID, property)
		if err != nil {
			return nil, nil, nil, err
		}
		if value != nil {
			values[sourceID] = *value
		}
		files = append(files, hosted...)
		for _, reason := range reasons {
			issues = append(issues, notionImportPropertyIssueSource(
				databaseID, sourceID, notionImportPropertyName(name, property), reason, 0,
			))
		}
	}
	return values, issues, files, nil
}

// notionProviderPropertyValue is the native value a Notion property value
// becomes, or nil when it keeps none.
func notionProviderPropertyValue(property notionProviderProperty, kind string) (any, error) {
	value, _, _, err := notionImportRowValue(nil, "", "", property)
	if err != nil || value == nil || value.Type != kind {
		return nil, err
	}
	return value.Value, nil
}

// notionImportRowValue converts one Notion property value. It returns nil
// for an empty or computed value, the report reasons for what the value
// loses, and the hosted files it holds.
func notionImportRowValue(
	app core.App, rowID, propertySourceID string, property notionProviderProperty,
) (*notionDatabaseRowValuePlan, []string, []stagedNotionSource, error) {
	kind := notionGraphValueType(property.Type)
	raw := notionImportPropertySettings(property)
	empty := len(raw) == 0 || string(raw) == "null"
	plan := func(value any) *notionDatabaseRowValuePlan {
		return &notionDatabaseRowValuePlan{Type: kind, Value: value}
	}
	omitted := []string{notionImportIssuePropertyValueOmitted}
	switch kind {
	case graphValueTypeTitle, graphValueTypeRichText:
		parts, err := notionProviderRichTextParts(raw)
		if err != nil {
			return nil, omitted, nil, nil
		}
		text, spans, kept := notionImportRichTextCell(parts, kind == graphValueTypeTitle)
		reasons := []string(nil)
		if !kept {
			reasons = append(reasons, notionImportIssuePropertyFormattingOmitted)
		}
		text, spans, shortened := notionImportFittedCell(text, spans, kind == graphValueTypeTitle)
		if shortened != "" {
			reasons = append(reasons, shortened)
		}
		if property.HasMore && shortened != notionImportIssuePropertyValueTruncated {
			reasons = append(reasons, notionImportIssuePropertyValueTruncated)
		}
		value := plan(text)
		value.Spans = spans
		return value, reasons, nil, nil
	case graphValueTypeNumber:
		var number *float64
		if json.Unmarshal(raw, &number) != nil {
			return nil, omitted, nil, nil
		}
		if number == nil {
			return nil, nil, nil, nil
		}
		return plan(*number), nil, nil, nil
	case graphValueTypeSelect, graphValueTypeStatus:
		if empty {
			return nil, nil, nil, nil
		}
		option := notionImportProviderOption{}
		if json.Unmarshal(raw, &option) != nil || strings.TrimSpace(option.ID+option.Name) == "" {
			return nil, omitted, nil, nil
		}
		return plan(notionImportOptionID(option.ID, option.Name)), nil, nil, nil
	case graphValueTypeMultiSelect:
		options := []notionImportProviderOption{}
		if !empty && json.Unmarshal(raw, &options) != nil {
			return nil, omitted, nil, nil
		}
		ids := make([]string, 0, len(options))
		seen := map[string]struct{}{}
		for _, option := range options {
			id := notionImportOptionID(option.ID, option.Name)
			if _, duplicate := seen[id]; !duplicate {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		return plan(ids), nil, nil, nil
	case graphValueTypeDate:
		if empty {
			return nil, nil, nil, nil
		}
		date, valid := notionImportDateValue(raw)
		if !valid {
			return nil, omitted, nil, nil
		}
		return plan(date), nil, nil, nil
	case graphValueTypePerson:
		people, err := notionProviderPeopleValue(raw)
		if err != nil {
			return nil, omitted, nil, nil
		}
		list, _ := people.([]any)
		kept, fits := notionImportFittedList(list)
		if property.HasMore || !fits {
			return plan(kept), []string{notionImportIssuePropertyValueTruncated}, nil, nil
		}
		return plan(people), nil, nil, nil
	case graphValueTypeFiles:
		return notionImportFilesValue(app, rowID, propertySourceID, property.ID, raw)
	case graphValueTypeCheckbox:
		checked := false
		if !empty && json.Unmarshal(raw, &checked) != nil {
			return nil, omitted, nil, nil
		}
		return plan(checked), nil, nil, nil
	case graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		var text *string
		if json.Unmarshal(raw, &text) != nil {
			return nil, omitted, nil, nil
		}
		if text == nil || *text == "" {
			return nil, nil, nil, nil
		}
		return plan(*text), nil, nil, nil
	case graphValueTypeRelation:
		related := []struct {
			ID string `json:"id"`
		}{}
		if !empty && json.Unmarshal(raw, &related) != nil {
			return nil, omitted, nil, nil
		}
		ids := make([]string, 0, len(related))
		for _, page := range related {
			if id := strings.TrimSpace(page.ID); id != "" {
				ids = append(ids, id)
			}
		}
		ids, fits := notionImportFittedList(ids)
		reasons := []string(nil)
		if property.HasMore || !fits {
			reasons = []string{notionImportIssuePropertyValueTruncated}
		}
		if len(ids) == 0 {
			return nil, reasons, nil, nil
		}
		return plan(ids), reasons, nil, nil
	case graphValueTypeUniqueID:
		unique := struct {
			Number *float64 `json:"number"`
		}{}
		if !empty && json.Unmarshal(raw, &unique) != nil {
			return nil, omitted, nil, nil
		}
		if unique.Number == nil {
			return nil, nil, nil, nil
		}
		if *unique.Number < 1 || *unique.Number > float64(maxDatabaseUniqueIDValue) || *unique.Number != float64(int64(*unique.Number)) {
			return nil, omitted, nil, nil
		}
		return plan(*unique.Number), nil, nil, nil
	case graphValueTypeVerification:
		if empty {
			return nil, nil, nil, nil
		}
		verification, complete := notionImportVerificationValue(raw)
		if verification == nil {
			return nil, omitted, nil, nil
		}
		if !complete {
			return plan(verification), omitted, nil, nil
		}
		return plan(verification), nil, nil, nil
	case graphValueTypePlace:
		if empty {
			return nil, nil, nil, nil
		}
		place, err := notionDatabaseRowPagePlace(raw)
		if err != nil {
			return nil, omitted, nil, nil
		}
		return plan(place), nil, nil, nil
	}
	// Formulas, rollups, created and edited times and people, and buttons
	// are computed by Leafen.
	return nil, nil, nil, nil
}

// notionImportPlainRichText joins Notion rich text into the plain text a
// database cell keeps, and reports whether the text had formatting, links,
// mentions or equations the cell cannot keep.
// notionImportDateValue reads a Notion date value into a native date.
func notionImportDateValue(raw json.RawMessage) (map[string]any, bool) {
	date := struct {
		Start    string  `json:"start"`
		End      *string `json:"end"`
		TimeZone *string `json:"time_zone"`
	}{}
	if json.Unmarshal(raw, &date) != nil || strings.TrimSpace(date.Start) == "" {
		return nil, false
	}
	value := map[string]any{"start": date.Start}
	if date.End != nil && strings.TrimSpace(*date.End) != "" {
		value["end"] = *date.End
	}
	if date.TimeZone != nil && strings.TrimSpace(*date.TimeZone) != "" {
		value["timeZone"] = strings.TrimSpace(*date.TimeZone)
	}
	return canonicalGraphDateValue(value)
}

// notionImportVerificationValue reads a Notion verification into the state
// and dates Leafen keeps. Who verified is a Notion user Leafen cannot name, so
// a verification that names one is reported incomplete.
func notionImportVerificationValue(raw json.RawMessage) (map[string]any, bool) {
	verification := struct {
		State      string          `json:"state"`
		VerifiedBy json.RawMessage `json:"verified_by"`
		Date       *struct {
			Start string  `json:"start"`
			End   *string `json:"end"`
		} `json:"date"`
	}{}
	if json.Unmarshal(raw, &verification) != nil {
		return nil, false
	}
	switch verification.State {
	case "verified", "unverified", "expired":
	default:
		return nil, false
	}
	value := map[string]any{"state": verification.State}
	complete := len(verification.VerifiedBy) == 0 || string(verification.VerifiedBy) == "null"
	if verification.Date != nil && verification.State != "unverified" {
		date, _, err := notionDatabaseRowPageVerificationDateValue(&notionDatabaseRowPageVerificationDate{
			Start: verification.Date.Start, End: verification.Date.End,
		})
		if err != nil {
			complete = false
		} else {
			value["date"] = date
		}
	}
	return value, complete
}

// notionImportFilesValue stages a files value: an external file keeps its
// link, and a Notion-hosted file becomes a file source the import copies.
func notionImportFilesValue(
	app core.App, rowID, propertySourceID, propertyID string, raw json.RawMessage,
) (*notionDatabaseRowValuePlan, []string, []stagedNotionSource, error) {
	provided := []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		File struct {
			URL string `json:"url"`
		} `json:"file"`
		External struct {
			URL string `json:"url"`
		} `json:"external"`
	}{}
	if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &provided) != nil {
		return nil, []string{notionImportIssuePropertyValueOmitted}, nil, nil
	}
	items := make([]any, 0, len(provided))
	sources := []stagedNotionSource{}
	complete := true
	for index, file := range provided {
		name := file.Name
		if name == "" {
			name = "file"
		}
		if utf8.RuneCountInString(name) > maxGraphExternalFileNameRunes {
			name = string([]rune(name)[:maxGraphExternalFileNameRunes])
		}
		switch file.Type {
		case "external":
			// A linked file keeps its address as Notion holds it, any text.
			link := file.External.URL
			if !validGraphStoredFileAddress(link) {
				complete = false
				continue
			}
			items = append(items, map[string]any{"source": graphExternalFileSource, "url": link, "name": name})
		case "file":
			link := strings.TrimSpace(file.File.URL)
			if link == "" || app == nil {
				complete = false
				continue
			}
			key, err := notionImportEncryptionKey(app)
			if err != nil {
				return nil, nil, nil, err
			}
			cipher, err := security.Encrypt([]byte(link), key)
			if err != nil {
				return nil, nil, nil, err
			}
			sourceID := rowID + notionImportPropertyFileMarker + propertyID + ":" + strconv.Itoa(index)
			sources = append(sources, stagedNotionSource{
				SourceID: sourceID, Kind: graphBlockTypeFile, ParentSourceID: rowID, ParentKind: "page",
				Title: name, Text: notionFileSourcePrefix + `{"caption":""}`, FileURLCipher: cipher,
			})
			items = append(items, map[string]any{notionImportPropertyFileSourceKey: sourceID, "name": name})
		default:
			complete = false
		}
	}
	var reasons []string
	if !complete {
		reasons = []string{notionImportIssuePropertyValueOmitted}
	}
	if len(items) == 0 {
		return nil, reasons, sources, nil
	}
	return &notionDatabaseRowValuePlan{Type: graphValueTypeFiles, Value: items}, reasons, sources, nil
}

// notionImportPropertyFileTarget returns the row and property of a hosted
// file a files value holds, and whether the source is one.
func notionImportPropertyFileTarget(sourceID string) (string, string, int, bool) {
	rowID, rest, found := strings.Cut(sourceID, notionImportPropertyFileMarker)
	if !found || rowID == "" {
		return "", "", 0, false
	}
	separator := strings.LastIndex(rest, ":")
	if separator <= 0 {
		return "", "", 0, false
	}
	index, err := strconv.Atoi(rest[separator+1:])
	if err != nil || index < 0 {
		return "", "", 0, false
	}
	return rowID, rest[:separator], index, true
}

// notionImportOwnedFileSource reports whether a staged file belongs to a
// page, database or row rather than being a block of its own.
func notionImportOwnedFileSource(sourceID string) bool {
	if target, _ := notionImportPresentationFileTarget(sourceID); target != "" {
		return true
	}
	_, _, _, property := notionImportPropertyFileTarget(sourceID)
	return property
}

// refreshNotionImportPropertyFileURL reads the row again for a fresh address
// of a hosted file in one of its files values.
func refreshNotionImportPropertyFileURL(
	ctx context.Context, client notionProviderClient, sourceID string,
) (string, string, error) {
	rowID, propertyID, index, ok := notionImportPropertyFileTarget(sourceID)
	if !ok {
		return "", "", errors.New(errorCodeNotionImportFileRefreshFailed)
	}
	row, err := client.page(ctx, rowID)
	if err != nil || row.ID != rowID {
		return "", "", errors.New(errorCodeNotionImportFileRefreshFailed)
	}
	for _, property := range row.Properties {
		if property.ID != propertyID || property.Type != "files" {
			continue
		}
		files := []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			File struct {
				URL string `json:"url"`
			} `json:"file"`
		}{}
		if json.Unmarshal(property.Settings, &files) != nil || index >= len(files) ||
			files[index].Type != "file" || strings.TrimSpace(files[index].File.URL) == "" {
			break
		}
		name := strings.TrimSpace(files[index].Name)
		if name == "" {
			name = "file"
		}
		return strings.TrimSpace(files[index].File.URL), name, nil
	}
	return "", "", errors.New(errorCodeNotionImportFileRefreshFailed)
}

// notionImportFittedList keeps a list's items as far as a row value holds
// in one change, with room for the collaborative document's own encoding of
// it (half a change's bytes of JSON), and says whether all of them fit.
func notionImportFittedList[T any](items []T) ([]T, bool) {
	size := 2
	for index, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			return items[:index], false
		}
		size += len(encoded) + 1
		if size > maxGraphChangeBytes/2 {
			return items[:index], false
		}
	}
	return items, true
}

// notionProviderUserID is a Notion person's ID, or nothing.
func notionProviderUserID(user *notionProviderUser) string {
	if user == nil {
		return ""
	}
	return strings.TrimSpace(user.ID)
}
