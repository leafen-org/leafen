package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// prop() reads a property as a formula value, as the editor does: options
// by name, related pages and people as references, dates as dates, and
// the row page's times and people.

// databaseFormulaTime is the clock and time zone a query reads formula
// dates in; left empty, formulas read the current time in UTC.
type databaseFormulaTime struct {
	Location *time.Location
	Now      time.Time
	// Language is the reader's, whose words dates read as text in; ""
	// reads English, as the Public API does (database_formula_locale.go).
	Language string
}

// formulaTime is the plan's clock and time zone, dates read as text in
// language.
func (plan databaseQueryPlan) formulaTime(language string) databaseFormulaTime {
	return databaseFormulaTime{Location: plan.Location, Now: plan.ReferenceTime, Language: language}
}

// databaseFormulaPeople names the people formulas read, each read once,
// and gives their emails only to a reader who may see them.
type databaseFormulaPeople struct {
	app   core.App
	names map[string]string
	// workspaceID names its connections' bots a person value holds
	// (notion_people_value_bots.go); "" names none.
	workspaceID string
	// emails holds the emails read so far; nil when the reader sees none.
	emails map[string]string
	// languages holds the languages read so far of whoever queries
	// (database_query_search_dates.go).
	languages map[string]string
	// request is the querying person's own request, where there is one.
	request context.Context
}

func newDatabaseFormulaPeople(app core.App, names map[string]string) *databaseFormulaPeople {
	if names == nil {
		names = map[string]string{}
	}
	return &databaseFormulaPeople{app: app, names: names}
}

func (people *databaseFormulaPeople) name(id string) string {
	if people == nil {
		return ""
	}
	if name, ok := people.names[id]; ok {
		return name
	}
	name := ""
	if people.app != nil && notionBotUserIDShape(id) {
		// A connection's bot reads by its connection's name, as Notion's
		// formula reads an integration's (seen 2026-11-12).
		if people.workspaceID != "" {
			if bots, err := loadPeopleValueBots(people.app, people.workspaceID, []string{id}); err == nil {
				name = strings.TrimSpace(bots[id].Name)
			}
		}
	} else if people.app != nil && peopleValueInviteeIDShape(id) {
		// Someone invited reads by the address, as the app names them
		// (people_value_invitees.go), or as the account that proved it.
		if people.workspaceID != "" {
			if invitees, err := loadPeopleValueInvitees(people.app, people.workspaceID, []string{id}); err == nil {
				name = invitees[id].Email
				if record, err := people.app.FindRecordById("users", invitees[id].AccountID); err == nil {
					name = strings.TrimSpace(record.GetString("name"))
				}
			}
		}
	} else if people.app != nil {
		if record, err := people.app.FindRecordById("users", id); err == nil {
			name = strings.TrimSpace(record.GetString("name"))
		}
	}
	people.names[id] = name
	return name
}

// showEmails lets formulas read people's emails, for a reader who is a
// member of the workspace; a guest, a public page and a computation with
// no reader get none.
func (people *databaseFormulaPeople) showEmails() {
	if people != nil && people.emails == nil {
		people.emails = map[string]string{}
	}
}

func (people *databaseFormulaPeople) email(id string) string {
	if people == nil || people.emails == nil || people.app == nil {
		return ""
	}
	if email, ok := people.emails[id]; ok {
		return email
	}
	email := ""
	if record, err := people.app.FindRecordById("users", id); err == nil {
		email = strings.TrimSpace(record.Email())
	}
	people.emails[id] = email
	return email
}

// personReference is a person as formulas read them: their name, and their
// email where the reader may see it.
func (people *databaseFormulaPeople) personReference(id, name string) databaseFormulaReference {
	if name == "" {
		name = people.name(id)
	}
	reference := databaseFormulaNamedReference("person", id, name)
	reference.Email = people.email(id)
	return reference
}

func databaseFormulaNamedReference(kind, id, name string) databaseFormulaReference {
	if name == "" {
		name = id
	}
	return databaseFormulaReference{Kind: kind, ID: id, Name: name}
}

func databaseFormulaInstant(milliseconds int64) any {
	if milliseconds < 1 {
		return ""
	}
	return databaseFormulaDate{Text: time.UnixMilli(milliseconds).UTC().Format("2006-01-02T15:04:05Z")}
}

func (resolver *databaseComputedResolver) formulaProperty(
	row storedDatabaseQueryRow, property storedDatabaseQueryProperty,
) (any, error) {
	switch property.Type {
	case graphValueTypeRollup:
		value, err := resolver.evaluate(row, property)
		return databaseFormulaInput(value), err
	case graphValueTypeFormula:
		// Another formula's result is read as it is, made empty text too
		// (databaseFormulaKeptResult); one that failed fails the formula
		// reading it.
		value, err := resolver.evaluate(row, property)
		key := row.ID + "\x00" + property.ID
		if err != nil {
			return nil, err
		}
		if failure := resolver.failed[key]; failure != nil {
			return nil, failure
		}
		if result, unstored := resolver.unstored[key]; unstored {
			return databaseFormulaKeptResult{result}, nil
		}
		return databaseFormulaKeptResult{databaseFormulaInput(value)}, nil
	case graphValueTypeCreatedTime:
		return databaseFormulaInstant(row.Created), nil
	case graphValueTypeLastEditedTime:
		return databaseFormulaInstant(row.Updated), nil
	case graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		id := row.CreatedBy
		if property.Type == graphValueTypeLastEditedBy {
			id = row.LastEditedBy
		}
		// As Notion's, who made or last edited a page is one person; a
		// person property holds a list.
		if id == "" {
			return "", nil
		}
		return resolver.context.People.personReference(id, ""), nil
	}
	value := databaseQueryRawValue(databaseValueAsRead(property.Type, property.Config, row.Values[property.ID].Value, row.CreatedBy))
	if value == nil {
		switch property.Type {
		// A list property never set is an empty list, as Notion reads it
		// (seen 2026-11-12: prop("Tags").map(current) of none is empty and
		// its length() 0).
		case graphValueTypePerson, graphValueTypeMultiSelect, graphValueTypeRelation:
			return []any{}, nil
		case graphValueTypeCheckbox:
			// A checkbox never set is unchecked, as Notion reads it (seen
			// 2026-11-12: prop("Done") false) and the app's formulas do.
			return false, nil
		}
		return "", nil
	}
	switch property.Type {
	case graphValueTypeSelect, graphValueTypeStatus:
		return databaseFormulaOptionName(property, databaseQueryValueText(value), false), nil
	case graphValueTypeMultiSelect:
		names, seen := []any{}, map[string]bool{}
		for _, id := range databaseFormulaItems(value) {
			name := databaseFormulaOptionName(property, databaseQueryValueText(id), true)
			if name != "" && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
		return names, nil
	case graphValueTypeRelation:
		return resolver.formulaPages(property, value), nil
	case graphValueTypePerson:
		people, seen := []any{}, map[string]bool{}
		for _, item := range databaseFormulaItems(value) {
			id, name := databaseQueryValueText(item), ""
			if object, ok := item.(map[string]any); ok {
				id, _ = object["id"].(string)
				name, _ = object["name"].(string)
			}
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			people = append(people, resolver.context.People.personReference(id, strings.TrimSpace(name)))
		}
		return people, nil
	case graphValueTypeDate:
		return databaseFormulaPropertyDate(value), nil
	case graphValueTypeFiles:
		names := []string{}
		for _, item := range databaseFormulaItems(value) {
			if object, ok := item.(map[string]any); ok {
				name, _ := object["name"].(string)
				names = append(names, name)
			}
		}
		return strings.Join(names, ", "), nil
	}
	switch value.(type) {
	case float64, bool, string:
		return value, nil
	}
	return databaseQueryValueText(value), nil
}

func databaseFormulaItems(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return []any{value}
}

// databaseFormulaOptionName names an option; a multi-select value may also
// hold an option's name.
func databaseFormulaOptionName(property storedDatabaseQueryProperty, id string, byName bool) string {
	for _, option := range databaseQueryParsedPropertyConfig(property).Options {
		if option.ID == id || (byName && option.Name == id) {
			return option.Name
		}
	}
	return id
}

// relationTitles are a relation's related pages as a rollup showing them
// shows them: by their titles, an untitled page or one not read left out,
// as a query's turn reads them (loadDatabaseQueryRollupRelation).
func (resolver *databaseComputedResolver) relationTitles(value any) any {
	titles := []any{}
	for _, id := range databaseQueryValueStrings(value) {
		page, ok := resolver.row(id)
		if !ok {
			continue
		}
		for _, candidate := range page.Values {
			if candidate.Type != graphValueTypeTitle {
				continue
			}
			if title, _ := databaseQueryRawValue(candidate.Value).(string); strings.TrimSpace(title) != "" {
				titles = append(titles, title)
			}
			break
		}
	}
	if len(titles) == 0 {
		return nil
	}
	return titles
}

// formulaPages are a relation's related rows the evaluation read, as page
// references named by their titles and knowing their database.
func (resolver *databaseComputedResolver) formulaPages(property storedDatabaseQueryProperty, value any) []any {
	target := struct {
		RelationDatabaseID string `json:"relationDatabaseId"`
	}{}
	_ = json.Unmarshal([]byte(property.Config), &target)
	if target.RelationDatabaseID == "" {
		target.RelationDatabaseID = property.DataSourceID
	}
	pages := []any{}
	for _, item := range databaseFormulaItems(value) {
		id, _ := item.(string)
		page, ok := resolver.row(id)
		if id == "" || !ok {
			continue
		}
		title := ""
		for _, candidate := range page.Values {
			if candidate.Type == graphValueTypeTitle {
				title, _ = databaseQueryRawValue(candidate.Value).(string)
				break
			}
		}
		pages = append(pages, databaseFormulaReference{
			Kind: "page", ID: id, Name: databaseFormulaTrim(title), DataSourceID: target.RelationDatabaseID,
		})
	}
	return pages
}
