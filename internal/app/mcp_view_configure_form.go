package app

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// What a configure string says of a form view: whether it takes responses,
// from whom, and the questions it asks (mcp_view_configure.go). Its questions
// read back in fetch's questions, each by its name and property.

func (change *mcpViewConfigure) formSetting(words *mcpViewWords) error {
	yes, no := true, false
	switch {
	case words.keyword("CLOSE"):
		change.form.closed = &yes
	case words.keyword("OPEN"):
		change.form.closed = &no
	case words.keyword("ANONYMOUS"):
		value, ok := words.oneOf("true", "false")
		if !ok {
			return change.unreadable("FORM ANONYMOUS", "FORM ANONYMOUS true or false")
		}
		if value == "true" {
			change.form.anonymous = &yes
		} else {
			change.form.anonymous = &no
		}
	case words.keyword("PERMISSIONS"):
		value, ok := words.value()
		value = strings.ToLower(value)
		if _, valid := formSubmitterAccessRole(value); !ok || !valid {
			return change.unreadable("FORM PERMISSIONS", "FORM PERMISSIONS none, comment_only, reader, read_and_write or editor")
		}
		change.form.access = &value
	case words.keyword("QUESTIONS"):
		return change.formQuestions(words)
	default:
		return change.unreadable("FORM", "FORM CLOSE, FORM OPEN, FORM ANONYMOUS true|false, FORM PERMISSIONS … or FORM QUESTIONS …")
	}
	return nil
}

// mcpFormQuestionsForm is how FORM QUESTIONS is written.
const mcpFormQuestionsForm = `FORM QUESTIONS "Property" [AS "question"] [REQUIRED] [MAX 2] [DROPDOWN], "Property"`

// formQuestions sets the questions a form asks, in order: a property it
// asked already keeps that question's settings, another is asked as the
// screen asks a new one (createFormQuestion), each with what its words say.
// A form an agent first asks questions of opens to the workspace's members,
// as a new form on the screen does; the questions are checked against who
// answers it (checkNotionViewFormAudience).
func (change *mcpViewConfigure) formQuestions(words *mcpViewWords) error {
	form := mcpViewFormConfiguration(change.config)
	asked := map[string]formQuestion{}
	for _, question := range form.Questions {
		asked[question.PropertyID] = question
	}
	questions := []formQuestion{}
	for {
		name, ok := words.quoted()
		if !ok {
			return change.unreadable("FORM QUESTIONS", mcpFormQuestionsForm)
		}
		property, err := change.property(name)
		if err != nil {
			return err
		}
		if !formWritablePropertyTypes[property.NativeType] {
			return mcpInvalid("configure: a form can't ask %q, a %s property", name, property.OfficialType)
		}
		if slices.ContainsFunc(questions, func(question formQuestion) bool { return question.PropertyID == property.ID }) {
			return mcpInvalid("configure: a form asks %q once", name)
		}
		question, kept := asked[property.ID]
		if !kept {
			question = mcpNewFormQuestion(property)
		}
		if err := change.formQuestionWords(words, &question, property); err != nil {
			return err
		}
		question.Position = len(questions)
		questions = append(questions, question)
		if !words.mark(",") {
			break
		}
	}
	// A condition stays only while the question it asks after does.
	form.ConditionGroups = slices.DeleteFunc(form.ConditionGroups, func(group formConditionGroup) bool {
		return !slices.ContainsFunc(questions, func(question formQuestion) bool { return question.ID == group.SourceQuestionID })
	})
	for index := range questions {
		if !slices.ContainsFunc(form.ConditionGroups, func(group formConditionGroup) bool { return group.ID == questions[index].ConditionGroupID }) {
			questions[index].ConditionGroupID = ""
		}
	}
	form.Questions = questions
	encoded, err := json.Marshal(form)
	value := map[string]any{}
	if err != nil || json.Unmarshal(encoded, &value) != nil {
		return failedBecause(errMCPToolFailed, err)
	}
	// The questions are checked against who answers the form, as the editor
	// checks them, whenever they change: a form first given questions opens
	// to the workspace's members, and an open or closed one stays so.
	if change.form.closed == nil {
		audience, opened := change.config["leafenFormAudience"].(string)
		closed := opened && audience == formAudienceNone
		change.form.closed = &closed
	}
	change.config["leafenForm"] = value
	return nil
}

// formQuestionWords reads what follows a question's property: its own name,
// whether it must be answered, how many choices it takes and whether they
// show as a dropdown.
func (change *mcpViewConfigure) formQuestionWords(words *mcpViewWords, question *formQuestion, property notionViewProperty) error {
	choices := property.NativeType == graphValueTypeSelect || property.NativeType == graphValueTypeMultiSelect ||
		property.NativeType == graphValueTypeStatus
	for !words.done() && !(words.tokens[words.at].kind == mcpViewTokenMark && words.tokens[words.at].text == ",") {
		switch {
		case words.keyword("AS"):
			label, ok := words.quoted()
			if !ok || strings.TrimSpace(label) == "" {
				return change.unreadable("FORM QUESTIONS … AS", mcpFormQuestionsForm)
			}
			question.Label, question.SyncWithPropertyName = label, false
		case words.keyword("REQUIRED"):
			question.Required = true
		case words.keyword("MAX"):
			raw, _ := words.value()
			most, err := strconv.Atoi(raw)
			if err != nil || most < 1 || property.NativeType != graphValueTypeMultiSelect && property.NativeType != graphValueTypePerson &&
				property.NativeType != graphValueTypeRelation && property.NativeType != graphValueTypeFiles {
				return mcpInvalid("configure: MAX takes a whole number of at least 1, for a multi-select, person, relation or files question")
			}
			question.MaxSelections = &most
		case words.keyword("DROPDOWN"):
			if !choices {
				return mcpInvalid("configure: DROPDOWN is for a select, multi-select or status question")
			}
			question.OptionsDisplay = "dropdown"
		default:
			return mcpInvalid("configure: %q doesn't read in FORM QUESTIONS (%s)", words.tokens[words.at].text, mcpFormQuestionsForm)
		}
	}
	return nil
}

// mcpNewFormQuestion asks a property as the screen's new question does.
func mcpNewFormQuestion(property notionViewProperty) formQuestion {
	question := formQuestion{
		ID: "question:" + property.ID, PropertyID: property.ID, Label: property.Name, SyncWithPropertyName: true,
	}
	if question.Label == "" {
		question.Label = property.ID
	}
	switch property.NativeType {
	case graphValueTypeRichText:
		short := false
		question.LongAnswer = &short
	case graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypeStatus:
		question.OptionsDisplay = "list"
	}
	return question
}

// mcpViewFormConfiguration is a form view's form, or the one a new form starts
// with on the screen (createDefaultFormViewConfig) for a view never set up.
func mcpViewFormConfiguration(config map[string]any) formViewConfiguration {
	form := formViewConfiguration{}
	if raw, configured := config["leafenForm"]; configured {
		encoded, err := json.Marshal(raw)
		if err == nil && json.Unmarshal(encoded, &form) == nil && form.Version == 1 {
			return form
		}
	}
	submitAnother := false
	return formViewConfiguration{
		Version: 1, Questions: []formQuestion{},
		Submit: formSubmitConfiguration{ButtonColor: formButtonColorBlue, ShowSubmitAnother: &submitAnother},
	}
}
