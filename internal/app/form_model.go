package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errFormInvalid   = errors.New(errorCodeFormInvalid)
	errFormNotFound  = errors.New(errorCodeFormNotFound)
	errFormForbidden = errors.New(errorCodeFormForbidden)
	errFormConflict  = errors.New(errorCodeFormConflict)
	errFormRateLimit = errors.New(errorCodeRateLimited)
)

// formInvalid refuses a request for the reason given (request_refusal.go).
func formInvalid(format string, args ...any) error {
	return refuse(errFormInvalid, format, args...)
}

var formWritablePropertyTypes = map[string]bool{
	graphValueTypeTitle: true, graphValueTypeRichText: true, graphValueTypeNumber: true,
	graphValueTypeSelect: true, graphValueTypeMultiSelect: true, graphValueTypeStatus: true,
	graphValueTypeDate: true, graphValueTypeCheckbox: true, graphValueTypeURL: true,
	graphValueTypeEmail: true, graphValueTypePhone: true, graphValueTypePerson: true,
	graphValueTypeRelation: true, graphValueTypePlace: true, graphValueTypeFiles: true,
}

type storedFormView struct {
	ID           string `db:"id"`
	DataSourceID string `db:"data_source_id"`
	Type         string `db:"type"`
	Name         string `db:"name"`
	Position     string `db:"position"`
	Config       string `db:"config"`
	Updated      int64  `db:"updated"`
}

type storedFormProperty struct {
	ID           string `db:"id"`
	DataSourceID string `db:"data_source_id"`
	Type         string `db:"type"`
	Name         string `db:"name"`
	Config       string `db:"config"`
	Updated      int64  `db:"updated"`
}

type formSource struct {
	View       storedFormView
	ViewConfig map[string]any
	Form       formViewConfiguration
	Properties []storedFormProperty
}

func normalizeFormIdentifier(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) ||
		utf8.RuneCountInString(value) > maxFormIdentifierCharacters {
		return "", errFormInvalid
	}
	return value, nil
}

func validateFormText(value string, maxBytes int) error {
	if !utf8.ValidString(value) || len([]byte(value)) > maxBytes {
		return errFormInvalid
	}
	return nil
}

// formIdentifierField is a request's id or name of something, named by its
// field when it isn't one.
func formIdentifierField(field, value string) (string, error) {
	normalized, err := normalizeFormIdentifier(value)
	if err != nil {
		return "", formInvalid("%s is required: an id of up to %d characters", field, maxFormIdentifierCharacters)
	}
	return normalized, nil
}

// formTextField is a form's words, named by their field when they're too long.
func formTextField(field, value string, maxBytes int) error {
	if validateFormText(value, maxBytes) != nil {
		return formInvalid("%s is text of up to %d bytes", field, maxBytes)
	}
	return nil
}

var formButtonColors = []string{formButtonColorBlue, formButtonColorGray, formButtonColorGreen,
	formButtonColorRed, formButtonColorOrange, formButtonColorPurple,
	formButtonColorBrown, formButtonColorYellow, formButtonColorAzure, formButtonColorPink}

// An empty button text, title or body shows its default words, as Notion's
// new form leaves them empty (seen 2026-11-08).
func validateFormSubmissionConfiguration(value formSubmitConfiguration) error {
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"form.submit.buttonLabel", value.ButtonLabel, 512}, {"form.submit.confirmationTitle", value.ConfirmationTitle, 512},
		{"form.submit.confirmationBody", value.ConfirmationBody, 2 * 1024}} {
		if err := formTextField(field.name, field.value, field.limit); err != nil {
			return err
		}
	}
	if !slices.Contains(formButtonColors, value.ButtonColor) {
		return formInvalid("form.submit.buttonColor is one of %s", strings.Join(formButtonColors, ", "))
	}
	return nil
}

func validateAndNormalizeForm(
	value formViewConfiguration,
	properties []storedFormProperty,
	audience string,
) (formViewConfiguration, error) {
	if value.Version != 1 {
		return formViewConfiguration{}, formInvalid("form.version is 1")
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{{"form.title", value.Title, 512}, {"form.description", value.Description, 2 * 1024}, {"form.icon", value.Icon, 2 * 1024},
		{"form.cover", value.Cover, 4 * 1024}} {
		if err := formTextField(field.name, field.value, field.limit); err != nil {
			return formViewConfiguration{}, err
		}
	}
	// The cover's place from its top, as a page's cover keeps it.
	if value.CoverPosition != nil && (*value.CoverPosition < 0 || *value.CoverPosition > 100) {
		return formViewConfiguration{}, formInvalid("form.coverPosition is from 0 (top) to 100")
	}
	if err := validateFormSubmissionConfiguration(value.Submit); err != nil {
		return formViewConfiguration{}, err
	}
	byID := make(map[string]storedFormProperty, len(properties))
	for _, property := range properties {
		if _, duplicate := byID[property.ID]; duplicate || property.DataSourceID == "" {
			return formViewConfiguration{}, errFormConflict
		}
		byID[property.ID] = property
	}
	seenQuestion := map[string]bool{}
	seenQuestionID := map[string]bool{}
	seenPosition := map[int]bool{}
	for index := range value.Questions {
		question := &value.Questions[index]
		at := fmt.Sprintf("form.questions[%d]", index)
		propertyID, err := formIdentifierField(at+".propertyId", question.PropertyID)
		if err != nil {
			return formViewConfiguration{}, err
		}
		if seenQuestion[propertyID] {
			return formViewConfiguration{}, formInvalid("%s.propertyId %q has a question already: one question a property", at, propertyID)
		}
		if question.Position < 0 || question.Position >= len(value.Questions) || seenPosition[question.Position] {
			return formViewConfiguration{}, formInvalid("%s.position is its place from 0 to %d, each question's its own", at, len(value.Questions)-1)
		}
		property, ok := byID[propertyID]
		if !ok {
			return formViewConfiguration{}, formInvalid("%s.propertyId %q isn't a property of the form's database", at, propertyID)
		}
		if !formWritablePropertyTypes[property.Type] {
			return formViewConfiguration{}, formInvalid("%s asks for %q, a %s property, which a form doesn't fill in", at, property.Name, property.Type)
		}
		if question.Hidden && question.Required {
			return formViewConfiguration{}, formInvalid("%s is hidden and required at once: a hidden question isn't required", at)
		}
		if audience == formAudienceWeb && (property.Type == graphValueTypePerson || property.Type == graphValueTypeRelation) {
			return formViewConfiguration{}, formInvalid("%s asks for %q, a %s property, which a form open to the web doesn't ask", at, property.Name, property.Type)
		}
		question.PropertyID = propertyID
		question.ID, err = formIdentifierField(at+".id", question.ID)
		if err != nil {
			return formViewConfiguration{}, err
		}
		if seenQuestionID[question.ID] {
			return formViewConfiguration{}, formInvalid("%s.id %q is another question's too", at, question.ID)
		}
		// A question in step with its property is named by it, unless it keeps
		// its own name, as an API form's "Title" does (formQuestionOwnLabel).
		if question.SyncWithPropertyName && !formQuestionOwnLabel(question) {
			question.Label = property.Name
		}
		if err := formTextField(at+".label", question.Label, 512); err != nil {
			return formViewConfiguration{}, err
		}
		if err := formQuestionTexts(at, *question, property); err != nil {
			return formViewConfiguration{}, err
		}
		// What a question of one kind of property only may set, named with
		// the kinds it is for.
		onlyFor := func(setting, kinds string) error {
			return formInvalid("%s.%s is only for a question on a %s property; %q is %s", at, setting, kinds, property.Name, property.Type)
		}
		if property.Type != graphValueTypeRichText && property.Type != graphValueTypeTitle && question.LongAnswer != nil {
			return formViewConfiguration{}, onlyFor("longAnswer", "title or rich_text")
		}
		if property.Type != graphValueTypeDate && question.IncludeEndDate != nil {
			return formViewConfiguration{}, onlyFor("includeEndDate", "date")
		}
		if property.Type != graphValueTypeDate && question.IncludeTime != nil {
			return formViewConfiguration{}, onlyFor("includeTime", "date")
		}
		choice := property.Type == graphValueTypeSelect || property.Type == graphValueTypeMultiSelect || property.Type == graphValueTypeStatus
		if question.OptionsDisplay != "" && !choice {
			return formViewConfiguration{}, onlyFor("optionsDisplay", "select, multi_select or status")
		}
		if question.OptionsDisplay != "" && question.OptionsDisplay != "list" && question.OptionsDisplay != "dropdown" {
			return formViewConfiguration{}, formInvalid("%s.optionsDisplay is list or dropdown", at)
		}
		multi := property.Type == graphValueTypeMultiSelect || property.Type == graphValueTypePerson || property.Type == graphValueTypeRelation
		if question.MaxSelections != nil && !multi {
			return formViewConfiguration{}, onlyFor("maxSelections", "multi_select, person or relation")
		}
		if question.MaxSelections != nil && *question.MaxSelections < 1 {
			return formViewConfiguration{}, formInvalid("%s.maxSelections is 1 or more", at)
		}
		seenQuestion[propertyID] = true
		seenQuestionID[question.ID] = true
		seenPosition[question.Position] = true
	}
	if err := validateFormConditionGroups(value, byID); err != nil {
		return formViewConfiguration{}, err
	}
	sort.SliceStable(value.Questions, func(left, right int) bool {
		return value.Questions[left].Position < value.Questions[right].Position
	})
	return value, nil
}

// validateFormConditionGroups keeps a form's condition groups under questions
// they can read, a choice's options or a checkbox's state, without a question
// ending up under itself.
func validateFormConditionGroups(value formViewConfiguration, properties map[string]storedFormProperty) error {
	questions := make(map[string]formQuestion, len(value.Questions))
	for _, question := range value.Questions {
		questions[question.ID] = question
	}
	groups := make(map[string]formConditionGroup, len(value.ConditionGroups))
	for index, group := range value.ConditionGroups {
		at := fmt.Sprintf("form.conditionGroups[%d]", index)
		if _, err := formIdentifierField(at+".id", group.ID); err != nil || group.ID != strings.TrimSpace(group.ID) {
			return formInvalid("%s.id is required: an id of up to %d characters, without spaces around it", at, maxFormIdentifierCharacters)
		}
		if _, duplicate := groups[group.ID]; duplicate {
			return formInvalid("%s.id %q is another group's too", at, group.ID)
		}
		source, known := questions[group.SourceQuestionID]
		if !known {
			return formInvalid("%s.sourceQuestionId %q is no question's id in form.questions", at, group.SourceQuestionID)
		}
		switch properties[source.PropertyID].Type {
		case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
			if group.Checked != nil {
				return formInvalid("%s.checked is for a checkbox question's group; a choice's group names optionIds", at)
			}
		case graphValueTypeCheckbox:
			if len(group.OptionIDs) != 0 || group.MatchAll != nil {
				return formInvalid("%s.optionIds and matchAll are for a choice question's group; a checkbox's group sets checked", at)
			}
		default:
			return formInvalid("%s hangs on question %q, which is neither a choice (select, multi_select, status) nor a checkbox", at, group.SourceQuestionID)
		}
		for _, optionID := range group.OptionIDs {
			if _, err := normalizeFormIdentifier(optionID); err != nil {
				return formInvalid("%s.optionIds holds an empty or too long id", at)
			}
		}
		groups[group.ID] = group
	}
	for index, question := range value.Questions {
		if question.ConditionGroupID == "" {
			continue
		}
		seen := map[string]bool{question.ID: true}
		for at := question; at.ConditionGroupID != ""; {
			group, known := groups[at.ConditionGroupID]
			if !known {
				return formInvalid("form.questions[%d].conditionGroupId %q is no group's id in form.conditionGroups", index, at.ConditionGroupID)
			}
			if seen[group.SourceQuestionID] {
				return formInvalid("form.questions[%d] ends up under itself through its condition groups", index)
			}
			seen[group.SourceQuestionID] = true
			at = questions[group.SourceQuestionID]
		}
	}
	return nil
}

// formConditionMet says whether a group's questions are asked, from the raw
// answer to its source; one with nothing chosen to look for is never asked.
func formConditionMet(group formConditionGroup, answer any) bool {
	if group.Checked != nil {
		checked, _ := answer.(bool)
		return checked == *group.Checked
	}
	if len(group.OptionIDs) == 0 {
		return false
	}
	chosen := map[string]bool{}
	switch value := answer.(type) {
	case string:
		chosen[value] = true
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok {
				chosen[text] = true
			}
		}
	}
	matches := 0
	for _, optionID := range group.OptionIDs {
		if chosen[optionID] {
			matches++
		}
	}
	if group.MatchAll != nil && *group.MatchAll {
		return matches == len(group.OptionIDs)
	}
	return matches > 0
}

// formQuestionsNotAsked names the properties of the questions a form's
// conditions leave out, given the answers sent: a question is asked only while
// every group above it holds.
func formQuestionsNotAsked(form formViewConfiguration, answers map[string]any) map[string]bool {
	notAsked := map[string]bool{}
	if len(form.ConditionGroups) == 0 {
		return notAsked
	}
	questions := make(map[string]formQuestion, len(form.Questions))
	for _, question := range form.Questions {
		questions[question.ID] = question
	}
	groups := make(map[string]formConditionGroup, len(form.ConditionGroups))
	for _, group := range form.ConditionGroups {
		groups[group.ID] = group
	}
	for _, question := range form.Questions {
		for at := question; at.ConditionGroupID != ""; {
			group := groups[at.ConditionGroupID]
			source := questions[group.SourceQuestionID]
			if !formConditionMet(group, answers[source.PropertyID]) {
				notAsked[question.PropertyID] = true
				break
			}
			at = source
		}
	}
	return notAsked
}

func loadStoredFormView(
	app core.App,
	owner, pageID, dataSourceID, viewID string,
) (storedFormView, map[string]any, error) {
	view := storedFormView{}
	err := app.DB().NewQuery(`
		SELECT [[view]].[[id]], [[view]].[[data_source_id]], [[view]].[[type]], [[view]].[[name]],
			[[view]].[[position]], [[view]].[[config]], [[view]].[[updated]]
		FROM {{leafen_database_views}} AS [[view]]
		WHERE [[view]].[[owner]] = {:owner} AND [[view]].[[page_id]] = {:pageId}
			AND [[view]].[[data_source_id]] = {:sourceId} AND [[view]].[[id]] = {:viewId}
			AND ` + databaseSourceNotTrashedSQL("[[view]].[[data_source_id]]") + `
		LIMIT 2
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "sourceId": dataSourceID, "viewId": viewID,
	}).One(&view)
	if err != nil {
		return storedFormView{}, nil, refuse(errFormNotFound, "viewId names no view of dataSourceId's database on pageId")
	}
	if view.Type != graphViewTypeForm {
		return storedFormView{}, nil, refuse(errFormNotFound,
			"viewId names a %s view; a form is a view of type form: add one with a content-graph upsertView, then configure it", view.Type)
	}
	config := map[string]any{}
	if json.Unmarshal([]byte(view.Config), &config) != nil || config == nil {
		return storedFormView{}, nil, errFormConflict
	}
	return view, config, nil
}

func loadStoredFormProperties(
	app core.App,
	owner, pageID, dataSourceID string,
	propertyIDs []string,
) ([]storedFormProperty, error) {
	if len(propertyIDs) == 0 {
		return []storedFormProperty{}, nil
	}
	params := dbx.Params{"owner": owner, "pageId": pageID, "sourceId": dataSourceID}
	placeholders := make([]string, len(propertyIDs))
	for index, propertyID := range propertyIDs {
		key := fmt.Sprintf("property%d", index)
		params[key] = propertyID
		placeholders[index] = "{:" + key + "}"
	}
	rows := []storedFormProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[type]], [[name]], [[config]], [[updated]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[data_source_id]] = {:sourceId} AND [[id]] IN (` + joinSQL(placeholders) + `)
		LIMIT {:limit}
	`).Bind(mergeDBParams(params, dbx.Params{"limit": len(propertyIDs) + 1})).All(&rows)
	if err != nil {
		return nil, refusedUnlessStorage(errFormConflict, err)
	}
	byID := make(map[string]storedFormProperty, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	// Which question names what isn't there, or a property twice: a form
	// sent to be set says so (loadFormSource), a kept one has changed.
	ordered := make([]storedFormProperty, len(propertyIDs))
	asked := make(map[string]bool, len(propertyIDs))
	for index, propertyID := range propertyIDs {
		property, ok := byID[propertyID]
		switch {
		case !ok:
			return nil, refuse(errFormConflict,
				"form.questions[%d].propertyId %q isn't a property of the form's database", index, propertyID)
		case asked[propertyID]:
			return nil, refuse(errFormConflict,
				"form.questions[%d].propertyId %q has a question already: one question a property", index, propertyID)
		}
		asked[propertyID] = true
		ordered[index] = property
	}
	if len(rows) != len(byID) {
		return nil, errFormConflict
	}
	return ordered, nil
}

func loadFormSource(
	app core.App,
	owner, pageID, dataSourceID, viewID, audience string,
	requested *formViewConfiguration,
) (formSource, error) {
	view, config, err := loadStoredFormView(app, owner, pageID, dataSourceID, viewID)
	if err != nil {
		return formSource{}, err
	}
	form := formViewConfiguration{}
	if requested != nil {
		form = *requested
	} else {
		raw, exists := config["leafenForm"]
		encoded, marshalErr := json.Marshal(raw)
		if !exists || marshalErr != nil || json.Unmarshal(encoded, &form) != nil {
			return formSource{}, errFormNotFound
		}
	}
	propertyIDs := make([]string, len(form.Questions))
	for index, question := range form.Questions {
		propertyIDs[index] = strings.TrimSpace(question.PropertyID)
	}
	properties, err := loadStoredFormProperties(app, owner, pageID, dataSourceID, propertyIDs)
	if reason := refusalReason(err); requested != nil && reason != "" {
		return formSource{}, formInvalid("%s", reason)
	}
	if err != nil {
		return formSource{}, err
	}
	normalized, err := validateAndNormalizeForm(form, properties, audience)
	if err != nil {
		return formSource{}, err
	}
	return formSource{View: view, ViewConfig: config, Form: normalized, Properties: properties}, nil
}

func formDefinitionRevision(link storedFormLink, source formSource) (string, error) {
	payload := struct {
		Link       []any                 `json:"link"`
		View       []any                 `json:"view"`
		Form       formViewConfiguration `json:"form"`
		Properties [][]any               `json:"properties"`
	}{
		Link: []any{link.ID, link.Audience, link.Enabled},
		View: []any{source.View.ID, source.View.DataSourceID, source.View.Type},
		Form: source.Form,
	}
	for _, property := range source.Properties {
		var config any
		if json.Unmarshal([]byte(property.Config), &config) != nil {
			return "", errFormConflict
		}
		payload.Properties = append(payload.Properties, []any{
			property.ID, property.DataSourceID, property.Type, property.Name, config,
		})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// formQuestionMostSelections is how many answers a question takes: a relation
// held to one page takes one whatever the question says, as Notion's (seen
// 2026-11-06: "(최대 1개 선택)" to its respondent, its "최대 선택 항목 수"
// fixed at 1); any other, the question's own most, if it sets one.
func formQuestionMostSelections(question formQuestion, property storedFormProperty) *int {
	if property.Type == graphValueTypeRelation {
		config := struct {
			Limit int `json:"limit"`
		}{}
		if json.Unmarshal([]byte(property.Config), &config) == nil && config.Limit == 1 {
			one := 1
			return &one
		}
	}
	return question.MaxSelections
}

// formQuestionOwnLabel tells whether a question in step with its property
// keeps its own name: renaming the property leaves it, and renaming the
// question still renames the property, as Notion's API form "Title" question
// does (seen 2026-11-12: "Title" over a property renamed twice, its sync on).
func formQuestionOwnLabel(question *formQuestion) bool {
	return question.OwnLabel != nil && *question.OwnLabel
}
