package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errNotionDatabaseRowPageProjectionInvalid = errors.New("invalid Notion database row-page projection")

type notionDatabaseRowPagePropertyTypeError struct {
	Type string
}

func (e *notionDatabaseRowPagePropertyTypeError) Error() string {
	return fmt.Sprintf("database row-page property type %q is not supported", e.Type)
}

type storedNotionDatabaseRowPageProperty struct {
	ID        string         `db:"id"`
	Type      string         `db:"type"`
	Name      string         `db:"name"`
	Config    string         `db:"config"`
	ValueType sql.NullString `db:"value_type"`
	Value     sql.NullString `db:"value"`
	Spans     sql.NullString `db:"spans"`
	// PublicID is the property's ID as the Public API gives it
	// (notion_property_public_id.go).
	PublicID string `db:"-"`
	// FormulaResult is, for a formula, the kind of value its result is
	// (notionViewFormulaResult), which an empty result reads as; "" where
	// unknown.
	FormulaResult string `db:"-"`
}

type notionDatabaseRowPageProjectedProperty struct {
	// ID is Leafen's; PublicID the one the property goes out with.
	ID        string
	PublicID  string
	Name      string
	Type      string
	PageValue map[string]any
	Items     []map[string]any
	Paginated bool
}

func loadNotionDatabaseRowPageProperties(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	fileIssuer *notionBlockFileCapabilityIssuer,
) ([]notionDatabaseRowPageProjectedProperty, error) {
	rows, err := loadStoredNotionDatabaseRowPageProperties(app, authorization.Authority)
	if err != nil {
		return nil, err
	}
	computed, computedUnsupported, err := loadNotionDatabaseRowPageComputedValues(
		app, authorization.Identity, authorization.Authority, rows,
	)
	if err != nil {
		return nil, err
	}
	access, err := newNotionRowPropertyAccess(app, authorization.Identity.PrincipalID, authorization.Authority, rows)
	if err != nil {
		return nil, err
	}
	gone, empty, err := access.redact(rows, computed)
	if err != nil {
		return nil, err
	}
	projected := make([]notionDatabaseRowPageProjectedProperty, 0, len(rows))
	names := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if !notionDatabaseRowPagePropertyVisible(row.Type) || gone[row.ID] {
			continue
		}
		if _, duplicate := names[row.Name]; duplicate {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		names[row.Name] = struct{}{}
		property, err := projectNotionRowProperty(
			app, authorization, row, computed, computedUnsupported, fileIssuer, empty[row.ID],
		)
		if err != nil {
			return nil, err
		}
		projected = append(projected, property)
	}
	return projected, nil
}

func loadNotionDatabaseRowPageProperty(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	propertyID string,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (notionDatabaseRowPageProjectedProperty, bool, error) {
	return loadNotionDatabaseRowPagePropertyWith(app, authorization, propertyID, fileIssuer, nil)
}

// loadNotionDatabaseRowPagePropertyWith is one property of a row page: a
// formula or rollup computed through contexts (a nil memo loads the query
// context), any other read as stored — the row's computed values are worked
// out only for one of them (2026-11-12: a rollup showing 100 related rows'
// numbers computed each row's rollups too, loading its source's context).
func loadNotionDatabaseRowPagePropertyWith(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	propertyID string,
	fileIssuer *notionBlockFileCapabilityIssuer,
	contexts *databaseQueryContextMemo,
) (notionDatabaseRowPageProjectedProperty, bool, error) {
	rows, err := loadStoredNotionDatabaseRowPageProperties(app, authorization.Authority)
	if err != nil {
		return notionDatabaseRowPageProjectedProperty{}, false, err
	}
	// The property is named by its ID — Leafen's, or its public one as the
	// schema gives it or decoded — or by its name, as Notion's endpoint takes
	// it (seen 2026-11-09).
	wanted, wantedType, key := "", "", notionPropertyIDKey(propertyID)
	for _, row := range rows {
		if row.ID == propertyID || notionPropertyIDKey(row.PublicID) == key {
			wanted, wantedType = row.ID, row.Type
			break
		}
	}
	for _, row := range rows {
		if wanted == "" && row.Name == propertyID {
			wanted, wantedType = row.ID, row.Type
		}
	}
	if wanted == "" {
		return notionDatabaseRowPageProjectedProperty{}, false, nil
	}
	computed, computedUnsupported := map[string]databaseComputedValue{}, false
	if wantedType == graphValueTypeFormula || wantedType == graphValueTypeRollup {
		computed, computedUnsupported, err = loadNotionDatabaseRowPageComputedValuesWith(
			app, authorization.Identity, authorization.Authority, rows, contexts,
		)
		if err != nil {
			return notionDatabaseRowPageProjectedProperty{}, false, err
		}
	}
	access, err := newNotionRowPropertyAccess(app, authorization.Identity.PrincipalID, authorization.Authority, rows)
	if err != nil {
		return notionDatabaseRowPageProjectedProperty{}, false, err
	}
	gone, empty, err := access.redact(rows, computed)
	if err != nil {
		return notionDatabaseRowPageProjectedProperty{}, false, err
	}
	for _, row := range rows {
		if wanted == "" || row.ID != wanted {
			continue
		}
		if !notionDatabaseRowPagePropertyVisible(row.Type) || gone[row.ID] {
			return notionDatabaseRowPageProjectedProperty{}, false, nil
		}
		property, err := projectNotionRowProperty(
			app, authorization, row, computed, computedUnsupported, fileIssuer, empty[row.ID],
		)
		return property, err == nil, err
	}
	return notionDatabaseRowPageProjectedProperty{}, false, nil
}

func loadStoredNotionDatabaseRowPageProperties(
	app core.App,
	authority databaseRowPageAuthority,
) ([]storedNotionDatabaseRowPageProperty, error) {
	rows := []storedNotionDatabaseRowPageProperty{}
	err := app.DB().NewQuery(`
		SELECT [[property]].[[id]], [[property]].[[type]], [[property]].[[name]],
			[[property]].[[config]],
			[[value]].[[type]] AS [[value_type]], [[value]].[[value]], [[value]].[[spans]]
		FROM {{leafen_database_properties}} AS [[property]]
		LEFT JOIN {{leafen_database_row_values}} AS [[value]]
			ON [[value]].[[row_id]]={:rowId}
			AND [[value]].[[property_id]]=[[property]].[[id]]
			AND [[value]].[[owner]]=[[property]].[[owner]]
			AND [[value]].[[workspace_id]]=[[property]].[[workspace_id]]
			AND [[value]].[[page_id]]=[[property]].[[page_id]]
		WHERE [[property]].[[owner]]={:ownerId}
			AND [[property]].[[workspace_id]]={:workspaceId}
			AND [[property]].[[page_id]]={:pageId}
			AND [[property]].[[data_source_id]]={:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
		ORDER BY [[property]].[[position]], [[property]].[[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"rowId": authority.RowID, "ownerId": authority.OwnerID,
		"workspaceId": authority.WorkspaceID, "pageId": authority.ContainerPageID,
		"dataSourceId": authority.DataSourceID, "limit": maxDatabaseQueryProperties + 1,
	}).All(&rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows) > maxDatabaseQueryProperties {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	for _, row := range rows {
		if row.ID == "" || strings.TrimSpace(row.Name) == "" {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
	}
	if err := withNotionPublicPropertyIDs(app, authority.DataSourceID, rows); err != nil {
		return nil, err
	}
	// A template opened as a page holds its own values (notion_database_template_page.go).
	if authority.Template {
		if err := withDatabaseTemplateValues(app, authority.RowID, rows); err != nil {
			return nil, err
		}
	}
	withNotionFormulaResults(rows)
	return rows, nil
}

// withNotionPublicPropertyIDs gives each of a data source's properties the
// ID it goes out with.
func withNotionPublicPropertyIDs(app core.App, dataSourceID string, rows []storedNotionDatabaseRowPageProperty) error {
	ids, err := loadNotionPropertyIDs(app, dataSourceID)
	if err != nil {
		return err
	}
	for index := range rows {
		rows[index].PublicID = ids.publicID(rows[index].ID)
	}
	return nil
}

// publicID is the property's ID as it goes out; one read without its data
// source's IDs takes the one it would have first.
func (stored storedNotionDatabaseRowPageProperty) publicID() string {
	if stored.PublicID != "" {
		return stored.PublicID
	}
	if normalizeNotionDatabaseRowPagePropertyType(stored.Type) == graphValueTypeTitle {
		return notionTitlePropertyID
	}
	return notionPropertyIDs{}.publicID(stored.ID)
}

func projectNotionDatabaseRowPageProperty(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	stored storedNotionDatabaseRowPageProperty,
	computed map[string]databaseComputedValue,
	computedUnsupported bool,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (notionDatabaseRowPageProjectedProperty, error) {
	propertyType := normalizeNotionDatabaseRowPagePropertyType(stored.Type)
	if stored.ValueType.Valid && !graphValueTypesShareRepresentation(
		normalizeNotionDatabaseRowPagePropertyType(stored.ValueType.String), propertyType,
	) {
		return notionDatabaseRowPageProjectedProperty{}, errNotionDatabaseRowPageProjectionInvalid
	}
	officialType := notionDatabaseRowPageOfficialPropertyType(propertyType)
	result := notionDatabaseRowPageProjectedProperty{
		ID: stored.ID, PublicID: stored.publicID(), Name: stored.Name, Type: officialType,
	}
	value := any(nil)
	switch propertyType {
	case graphValueTypeTitle, graphValueTypeRichText:
		text, err := notionDatabaseRowPageStringValue(stored)
		if err != nil {
			return result, err
		}
		// A cell with formatting, links or mentions gives them as rich text;
		// one the official form can't hold gives its text, a mention of a
		// page the connection can't reach "Untitled" in it.
		spans := databaseStoredSpans(stored.Spans.String)
		if words, hid := spansAsRead(notionIdentityCloses(app, authorization.Identity), spans); hid {
			text = words
		}
		richText := notionPageTitleRichText(text)
		if len(spans) > 0 {
			formatted, ok, err := notionSpansRichText(app, notionBlockAuthorization{
				Identity: authorization.Identity, Page: authorization.Page,
			}, spans, fileIssuer)
			if err != nil {
				return result, err
			}
			if ok {
				richText = formatted
			}
		}
		value = richText
		result.Paginated = true
		result.Items = make([]map[string]any, len(richText))
		for index, item := range richText {
			result.Items[index] = map[string]any{
				"object": "property_item", "id": result.PublicID,
				"type": officialType, officialType: item,
			}
		}
	case graphValueTypeNumber:
		if stored.Value.Valid {
			var number float64
			if json.Unmarshal([]byte(stored.Value.String), &number) != nil {
				return result, errNotionDatabaseRowPageProjectionInvalid
			}
			value = number
		}
	case graphValueTypeCheckbox:
		value = false
		if stored.Value.Valid {
			var checked bool
			if json.Unmarshal([]byte(stored.Value.String), &checked) != nil {
				return result, errNotionDatabaseRowPageProjectionInvalid
			}
			value = checked
		}
	case graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		text, err := notionDatabaseRowPageStringValue(stored)
		if err != nil {
			return result, err
		}
		if text != "" {
			value = text
		}
	case graphValueTypeDate:
		if stored.Value.Valid {
			var date map[string]any
			raw := json.RawMessage(stored.Value.String)
			if json.Unmarshal(raw, &date) != nil || !graphDateValue(date) {
				return result, errNotionDatabaseRowPageProjectionInvalid
			}
			start, _ := date["start"].(string)
			end, _ := date["end"].(string)
			zone, _ := date["timeZone"].(string)
			value = notionDatePropertyObject(start, end, zone)
		}
	case graphValueTypeSelect, graphValueTypeStatus:
		optionID, err := notionDatabaseRowPageOptionValue(stored)
		if err != nil {
			return result, err
		}
		if optionID != "" {
			options, err := notionDatabaseRowPageOptions(stored)
			if err != nil {
				return result, err
			}
			option, found := notionDatabaseRowPageOptionByID(options, optionID)
			if !found {
				return result, errNotionDatabaseRowPageProjectionInvalid
			}
			value = option.response()
		}
	case graphValueTypeMultiSelect:
		optionIDs := []string{}
		if stored.Value.Valid && json.Unmarshal([]byte(stored.Value.String), &optionIDs) != nil {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		options, err := notionDatabaseRowPageOptions(stored)
		if err != nil {
			return result, err
		}
		selected := make([]map[string]any, 0, len(optionIDs))
		seen := make(map[string]struct{}, len(optionIDs))
		for _, optionID := range optionIDs {
			if _, duplicate := seen[optionID]; duplicate {
				return result, errNotionDatabaseRowPageProjectionInvalid
			}
			seen[optionID] = struct{}{}
			// One the property no longer has is not set (databaseOptionValueKnown).
			if option, found := notionDatabaseRowPageOptionByID(options, optionID); found {
				selected = append(selected, option.response())
			}
		}
		value = selected
	case graphValueTypePerson:
		peopleIDs := []string{}
		if stored.Value.Valid && json.Unmarshal([]byte(stored.Value.String), &peopleIDs) != nil {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		// A person that defaults to the creator reads as the row's creator
		// while it names no one, as Notion's API returns it.
		if len(peopleIDs) == 0 && authorization.Authority.CreatedBy != "" &&
			databasePersonDefaultsToCreator(stored.Type, stored.Config) {
			peopleIDs = []string{authorization.Authority.CreatedBy}
		}
		people, err := notionDatabaseRowPagePeople(app, authorization.Identity, peopleIDs)
		if err != nil {
			return result, err
		}
		value = people
		result.Paginated = true
		result.Items = make([]map[string]any, len(people))
		for index, person := range people {
			result.Items[index] = map[string]any{
				"object": "property_item", "id": result.PublicID,
				"type": officialType, officialType: person,
			}
		}
	case graphValueTypeRelation:
		relationIDs := []string{}
		if stored.Value.Valid && json.Unmarshal([]byte(stored.Value.String), &relationIDs) != nil {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		relations, err := projectNotionDatabaseRowPageRelations(
			app, authorization, stored, relationIDs,
		)
		if err != nil {
			return result, err
		}
		value = relations
		result.Paginated = true
		result.Items = make([]map[string]any, len(relations))
		for index, relation := range relations {
			result.Items[index] = map[string]any{
				"object": "property_item", "id": result.PublicID,
				"type": officialType, officialType: relation,
			}
		}
	case graphValueTypeCreatedTime:
		if stored.ValueType.Valid || stored.Value.Valid {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		projected, err := notionDatabaseRowPageTime(authorization.Authority.CreatedAt)
		if err != nil {
			return result, err
		}
		value = projected
	case graphValueTypeLastEditedTime:
		if stored.ValueType.Valid || stored.Value.Valid {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		projected, err := notionDatabaseRowPageTime(authorization.Authority.UpdatedAt)
		if err != nil {
			return result, err
		}
		value = projected
	case graphValueTypeCreatedBy:
		if stored.ValueType.Valid || stored.Value.Valid {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		projected, err := notionDatabaseRowPageAuthor(app, authorization, false)
		if err != nil {
			return result, err
		}
		value = projected
	case graphValueTypeLastEditedBy:
		if stored.ValueType.Valid || stored.Value.Valid {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		projected, err := notionDatabaseRowPageAuthor(app, authorization, true)
		if err != nil {
			return result, err
		}
		value = projected
	case graphValueTypeUniqueID:
		projected, err := notionDatabaseRowPageUniqueID(stored)
		if err != nil {
			return result, err
		}
		value = projected
	case graphValueTypeButton:
		if stored.ValueType.Valid || stored.Value.Valid {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		value = map[string]any{}
	case graphValueTypeLastVisitedTime:
		// Nothing fills a last visited time in (decided 2026-10-16), so it
		// reads as Notion's API reads one its connection never visited: null.
	case graphValueTypePlace:
		if stored.Value.Valid {
			projected, err := notionDatabaseRowPagePlace(json.RawMessage(stored.Value.String))
			if err != nil {
				return result, err
			}
			value = projected
		}
	case graphValueTypeVerification:
		if stored.Value.Valid {
			projected, err := notionDatabaseRowPageVerification(
				json.RawMessage(stored.Value.String), time.Now().UTC(),
			)
			if err != nil {
				return result, err
			}
			value = projected
		}
	case graphValueTypeFiles:
		files, err := notionDatabaseRowPageFiles(app, authorization, stored, fileIssuer)
		if err != nil {
			return result, err
		}
		value = files
	case graphValueTypeFormula:
		computedValue, found := computed[stored.ID]
		if !found && !computedUnsupported {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		projected, err := notionDatabaseRowPageFormulaValue(stored, computedValue, computedUnsupported)
		if err != nil {
			return result, err
		}
		value = projected
	case graphValueTypeRollup:
		computedValue, found := computed[stored.ID]
		if !found && !computedUnsupported {
			return result, errNotionDatabaseRowPageProjectionInvalid
		}
		projected, err := notionDatabaseRowPageRollupValue(stored, computedValue, computedUnsupported)
		if err != nil {
			return result, err
		}
		if projected, err = notionRollupShownPageValue(app, authorization, stored.ID, projected, fileIssuer); err != nil {
			return result, err
		}
		value = projected
	default:
		return result, &notionDatabaseRowPagePropertyTypeError{Type: propertyType}
	}
	result.PageValue = map[string]any{
		"id": result.PublicID, "type": officialType, officialType: value,
	}
	if propertyType == graphValueTypeRelation {
		relations := value.([]map[string]any)
		if len(relations) > 25 {
			result.PageValue[officialType] = relations[:25]
		}
		result.PageValue["has_more"] = len(relations) > 25
	}
	return result, nil
}

func notionDatabaseRowPagePropertyUser(internalID string) (map[string]any, error) {
	publicID, err := notionCompatibleUserID(internalID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"object": "user", "id": publicID}, nil
}

// notionDatabaseRowPageWholeUser is a creator or last editor as their
// property reads them.
func notionDatabaseRowPageWholeUser(app core.App, identity notionOAuthBearerIdentity, internalID string) (map[string]any, error) {
	people, err := notionDatabaseRowPagePeople(app, identity, []string{internalID})
	if err != nil {
		return nil, err
	}
	if len(people) == 0 {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	return people[0], nil
}

func notionDatabaseRowPageUniqueID(stored storedNotionDatabaseRowPageProperty) (map[string]any, error) {
	config := struct {
		IDPrefix any `json:"idPrefix"`
	}{}
	if json.Unmarshal([]byte(stored.Config), &config) != nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	var prefix any
	if config.IDPrefix != nil {
		configured, ok := config.IDPrefix.(string)
		if !ok {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		configured = strings.TrimSpace(configured)
		if configured != "" {
			prefix = configured
		}
	}
	var number any
	if stored.Value.Valid {
		var decoded float64
		if json.Unmarshal([]byte(stored.Value.String), &decoded) != nil || decoded < 1 ||
			decoded > 9007199254740991 || math.Trunc(decoded) != decoded {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		number = int64(decoded)
	}
	return map[string]any{"number": number, "prefix": prefix}, nil
}

func notionDatabaseRowPageStringValue(stored storedNotionDatabaseRowPageProperty) (string, error) {
	if !stored.Value.Valid {
		return "", nil
	}
	var value string
	if json.Unmarshal([]byte(stored.Value.String), &value) != nil {
		return "", errNotionDatabaseRowPageProjectionInvalid
	}
	return value, nil
}

func normalizeNotionDatabaseRowPagePropertyType(value string) string {
	if value == "text" {
		return graphValueTypeRichText
	}
	return strings.TrimSpace(value)
}

// notionDatabaseRowPagePropertyVisible reports whether Notion's API shows a
// property of the type. Leafen's own location, which Notion's API doesn't
// name, is left out (user decision 2026-09-25); a last visited time is
// Notion's API's own (seen 2026-11-12: a data source reads it as
// {"last_visited_time": {}}, a page as null).
func notionDatabaseRowPagePropertyVisible(value string) bool {
	return normalizeNotionDatabaseRowPagePropertyType(value) != graphValueTypeLocation
}

// mcpDatabasePropertyShown reports whether Leafen's MCP shows a property of
// the type: as the API, but without a last visited time, how Notion's MCP
// shows one not having been seen.
func mcpDatabasePropertyShown(value string) bool {
	return notionDatabaseRowPagePropertyVisible(value) &&
		normalizeNotionDatabaseRowPagePropertyType(value) != graphValueTypeLastVisitedTime
}

func notionDatabaseRowPageOfficialPropertyType(value string) string {
	if value == graphValueTypePhone {
		return "phone_number"
	}
	if value == graphValueTypePerson {
		return "people"
	}
	return value
}

func notionDatabaseRowPageTime(value int64) (string, error) {
	if value < 1 {
		return "", errNotionDatabaseRowPageProjectionInvalid
	}
	return notionMinuteTimestamp(time.UnixMilli(value)), nil
}
