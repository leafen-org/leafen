package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode"
)

func normalizeFormSubmissionAnswers(
	source formSource,
	answers map[string]any,
) (map[string]graphRowValue, error) {
	questionByProperty := make(map[string]formQuestion, len(source.Form.Questions))
	propertyByID := make(map[string]storedFormProperty, len(source.Properties))
	for index, question := range source.Form.Questions {
		questionByProperty[question.PropertyID] = question
		propertyByID[question.PropertyID] = source.Properties[index]
	}
	for propertyID := range answers {
		question, exists := questionByProperty[propertyID]
		if !exists || question.Hidden {
			return nil, formInvalid("answers names %q, which is no question of this form to answer", propertyID)
		}
	}
	values := make(map[string]graphRowValue, len(answers)+1)
	notAsked := formQuestionsNotAsked(source.Form, answers)
	for _, question := range source.Form.Questions {
		if question.Hidden {
			continue
		}
		property := propertyByID[question.PropertyID]
		// A question its condition leaves out neither needs an answer nor keeps
		// one typed before the answer it hangs on changed.
		raw, exists := answers[question.PropertyID]
		if notAsked[question.PropertyID] {
			if property.Type == graphValueTypeTitle {
				values[property.ID] = graphRowValue{Type: property.Type, Value: json.RawMessage(`""`)}
			}
			continue
		}
		if !exists || emptyFormAnswer(raw) {
			if question.Required {
				return nil, formInvalid("answers lacks %q (“%s”), which is required", question.PropertyID, question.Label)
			}
			if property.Type == graphValueTypeTitle {
				values[property.ID] = graphRowValue{
					Type: property.Type, Value: json.RawMessage(`""`),
				}
			}
			continue
		}
		normalized, err := normalizeFormAnswer(property, question, raw)
		if err != nil && !errors.Is(err, errFormInvalid) {
			return nil, err
		}
		if err != nil {
			return nil, formInvalid("answers[%q] (“%s”) takes %s", question.PropertyID, question.Label, formAnswerTakes(property, question))
		}
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return nil, errFormInvalid
		}
		values[property.ID] = graphRowValue{Type: property.Type, Value: encoded}
	}
	if len(values) == 0 {
		return nil, formInvalid("answers answers no question")
	}
	return values, nil
}

// formAnswerTakes is what an answer to the question is, said to whoever sent
// one that isn't.
func formAnswerTakes(property storedFormProperty, question formQuestion) string {
	most := ", each once"
	if limit := formQuestionMostSelections(question, property); limit != nil {
		most = fmt.Sprintf(", each once, up to %d", *limit)
	}
	switch property.Type {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypePhone:
		return fmt.Sprintf("text, of up to %d bytes", maxFormScalarBytes)
	case graphValueTypeURL:
		return "an http or https address"
	case graphValueTypeEmail:
		return "an email address"
	case graphValueTypeNumber:
		return "a number"
	case graphValueTypeCheckbox:
		return "true or false"
	case graphValueTypePlace:
		return `{"lat", "lon"}, with a "name" and "address" if you like`
	case graphValueTypeFiles:
		return fmt.Sprintf("a list of up to %d ids of files uploaded to this form for it, each once", maxFormFilesPerAnswer)
	case graphValueTypeDate:
		day := `"2026-11-05"`
		if question.IncludeTime != nil && *question.IncludeTime {
			day = `"2026-11-05T09:30"`
		}
		if question.IncludeEndDate != nil && *question.IncludeEndDate {
			return "a day as " + day + `, or {"start", "end"} with the end not before the start`
		}
		return "a day as " + day
	case graphValueTypeSelect, graphValueTypeStatus:
		return "one of its options' ids, as the form's definition lists them"
	case graphValueTypeMultiSelect:
		return "a list of its options' ids" + most
	case graphValueTypePerson:
		return "a list of people's ids" + most
	case graphValueTypeRelation:
		return "a list of pages' ids" + most
	}
	return "no answer from a form"
}

func emptyFormAnswer(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return text == ""
	}
	if values, ok := value.([]any); ok {
		return len(values) == 0
	}
	return false
}

func normalizeFormAnswer(
	property storedFormProperty,
	question formQuestion,
	value any,
) (any, error) {
	switch property.Type {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypePhone:
		text, err := normalizeFormAnswerString(value)
		return text, err
	case graphValueTypeURL:
		text, err := normalizeFormAnswerString(value)
		if err != nil {
			return nil, err
		}
		parsed, parseErr := url.Parse(text)
		if parseErr != nil || parsed.Host == "" || parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errFormInvalid
		}
		return text, nil
	case graphValueTypeEmail:
		text, err := normalizeFormAnswerString(value)
		if err != nil || !validFormEmail(text) {
			return nil, errFormInvalid
		}
		return text, nil
	case graphValueTypeNumber:
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, errFormInvalid
		}
		return number, nil
	case graphValueTypeCheckbox:
		checked, ok := value.(bool)
		if !ok {
			return nil, errFormInvalid
		}
		return checked, nil
	case graphValueTypePlace:
		return normalizeFormPlaceAnswer(value)
	case graphValueTypeFiles:
		// The files sent for it, by id; which become the page's is resolveFormFileAnswers's.
		return formFileAnswerIDs(value)
	case graphValueTypeDate:
		return normalizeFormDateAnswer(question, value)
	case graphValueTypeSelect, graphValueTypeStatus:
		text, err := normalizeFormAnswerString(value)
		if err != nil || !formChoiceIDs(property)[text] {
			return nil, errFormInvalid
		}
		return text, nil
	case graphValueTypeMultiSelect, graphValueTypePerson, graphValueTypeRelation:
		ids, err := normalizeFormSelectionIDs(value, formQuestionMostSelections(question, property))
		if err != nil {
			return nil, err
		}
		if property.Type == graphValueTypeMultiSelect {
			allowed := formChoiceIDs(property)
			for _, id := range ids {
				if !allowed[id] {
					return nil, errFormInvalid
				}
			}
			return ids, nil
		}
		return ids, nil
	default:
		return nil, errFormInvalid
	}
}

// normalizeFormDateAnswer takes a day, or {start, end} with the end and the
// time its question asks for: "2026-11-05" or "2026-11-05T09:30" on each end.
func normalizeFormDateAnswer(question formQuestion, value any) (any, error) {
	withTime := question.IncludeTime != nil && *question.IncludeTime
	withEnd := question.IncludeEndDate != nil && *question.IncludeEndDate
	part := func(raw any) (string, error) {
		text, err := normalizeFormAnswerString(raw)
		if err != nil {
			return "", err
		}
		layout := "2006-01-02"
		if withTime {
			layout = "2006-01-02T15:04"
		}
		parsed, parseErr := time.Parse(layout, text)
		if parseErr != nil || parsed.Format(layout) != text {
			return "", errFormInvalid
		}
		return text, nil
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		start, err := part(value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"start": start}, nil
	}
	start, err := part(object["start"])
	if err != nil {
		return nil, err
	}
	result := map[string]any{"start": start}
	if rawEnd, present := object["end"]; present && rawEnd != nil && rawEnd != "" {
		end, err := part(rawEnd)
		if err != nil || !withEnd || end < start {
			return nil, errFormInvalid
		}
		result["end"] = end
	}
	return result, nil
}

func normalizeFormAnswerString(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", errFormInvalid
	}
	text = strings.TrimSpace(text)
	if text == "" || validateFormText(text, maxFormScalarBytes) != nil {
		return "", errFormInvalid
	}
	return text, nil
}

func normalizeFormSelectionIDs(value any, most *int) ([]string, error) {
	raw, ok := value.([]any)
	if !ok {
		return nil, errFormInvalid
	}
	// As many as the question takes (formQuestionMostSelections).
	if most != nil && len(raw) > *most {
		return nil, errFormInvalid
	}
	ids := make([]string, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for index, entry := range raw {
		text, ok := entry.(string)
		if !ok {
			return nil, errFormInvalid
		}
		id, err := normalizeFormIdentifier(text)
		if err != nil {
			return nil, errFormInvalid
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, errFormInvalid
		}
		seen[id] = struct{}{}
		ids[index] = id
	}
	return ids, nil
}

func formChoiceIDs(property storedFormProperty) map[string]bool {
	config := struct {
		Options []struct {
			ID string `json:"id"`
		} `json:"options"`
	}{}
	if json.Unmarshal([]byte(property.Config), &config) != nil {
		return map[string]bool{}
	}
	result := make(map[string]bool, len(config.Options))
	for _, option := range config.Options {
		id := strings.TrimSpace(option.ID)
		if id != "" {
			result[id] = true
		}
	}
	return result
}

func validFormEmail(value string) bool {
	at := strings.LastIndex(value, "@")
	return at > 0 && at < len(value)-3 && strings.Contains(value[at+1:], ".") &&
		!strings.ContainsAny(value, " \t\r\n")
}

// normalizeFormPlaceAnswer keeps a place answer's point and the name and
// address typed for it, as a place property holds them.
func normalizeFormPlaceAnswer(value any) (any, error) {
	place, ok := value.(map[string]any)
	if !ok {
		return nil, errFormInvalid
	}
	lat, latOK := place["lat"].(float64)
	lon, lonOK := place["lon"].(float64)
	if !latOK || !lonOK || !validPlacePoint(lat, lon) {
		return nil, errFormInvalid
	}
	answer := map[string]any{"lat": lat, "lon": lon}
	for _, key := range []string{"name", "address"} {
		raw, present := place[key]
		if !present || raw == nil {
			continue
		}
		text, ok := raw.(string)
		if !ok || len([]rune(text)) > 2000 || strings.IndexFunc(text, unicode.IsControl) >= 0 {
			return nil, errFormInvalid
		}
		if text = strings.TrimSpace(text); text != "" {
			answer[key] = text
		}
	}
	for key := range place {
		if key != "lat" && key != "lon" && key != "name" && key != "address" {
			return nil, errFormInvalid
		}
	}
	return answer, nil
}
