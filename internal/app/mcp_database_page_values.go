package app

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's MCP writes a database page's properties as SQLite values: text as
// strings, numbers as numbers, a checkbox as __YES__ or __NO__, a select by
// its option's name, multi-select and relation and people as lists, a date
// split into date:{name}:start, :end and :is_datetime, a place split into
// place:{name}:name, :address, :latitude, :longitude and :google_place_id,
// and null to clear a value. Properties named id or url take a userDefined:
// prefix. mcpDatabasePageProperties reads them against the data source's
// properties as the Public API's property objects, for the row page planner.

const (
	mcpDatabaseDatePrefix  = "date:"
	mcpDatabasePlacePrefix = "place:"
	mcpDatabaseUserDefined = "userDefined:"
)

// mcpDatabasePageProperties reads an MCP page tool's database properties as
// the Public API's property objects, keyed by property ID; said gathers what
// reading their text said (mcpInlineMarkdownSpans).
func mcpDatabasePageProperties(
	app core.App,
	authority mcpAuthority,
	authorization notionDatabaseRowPageAuthorization,
	input map[string]any,
	sent mcpSentReferences,
	said *mcpMarkdownSaid,
) (map[string]json.RawMessage, error) {
	container := authorization.Container
	schema, err := loadStoredNotionDatabaseRowPageProperties(app, databaseRowPageAuthority{
		OwnerID: container.OwnerID, WorkspaceID: container.WorkspaceID,
		ContainerPageID: container.PageID, DataSourceID: container.DataSource,
	})
	if err != nil {
		return nil, err
	}
	byName := make(map[string]storedNotionDatabaseRowPageProperty, len(schema))
	for _, property := range schema {
		if mcpDatabasePropertyShown(property.Type) {
			byName[property.Name] = property
		}
	}
	find := func(name string) (storedNotionDatabaseRowPageProperty, error) {
		property, found := byName[name]
		if !found {
			return property, mcpDatabasePageValueInvalid("database property %q does not exist", name)
		}
		return property, nil
	}
	values := map[string]any{}
	dates, places := map[string]map[string]any{}, map[string]map[string]any{}
	for key, value := range input {
		for prefix, parts := range map[string]map[string]map[string]any{
			mcpDatabaseDatePrefix: dates, mcpDatabasePlacePrefix: places,
		} {
			if name, part, split := mcpDatabaseSplitKey(key, prefix); split {
				if parts[name] == nil {
					parts[name] = map[string]any{}
				}
				parts[name][part] = value
				key = ""
				break
			}
		}
		if key == "" {
			continue
		}
		if name, prefixed := strings.CutPrefix(key, mcpDatabaseUserDefined); prefixed {
			key = name
		} else if lower := strings.ToLower(key); lower == "id" || lower == "url" {
			return nil, mcpDatabasePageValueInvalid("the property %q is written as %s%s", key, mcpDatabaseUserDefined, key)
		}
		values[key] = value
	}
	request := make(map[string]json.RawMessage, len(values)+len(dates)+len(places))
	add := func(property storedNotionDatabaseRowPageProperty, payload any) error {
		if _, duplicate := request[property.ID]; duplicate {
			return mcpDatabasePageValueInvalid("database property %q was given more than once", property.Name)
		}
		encoded, err := json.Marshal(map[string]any{
			notionDatabaseRowPageOfficialPropertyType(normalizeNotionDatabaseRowPagePropertyType(property.Type)): payload,
		})
		if err != nil {
			return err
		}
		request[property.ID] = encoded
		return nil
	}
	for name, value := range values {
		property, err := find(name)
		if err != nil {
			return nil, err
		}
		payload, err := mcpDatabasePageValue(app, authority, authorization, property, value, sent, said)
		if err != nil {
			return nil, err
		}
		if err := add(property, payload); err != nil {
			return nil, err
		}
	}
	for name, parts := range dates {
		property, err := find(name)
		if err != nil {
			return nil, err
		}
		if normalizeNotionDatabaseRowPagePropertyType(property.Type) != graphValueTypeDate {
			return nil, mcpDatabasePageValueInvalid("database property %q is not a date", name)
		}
		payload, err := mcpDatabasePageDate(name, parts, authority.clock())
		if err != nil {
			return nil, err
		}
		if err := add(property, payload); err != nil {
			return nil, err
		}
	}
	for name, parts := range places {
		property, err := find(name)
		if err != nil {
			return nil, err
		}
		if normalizeNotionDatabaseRowPagePropertyType(property.Type) != graphValueTypePlace {
			return nil, mcpDatabasePageValueInvalid("database property %q is not a place", name)
		}
		payload, err := mcpDatabasePagePlace(name, parts)
		if err != nil {
			return nil, err
		}
		if err := add(property, payload); err != nil {
			return nil, err
		}
	}
	return request, nil
}

// mcpDatabaseSplitKey reads date:{name}:{part} or place:{name}:{part}; a
// property's name may hold colons of its own.
func mcpDatabaseSplitKey(key, prefix string) (string, string, bool) {
	rest, found := strings.CutPrefix(key, prefix)
	if !found {
		return "", "", false
	}
	at := strings.LastIndex(rest, ":")
	if at <= 0 {
		return "", "", false
	}
	return rest[:at], rest[at+1:], true
}

// mcpDatabasePageValue reads one property's SQLite value as the Public API's
// payload for its type; the planner then checks it as it checks any request.
func mcpDatabasePageValue(
	app core.App,
	authority mcpAuthority,
	authorization notionDatabaseRowPageAuthorization,
	property storedNotionDatabaseRowPageProperty,
	value any,
	sent mcpSentReferences,
	said *mcpMarkdownSaid,
) (any, error) {
	text := func(content string) []map[string]any {
		if content == "" {
			return []map[string]any{}
		}
		return []map[string]any{{"type": "text", "text": map[string]any{"content": content}}}
	}
	name := property.Name
	switch nativeType := normalizeNotionDatabaseRowPagePropertyType(property.Type); nativeType {
	case graphValueTypeTitle, graphValueTypeRichText:
		content, ok := value.(string)
		if value != nil && !ok {
			return nil, mcpDatabasePageValueInvalid("database property %q takes text", name)
		}
		// Its text reads as an MCP write's inline Markdown (mcp_inline_markdown.go).
		words, spans, read := mcpInlineMarkdownSpans(content, mcpValueSitePages(app, authority, authorization.Page))
		said.add(read)
		if len(spans) == 0 {
			return text(words), nil
		}
		return mcpPropertyRichText(app, authority, authorization.Page, spans)
	case graphValueTypeNumber:
		if value == nil {
			return nil, nil
		}
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, mcpDatabasePageValueInvalid("database property %q takes a number", name)
		}
		return number, nil
	case graphValueTypeCheckbox:
		switch value {
		case "__YES__":
			return true, nil
		case "__NO__", nil:
			return false, nil
		}
		return nil, mcpDatabasePageValueInvalid("database property %q takes __YES__ or __NO__", name)
	case graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		content, ok := value.(string)
		if value != nil && !ok {
			return nil, mcpDatabasePageValueInvalid("database property %q takes text", name)
		}
		if content == "" {
			return nil, nil
		}
		return content, nil
	case graphValueTypeSelect, graphValueTypeStatus:
		option, ok := value.(string)
		if value != nil && !ok {
			return nil, mcpDatabasePageValueInvalid("database property %q takes an option's name", name)
		}
		if option == "" {
			return nil, nil
		}
		return map[string]any{"name": option}, nil
	case graphValueTypeMultiSelect:
		names, err := mcpDatabasePageList(name, value, `["Option A", "Option B"]`)
		if err != nil {
			return nil, err
		}
		options := make([]map[string]any, 0, len(names))
		for _, option := range names {
			options = append(options, map[string]any{"name": option})
		}
		return options, nil
	case graphValueTypeRelation:
		references, err := mcpDatabasePageList(name, value, `["<page URL or ID>"]`)
		if err != nil {
			return nil, err
		}
		return mcpDatabasePageRelations(app, authority, authorization, property, references, sent)
	case graphValueTypePerson:
		references, err := mcpDatabasePageList(name, value, `["user://<ID>"]`)
		if err != nil {
			return nil, err
		}
		people := make([]map[string]any, 0, len(references))
		for _, reference := range references {
			userID, err := mcpDatabasePageUserID(reference)
			if err != nil {
				return nil, mcpDatabasePageValueInvalid("database property %q names someone it can't: %s", name, reference)
			}
			sent.note(userID, notionPersonReferenceID(reference))
			people = append(people, map[string]any{"id": userID})
		}
		return people, nil
	case graphValueTypeFiles:
		return mcpDatabasePageFiles(app, authority, authorization, property, value)
	case graphValueTypeDate:
		return nil, mcpDatabasePageValueInvalid("database property %q is written as date:%s:start", name, name)
	case graphValueTypePlace:
		return nil, mcpDatabasePageValueInvalid("database property %q is written as place:%s:latitude and the rest", name, name)
	case graphValueTypeVerification:
		return nil, mcpDatabasePageValueInvalid("database property %q is set with update_verification", name)
	}
	// Formulas, rollups, buttons and the recorded times, people and IDs
	// can't be written; the planner says so.
	return value, nil
}

// mcpDatabasePageFiles reads a files value, a list as the other lists are
// (mcpDatabasePageItems): uploaded files, by their file_upload_id,
// file-upload://<ID> or as file_upload objects, each under the name given
// it, the name the row holds it under (mcpFilesNamesHeld) or the name it
// was uploaded with, taken into the row's workspace by the one rule for
// files named by ID (mcp_file_attach.go), and linked files as the API
// writes them, as fetch and SQL give them. Leafen has no folders.
func mcpDatabasePageFiles(
	app core.App, authority mcpAuthority, authorization notionDatabaseRowPageAuthorization,
	property storedNotionDatabaseRowPageProperty, value any,
) (any, error) {
	name := property.Name
	items, err := mcpDatabasePageItems(name, mcpFilesValueListed(value), `["<file_upload_id>"]`)
	if err != nil {
		return nil, err
	}
	held, err := mcpFilesNamesHeld(app, authorization.Authority.RowID, property.ID)
	if err != nil {
		return nil, err
	}
	files := make([]map[string]any, 0, len(items))
	for _, item := range items {
		reference, _ := item.(string)
		given := ""
		// A linked file as fetch and SQL give one (mcp_files_value.go).
		if object, ok := item.(map[string]any); ok {
			if external, linked := mcpExternalFileWritten(object); linked {
				files = append(files, external)
				continue
			}
		}
		if object, ok := item.(map[string]any); ok {
			upload, _ := object["file_upload"].(map[string]any)
			reference, _ = upload["id"].(string)
			given, _ = object["name"].(string)
			if kind, _ := object["type"].(string); kind != "file_upload" {
				reference = ""
			}
		}
		if strings.TrimSpace(reference) == "" {
			return nil, mcpDatabasePageValueInvalid(
				"database property %q takes uploaded files, by file_upload_id or file-upload://<ID>, and files linked from elsewhere as "+
					`{"type":"external","name":…,"external":{"url":…}}; Leafen has no folders`, name,
			)
		}
		record, err := mcpAttachableFile(app, authority, reference, authorization.Container.WorkspaceID)
		if err != nil {
			return nil, err
		}
		publicID, err := ensureNotionObjectIdentity(
			app, notionObjectIdentityFileUpload, authorization.Container.WorkspaceID, record.Id,
		)
		if err != nil {
			return nil, err
		}
		named, kept := held[record.Id]
		if !kept {
			named = canonicalGraphFileContent(record).Name
		}
		files = append(files, map[string]any{
			"type": "file_upload", "file_upload": map[string]any{"id": publicID},
			"name": cmp.Or(strings.TrimSpace(given), named),
		})
	}
	return files, nil
}

// mcpDatabasePageList reads a list of names or references
// (mcpDatabasePageItems), each text.
func mcpDatabasePageList(name string, value any, example string) ([]string, error) {
	items, err := mcpDatabasePageItems(name, value, example)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, mcpDatabasePageValueInvalid("database property %q takes a list of text", name)
		}
		result = append(result, strings.TrimSpace(text))
	}
	return result, nil
}

// mcpDatabasePageUserID is the Notion-compatible ID of someone named by it,
// by a user:// link or an SQL row's notion_user- value, or by their Leafen
// ID; a bot of the workspace's connections (notion_people_value_bots.go) or
// someone invited (people_value_invitees.go) by their ID, which the write
// then looks for.
func mcpDatabasePageUserID(reference string) (string, error) {
	reference = notionPersonReferenceID(reference)
	if validNotionCompatibleInternalID(reference) {
		return notionCompatibleUserID(reference)
	}
	if notionBotUserIDShape(reference) || peopleValueInviteeIDShape(reference) {
		return reference, nil
	}
	if _, err := resolveNotionCompatibleUserID(reference); err != nil {
		return "", err
	}
	return reference, nil
}

// mcpDatabasePageDate reads date:{name}:start, :end and :is_datetime; a date
// that isn't a time keeps its day only, and a time without an offset is on
// the person's clock (personClockWritten).
func mcpDatabasePageDate(name string, parts map[string]any, clock *time.Location) (any, error) {
	for part := range parts {
		if part != "start" && part != "end" && part != "is_datetime" && part != "deadline" {
			return nil, mcpDatabasePageValueInvalid("date:%s:%s is not a date part", name, part)
		}
	}
	start, startText := parts["start"].(string)
	if parts["start"] == nil {
		if parts["end"] != nil || parts["deadline"] != nil {
			return nil, mcpDatabasePageValueInvalid("date:%s:end and date:%s:deadline need date:%s:start", name, name, name)
		}
		return nil, nil
	}
	if !startText {
		return nil, mcpDatabasePageValueInvalid("date:%s:start takes an ISO date", name)
	}
	withTime := true
	switch flag := parts["is_datetime"]; flag {
	case nil:
		withTime = strings.Contains(start, "T")
	case float64(0), "0":
		withTime = false
	case float64(1), "1":
	default:
		return nil, mcpDatabasePageValueInvalid("date:%s:is_datetime takes 0 or 1", name)
	}
	day := func(value string) string {
		if !withTime && len(value) > 10 {
			return value[:10]
		}
		return personClockWritten(value, clock)
	}
	date := map[string]any{"start": day(start)}
	if end, ok := parts["end"].(string); ok && end != "" {
		date["end"] = day(end)
	} else if parts["end"] != nil && !ok {
		return nil, mcpDatabasePageValueInvalid("date:%s:end takes an ISO date", name)
	}
	// Leafen's own: the days before the date its notices go; left out, the
	// item's stays, and "" clears it (deadline_values.go).
	if deadline, present := parts["deadline"]; present {
		days, ok := deadline.(string)
		if deadline != nil && !ok {
			return nil, mcpDatabasePageValueInvalid("date:%s:deadline takes days as \"7,3,1,0\", or \"\" to clear", name)
		}
		if days = strings.TrimSpace(days); days != "" {
			canonical, _, valid := canonicalDeadlineDays(days)
			if !valid {
				return nil, mcpDatabasePageValueInvalid("date:%s:deadline takes days as \"7,3,1,0\" (0 to 365, at most 8), or \"\" to clear", name)
			}
			days = canonical
		}
		date["deadline"] = days
	}
	return date, nil
}

// mcpDatabasePagePlace reads place:{name}:… as the Public API's place; all
// parts null clears it.
func mcpDatabasePagePlace(name string, parts map[string]any) (any, error) {
	place := map[string]any{}
	empty := true
	for part, value := range parts {
		key := map[string]string{
			"name": "name", "address": "address", "latitude": "lat", "longitude": "lon",
			"google_place_id": "google_place_id",
		}[part]
		if key == "" {
			return nil, mcpDatabasePageValueInvalid("place:%s:%s is not a place part", name, part)
		}
		if value != nil {
			empty = false
			place[key] = value
		}
	}
	if empty {
		return nil, nil
	}
	return place, nil
}

func mcpDatabasePageValueInvalid(format string, values ...any) error {
	return fmt.Errorf("%w: %s", errMCPToolInvalid, fmt.Sprintf(format, values...))
}

// mcpDatabasePageItems reads a list value — options, relations, people and
// files alike: a JSON array, the same written as text, as fetch and SQL read
// them and Notion's tool takes them (seen 2026-11-04), or one item; null is
// an empty list. Any other is refused with the list's form, an example
// (example) beside it.
func mcpDatabasePageItems(name string, value any, example string) ([]any, error) {
	var items []any
	switch typed := value.(type) {
	case nil:
		return []any{}, nil
	case []any:
		return typed, nil
	case string:
		if !strings.HasPrefix(strings.TrimSpace(typed), "[") {
			return []any{typed}, nil
		}
		if json.Unmarshal([]byte(typed), &items) == nil {
			return items, nil
		}
	}
	return nil, mcpDatabasePageValueInvalid(
		"database property %q takes a list: a JSON array such as %s, or the same written as text", name, example,
	)
}
