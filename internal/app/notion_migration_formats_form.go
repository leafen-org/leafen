package app

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A Notion form's own words, which Notion's API doesn't give a connection
// (its view says only whether it takes responses and from whom): the form's
// title and description and its questions in order, each with its name,
// whether it must be answered, its description and a checkbox's label. The
// reader reads them in the person's Notion (notion_migration_formats_reader.js:
// the view's form block, that block's layout, the layout's questions, seen
// 2026-11-16) and the run gives the imported form view them as the form
// editor keeps them (leafenForm), a checkbox's label beside its box too.

type notionMigrationFormFormat struct {
	Title       string                              `json:"title"`
	Description string                              `json:"description"`
	Questions   []notionMigrationFormQuestionFormat `json:"questions"`
	// Who may answer it, read from the form block's sharing: web (anyone
	// with its link), workspace (the members) or none; empty when the
	// reading couldn't tell, and the form stays as the import made it.
	Audience string `json:"audience,omitempty"`
}

// notionMigrationFormOpen is a form whose answering the reading set, opened
// once its view is saved: its link is what lets people answer.
type notionMigrationFormOpen struct {
	PageID, DataSourceID, ViewID, Audience string
}

type notionMigrationFormQuestionFormat struct {
	// Property is the Notion property it asks, "title" for the title.
	Property      string `json:"property"`
	Name          string `json:"name"`
	Required      bool   `json:"required"`
	Description   string `json:"description"`
	Sync          bool   `json:"sync"`
	CheckboxLabel string `json:"checkboxLabel"`
}

// form gives an imported form view Notion's questions, in config, and says
// what it left: "questions" when a question's property didn't come or the
// form editor refuses them.
func (matcher *notionMigrationFormatsMatcher) form(
	databaseID, pageID, viewID string, view databaseViewAuthority, format notionMigrationFormFormat, config map[string]any,
) []string {
	properties, err := matcher.properties(databaseID)
	if err != nil {
		return []string{"questions"}
	}
	left := []string{}
	form := mcpViewFormConfiguration(nil)
	form.Title, form.Description = strings.TrimSpace(format.Title), strings.TrimSpace(format.Description)
	asked, boxLabels := []string{}, []string{}
	for _, notionQuestion := range format.Questions {
		propertyID := properties[notionImportPropertyKey(notionQuestion.Property)]
		if propertyID == "" {
			if !slices.Contains(left, "questions") {
				left = append(left, "questions")
			}
			continue
		}
		question := formQuestion{
			ID: "question:" + propertyID, PropertyID: propertyID, Label: strings.TrimSpace(notionQuestion.Name),
			Description: strings.TrimSpace(notionQuestion.Description), Required: notionQuestion.Required,
			SyncWithPropertyName: notionQuestion.Sync, Position: len(form.Questions),
		}
		form.Questions = append(form.Questions, question)
		asked = append(asked, propertyID)
		boxLabels = append(boxLabels, strings.TrimSpace(notionQuestion.CheckboxLabel))
	}
	stored, err := matcher.formProperties(pageID, view.DataSourceID, asked)
	if err != nil {
		return append(left, "questions")
	}
	byID := map[string]storedFormProperty{}
	for _, property := range stored {
		byID[property.ID] = property
	}
	for index := range form.Questions {
		// A question in step with its property that Notion still names
		// otherwise keeps that name, as the title's "Title" of a form the API
		// made does (seen 2026-11-16).
		question := &form.Questions[index]
		if question.SyncWithPropertyName && question.Label != "" && question.Label != byID[question.PropertyID].Name {
			own := true
			question.OwnLabel = &own
		}
		switch byID[form.Questions[index].PropertyID].Type {
		case graphValueTypeCheckbox:
			question.CheckboxLabel = boxLabels[index]
		case graphValueTypeRichText:
			short := false
			form.Questions[index].LongAnswer = &short
		case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
			form.Questions[index].OptionsDisplay = "list"
		}
	}
	audience := formAudienceWorkspace
	if configured, ok := config["leafenFormAudience"].(string); ok && configured != "" {
		audience = configured
	}
	// Who may answer, as the person's Notion has it (2026-11-16: a form
	// open to members came closed, the API giving no word of it). Leafen's
	// web form takes no person or relation question: one asking such stays
	// open to the members.
	read := format.Audience == formAudienceWeb || format.Audience == formAudienceWorkspace || format.Audience == formAudienceNone
	if read {
		audience = format.Audience
	}
	normalized, err := validateAndNormalizeForm(form, stored, audience)
	if err != nil && read && audience == formAudienceWeb {
		audience = formAudienceWorkspace
		normalized, err = validateAndNormalizeForm(form, stored, audience)
	}
	if err != nil {
		return append(left, "questions")
	}
	if read {
		config["leafenFormAudience"] = audience
		access, _ := config["leafenFormSubmitterAccess"].(string)
		config["leafenFormSubmitterAccess"] = formSubmitterAccessOrNone(access)
		matcher.forms = append(matcher.forms, notionMigrationFormOpen{
			PageID: pageID, DataSourceID: view.DataSourceID, ViewID: viewID, Audience: audience,
		})
	}
	encoded, err := json.Marshal(normalized)
	value := map[string]any{}
	if err != nil || json.Unmarshal(encoded, &value) != nil {
		return append(left, "questions")
	}
	config["leafenForm"] = value
	delete(config, "leafenFormStart")
	return left
}

// formProperties reads the properties a form asks, as the form editor
// checks them.
func (matcher *notionMigrationFormatsMatcher) formProperties(pageID, dataSourceID string, ids []string) ([]storedFormProperty, error) {
	properties := []storedFormProperty{}
	for _, id := range ids {
		property := storedFormProperty{}
		err := matcher.app.DB().NewQuery(`
			SELECT [[id]],[[data_source_id]],[[type]],[[name]],[[config]],[[updated]] FROM {{leafen_database_properties}}
			WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:sourceId} AND [[id]]={:id}
		`).Bind(dbx.Params{"pageId": pageID, "sourceId": dataSourceID, "id": id}).One(&property)
		if err != nil {
			return nil, err
		}
		properties = append(properties, property)
	}
	return properties, nil
}

// openForms gives each form the reading set its link, so the people it
// names may answer; a web form the server's sharing rules refuse stays open
// to the members.
func (matcher *notionMigrationFormatsMatcher) openForms() error {
	for _, open := range matcher.forms {
		link, found, err := loadStoredFormLinkByView(matcher.app, matcher.payload.AuthorityOwnerID, open.PageID, open.ViewID)
		if err != nil {
			return err
		}
		if !found {
			link = storedFormLink{
				ID: core.GenerateDefaultRandomId(), Owner: matcher.payload.AuthorityOwnerID, WorkspaceID: matcher.job.WorkspaceID,
				PageID: open.PageID, DataSourceID: open.DataSourceID, ViewID: open.ViewID,
				CreatedBy: matcher.job.Owner, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
		}
		token, err := formToken(matcher.app, link.ID)
		if err != nil {
			return err
		}
		err = saveFormLink(matcher.app, link, formTokenDigest(token), open.Audience)
		if err != nil && open.Audience == formAudienceWeb {
			err = saveFormLink(matcher.app, link, formTokenDigest(token), formAudienceWorkspace)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
