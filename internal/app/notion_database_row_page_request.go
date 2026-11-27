package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionDatabaseRowPageRequestError struct {
	message string
	// missingPage is a related page the request names that the caller
	// can't open, which the Public API answers as Notion's does
	// (notionRelationPageNotFound).
	missingPage string
}

func (e *notionDatabaseRowPageRequestError) Error() string {
	return e.message
}

// notionDatabaseRowPageUserMissing is a person a request names who isn't in the
// workspace, answered as Notion answers one it can't find.
type notionDatabaseRowPageUserMissing struct{ id string }

func (e *notionDatabaseRowPageUserMissing) Error() string { return "user not found: " + e.id }

// notionUserNotFoundMessage says a person can't be found, as Notion's API does.
func notionUserNotFoundMessage(publicID string) string {
	return "There's no user with ID: " + publicID + " that this integration can reach. Reading people takes its capability " +
		"to read user information, and even then it sees only the members and guests of its own workspace."
}

func notionDatabaseRowPageRequestInvalid(format string, values ...any) error {
	return &notionDatabaseRowPageRequestError{message: fmt.Sprintf(format, values...)}
}

// planNotionDatabaseRowPageRequestValues reads a request's property values,
// and the change to the properties that the new options they name make.
func planNotionDatabaseRowPageRequestValues(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	properties notionRequestProperties,
	ensureTitle bool,
) (map[string]graphRowValue, []graphOperation, error) {
	container := authorization.Container
	authority := databaseRowPageAuthority{
		OwnerID: container.OwnerID, WorkspaceID: container.WorkspaceID,
		ContainerPageID: container.PageID, DataSourceID: container.DataSource,
	}
	schema, err := loadStoredNotionDatabaseRowPageProperties(app, authority)
	if err != nil {
		return nil, nil, err
	}
	index, err := newNotionRowPropertyIndex(schema)
	if err != nil {
		return nil, nil, err
	}
	titleIDs := index.titleIDs
	values := make(map[string]graphRowValue, properties.count()+1)
	newOptions := map[string][]databaseRowPageNewOption{}
	for _, key := range properties.keys {
		raw := properties.values[key]
		property, found := index.find(key)
		if !found {
			// A property the source doesn't have, refused as Notion's API refuses it (seen 2026-11-09).
			return nil, nil, notionDatabaseRowPageRequestInvalid("%s isn't a property of this database.", key)
		}
		if _, duplicate := values[property.ID]; duplicate {
			return nil, nil, notionDatabaseRowPageRequestInvalid("database property %q was supplied more than once", key)
		}
		added := []databaseRowPageNewOption{}
		value, err := notionDatabaseRowPageRequestValue(
			app, authorization, property, raw, &added,
		)
		if err != nil {
			return nil, nil, err
		}
		values[property.ID] = value
		if len(added) > 0 {
			newOptions[property.ID] = added
		}
	}
	if ensureTitle {
		if _, supplied := values[titleIDs[0]]; !supplied {
			encoded, _ := json.Marshal("")
			values[titleIDs[0]] = graphRowValue{
				Type: graphValueTypeTitle, Value: encoded,
			}
		}
	}
	if len(newOptions) == 0 {
		return values, nil, nil
	}
	operations, err := planDatabaseRowPageNewOptions(app, authority, newOptions, values)
	return values, operations, err
}

func notionDatabaseRowPageRequestValue(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	property storedNotionDatabaseRowPageProperty,
	raw json.RawMessage,
	added *[]databaseRowPageNewOption,
) (graphRowValue, error) {
	nativeType := normalizeNotionDatabaseRowPagePropertyType(property.Type)
	officialType := notionDatabaseRowPageOfficialPropertyType(nativeType)
	if _, readOnly := notionRowReadOnlyTypes[nativeType]; readOnly {
		return graphRowValue{}, notionDatabaseRowPageRequestInvalid(
			"database property %q is read-only", property.Name,
		)
	}
	if _, supported := map[string]struct{}{
		graphValueTypeTitle: {}, graphValueTypeRichText: {}, graphValueTypeNumber: {},
		graphValueTypeCheckbox: {}, graphValueTypeDate: {}, graphValueTypeURL: {},
		graphValueTypeEmail: {}, graphValueTypePhone: {}, graphValueTypeSelect: {},
		graphValueTypeStatus: {}, graphValueTypeMultiSelect: {}, graphValueTypePerson: {},
		graphValueTypeRelation: {}, graphValueTypePlace: {}, graphValueTypeVerification: {},
		graphValueTypeFiles: {},
	}[nativeType]; !supported {
		return graphRowValue{}, &notionDatabaseRowPagePropertyTypeError{Type: officialType}
	}
	object := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property value must be an object")
	}
	payload, present := object[officialType]
	if !present {
		return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q must contain %s", property.Name, officialType)
	}
	for key := range object {
		if key != "type" && key != officialType {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q contains an unsupported field", property.Name)
		}
	}
	if encodedType, present := object["type"]; present {
		var requestedType string
		if json.Unmarshal(encodedType, &requestedType) != nil || requestedType != officialType {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q type does not match its schema", property.Name)
		}
	}
	// A null number, date or place clears the value, as in Notion; the graph
	// mutation removes the cell.
	var value any
	switch nativeType {
	case graphValueTypeTitle, graphValueTypeRichText:
		// Formatting, links, mentions and equations are kept as the cell's
		// spans; mentions are checked and titled before the text is measured.
		// A mention Notion refuses for what it names is said alone. A value
		// the MCP read from Markdown keeps a mention of someone who isn't
		// here, as Notion's MCP does (seen 2026-11-11).
		markdown := notionIdentityWritesMarkdown(authorization.Identity)
		spans, err := notionTextRequestSpans(payload, markdown)
		if alone, refused := notionMentionRefusedAlone(err); refused {
			return graphRowValue{}, alone
		}
		if err != nil {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q rich text is invalid", property.Name)
		}
		preparation := newNotionMentionPreparation(app, authorization.Identity, authorization.Page)
		preparation.markdown = markdown
		// What the row holds stays (notion_mention_held.go); a new row holds nothing.
		preparation.held = newNotionHeldMentions(app, authorization.Page.Workspace.OwnerID, authorization.Authority.RowID)
		if err := notionSpansHere(app, authorization.Page, preparation.prepare)(spans); err != nil {
			if alone, refused := notionMentionRefusedAlone(err); refused {
				return graphRowValue{}, alone
			}
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q mentions something it can't", property.Name)
		}
		var text strings.Builder
		for _, span := range spans {
			text.WriteString(span.Text)
		}
		// A title is held to the page title's length; other text, as
		// Notion's, only to the request's pieces.
		if nativeType == graphValueTypeTitle && len([]rune(text.String())) > maxGraphTitleRunes {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q is too long", property.Name)
		}
		encoded, err := json.Marshal(text.String())
		if err != nil {
			return graphRowValue{}, err
		}
		cellSpans, err := normalizeGraphRowValueSpans(nativeType, encoded, spans)
		if err != nil {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q rich text is invalid", property.Name)
		}
		return graphRowValue{Type: nativeType, Value: encoded, Spans: cellSpans}, nil
	case graphValueTypeNumber:
		if string(payload) == "null" {
			break
		}
		var number float64
		if json.Unmarshal(payload, &number) != nil {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q number is invalid", property.Name)
		}
		value = number
	case graphValueTypeCheckbox:
		var checked bool
		if json.Unmarshal(payload, &checked) != nil {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q checkbox is invalid", property.Name)
		}
		value = checked
	case graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		var text *string
		if json.Unmarshal(payload, &text) != nil {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q text is invalid", property.Name)
		}
		if text == nil {
			value = ""
		} else {
			value = *text
		}
	case graphValueTypeDate:
		if string(payload) == "null" {
			break
		}
		date := map[string]any{}
		if json.Unmarshal(payload, &date) != nil || !graphDateValue(date) {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q date is invalid", property.Name)
		}
		kept, err := notionDatabaseRowPageRequestDate(property, date)
		if err != nil {
			return graphRowValue{}, err
		}
		date["start"] = kept.start
		delete(date, "end")
		if kept.end != "" {
			date["end"] = kept.end
		}
		delete(date, "time_zone")
		if kept.zone != "" {
			date["timeZone"] = kept.zone
		}
		// Reminders are set in Leafen; the Notion date object has none.
		delete(date, "reminder")
		// A deadline is Leafen's own (deadline_values.go): a request that
		// doesn't name it keeps the item's, and null or "" clears it.
		if _, named := date["deadline"]; !named && authorization.Authority.RowID != "" {
			var stored string
			_ = app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}`).
				Bind(dbx.Params{"rowId": authorization.Authority.RowID, "propertyId": property.ID}).Row(&stored)
			held := map[string]any{}
			if json.Unmarshal([]byte(stored), &held) == nil {
				if days, _ := held["deadline"].(string); days != "" {
					date["deadline"] = days
				}
			}
		}
		canonical, valid := canonicalGraphDateValue(date)
		if !valid {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid("database property %q date is invalid", property.Name)
		}
		value = canonical
	case graphValueTypeSelect, graphValueTypeStatus:
		if string(payload) == "null" {
			value = ""
			break
		}
		option, err := notionDatabaseRowPageRequestOption(property, payload, added)
		if err != nil {
			return graphRowValue{}, err
		}
		value = option.ID
	case graphValueTypeMultiSelect:
		items := []json.RawMessage{}
		if json.Unmarshal(payload, &items) != nil || len(items) > maxNotionDatabaseRowPageOptions {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid(
				"database property %q multi_select is invalid", property.Name,
			)
		}
		selected := make([]string, 0, len(items))
		seen := make(map[string]struct{}, len(items))
		for _, item := range items {
			option, err := notionDatabaseRowPageRequestOption(property, item, added)
			if err != nil {
				return graphRowValue{}, err
			}
			// An option named twice is set once, as Notion sets it.
			if _, duplicate := seen[option.ID]; duplicate {
				continue
			}
			seen[option.ID] = struct{}{}
			selected = append(selected, option.ID)
		}
		value = selected
	case graphValueTypePerson:
		items := []struct {
			ID string `json:"id"`
		}{}
		if json.Unmarshal(payload, &items) != nil || len(items) > maxNotionDatabaseRowPagePeople {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid(
				"database property %q people is invalid", property.Name,
			)
		}
		// An ID is taken with or without its dashes, as Notion's.
		named := make([]string, 0, len(items))
		for _, item := range items {
			id, _ := normalizeNotionUUIDRequest(strings.TrimSpace(item.ID))
			named = append(named, id)
		}
		// The bots are looked for in lower case too: one named in capitals is
		// someone's, not found as Notion's own people are in capitals.
		lowered := make([]string, 0, len(named))
		for _, id := range named {
			lowered = append(lowered, strings.ToLower(id))
		}
		bots, err := loadPeopleValueBots(app, authorization.Identity.WorkspaceID, append(lowered, named...))
		if err != nil {
			return graphRowValue{}, err
		}
		// Someone invited into a person value (people_value_invitees.go) is
		// taken while the address holds the invitation, as the account
		// that proved it once one has.
		invitees, err := loadPeopleValueInvitees(app, authorization.Identity.WorkspaceID, named)
		if err != nil {
			return graphRowValue{}, err
		}
		// Whom the value holds already stays (notion_people_value_held.go).
		held, err := notionPeopleValueHeld(app, authorization.Authority.RowID, property.ID)
		if err != nil {
			return graphRowValue{}, err
		}
		values, peopleIDs, peopleNamed := make([]string, 0, len(items)), []string{}, []string{}
		seen := make(map[string]struct{}, len(items))
		for _, id := range named {
			stored, err := resolveNotionCompatibleUserID(id)
			invitee, invited := invitees[id]
			if bot, found := bots[id]; err != nil && found && peopleValueBotKnownTo(bot, authorization.Identity) {
				stored, err = bot.ID, nil
			} else if err != nil && invited && invitee.AccountID != "" {
				stored, err = invitee.AccountID, nil
				peopleIDs, peopleNamed = append(peopleIDs, stored), append(peopleNamed, id)
			} else if err != nil && invited && invitee.Invited {
				stored, err = invitee.ID, nil
			} else if err != nil && held[id] {
				stored, err = id, nil
			} else if err == nil && !held[stored] {
				peopleIDs, peopleNamed = append(peopleIDs, stored), append(peopleNamed, id)
			}
			// One no one here has is not found, as Notion's, when it is a
			// person's or bot's ID here in other letters or shaped as a
			// person's, and otherwise no group's (notionPeopleValueUserShaped).
			_, known := bots[strings.ToLower(id)]
			if _, err := resolveNotionCompatibleUserID(strings.ToLower(id)); err == nil || invited {
				known = true
			}
			if _, isUUID := normalizeNotionUUIDRequest(id); err != nil && isUUID && !known && !notionPeopleValueUserShaped(id) {
				return graphRowValue{}, notionDatabaseRowPageRequestInvalid("%s", notionPeopleValueGroupRefused)
			} else if err != nil && isUUID {
				return graphRowValue{}, &notionDatabaseRowPageUserMissing{id: id}
			}
			if err != nil {
				return graphRowValue{}, notionDatabaseRowPageRequestInvalid(
					"database property %q contains an invalid user", property.Name,
				)
			}
			// A person named twice is set once, as Notion sets them.
			if _, duplicate := seen[stored]; duplicate {
				continue
			}
			seen[stored] = struct{}{}
			values = append(values, stored)
		}
		known, err := notionPeopleInWorkspace(app, authorization.Identity.WorkspaceID, peopleIDs)
		if err != nil {
			return graphRowValue{}, err
		}
		for index, internalID := range peopleIDs {
			if _, found := known[internalID]; !found {
				return graphRowValue{}, &notionDatabaseRowPageUserMissing{id: peopleNamed[index]}
			}
		}
		value = values
	case graphValueTypeRelation:
		relationIDs, err := notionDatabaseRowPageRequestRelations(
			app, authorization, property, payload,
		)
		if err != nil {
			return graphRowValue{}, err
		}
		value = relationIDs
	case graphValueTypePlace:
		if string(payload) == "null" {
			break
		}
		place, err := notionDatabaseRowPagePlace(payload)
		if err != nil {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid(
				"database property %q place is invalid", property.Name,
			)
		}
		value = place
	case graphValueTypeVerification:
		if string(payload) == "null" {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid(
				"database property %q verification is invalid", property.Name,
			)
		}
		verification, err := notionDatabaseRowPageVerificationRequest(
			payload, authorization.Identity.PrincipalID,
		)
		if err != nil {
			return graphRowValue{}, notionDatabaseRowPageRequestInvalid(
				"database property %q verification is invalid", property.Name,
			)
		}
		value = verification
	case graphValueTypeFiles:
		files, err := notionDatabaseRowPageRequestFiles(
			app, authorization, property, payload,
		)
		if err != nil {
			return graphRowValue{}, err
		}
		value = files
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return graphRowValue{}, err
	}
	return graphRowValue{Type: nativeType, Value: encoded}, nil
}

// notionDatabaseRowPageRequestDate reads a date property's ends and zone as
// Notion keeps them (notion_date_reading.go), refusing what its API refuses,
// in Leafen's words; an end Leafen can't read, the request check aside, is said
// as that check says it. A time outside the years 0 to 9999, which Notion
// keeps as "Invalid DateTime", Leafen refuses, saying why.
func notionDatabaseRowPageRequestDate(
	property storedNotionDatabaseRowPageProperty, date map[string]any,
) (notionDateKept, error) {
	rawID := notionPropertyRawID(property.publicID())
	read := func(end string) (*notionDateEnd, error) {
		text, _ := date[end].(string)
		if end == "end" && strings.TrimSpace(text) == "" {
			return nil, nil
		}
		read, ok := readNotionDateEnd(text)
		if !ok {
			return nil, notionDatabaseRowPageRequestInvalid("%s", notionBodyFault(
				"body.properties."+property.Name+".date."+end, "a valid ISO 8601 date string", fmt.Sprintf("%q", text)))
		}
		if !read.keepable() {
			return nil, notionDatabaseRowPageRequestInvalid("%s has a date out of range: its year is from 0 to 9999.", rawID)
		}
		return &read, nil
	}
	start, err := read("start")
	if err != nil {
		return notionDateKept{}, err
	}
	end, err := read("end")
	if err != nil {
		return notionDateKept{}, err
	}
	zone, _ := date["time_zone"].(string)
	if fault := notionDatePropertyFault(rawID, *start, end, zone != ""); fault != "" {
		return notionDateKept{}, notionDatabaseRowPageRequestInvalid("%s", fault)
	}
	return keepNotionDate(*start, end, zone, time.Now()), nil
}
